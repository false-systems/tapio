package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/performance"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type CollectorServer struct {
	pb.UnimplementedCollectorServiceServer
	logger     *zap.Logger
	eventStore EventStore
	tracer     trace.Tracer
	pipeline   *performance.EventPipeline
	batchProc  *performance.BatchProcessor[*domain.Event]
	ringBuffer *performance.RingBuffer

	// Collector registry
	collectorsMu sync.RWMutex
	collectors   map[string]*collectorInfo

	// Stream management
	streamsMu     sync.RWMutex
	activeStreams map[string]*collectorStream

	// Metrics
	totalEvents     atomic.Uint64
	eventsPerSecond atomic.Uint64
	totalBatches    atomic.Uint64
	failedEvents    atomic.Uint64
	droppedEvents   atomic.Uint64

	// Configuration
	maxEventsPerSec    int64
	maxBatchSize       int
	compressionEnabled bool
	heartbeatInterval  time.Duration
}

type collectorInfo struct {
	ID               string
	Type             string
	Version          string
	Node             *pb.NodeInfo
	Capabilities     *pb.CollectorCapabilities
	Config           *pb.CollectorConfig
	RegisteredAt     time.Time
	LastSeen         time.Time
	LastConfigUpdate time.Time
	State            pb.CollectorState
	StreamID         string
	Metrics          *collectorMetrics
	mu               sync.RWMutex
}

type collectorMetrics struct {
	EventsReceived  uint64
	EventsProcessed uint64
	EventsDropped   uint64
	BatchesReceived uint64
	BytesReceived   uint64
	LastEventTime   time.Time
	ErrorCount      uint64
	CurrentRate     float64
	AverageLatency  time.Duration
}

type collectorStream struct {
	ID          string
	CollectorID string
	Stream      pb.CollectorService_StreamEventsServer
	Context     context.Context
	Cancel      context.CancelFunc

	// Flow control
	MaxEventsPerSec uint32
	MaxBatchSize    uint32
	CurrentRate     atomic.Uint32

	// Metrics
	EventsReceived  atomic.Uint64
	BatchesReceived atomic.Uint64
	BytesReceived   atomic.Uint64
	EventsSent      atomic.Uint64
	Errors          atomic.Uint64

	// Rate limiting
	rateLimiter *rateLimiter

	// Compression
	compressionType pb.CompressionType
	compressor      Compressor

	LastActivity time.Time
	mu           sync.RWMutex
}

type rateLimiter struct {
	rate     uint32
	tokens   atomic.Uint32
	lastFill time.Time
	mu       sync.Mutex
}

type Compressor interface {
	Compress(data []byte) ([]byte, error)
	Decompress(data []byte) ([]byte, error)
}

func NewCollectorServer(
	logger *zap.Logger,
	eventStore EventStore,
	tracer trace.Tracer,
) *CollectorServer {
	// Create performance components
	pipelineConfig := performance.DefaultPipelineConfig()
	pipelineConfig.WorkersPerStage = 16
	pipelineConfig.BufferSize = 16384 // Must be power of 2
	pipelineConfig.BatchSize = 1000

	// Create stages for the pipeline
	stages := []performance.Stage{
		// Add default stages here - in production, these would be actual implementations
	}

	pipeline, err := performance.NewEventPipeline(stages, pipelineConfig)
	if err != nil {
		logger.Error("Failed to create event pipeline", zap.Error(err))
		return nil
	}

	batchProc := performance.NewBatchProcessor[*domain.Event](
		1000,                 // batch size
		100*time.Millisecond, // timeout
		10000,                // max queue size
		func(ctx context.Context, batch []*domain.Event) error {
			// Process batch of events
			for _, event := range batch {
				if err := eventStore.Store(ctx, convertEventToProto(event)); err != nil {
					logger.Error("Failed to store event", zap.Error(err))
				}
			}
			return nil
		},
	)

	ringBuffer, err := performance.NewRingBuffer(1024 * 1024) // 1MB buffer
	if err != nil {
		logger.Error("Failed to create ring buffer", zap.Error(err))
		return nil
	}

	server := &CollectorServer{
		logger:             logger,
		eventStore:         eventStore,
		tracer:             tracer,
		pipeline:           pipeline,
		batchProc:          batchProc,
		ringBuffer:         ringBuffer,
		collectors:         make(map[string]*collectorInfo),
		activeStreams:      make(map[string]*collectorStream),
		maxEventsPerSec:    165000,
		maxBatchSize:       10000,
		compressionEnabled: true,
		heartbeatInterval:  30 * time.Second,
	}

	// Start background workers
	go server.metricsReporter()
	go server.healthChecker()
	go server.flowControlManager()

	// Start pipeline
	if err := pipeline.Start(); err != nil {
		logger.Error("Failed to start pipeline", zap.Error(err))
		return nil
	}
	if err := batchProc.Start(); err != nil {
		logger.Error("Failed to start batch processor", zap.Error(err))
		return nil
	}

	return server
}

