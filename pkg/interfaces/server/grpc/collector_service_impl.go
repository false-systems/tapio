package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CollectorServiceImpl implements the CollectorService from proto
type CollectorServiceImpl struct {
	pb.UnimplementedCollectorServiceServer

	logger *zap.Logger
	tracer trace.Tracer

	// Dependencies
	collectorMgr *manager.CollectorManager
	pipeline     pipeline.IntelligencePipeline
	registry     CollectorRegistry

	// Configuration
	maxBatchSize       int
	maxEventsPerSecond int

	// Active streams
	streamsMutex sync.RWMutex
	streams      map[string]*collectorStream

	// Statistics
	stats struct {
		eventsReceived   atomic.Uint64
		batchesReceived  atomic.Uint64
		streamsActive    atomic.Int32
		collectorsActive atomic.Int32
	}

	startTime time.Time
}

type collectorStream struct {
	id          string
	collectorID string
	stream      pb.CollectorService_StreamEventsServer
	ctx         context.Context
	cancel      context.CancelFunc

	// Metrics
	eventsReceived uint64
	eventsSent     uint64
	startTime      time.Time
	lastActivity   time.Time
}

// NewCollectorServiceImpl creates a new collector service implementation
func NewCollectorServiceImpl(logger *zap.Logger, tracer trace.Tracer) *CollectorServiceImpl {
	return &CollectorServiceImpl{
		logger:             logger,
		tracer:             tracer,
		maxBatchSize:       1000,
		maxEventsPerSecond: 10000,
		streams:            make(map[string]*collectorStream),
		startTime:          time.Now(),
	}
}

// SetDependencies injects dependencies
func (s *CollectorServiceImpl) SetDependencies(collectorMgr *manager.CollectorManager, pipelineInstance pipeline.IntelligencePipeline, registry CollectorRegistry) {
	s.collectorMgr = collectorMgr
	s.pipeline = pipelineInstance
	s.registry = registry
}

// StreamEvents implements bidirectional streaming for collectors
func (s *CollectorServiceImpl) StreamEvents(stream pb.CollectorService_StreamEventsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "collector.stream_events")
	defer span.End()

	// Extract collector ID from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}

	collectorIDs := md.Get("collector-id")
	if len(collectorIDs) == 0 {
		return status.Error(codes.Unauthenticated, "collector ID required")
	}
	collectorID := collectorIDs[0]

	// Verify collector is registered
	info := s.registry.GetCollector(collectorID)
	if info == nil {
		return status.Error(codes.PermissionDenied, "collector not registered")
	}

	// Create stream context
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create stream instance
	cs := &collectorStream{
		id:           fmt.Sprintf("stream_%s_%d", collectorID, time.Now().UnixNano()),
		collectorID:  collectorID,
		stream:       stream,
		ctx:          streamCtx,
		cancel:       cancel,
		startTime:    time.Now(),
		lastActivity: time.Now(),
	}

	// Register stream
	s.streamsMutex.Lock()
	s.streams[cs.id] = cs
	s.streamsMutex.Unlock()

	s.stats.streamsActive.Add(1)
	defer func() {
		s.stats.streamsActive.Add(-1)
		s.streamsMutex.Lock()
		delete(s.streams, cs.id)
		s.streamsMutex.Unlock()
	}()

	span.SetAttributes(
		attribute.String("collector.id", collectorID),
		attribute.String("stream.id", cs.id),
	)

	s.logger.Info("Collector stream established",
		zap.String("collector_id", collectorID),
		zap.String("stream_id", cs.id),
	)

	// Process stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				span.RecordError(err)
				return err
			}

			cs.lastActivity = time.Now()

			// Handle request based on payload type
			switch payload := req.Payload.(type) {
			case *pb.StreamRequest_Events:
				if err := s.handleEventBatch(cs, payload.Events); err != nil {
					span.RecordError(err)
					if err := s.sendErrorResponse(stream, req.Sequence, err); err != nil {
						return err
					}
				}

			case *pb.StreamRequest_FlowControl:
				s.handleFlowControl(cs, payload.FlowControl)

			case *pb.StreamRequest_Status:
				s.handleStatusUpdate(cs, payload.Status)

			case *pb.StreamRequest_Ack:
				// Process acknowledgment
				s.logger.Debug("Received ack",
					zap.String("collector_id", collectorID),
					zap.Uint64("sequence", payload.Ack.Sequence),
				)
			}
		}
	}
}

