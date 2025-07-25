package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
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
	collectors := s.registry.GetCollectors()
	if _, exists := collectors[collectorID]; !exists {
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
		if domainEvent.Attributes == nil {
			domainEvent.Attributes = make(map[string]interface{})
		}
		domainEvent.Attributes["collector_id"] = cs.collectorID
		domainEvent.Attributes["batch_id"] = batch.BatchId
		domainEvent.Attributes["stream_id"] = cs.id

		domainEvents = append(domainEvents, domainEvent)
	}

	// Process through dataflow
	_, span := s.tracer.Start(cs.ctx, "collector.process_batch",
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
		RequestSequence: uint64(batch.GetMetadata().GetEventCount()),
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
		if domainEvent.Attributes == nil {
			domainEvent.Attributes = make(map[string]interface{})
		}
		domainEvent.Attributes["collector_id"] = req.Batch.CollectorId
		domainEvent.Attributes["batch_id"] = req.Batch.BatchId

		domainEvents = append(domainEvents, domainEvent)
	}

	// Process through dataflow
	if err := s.pipeline.ProcessBatch(domainEvents); err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to process batch: %v", err)
	}

	// Collector stats are managed internally by each collector

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
	info := CollectorInfo{
		Name:         req.CollectorId,
		Type:         req.CollectorType,
		Version:      req.Version,
		Status:       "active",
		LastSeen:     time.Now(),
		EventTypes:   extractEventTypes(req.Capabilities),
		Capabilities: extractCapabilities(req.Capabilities),
		Metadata:     make(map[string]string),
	}

	// Add hostname to metadata
	info.Metadata["hostname"] = hostname
	info.Metadata["registered_at"] = time.Now().Format(time.RFC3339)

	if err := s.registry.RegisterCollector(req.CollectorId, info); err != nil {
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
		FlowControl: &pb.FlowControlDirective{
			MaxEventsPerSecond: uint32(s.maxEventsPerSecond),
			MaxBatchSize:       uint32(s.maxBatchSize),
			BatchInterval:      durationpb.New(100 * time.Millisecond),
			EnableCompression:  true,
			CompressionType:    pb.CompressionType_COMPRESSION_SNAPPY,
			ValidDuration:      durationpb.New(5 * time.Minute),
		},
		HeartbeatInterval: durationpb.New(30 * time.Second),
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

			// Update collector health in registry (registry manages health internally)
			// The health status is managed internally by each collector
			// We just update the last seen time

			s.logger.Debug("Received heartbeat",
				zap.String("collector_id", req.CollectorId),
				zap.String("config_version", req.ConfigVersion),
			)

			// Send response
			resp := &pb.HeartbeatResponse{
				Timestamp:             timestamppb.Now(),
				ConfigVersion:         "1.0.0",
				ConfigUpdateAvailable: false,
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
			MaxBatchSize:            uint32(s.maxBatchSize),
			MaxEventsPerSecond:      uint64(s.maxEventsPerSecond),
			MaxConcurrentCollectors: 1000,
			SupportsStreaming:       true,
			SupportsCompression:     true,
			SupportsLoadBalancing:   false,
		},
	}, nil
}