func (s *CollectorServer) StreamEvents(stream pb.CollectorService_StreamEventsServer) error {
	ctx := stream.Context()
	streamID := fmt.Sprintf("stream-%d", time.Now().UnixNano())

	collStream := &collectorStream{
		ID:              streamID,
		Stream:          stream,
		Context:         ctx,
		LastActivity:    time.Now(),
		compressionType: pb.CompressionType_COMPRESSION_NONE,
		rateLimiter:     newRateLimiter(uint32(s.maxEventsPerSec / 100)), // Per-stream limit
	}

	// Cancel context
	ctx, cancel := context.WithCancel(ctx)
	collStream.Cancel = cancel
	defer cancel()

	// Register stream
	s.streamsMu.Lock()
	s.activeStreams[streamID] = collStream
	s.streamsMu.Unlock()

	defer func() {
		s.streamsMu.Lock()
		delete(s.activeStreams, streamID)
		s.streamsMu.Unlock()

		// Update collector state
		if collStream.CollectorID != "" {
			s.updateCollectorState(collStream.CollectorID, pb.CollectorState_COLLECTOR_STOPPED)
		}

		s.logger.Info("Collector stream closed",
			zap.String("stream_id", streamID),
			zap.String("collector_id", collStream.CollectorID),
			zap.Uint64("events_received", collStream.EventsReceived.Load()),
			zap.Uint64("batches_received", collStream.BatchesReceived.Load()),
			zap.Uint64("bytes_received", collStream.BytesReceived.Load()),
		)
	}()

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
				collStream.Errors.Add(1)
				s.logger.Error("Stream receive error",
					zap.Error(err),
					zap.String("stream_id", streamID))
				return err
			}

			collStream.LastActivity = time.Now()

			// Process request by type
			switch payload := req.Payload.(type) {
			case *pb.StreamRequest_Events:
				if err := s.processEventBatch(ctx, collStream, payload.Events); err != nil {
					s.sendErrorResponse(stream, req.Sequence, err)
				} else {
					s.sendEventAck(stream, req.Sequence, payload.Events)
				}

			case *pb.StreamRequest_FlowControl:
				s.processFlowControl(ctx, collStream, payload.FlowControl)

			case *pb.StreamRequest_Status:
				s.processCollectorStatus(ctx, collStream, payload.Status)

			case *pb.StreamRequest_Ack:
				s.processAck(ctx, collStream, payload.Ack)
			}
		}
	}
}

func (s *CollectorServer) processEventBatch(ctx context.Context, stream *collectorStream, batch *pb.CollectorEventBatch) error {
	start := time.Now()

	// Update metrics
	stream.BatchesReceived.Add(1)
	stream.EventsReceived.Add(uint64(len(batch.Events)))
	s.totalBatches.Add(1)

	// Decompress if needed
	var events []*pb.Event
	if batch.Compression != pb.CompressionType_COMPRESSION_NONE {
		// Decompress batch data
		// Implementation depends on compression type
		events = batch.Events
	} else {
		events = batch.Events
	}

	// Rate limiting
	if !stream.rateLimiter.Allow(uint32(len(events))) {
		s.droppedEvents.Add(uint64(len(events)))
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}

	// Process events through pipeline
	domainEvents := make([]*domain.Event, 0, len(events))
	for _, event := range events {
		// Enrich event
		event.CollectorId = stream.CollectorID
		if event.Timestamp == nil {
			event.Timestamp = timestamppb.Now()
		}
		event.ProcessedAt = timestamppb.Now()

		domainEvents = append(domainEvents, convertEventFromProto(event))
	}

	// Submit to pipeline for processing
	processed := 0
	failed := 0

	// Convert domain events to pipeline events
	pipelineEvents := make([]*performance.Event, 0, len(domainEvents))
	for _, event := range domainEvents {
		pipelineEvent := &performance.Event{
			ID:        uint64(time.Now().UnixNano()),
			Type:      string(event.Type),
			Timestamp: event.Timestamp.UnixNano(),
			Data:      unsafe.Pointer(event),
		}
		pipelineEvents = append(pipelineEvents, pipelineEvent)
	}

	// Submit events to pipeline
	if err := s.pipeline.SubmitBatch(pipelineEvents); err != nil {
		failed = len(pipelineEvents)
		s.failedEvents.Add(uint64(failed))
		s.logger.Error("Failed to submit events to pipeline",
			zap.Error(err),
			zap.String("collector_id", stream.CollectorID))
	} else {
		processed = len(pipelineEvents)
		s.totalEvents.Add(uint64(processed))
	}

	// Update collector metrics
	s.updateCollectorMetrics(stream.CollectorID, batch, processed, failed, time.Since(start))

	// Store batch metadata
	if batch.Metadata != nil {
		s.logger.Debug("Batch metadata",
			zap.String("batch_id", batch.BatchId),
			zap.Uint32("event_count", batch.Metadata.EventCount),
			zap.Uint64("uncompressed_size", batch.Metadata.UncompressedSize),
			zap.Uint32("dropped_events", batch.Metadata.DroppedEvents),
		)
	}

	return nil
}