// handleEventBatch processes incoming events
func (s *CollectorServiceImpl) handleEventBatch(cs *collectorStream, batch *pb.CollectorEventBatch) error {
	if batch == nil || len(batch.Events) == 0 {
		return nil
	}

	// Update metrics
	cs.eventsReceived += uint64(len(batch.Events))
	s.stats.eventsReceived.Add(uint64(len(batch.Events)))
	s.stats.batchesReceived.Add(1)

	// Convert proto events to domain events
	domainEvents := make([]*domain.UnifiedEvent, 0, len(batch.Events))
	for _, protoEvent := range batch.Events {
		domainEvent := s.convertProtoToDomainEvent(protoEvent)

		// Add collector metadata
		if domainEvent.Metadata == nil {
			domainEvent.Metadata = make(map[string]interface{})
		}
		domainEvent.Metadata["collector_id"] = cs.collectorID
		domainEvent.Metadata["batch_id"] = batch.BatchId
		domainEvent.Metadata["stream_id"] = cs.id

		domainEvents = append(domainEvents, domainEvent)
	}

	// Process through dataflow
	ctx, span := s.tracer.Start(cs.ctx, "collector.process_batch",
		trace.WithAttributes(
			attribute.String("collector.id", cs.collectorID),
			attribute.String("batch.id", batch.BatchId),
			attribute.Int("batch.size", len(batch.Events)),
		),
	)
	defer span.End()

	if err := s.pipeline.ProcessBatch(domainEvents); err != nil {
		return fmt.Errorf("failed to process batch: %w", err)
	}

	// Send acknowledgment
	ack := &pb.CollectorEventAck{
		BatchId:         batch.BatchId,
		RequestSequence: batch.GetMetadata().GetEventCount(),
		ProcessedCount:  uint32(len(batch.Events)),
		FailedCount:     0,
	}

	resp := &pb.StreamResponse{
		Sequence:  cs.eventsReceived,
		Timestamp: timestamppb.Now(),
		Payload: &pb.StreamResponse_EventAck{
			EventAck: ack,
		},
	}

	return cs.stream.Send(resp)
}

// SendEventBatch handles non-streaming batch submission
func (s *CollectorServiceImpl) SendEventBatch(ctx context.Context, req *pb.EventBatchRequest) (*pb.EventBatchResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.send_event_batch")
	defer span.End()

	if req.Batch == nil || len(req.Batch.Events) == 0 {
		return nil, status.Error(codes.InvalidArgument, "batch cannot be empty")
	}

	// Update metrics
	s.stats.eventsReceived.Add(uint64(len(req.Batch.Events)))
	s.stats.batchesReceived.Add(1)

	// Convert and process events
	domainEvents := make([]*domain.UnifiedEvent, 0, len(req.Batch.Events))
	for _, protoEvent := range req.Batch.Events {
		domainEvent := s.convertProtoToDomainEvent(protoEvent)

		// Add metadata
		if domainEvent.Metadata == nil {
			domainEvent.Metadata = make(map[string]interface{})
		}
		domainEvent.Metadata["collector_id"] = req.Batch.CollectorId
		domainEvent.Metadata["batch_id"] = req.Batch.BatchId

		domainEvents = append(domainEvents, domainEvent)
	}

	// Process through dataflow
	if err := s.pipeline.ProcessBatch(domainEvents); err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to process batch: %v", err)
	}

	// Update collector stats
	s.registry.UpdateStats(req.Batch.CollectorId, manager.CollectorStatistics{
		EventsReceived: uint64(len(req.Batch.Events)),
		LastEventTime:  time.Now(),
	})

	span.SetAttributes(
		attribute.String("collector.id", req.Batch.CollectorId),
		attribute.Int("batch.size", len(req.Batch.Events)),
		attribute.String("batch.id", req.Batch.BatchId),
	)

	return &pb.EventBatchResponse{
		Success: true,
		Message: fmt.Sprintf("Processed %d events", len(req.Batch.Events)),
		Ack: &pb.CollectorEventAck{
			BatchId:        req.Batch.BatchId,
			ProcessedCount: uint32(len(req.Batch.Events)),
			FailedCount:    0,
		},
	}, nil
}