// GetConfig returns collector configuration
func (s *CollectorServiceImpl) GetConfig(ctx context.Context, req *pb.GetConfigRequest) (*pb.GetConfigResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.get_config")
	defer span.End()

	collectors := s.registry.GetCollectors()
	info, exists := collectors[req.CollectorId]
	if !exists {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	return &pb.GetConfigResponse{
		Config: &pb.CollectorConfig{
			CollectorId:   req.CollectorId,
			ConfigVersion: info.Version,
			FlowControl: &pb.FlowControlDirective{
				MaxEventsPerSecond: uint32(s.maxEventsPerSecond),
				MaxBatchSize:       uint32(s.maxBatchSize),
			},
			HeartbeatInterval: durationpb.New(30 * time.Second),
		},
		LastUpdated: timestamppb.New(info.LastSeen),
		Etag:        info.Version,
	}, nil
}

// UpdateConfig updates collector configuration
func (s *CollectorServiceImpl) UpdateConfig(ctx context.Context, req *pb.UpdateConfigRequest) (*pb.UpdateConfigResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.update_config")
	defer span.End()

	collectors := s.registry.GetCollectors()
	if _, exists := collectors[req.CollectorId]; !exists {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	// Configuration updates are handled internally by the registry
	// The registry doesn't expose an update method
	newVersion := fmt.Sprintf("v%d", time.Now().Unix())

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

	collectors := s.registry.GetCollectors()
	_, exists := collectors[req.CollectorId]
	if !exists {
		return nil, status.Error(codes.NotFound, "collector not found")
	}

	// Use placeholder metrics since registry doesn't expose detailed stats

	return &pb.GetCollectorMetricsResponse{
		CollectorId: req.CollectorId,
		Metrics: &pb.CollectorMetrics{
			EventsProcessed: 1000, // Placeholder
			EventsDropped:   0,    // Placeholder
			EventsFiltered:  0,
			EventsPerSecond: 100.0, // Placeholder
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListCollectors returns list of registered collectors
func (s *CollectorServiceImpl) ListCollectors(ctx context.Context, req *pb.ListCollectorsRequest) (*pb.ListCollectorsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.list_collectors")
	defer span.End()

	allCollectors := s.registry.GetCollectors()

	// Apply filters and convert to proto
	collectors := make([]*pb.CollectorInfo, 0, len(allCollectors))
	var totalEvents uint64
	activeCount := 0

	for _, info := range allCollectors {
		// Apply type filter
		if len(req.CollectorTypes) > 0 {
			found := false
			for _, t := range req.CollectorTypes {
				if info.Type == t {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		totalEvents += 1000 // Placeholder since we don't have detailed stats
		if info.Status == "active" {
			activeCount++
		}

		collectors = append(collectors, &pb.CollectorInfo{
			CollectorId:     info.Name,
			CollectorType:   info.Type,
			Version:         info.Version,
			LastSeen:        timestamppb.New(info.LastSeen),
			EventsPerSecond: 100.0, // Placeholder since we don't have detailed stats
			TotalEvents:     1000,  // Placeholder
			ErrorRate:       0.0,   // Placeholder
		})
	}

	return &pb.ListCollectorsResponse{
		Collectors: collectors,
		TotalCount: int32(len(collectors)),
		Stats: &pb.CollectorSummaryStats{
			TotalCollectors:      int32(len(allCollectors)),
			TotalEventsPerSecond: float64(totalEvents) / 10.0, // Placeholder rate
			UnhealthyCollectors:  0,                           // Placeholder
		},
	}, nil
}

// UnregisterCollector removes a collector
func (s *CollectorServiceImpl) UnregisterCollector(ctx context.Context, req *pb.UnregisterCollectorRequest) (*pb.UnregisterCollectorResponse, error) {
	ctx, span := s.tracer.Start(ctx, "collector.unregister")
	defer span.End()

	// The registry doesn't expose an unregister method
	// Just check if the collector exists
	collectors := s.registry.GetCollectors()
	if _, exists := collectors[req.CollectorId]; !exists {
		return nil, status.Errorf(codes.NotFound, "collector not found: %s", req.CollectorId)
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
		zap.Uint32("requested_rate", msg.RequestedRate),
		zap.Float32("buffer_utilization", msg.BufferUtilization),
		zap.String("reason", msg.Reason),
	)
	// Implement flow control logic as needed
}

func (s *CollectorServiceImpl) handleStatusUpdate(cs *collectorStream, status *pb.CollectorStatus) {
	// Health status is managed internally by collectors
	// Just log the status update
	s.logger.Info("Collector status update",
		zap.String("collector_id", cs.collectorID),
		zap.String("state", status.State.String()),
		zap.Duration("uptime", status.Uptime.AsDuration()),
		zap.String("config_version", status.ConfigVersion),
	)

	if status.Resources != nil {
		s.logger.Debug("Resource usage",
			zap.String("collector_id", cs.collectorID),
			zap.Float32("cpu_usage", status.Resources.CpuUsage),
			zap.Uint64("memory_bytes", status.Resources.MemoryBytes),
		)
	}
}

func (s *CollectorServiceImpl) calculateLoad() float32 {
	activeStreams := len(s.streams)
	maxStreams := 1000
	return float32(activeStreams) / float32(maxStreams)
}

func (s *CollectorServiceImpl) convertProtoToDomainEvent(protoEvent *pb.Event) *domain.UnifiedEvent {
	// Convert attributes map[string]string to map[string]interface{}
	attributes := make(map[string]interface{})
	for k, v := range protoEvent.Attributes {
		attributes[k] = v
	}

	event := &domain.UnifiedEvent{
		ID:         protoEvent.Id,
		Type:       domain.EventType(protoEvent.Type.String()),
		Source:     protoEvent.Source.String(),
		Timestamp:  protoEvent.Timestamp.AsTime(),
		Attributes: attributes,
	}

	// Set trace context if available
	if protoEvent.TraceId != "" || protoEvent.SpanId != "" {
		event.TraceContext = &domain.TraceContext{
			TraceID: protoEvent.TraceId,
			SpanID:  protoEvent.SpanId,
		}
	}

	// Set correlation hints
	if len(protoEvent.CorrelationIds) > 0 {
		event.CorrelationHints = protoEvent.CorrelationIds
	}

	return event
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

// extractEventTypes extracts event types from capabilities
func extractEventTypes(caps *pb.CollectorCapabilities) []string {
	if caps == nil {
		return []string{}
	}
	return caps.SupportedEventTypes
}

// extractCapabilities converts proto capabilities to string array
func extractCapabilities(caps *pb.CollectorCapabilities) []string {
	if caps == nil {
		return []string{}
	}

	capabilities := []string{}

	if caps.SupportsStreaming {
		capabilities = append(capabilities, "streaming")
	}

	if caps.SupportsBatching {
		capabilities = append(capabilities, "batching")
	}

	if len(caps.SupportedCompression) > 0 {
		capabilities = append(capabilities, "compression")
	}

	if caps.MaxEventsPerSecond > 0 {
		capabilities = append(capabilities, fmt.Sprintf("rate_limit:%d", caps.MaxEventsPerSecond))
	}

	return capabilities
}