func (s *CollectorServer) processFlowControl(ctx context.Context, stream *collectorStream, msg *pb.FlowControlMessage) {
	s.logger.Info("Flow control request",
		zap.String("stream_id", stream.ID),
		zap.String("collector_id", stream.CollectorID),
		zap.Uint32("requested_rate", msg.RequestedRate),
		zap.Float32("buffer_utilization", msg.BufferUtilization),
		zap.String("memory_pressure", msg.MemoryPressure.String()),
	)

	// Adjust rate limits based on request
	directive := s.calculateFlowControlDirective(stream, msg)

	// Send response
	response := &pb.StreamResponse{
		Sequence:  0, // Will be set by caller
		Timestamp: timestamppb.Now(),
		Payload: &pb.StreamResponse_FlowControl{
			FlowControl: directive,
		},
	}

	if err := stream.Stream.Send(response); err != nil {
		s.logger.Error("Failed to send flow control directive", zap.Error(err))
	}

	// Update stream configuration
	stream.mu.Lock()
	stream.MaxEventsPerSec = directive.MaxEventsPerSecond
	stream.MaxBatchSize = directive.MaxBatchSize
	if directive.EnableCompression && directive.CompressionType != pb.CompressionType_COMPRESSION_NONE {
		stream.compressionType = directive.CompressionType
		// Initialize compressor
		stream.compressor = s.getCompressor(directive.CompressionType)
	}
	stream.mu.Unlock()
}

func (s *CollectorServer) processCollectorStatus(ctx context.Context, stream *collectorStream, status *pb.CollectorStatus) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	collector, exists := s.collectors[stream.CollectorID]
	if !exists {
		s.logger.Warn("Status update from unknown collector",
			zap.String("collector_id", stream.CollectorID))
		return
	}

	// Update collector info
	collector.mu.Lock()
	collector.State = status.State
	collector.LastSeen = time.Now()

	// Update metrics from status
	if status.Stats != nil {
		collector.Metrics.EventsReceived = status.Stats.TotalEvents
		collector.Metrics.CurrentRate = float64(status.Stats.EventsPerSecond)
	}
	collector.mu.Unlock()

	// Log warnings if any
	for _, warning := range status.Warnings {
		s.logger.Warn("Collector warning",
			zap.String("collector_id", stream.CollectorID),
			zap.String("warning", warning))
	}

	// Check resource utilization
	if status.Resources != nil {
		s.checkResourceUtilization(stream.CollectorID, status.Resources)
	}
}

func (s *CollectorServer) processAck(ctx context.Context, stream *collectorStream, ack *pb.AckMessage) {
	// Process acknowledgment from collector
	if !ack.Success {
		s.logger.Warn("Collector acknowledgment failure",
			zap.String("stream_id", stream.ID),
			zap.Uint64("sequence", ack.Sequence),
			zap.String("message", ack.Message))
	}
}

func (s *CollectorServer) SendEventBatch(ctx context.Context, req *pb.EventBatchRequest) (*pb.EventBatchResponse, error) {
	if req.Batch == nil || len(req.Batch.Events) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty batch")
	}

	// Process batch synchronously
	processed := 0
	failed := 0

	for _, event := range req.Batch.Events {
		if err := s.eventStore.Store(ctx, event); err != nil {
			failed++
			s.logger.Error("Failed to store event", zap.Error(err))
		} else {
			processed++
			s.totalEvents.Add(1)
		}
	}

	response := &pb.EventBatchResponse{
		Success: failed == 0,
		Message: fmt.Sprintf("Processed %d/%d events", processed, len(req.Batch.Events)),
	}

	if req.RequireAck {
		response.Ack = &pb.CollectorEventAck{
			BatchId:        req.Batch.BatchId,
			ProcessedCount: uint32(processed),
			FailedCount:    uint32(failed),
			ProcessedAt:    timestamppb.Now(),
		}
	}

	// Calculate flow control if needed
	s.collectorsMu.RLock()
	collector, exists := s.collectors[req.Batch.CollectorId]
	s.collectorsMu.RUnlock()

	if exists && collector.Metrics.CurrentRate > float64(collector.Config.FlowControl.MaxEventsPerSecond)*0.8 {
		response.FlowControl = s.calculateFlowControlDirective(nil, nil)
	}

	return response, nil
}