// RegisterCollector registers a new collector
func (s *CollectorServiceImpl) RegisterCollector(ctx context.Context, req *pb.CollectorRegistration) (*pb.CollectorConfig, error) {
	ctx, span := s.tracer.Start(ctx, "collector.register")
	defer span.End()

	if req.CollectorId == "" {
		return nil, status.Error(codes.InvalidArgument, "collector_id required")
	}

	// Get peer info
	p, _ := peer.FromContext(ctx)
	hostname := ""
	if p != nil {
		hostname = p.Addr.String()
	}

	// Register with registry
	info := &CollectorRegistryInfo{
		ID:           req.CollectorId,
		Type:         req.CollectorType,
		Version:      req.Version,
		Hostname:     hostname,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Status:       "active",
		Metadata:     req.Metadata,
		Capabilities: req.Capabilities,
	}

	if err := s.registry.RegisterCollector(info); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "collector already registered: %v", err)
	}

	s.stats.collectorsActive.Add(1)

	span.SetAttributes(
		attribute.String("collector.id", req.CollectorId),
		attribute.String("collector.type", req.CollectorType),
		attribute.String("collector.version", req.Version),
	)

	s.logger.Info("Collector registered",
		zap.String("collector_id", req.CollectorId),
		zap.String("type", req.CollectorType),
		zap.String("version", req.Version),
	)

	// Return configuration
	return &pb.CollectorConfig{
		CollectorId:   req.CollectorId,
		ConfigVersion: "1.0.0",
		Routes: []*pb.Route{
			{
				EventTypes: []pb.EventType{
					pb.EventType_EVENT_TYPE_NETWORK,
					pb.EventType_EVENT_TYPE_SYSCALL,
					pb.EventType_EVENT_TYPE_PROCESS,
					pb.EventType_EVENT_TYPE_FILE_SYSTEM,
					pb.EventType_EVENT_TYPE_KUBERNETES,
				},
				Destinations: []string{"primary"},
			},
		},
		RateLimits: &pb.RateLimits{
			EventsPerSecond: int32(s.maxEventsPerSecond),
			BurstSize:       int32(s.maxBatchSize),
		},
		RetryPolicy: &pb.RetryPolicy{
			MaxRetries:     3,
			InitialBackoff: durationpb.New(time.Second),
			MaxBackoff:     durationpb.New(30 * time.Second),
			BackoffFactor:  2.0,
		},
	}, nil
}

// Heartbeat handles periodic heartbeat from collectors
func (s *CollectorServiceImpl) Heartbeat(stream pb.CollectorService_HeartbeatServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "collector.heartbeat")
	defer span.End()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				span.RecordError(err)
				return err
			}

			// Update collector health
			if req.Status != nil {
				health := manager.CollectorHealth{
					Status:    "healthy",
					LastCheck: time.Now(),
					Details: map[string]interface{}{
						"state":          req.Status.State.String(),
						"uptime":         req.Status.Uptime.AsDuration().String(),
						"config_version": req.Status.ConfigVersion,
					},
				}

				if req.Status.Resources != nil {
					health.Details["cpu_usage"] = req.Status.Resources.CpuUsage
					health.Details["memory_usage"] = req.Status.Resources.MemoryUsage
				}

				s.registry.UpdateHealth(req.CollectorId, health)
			}

			// Send response
			resp := &pb.HeartbeatResponse{
				Timestamp: timestamppb.Now(),
				ServerInfo: &pb.ServerInfo{
					Version: "1.0.0",
					Uptime:  durationpb.New(time.Since(s.startTime)),
				},
				LoadInfo: &pb.LoadInfo{
					CurrentLoad: s.calculateLoad(),
					Capacity:    1.0,
				},
			}

			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