func (s *CollectorServer) RegisterCollector(ctx context.Context, req *pb.CollectorRegistration) (*pb.CollectorConfig, error) {
	if req.CollectorId == "" {
		return nil, status.Error(codes.InvalidArgument, "collector_id is required")
	}

	// Create collector info
	info := &collectorInfo{
		ID:           req.CollectorId,
		Type:         req.CollectorType,
		Version:      req.Version,
		Node:         req.Node,
		Capabilities: req.Capabilities,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		State:        pb.CollectorState_COLLECTOR_STARTING,
		Metrics:      &collectorMetrics{},
	}

	// Generate configuration
	config := s.generateCollectorConfig(req)
	info.Config = config

	// Register collector
	s.collectorsMu.Lock()
	s.collectors[req.CollectorId] = info
	s.collectorsMu.Unlock()

	s.logger.Info("Collector registered",
		zap.String("collector_id", req.CollectorId),
		zap.String("type", req.CollectorType),
		zap.String("version", req.Version),
		zap.String("node", req.Node.Hostname))

	return config, nil
}

func (s *CollectorServer) Heartbeat(stream pb.CollectorService_HeartbeatServer) error {
	ctx := stream.Context()
	collectorID := ""

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
				return err
			}

			collectorID = req.CollectorId

			// Update last seen
			s.updateCollectorLastSeen(collectorID)

			// Prepare response
			response := &pb.HeartbeatResponse{
				Timestamp: timestamppb.Now(),
			}

			// Check for config updates
			s.collectorsMu.RLock()
			collector, exists := s.collectors[collectorID]
			s.collectorsMu.RUnlock()

			if exists {
				response.ConfigVersion = collector.Config.ConfigVersion

				// Check if config update is available
				if req.ConfigVersion != collector.Config.ConfigVersion {
					response.ConfigUpdateAvailable = true
				}

				// Include server status
				response.ServerStatus = s.getServerStatus()

				// Calculate flow control if needed
				if collector.Metrics.CurrentRate > float64(collector.Config.FlowControl.MaxEventsPerSecond)*0.9 {
					response.FlowControl = s.calculateFlowControlDirective(nil, nil)
				}
			}

			if err := stream.Send(response); err != nil {
				return err
			}
		}
	}
}

func (s *CollectorServer) GetServerInfo(ctx context.Context, req *pb.ServerInfoRequest) (*pb.ServerInfoResponse, error) {
	return &pb.ServerInfoResponse{
		ServerVersion: "1.0.0",
		SupportedFeatures: []string{
			"streaming",
			"compression",
			"flow_control",
			"load_balancing",
			"hot_reload",
		},
		Capabilities: &pb.ServerCapabilities{
			MaxEventsPerSecond:      uint64(s.maxEventsPerSec),
			MaxConcurrentCollectors: 1000,
			MaxBatchSize:            uint32(s.maxBatchSize),
			SupportsStreaming:       true,
			SupportsCompression:     true,
			SupportsLoadBalancing:   true,
		},
		SupportedCompression: []pb.CompressionType{
			pb.CompressionType_COMPRESSION_NONE,
			pb.CompressionType_COMPRESSION_GZIP,
			pb.CompressionType_COMPRESSION_ZSTD,
			pb.CompressionType_COMPRESSION_LZ4,
			pb.CompressionType_COMPRESSION_SNAPPY,
		},
		RateLimits: &pb.RateLimitInfo{
			GlobalLimit:       uint32(s.maxEventsPerSec),
			PerCollectorLimit: uint32(s.maxEventsPerSec / 100),
			BurstLimit:        uint32(s.maxEventsPerSec / 10),
			Window:            durationpb.New(time.Second),
		},
	}, nil
}

// Management endpoints

func (s *CollectorServer) GetConfig(ctx context.Context, req *pb.GetConfigRequest) (*pb.GetConfigResponse, error) {
	s.collectorsMu.RLock()
	collector, exists := s.collectors[req.CollectorId]
	s.collectorsMu.RUnlock()

	if !exists {
		return nil, status.Errorf(codes.NotFound, "collector %s not found", req.CollectorId)
	}

	return &pb.GetConfigResponse{
		Config:      collector.Config,
		LastUpdated: timestamppb.New(collector.LastConfigUpdate),
		Etag:        fmt.Sprintf("%d", collector.LastConfigUpdate.Unix()),
	}, nil
}

func (s *CollectorServer) UpdateConfig(ctx context.Context, req *pb.UpdateConfigRequest) (*pb.UpdateConfigResponse, error) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	collector, exists := s.collectors[req.CollectorId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "collector %s not found", req.CollectorId)
	}

	// Validate configuration
	validationErrors := s.validateCollectorConfig(req.Config)
	if len(validationErrors) > 0 && !req.ValidateOnly {
		return &pb.UpdateConfigResponse{
			Success:          false,
			Message:          "Configuration validation failed",
			ValidationErrors: validationErrors,
		}, nil
	}

	if req.ValidateOnly {
		return &pb.UpdateConfigResponse{
			Success:          true,
			Message:          "Configuration is valid",
			ValidationErrors: validationErrors,
		}, nil
	}

	// Check etag for optimistic concurrency
	currentEtag := fmt.Sprintf("%d", collector.LastConfigUpdate.Unix())
	if req.Etag != "" && req.Etag != currentEtag {
		return &pb.UpdateConfigResponse{
			Success: false,
			Message: "Configuration has been modified",
		}, nil
	}

	// Apply configuration
	collector.Config = req.Config
	collector.Config.ConfigVersion = fmt.Sprintf("v%d", time.Now().Unix())
	collector.LastConfigUpdate = time.Now()

	return &pb.UpdateConfigResponse{
		Success:       true,
		Message:       "Configuration updated successfully",
		AppliedConfig: collector.Config,
		NewEtag:       fmt.Sprintf("%d", collector.LastConfigUpdate.Unix()),
	}, nil
}

func (s *CollectorServer) GetMetrics(ctx context.Context, req *pb.GetCollectorMetricsRequest) (*pb.GetCollectorMetricsResponse, error) {
	s.collectorsMu.RLock()
	collector, exists := s.collectors[req.CollectorId]
	s.collectorsMu.RUnlock()

	if !exists {
		return nil, status.Errorf(codes.NotFound, "collector %s not found", req.CollectorId)
	}

	metrics := &pb.CollectorMetrics{
		EventsProcessed: int64(collector.Metrics.EventsProcessed),
		EventsDropped:   int64(collector.Metrics.EventsDropped),
		EventsFiltered:  0, // TODO: Implement filtering metrics
		EventsPerSecond: collector.Metrics.CurrentRate,
		Resources:       s.getResourceUtilization(req.CollectorId),
		Network:         s.getNetworkMetrics(req.CollectorId),
		Errors:          s.getErrorMetrics(req.CollectorId),
	}

	// Add quality metrics
	metrics.Quality = &pb.QualityMetrics{
		AvgConfidence:       0.95, // TODO: Calculate from actual events
		ContextCompleteness: 0.87,
		ProcessingLatency:   s.getLatencyStats("processing"),
		CollectionLatency:   s.getLatencyStats("collection"),
		ErrorStats:          s.getErrorStats(req.CollectorId),
	}

	return &pb.GetCollectorMetricsResponse{
		CollectorId: req.CollectorId,
		Metrics:     metrics,
		Timestamp:   timestamppb.Now(),
	}, nil
}

func (s *CollectorServer) ListCollectors(ctx context.Context, req *pb.ListCollectorsRequest) (*pb.ListCollectorsResponse, error) {
	s.collectorsMu.RLock()
	defer s.collectorsMu.RUnlock()

	// Filter collectors
	collectors := make([]*pb.CollectorInfo, 0)
	for _, collector := range s.collectors {
		// Apply filters
		if len(req.CollectorTypes) > 0 && !contains(req.CollectorTypes, collector.Type) {
			continue
		}
		if len(req.NodeIds) > 0 && !contains(req.NodeIds, collector.Node.NodeId) {
			continue
		}
		if len(req.States) > 0 && !containsState(req.States, collector.State) {
			continue
		}

		info := &pb.CollectorInfo{
			CollectorId:     collector.ID,
			CollectorType:   collector.Type,
			Version:         collector.Version,
			State:           collector.State,
			Node:            collector.Node,
			RegisteredAt:    timestamppb.New(collector.RegisteredAt),
			LastSeen:        timestamppb.New(collector.LastSeen),
			EventsPerSecond: collector.Metrics.CurrentRate,
			TotalEvents:     int64(collector.Metrics.EventsReceived),
			ErrorRate:       float64(collector.Metrics.ErrorCount) / float64(collector.Metrics.EventsReceived),
			Health:          s.calculateCollectorHealth(collector),
		}

		collectors = append(collectors, info)
	}

	// Sort collectors
	sortCollectors(collectors, req.SortBy, req.Descending)

	// Paginate
	start := 0
	if req.PageToken != "" {
		// Parse page token
		start = parsePageToken(req.PageToken)
	}

	end := start + int(req.PageSize)
	if end > len(collectors) {
		end = len(collectors)
	}

	pageCollectors := collectors[start:end]

	// Calculate stats
	stats := s.calculateCollectorStats()

	response := &pb.ListCollectorsResponse{
		Collectors: pageCollectors,
		TotalCount: int32(len(collectors)),
		Stats:      stats,
	}

	if end < len(collectors) {
		response.NextPageToken = fmt.Sprintf("%d", end)
	}

	return response, nil
}