// GetServerInfo returns server information
func (s *CollectorServiceImpl) GetServerInfo(ctx context.Context, req *pb.ServerInfoRequest) (*pb.ServerInfoResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.get_server_info")
	defer span.End()

	return &pb.ServerInfoResponse{
		ServerVersion: "1.0.0",
		Capabilities: &pb.ServerCapabilities{
			SupportedEventTypes: []pb.EventType{
				pb.EventType_EVENT_TYPE_NETWORK,
				pb.EventType_EVENT_TYPE_SYSCALL,
				pb.EventType_EVENT_TYPE_PROCESS,
				pb.EventType_EVENT_TYPE_FILE_SYSTEM,
				pb.EventType_EVENT_TYPE_KUBERNETES,
				pb.EventType_EVENT_TYPE_HTTP,
				pb.EventType_EVENT_TYPE_GRPC,
				pb.EventType_EVENT_TYPE_DATABASE,
				pb.EventType_EVENT_TYPE_WORKFLOW,
			},
			MaxBatchSize:       int32(s.maxBatchSize),
			MaxEventsPerSecond: int32(s.maxEventsPerSecond),
		},
	}, nil
}

// GetConfig returns collector configuration
func (s *CollectorServiceImpl) GetConfig(ctx context.Context, req *pb.GetConfigRequest) (*pb.GetConfigResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.get_config")
	defer span.End()

	info := s.registry.GetCollector(req.CollectorId)
	if info == nil {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	return &pb.GetConfigResponse{
		Config: &pb.CollectorConfig{
			CollectorId:   req.CollectorId,
			ConfigVersion: info.ConfigVersion,
			Routes: []*pb.Route{
				{
					EventTypes:   []pb.EventType{pb.EventType_EVENT_TYPE_NETWORK},
					Destinations: []string{"primary"},
				},
			},
		},
		LastUpdated: timestamppb.New(info.LastConfigUpdate),
		Etag:        info.ConfigVersion,
	}, nil
}

// UpdateConfig updates collector configuration
func (s *CollectorServiceImpl) UpdateConfig(ctx context.Context, req *pb.UpdateConfigRequest) (*pb.UpdateConfigResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.update_config")
	defer span.End()

	info := s.registry.GetCollector(req.CollectorId)
	if info == nil {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	// Update configuration
	newVersion := fmt.Sprintf("v%d", time.Now().Unix())
	info.ConfigVersion = newVersion
	info.LastConfigUpdate = time.Now()

	s.registry.UpdateCollector(info)

	return &pb.UpdateConfigResponse{
		Success: true,
		Message: "Configuration updated successfully",
		AppliedConfig: &pb.CollectorConfig{
			CollectorId:   req.CollectorId,
			ConfigVersion: newVersion,
		},
		NewEtag: newVersion,
	}, nil
}

// GetMetrics returns collector metrics
func (s *CollectorServiceImpl) GetMetrics(ctx context.Context, req *pb.GetCollectorMetricsRequest) (*pb.GetCollectorMetricsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.get_metrics")
	defer span.End()

	info := s.registry.GetCollector(req.CollectorId)
	if info == nil {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	stats := info.Statistics
	uptime := time.Since(info.RegisteredAt)

	return &pb.GetCollectorMetricsResponse{
		CollectorId: req.CollectorId,
		Metrics: &pb.CollectorMetrics{
			EventsProcessed: int64(stats.EventsReceived),
			EventsDropped:   int64(stats.EventsDropped),
			EventsFiltered:  0,
			EventsPerSecond: float64(stats.EventsReceived) / uptime.Seconds(),
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListCollectors returns list of registered collectors
func (s *CollectorServiceImpl) ListCollectors(ctx context.Context, req *pb.ListCollectorsRequest) (*pb.ListCollectorsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.list_collectors")
	defer span.End()

	allCollectors := s.registry.ListCollectors()

	// Apply filters and convert to proto
	collectors := make([]*pb.CollectorInfo, 0, len(allCollectors))
	var totalEvents uint64
	activeCount := 0

	for _, info := range allCollectors {
		// Apply filters
		if req.FilterType != "" && info.Type != req.FilterType {
			continue
		}

		if req.FilterStatus != "" && info.Status != req.FilterStatus {
			continue
		}

		totalEvents += info.Statistics.EventsReceived
		if info.Status == "active" {
			activeCount++
		}

		collectors = append(collectors, &pb.CollectorInfo{
			CollectorId:     info.ID,
			CollectorType:   info.Type,
			Version:         info.Version,
			RegisteredAt:    timestamppb.New(info.RegisteredAt),
			LastSeen:        timestamppb.New(info.LastSeen),
			EventsPerSecond: float64(info.Statistics.EventsReceived) / time.Since(info.RegisteredAt).Seconds(),
		})
	}

	return &pb.ListCollectorsResponse{
		Collectors: collectors,
		TotalCount: int32(len(collectors)),
		Stats: &pb.CollectorSummaryStats{
			TotalCollectors:      int32(len(allCollectors)),
			ActiveCollectors:     int32(activeCount),
			TotalEventsProcessed: int64(totalEvents),
		},
	}, nil
}

// UnregisterCollector removes a collector
func (s *CollectorServiceImpl) UnregisterCollector(ctx context.Context, req *pb.UnregisterCollectorRequest) (*pb.UnregisterCollectorResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.unregister")
	defer span.End()

	if err := s.registry.UnregisterCollector(req.CollectorId); err != nil {
		return nil, status.Errorf(codes.NotFound, "collector not found: %v", err)
	}

	// Cancel active streams
	s.streamsMutex.Lock()
	for id, stream := range s.streams {
		if stream.collectorID == req.CollectorId {
			stream.cancel()
			delete(s.streams, id)
		}
	}
	s.streamsMutex.Unlock()

	s.stats.collectorsActive.Add(-1)

	s.logger.Info("Collector unregistered",
		zap.String("collector_id", req.CollectorId),
		zap.String("reason", req.Reason),
	)

	return &pb.UnregisterCollectorResponse{
		Success:        true,
		Message:        fmt.Sprintf("Collector %s unregistered: %s", req.CollectorId, req.Reason),
		UnregisteredAt: timestamppb.Now(),
	}, nil
}

// Helper methods

func (s *CollectorServiceImpl) sendErrorResponse(stream pb.CollectorService_StreamEventsServer, sequence uint64, err error) error {
	resp := &pb.StreamResponse{
		Sequence:  sequence,
		Timestamp: timestamppb.Now(),
		Payload: &pb.StreamResponse_Error{
			Error: &pb.ErrorResponse{
				Message: err.Error(),
			},
		},
	}
	return stream.Send(resp)
}

func (s *CollectorServiceImpl) handleFlowControl(cs *collectorStream, msg *pb.FlowControlMessage) {
	s.logger.Info("Flow control received",
		zap.String("stream_id", cs.id),
		zap.String("type", msg.Type.String()),
	)
	// Implement flow control logic as needed
}

func (s *CollectorServiceImpl) handleStatusUpdate(cs *collectorStream, status *pb.CollectorStatus) {
	health := manager.CollectorHealth{
		Status:    "healthy",
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"state":          status.State.String(),
			"uptime":         status.Uptime.AsDuration().String(),
			"config_version": status.ConfigVersion,
		},
	}

	if status.Resources != nil {
		health.Details["cpu_usage"] = status.Resources.CpuUsage
		health.Details["memory_usage"] = status.Resources.MemoryUsage
	}

	s.registry.UpdateHealth(cs.collectorID, health)
}

func (s *CollectorServiceImpl) calculateLoad() float32 {
	activeStreams := len(s.streams)
	maxStreams := 1000
	return float32(activeStreams) / float32(maxStreams)
}

func (s *CollectorServiceImpl) convertProtoToDomainEvent(protoEvent *pb.Event) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:            protoEvent.Id,
		Type:          domain.EventType(protoEvent.Type.String()),
		Severity:      domain.EventSeverity(protoEvent.Severity.String()),
		Source:        domain.EventSource(protoEvent.Source.String()),
		Message:       domain.EventMessage(protoEvent.Message),
		Timestamp:     protoEvent.Timestamp.AsTime(),
		Attributes:    protoEvent.Attributes,
		TraceID:       protoEvent.TraceId,
		SpanID:        protoEvent.SpanId,
		CorrelationID: protoEvent.CorrelationIds,
		Metadata:      make(map[string]interface{}),
	}
}

// GetStatistics returns service statistics
func (s *CollectorServiceImpl) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"events_received":   s.stats.eventsReceived.Load(),
		"batches_received":  s.stats.batchesReceived.Load(),
		"streams_active":    s.stats.streamsActive.Load(),
		"collectors_active": s.stats.collectorsActive.Load(),
		"uptime":            time.Since(s.startTime).String(),
	}
}