func (s *CollectorServer) UnregisterCollector(ctx context.Context, req *pb.UnregisterCollectorRequest) (*pb.UnregisterCollectorResponse, error) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	collector, exists := s.collectors[req.CollectorId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "collector %s not found", req.CollectorId)
	}

	// Check if collector is active
	if collector.State == pb.CollectorState_COLLECTOR_RUNNING && !req.Force {
		return nil, status.Error(codes.FailedPrecondition, "collector is still active, use force=true to remove")
	}

	// Close any active streams
	s.streamsMu.Lock()
	for streamID, stream := range s.activeStreams {
		if stream.CollectorID == req.CollectorId {
			stream.Cancel()
			delete(s.activeStreams, streamID)
		}
	}
	s.streamsMu.Unlock()

	// Count pending events
	pendingEvents := s.countPendingEvents(req.CollectorId)

	// Remove collector
	delete(s.collectors, req.CollectorId)

	s.logger.Info("Collector unregistered",
		zap.String("collector_id", req.CollectorId),
		zap.String("reason", req.Reason),
		zap.Int64("pending_events_dropped", pendingEvents))

	return &pb.UnregisterCollectorResponse{
		Success:              true,
		Message:              fmt.Sprintf("Collector %s unregistered", req.CollectorId),
		UnregisteredAt:       timestamppb.Now(),
		PendingEventsDropped: pendingEvents,
	}, nil
}

// Helper methods

func (s *CollectorServer) sendEventAck(stream pb.CollectorService_StreamEventsServer, sequence uint64, batch *pb.CollectorEventBatch) {
	ack := &pb.CollectorEventAck{
		BatchId:         batch.BatchId,
		RequestSequence: sequence,
		ProcessedCount:  uint32(len(batch.Events)),
		ProcessedAt:     timestamppb.Now(),
	}

	response := &pb.StreamResponse{
		Sequence:  sequence,
		Timestamp: timestamppb.Now(),
		Payload: &pb.StreamResponse_EventAck{
			EventAck: ack,
		},
	}

	_ = stream.Send(response)
}

func (s *CollectorServer) sendErrorResponse(stream pb.CollectorService_StreamEventsServer, sequence uint64, err error) {
	code := codes.Internal
	if st, ok := status.FromError(err); ok {
		code = st.Code()
	}

	errResp := &pb.ErrorResponse{
		Code:      pb.ErrorCode(code),
		Message:   err.Error(),
		Timestamp: timestamppb.Now(),
		Retryable: code == codes.ResourceExhausted || code == codes.Unavailable,
	}

	if code == codes.ResourceExhausted {
		errResp.RetryAfter = durationpb.New(time.Second)
	}

	response := &pb.StreamResponse{
		Sequence:  sequence,
		Timestamp: timestamppb.Now(),
		Payload: &pb.StreamResponse_Error{
			Error: errResp,
		},
	}

	_ = stream.Send(response)
}

func (s *CollectorServer) calculateFlowControlDirective(stream *collectorStream, msg *pb.FlowControlMessage) *pb.FlowControlDirective {
	// Calculate appropriate flow control settings
	maxRate := uint32(s.maxEventsPerSec / 100) // Per-collector limit

	if msg != nil {
		// Adjust based on memory pressure
		switch msg.MemoryPressure {
		case pb.MemoryPressure_MEMORY_PRESSURE_HIGH:
			maxRate = maxRate / 2
		case pb.MemoryPressure_MEMORY_PRESSURE_CRITICAL:
			maxRate = maxRate / 4
		}

		// Consider buffer utilization
		if msg.BufferUtilization > 0.8 {
			maxRate = uint32(float32(maxRate) * (1.0 - msg.BufferUtilization))
		}
	}

	return &pb.FlowControlDirective{
		MaxEventsPerSecond: maxRate,
		MaxBatchSize:       uint32(s.maxBatchSize),
		BatchInterval:      durationpb.New(100 * time.Millisecond),
		EnableCompression:  s.compressionEnabled,
		CompressionType:    pb.CompressionType_COMPRESSION_ZSTD,
		ValidDuration:      durationpb.New(5 * time.Minute),
	}
}

func (s *CollectorServer) generateCollectorConfig(req *pb.CollectorRegistration) *pb.CollectorConfig {
	// Generate appropriate configuration based on capabilities
	flowControl := &pb.FlowControlDirective{
		MaxEventsPerSecond: uint32(s.maxEventsPerSec / 100),
		MaxBatchSize:       uint32(s.maxBatchSize),
		BatchInterval:      durationpb.New(100 * time.Millisecond),
		EnableCompression:  false,
		ValidDuration:      durationpb.New(5 * time.Minute),
	}

	// Enable compression if supported
	if req.Capabilities != nil && len(req.Capabilities.SupportedCompression) > 0 {
		for _, comp := range req.Capabilities.SupportedCompression {
			if comp == pb.CompressionType_COMPRESSION_ZSTD {
				flowControl.EnableCompression = true
				flowControl.CompressionType = pb.CompressionType_COMPRESSION_ZSTD
				break
			}
		}
	}

	// Adjust rates based on requested configuration
	if req.ConfigRequest != nil {
		if req.ConfigRequest.PreferredRate > 0 {
			flowControl.MaxEventsPerSecond = min(req.ConfigRequest.PreferredRate, flowControl.MaxEventsPerSecond)
		}
		if req.ConfigRequest.PreferredBatchSize > 0 {
			flowControl.MaxBatchSize = min(req.ConfigRequest.PreferredBatchSize, flowControl.MaxBatchSize)
		}
	}

	routing := &pb.RoutingConfig{
		DefaultRoute: "default",
		Strategy:     pb.LoadBalancingStrategy_LOAD_BALANCING_ROUND_ROBIN,
	}

	endpoints := []*pb.ServerEndpoint{
		{
			Address:    "localhost",
			Port:       50051,
			TlsEnabled: false,
			Region:     "us-east-1",
			Weight:     100,
			Health:     pb.CollectorHealthStatus_HEALTH_HEALTHY,
		},
	}

	return &pb.CollectorConfig{
		CollectorId:       req.CollectorId,
		ConfigVersion:     fmt.Sprintf("v%d", time.Now().Unix()),
		FlowControl:       flowControl,
		Routing:           routing,
		Endpoints:         endpoints,
		HeartbeatInterval: durationpb.New(s.heartbeatInterval),
		ConfigTtl:         durationpb.New(24 * time.Hour),
		// LastUpdated is tracked separately in collectorInfo
	}
}

func (s *CollectorServer) updateCollectorState(collectorID string, state pb.CollectorState) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	if collector, exists := s.collectors[collectorID]; exists {
		collector.State = state
		collector.LastSeen = time.Now()
	}
}

func (s *CollectorServer) updateCollectorLastSeen(collectorID string) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	if collector, exists := s.collectors[collectorID]; exists {
		collector.LastSeen = time.Now()
	}
}

func (s *CollectorServer) updateCollectorMetrics(collectorID string, batch *pb.CollectorEventBatch, processed, failed int, duration time.Duration) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()

	collector, exists := s.collectors[collectorID]
	if !exists {
		return
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	collector.Metrics.EventsReceived += uint64(len(batch.Events))
	collector.Metrics.EventsProcessed += uint64(processed)
	collector.Metrics.EventsDropped += uint64(failed)
	collector.Metrics.BatchesReceived++
	collector.Metrics.LastEventTime = time.Now()

	// Update average latency
	if collector.Metrics.AverageLatency == 0 {
		collector.Metrics.AverageLatency = duration
	} else {
		collector.Metrics.AverageLatency = (collector.Metrics.AverageLatency + duration) / 2
	}

	// Calculate current rate (simple moving average)
	collector.Metrics.CurrentRate = float64(processed) / duration.Seconds()
}

func (s *CollectorServer) getServerStatus() *pb.ServerStatus {
	// Active streams count not used in current ServerStatus proto

	s.collectorsMu.RLock()
	activeCollectors := 0
	for _, collector := range s.collectors {
		if collector.State == pb.CollectorState_COLLECTOR_RUNNING {
			activeCollectors++
		}
	}
	s.collectorsMu.RUnlock()

	currentRate := s.eventsPerSecond.Load()
	load := float32(currentRate) / float32(s.maxEventsPerSec)

	return &pb.ServerStatus{
		Load:                load,
		AvailableCapacity:   uint32(s.maxEventsPerSec - int64(currentRate)),
		ConnectedCollectors: uint32(activeCollectors),
		ConfigVersion:       "v1",
		RateLimits: &pb.RateLimitInfo{
			GlobalLimit:       uint32(s.maxEventsPerSec),
			PerCollectorLimit: uint32(s.maxEventsPerSec / 100),
			BurstLimit:        uint32(s.maxEventsPerSec / 10),
			Window:            durationpb.New(time.Second),
		},
	}
}

func (s *CollectorServer) checkResourceUtilization(collectorID string, resources *pb.ResourceUtilization) {
	// Check if collector is under stress
	if resources.CpuUsage > 0.8 {
		s.logger.Warn("High CPU usage on collector",
			zap.String("collector_id", collectorID),
			zap.Float32("cpu_usage", resources.CpuUsage))
	}

	if resources.MemoryBytes > 0 && resources.MemoryLimit > 0 {
		memUsage := float64(resources.MemoryBytes) / float64(resources.MemoryLimit)
		if memUsage > 0.8 {
			s.logger.Warn("High memory usage on collector",
				zap.String("collector_id", collectorID),
				zap.Float64("memory_usage", memUsage))
		}
	}
}

func (s *CollectorServer) validateCollectorConfig(config *pb.CollectorConfig) []*pb.ConfigValidationError {
	var errors []*pb.ConfigValidationError

	if config.FlowControl == nil {
		errors = append(errors, &pb.ConfigValidationError{
			Field: "flow_control",
			Error: "flow control configuration is required",
		})
	} else {
		if config.FlowControl.MaxEventsPerSecond == 0 {
			errors = append(errors, &pb.ConfigValidationError{
				Field:      "flow_control.max_events_per_second",
				Error:      "must be greater than 0",
				Suggestion: fmt.Sprintf("use a value between 1000 and %d", s.maxEventsPerSec),
			})
		}
		if config.FlowControl.MaxBatchSize == 0 {
			errors = append(errors, &pb.ConfigValidationError{
				Field:      "flow_control.max_batch_size",
				Error:      "must be greater than 0",
				Suggestion: "use a value between 100 and 10000",
			})
		}
	}

	return errors
}

func (s *CollectorServer) getCompressor(compressionType pb.CompressionType) Compressor {
	// Return appropriate compressor implementation
	// This is a placeholder - actual implementation would use real compression libraries
	return nil
}

// Background workers

func (s *CollectorServer) metricsReporter() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Calculate events per second
		total := s.totalEvents.Load()
		rate := total - s.eventsPerSecond.Swap(total)

		s.logger.Debug("Metrics update",
			zap.Uint64("events_per_second", rate),
			zap.Uint64("total_events", total),
			zap.Uint64("failed_events", s.failedEvents.Load()),
			zap.Uint64("dropped_events", s.droppedEvents.Load()))
	}
}

func (s *CollectorServer) healthChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.collectorsMu.Lock()
		now := time.Now()

		for id, collector := range s.collectors {
			// Check if collector is stale
			if now.Sub(collector.LastSeen) > 2*s.heartbeatInterval {
				if collector.State != pb.CollectorState_COLLECTOR_STOPPED {
					collector.State = pb.CollectorState_COLLECTOR_ERROR
					s.logger.Warn("Collector appears to be offline",
						zap.String("collector_id", id),
						zap.Duration("last_seen", now.Sub(collector.LastSeen)))
				}
			}
		}

		s.collectorsMu.Unlock()
	}
}

func (s *CollectorServer) flowControlManager() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Analyze global load and adjust flow control
		currentRate := s.eventsPerSecond.Load()

		if float64(currentRate) > float64(s.maxEventsPerSec)*0.9 {
			// System under high load - apply back pressure
			s.streamsMu.RLock()
			for _, stream := range s.activeStreams {
				directive := &pb.FlowControlDirective{
					MaxEventsPerSecond: uint32(float64(stream.MaxEventsPerSec) * 0.8),
					MaxBatchSize:       stream.MaxBatchSize,
					EnableCompression:  true,
					CompressionType:    pb.CompressionType_COMPRESSION_ZSTD,
					ValidDuration:      durationpb.New(time.Minute),
				}

				response := &pb.StreamResponse{
					Timestamp: timestamppb.Now(),
					Payload: &pb.StreamResponse_FlowControl{
						FlowControl: directive,
					},
				}

				_ = stream.Stream.Send(response)
			}
			s.streamsMu.RUnlock()
		}
	}
}

// Utility functions

func newRateLimiter(rate uint32) *rateLimiter {
	return &rateLimiter{
		rate:     rate,
		tokens:   atomic.Uint32{},
		lastFill: time.Now(),
	}
}

func (r *rateLimiter) Allow(count uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(r.lastFill)
	newTokens := uint32(elapsed.Seconds() * float64(r.rate))

	current := r.tokens.Load()
	total := min(current+newTokens, r.rate)

	if total >= count {
		r.tokens.Store(total - count)
		r.lastFill = now
		return true
	}

	return false
}

func min(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsState(states []pb.CollectorState, state pb.CollectorState) bool {
	for _, s := range states {
		if s == state {
			return true
		}
	}
	return false
}
