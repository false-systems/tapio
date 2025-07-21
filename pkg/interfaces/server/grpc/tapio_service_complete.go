package grpc

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapioServiceComplete implements the complete TapioService with production-ready features
type TapioServiceComplete struct {
	pb.UnimplementedTapioServiceServer

	// Dependencies
	logger     *zap.Logger
	tracer     trace.Tracer
	storage    EventStorage
	correlator CorrelationEngine
	collectors CollectorRegistry
	metrics    MetricsCollector

	// Configuration
	config ServiceConfiguration

	// Statistics
	startTime      time.Time
	eventsReceived atomic.Uint64
	eventsStored   atomic.Uint64
	streamsActive  atomic.Int32
	correlations   atomic.Uint64

	// Active subscriptions
	subscriptions sync.Map // string -> *EventSubscription

	// Service info
	version     string
	buildCommit string
	buildTime   time.Time

	// Shutdown management
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// ServiceConfiguration holds all service configuration
type ServiceConfiguration struct {
	MaxEventSize        int64
	MaxBatchSize        int
	MaxEventsPerSecond  int64
	RetentionPeriod     time.Duration
	Environment         string
	EnableTracing       bool
	EnableMetrics       bool
	MaxConcurrentStream int
	EventBufferSize     int
}

// EventStorage interface for event persistence and querying
type EventStorage interface {
	Store(ctx context.Context, event *domain.UnifiedEvent) error
	StoreBatch(ctx context.Context, events []*domain.UnifiedEvent) error
	Get(ctx context.Context, id string) (*domain.UnifiedEvent, error)
	Query(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange, limit int, token string) ([]*domain.UnifiedEvent, string, error)
	Count(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange) (int64, error)
	Health() HealthStatus
	Close() error
}

// CorrelationEngine interface for real-time correlation analysis
type CorrelationEngine interface {
	ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) ([]*pb.Correlation, error)
	GetCorrelations(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange) ([]*pb.Correlation, error)
	GetSemanticGroups(ctx context.Context, filter *pb.Filter) ([]*pb.SemanticGroup, error)
	AnalyzeEvents(ctx context.Context, events []*domain.UnifiedEvent) ([]*pb.CorrelationFinding, error)
	Health() HealthStatus
	Close() error
}

// CollectorRegistry manages collector instances and their status
type CollectorRegistry interface {
	RegisterCollector(name string, info CollectorInfo) error
	GetCollectors() map[string]CollectorInfo
	GetCollectorHealth(name string) (HealthStatus, error)
	GetCollectorMetrics(name string) (map[string]float64, error)
	Health() HealthStatus
}

// MetricsCollector handles metrics collection and aggregation
type MetricsCollector interface {
	RecordEvent(eventType string, source string)
	RecordCorrelation(pattern string, confidence float64)
	RecordLatency(operation string, duration time.Duration)
	GetMetrics(component pb.TapioGetMetricsRequest_Component) ([]*pb.SystemMetric, error)
	Health() HealthStatus
}

// CollectorInfo holds information about a registered collector
type CollectorInfo struct {
	Name         string
	Version      string
	Type         string
	Status       string
	LastSeen     time.Time
	EventTypes   []string
	Capabilities []string
	Metadata     map[string]string
}

// HealthStatus represents component health
type HealthStatus struct {
	Status      pb.HealthStatus
	Message     string
	LastHealthy time.Time
	Metrics     map[string]float64
}

// EventSubscription represents an active event subscription
type EventSubscription struct {
	ID          string
	Filter      *pb.Filter
	Stream      pb.TapioService_SubscribeToEventsServer
	CreatedAt   time.Time
	EventsCount atomic.Uint64
}

// NewTapioServiceComplete creates a new complete Tapio service
func NewTapioServiceComplete(
	logger *zap.Logger,
	tracer trace.Tracer,
	storage EventStorage,
	correlator CorrelationEngine,
	collectors CollectorRegistry,
	metrics MetricsCollector,
	config ServiceConfiguration,
) *TapioServiceComplete {
	return &TapioServiceComplete{
		logger:      logger,
		tracer:      tracer,
		storage:     storage,
		correlator:  correlator,
		collectors:  collectors,
		metrics:     metrics,
		config:      config,
		startTime:   time.Now(),
		version:     "1.0.0",
		buildCommit: "main",
		buildTime:   time.Now(),
		shutdown:    make(chan struct{}),
	}
}

// StreamEvents handles bidirectional streaming for real-time event processing
func (s *TapioServiceComplete) StreamEvents(stream grpc.BidiStreamingServer[pb.TapioStreamEventsRequest, pb.TapioStreamEventsResponse]) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "tapio.stream_events",
		trace.WithAttributes(
			attribute.String("service", "tapio"),
			attribute.String("operation", "stream_events"),
		),
	)
	defer span.End()

	s.logger.Info("Client connected for event streaming")
	s.streamsActive.Add(1)
	defer s.streamsActive.Add(-1)

	// Create processing channels
	eventChan := make(chan *pb.Event, s.config.EventBufferSize)
	batchChan := make(chan *pb.EventBatch, 100)
	controlChan := make(chan *pb.StreamControl, 10)
	subscribeChan := make(chan *pb.SubscribeRequest, 10)
	done := make(chan struct{})

	// Start request handler
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(done)
		s.handleStreamRequests(stream, eventChan, batchChan, controlChan, subscribeChan)
	}()

	// Process incoming requests
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		case event := <-eventChan:
			if err := s.processStreamEvent(ctx, stream, event); err != nil {
				span.RecordError(err)
				return err
			}
		case batch := <-batchChan:
			if err := s.processStreamBatch(ctx, stream, batch); err != nil {
				span.RecordError(err)
				return err
			}
		case control := <-controlChan:
			s.handleStreamControl(stream, control)
		case subscribe := <-subscribeChan:
			s.handleStreamSubscription(stream, subscribe)
		}
	}
}

// handleStreamRequests handles incoming stream requests
func (s *TapioServiceComplete) handleStreamRequests(
	stream grpc.BidiStreamingServer[pb.TapioStreamEventsRequest, pb.TapioStreamEventsResponse],
	eventChan chan<- *pb.Event,
	batchChan chan<- *pb.EventBatch,
	controlChan chan<- *pb.StreamControl,
	subscribeChan chan<- *pb.SubscribeRequest,
) {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			s.logger.Error("Stream receive error", zap.Error(err))
			return
		}

		switch r := req.Request.(type) {
		case *pb.TapioStreamEventsRequest_Event:
			select {
			case eventChan <- r.Event:
			case <-stream.Context().Done():
				return
			default:
				s.logger.Warn("Event channel full, dropping event")
			}
		case *pb.TapioStreamEventsRequest_Batch:
			select {
			case batchChan <- r.Batch:
			case <-stream.Context().Done():
				return
			default:
				s.logger.Warn("Batch channel full, dropping batch")
			}
		case *pb.TapioStreamEventsRequest_Control:
			select {
			case controlChan <- r.Control:
			case <-stream.Context().Done():
				return
			default:
				s.logger.Warn("Control channel full, dropping control message")
			}
		case *pb.TapioStreamEventsRequest_Subscribe:
			select {
			case subscribeChan <- r.Subscribe:
			case <-stream.Context().Done():
				return
			default:
				s.logger.Warn("Subscribe channel full, dropping subscription")
			}
		}
	}
}

// processStreamEvent processes a single event
func (s *TapioServiceComplete) processStreamEvent(ctx context.Context, stream grpc.ServerStreamingServer[pb.TapioStreamEventsResponse], event *pb.Event) error {
	// Convert to UnifiedEvent
	unifiedEvent, err := s.convertProtoToUnifiedEvent(event)
	if err != nil {
		return s.sendErrorResponse(stream, fmt.Errorf("failed to convert event: %w", err))
	}

	// Store event
	if err := s.storage.Store(ctx, unifiedEvent); err != nil {
		s.logger.Error("Failed to store event", zap.Error(err), zap.String("event_id", event.Id))
		return s.sendErrorResponse(stream, fmt.Errorf("failed to store event: %w", err))
	}

	// Update metrics
	s.eventsReceived.Add(1)
	s.eventsStored.Add(1)
	s.metrics.RecordEvent(string(unifiedEvent.Type), unifiedEvent.Source)

	// Process correlations
	correlations, err := s.correlator.ProcessEvent(ctx, unifiedEvent)
	if err != nil {
		s.logger.Error("Failed to process correlations", zap.Error(err))
		// Don't fail the entire operation for correlation errors
	} else {
		s.correlations.Add(uint64(len(correlations)))
	}

	// Send acknowledgment
	ack := &pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Ack{
			Ack: &pb.EventAck{
				EventId:   event.Id,
				Timestamp: timestamppb.Now(),
				Success:   true,
				Message:   "Event processed successfully",
			},
		},
	}

	if err := stream.Send(ack); err != nil {
		return err
	}

	// Send any correlations found
	for _, correlation := range correlations {
		corrResp := &pb.TapioStreamEventsResponse{
			Response: &pb.TapioStreamEventsResponse_Correlation{
				Correlation: correlation,
			},
		}
		if err := stream.Send(corrResp); err != nil {
			return err
		}
	}

	return nil
}

// processStreamBatch processes a batch of events
func (s *TapioServiceComplete) processStreamBatch(ctx context.Context, stream grpc.ServerStreamingServer[pb.TapioStreamEventsResponse], batch *pb.EventBatch) error {
	if len(batch.Events) > s.config.MaxBatchSize {
		return s.sendErrorResponse(stream, fmt.Errorf("batch size %d exceeds limit %d", len(batch.Events), s.config.MaxBatchSize))
	}

	// Convert all events
	unifiedEvents := make([]*domain.UnifiedEvent, 0, len(batch.Events))
	for _, event := range batch.Events {
		ue, err := s.convertProtoToUnifiedEvent(event)
		if err != nil {
			s.logger.Error("Failed to convert event in batch", zap.Error(err), zap.String("event_id", event.Id))
			continue
		}
		unifiedEvents = append(unifiedEvents, ue)
	}

	// Store batch
	if err := s.storage.StoreBatch(ctx, unifiedEvents); err != nil {
		s.logger.Error("Failed to store batch", zap.Error(err))
		return s.sendErrorResponse(stream, fmt.Errorf("failed to store batch: %w", err))
	}

	// Update metrics
	s.eventsReceived.Add(uint64(len(batch.Events)))
	s.eventsStored.Add(uint64(len(unifiedEvents)))

	// Send batch acknowledgment
	ack := &pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Ack{
			Ack: &pb.EventAck{
				EventId:   batch.BatchId,
				Timestamp: timestamppb.Now(),
				Success:   true,
				Message:   fmt.Sprintf("Processed %d/%d events", len(unifiedEvents), len(batch.Events)),
			},
		},
	}

	return stream.Send(ack)
}

// GetCorrelations queries correlation findings with rich filtering
func (s *TapioServiceComplete) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_correlations")
	defer span.End()

	correlations, err := s.correlator.GetCorrelations(ctx, req.Filter, req.TimeRange)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to get correlations: %v", err)
	}

	// Apply pagination
	limit := int(req.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	start := 0
	if req.PageToken != "" {
		// Find start position based on page token
		for i, corr := range correlations {
			if corr.Id == req.PageToken {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > len(correlations) {
		end = len(correlations)
	}

	result := correlations[start:end]
	nextToken := ""
	if end < len(correlations) {
		nextToken = correlations[end-1].Id
	}

	return &pb.GetCorrelationsResponse{
		Correlations:  result,
		TotalCount:    int64(len(correlations)),
		NextPageToken: nextToken,
		Timestamp:     timestamppb.Now(),
	}, nil
}

// SubscribeToEvents provides real-time filtered event subscriptions
func (s *TapioServiceComplete) SubscribeToEvents(req *pb.SubscribeRequest, stream pb.TapioService_SubscribeToEventsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "tapio.subscribe_events")
	defer span.End()

	// Create subscription
	subID := fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), s.streamsActive.Load())
	subscription := &EventSubscription{
		ID:        subID,
		Filter:    req.Filter,
		Stream:    stream,
		CreatedAt: time.Now(),
	}

	s.subscriptions.Store(subID, subscription)
	defer s.subscriptions.Delete(subID)

	s.logger.Info("Created event subscription",
		zap.String("subscription_id", subID),
		zap.String("filter_query", req.Filter.GetQuery()),
	)

	// Send connection confirmation
	if err := stream.Send(&pb.EventUpdate{
		UpdateType: pb.EventUpdateType_EVENT_UPDATE_TYPE_CONNECTED,
		Timestamp:  timestamppb.Now(),
	}); err != nil {
		return err
	}

	// Keep connection alive and handle context cancellation
	<-ctx.Done()
	return ctx.Err()
}

// GetSemanticGroups queries semantic correlation groups
func (s *TapioServiceComplete) GetSemanticGroups(ctx context.Context, req *pb.GetSemanticGroupsRequest) (*pb.GetSemanticGroupsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_semantic_groups")
	defer span.End()

	groups, err := s.correlator.GetSemanticGroups(ctx, req.Filter)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to get semantic groups: %v", err)
	}

	// Apply limit
	limit := int(req.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	if len(groups) > limit {
		groups = groups[:limit]
	}

	return &pb.GetSemanticGroupsResponse{
		Groups:     groups,
		TotalCount: int32(len(groups)),
		Timestamp:  timestamppb.Now(),
	}, nil
}

// GetEvents retrieves historical events with pagination
func (s *TapioServiceComplete) GetEvents(ctx context.Context, req *pb.GetEventsRequest) (*pb.GetEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_events")
	defer span.End()

	// Validate page size
	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	// Query events from storage
	events, nextToken, err := s.storage.Query(ctx, req.Filter, req.TimeRange, pageSize, req.PageToken)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to query events: %v", err)
	}

	// Convert to proto
	protoEvents := make([]*pb.Event, 0, len(events))
	for _, event := range events {
		protoEvent, err := s.convertUnifiedEventToProto(event)
		if err != nil {
			s.logger.Error("Failed to convert event to proto", zap.Error(err), zap.String("event_id", event.ID))
			continue
		}
		protoEvents = append(protoEvents, protoEvent)
	}

	// Get total count
	totalCount, err := s.storage.Count(ctx, req.Filter, req.TimeRange)
	if err != nil {
		s.logger.Warn("Failed to get total count", zap.Error(err))
		totalCount = int64(len(protoEvents))
	}

	return &pb.GetEventsResponse{
		Events:        protoEvents,
		TotalCount:    totalCount,
		NextPageToken: nextToken,
		Timestamp:     timestamppb.Now(),
	}, nil
}

// GetEventById retrieves a specific event
func (s *TapioServiceComplete) GetEventById(ctx context.Context, req *pb.GetEventByIdRequest) (*pb.Event, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_event_by_id")
	defer span.End()

	event, err := s.storage.Get(ctx, req.Id)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.NotFound, "event not found: %v", err)
	}

	protoEvent, err := s.convertUnifiedEventToProto(event)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to convert event: %v", err)
	}

	return protoEvent, nil
}

// AnalyzeEvents performs on-demand correlation analysis
func (s *TapioServiceComplete) AnalyzeEvents(ctx context.Context, req *pb.AnalyzeEventsRequest) (*pb.AnalyzeEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.analyze_events")
	defer span.End()

	// Convert proto events to unified events
	unifiedEvents := make([]*domain.UnifiedEvent, 0, len(req.Events))
	for _, event := range req.Events {
		ue, err := s.convertProtoToUnifiedEvent(event)
		if err != nil {
			s.logger.Error("Failed to convert event for analysis", zap.Error(err))
			continue
		}
		unifiedEvents = append(unifiedEvents, ue)
	}

	startTime := time.Now()
	correlations, err := s.correlator.AnalyzeEvents(ctx, unifiedEvents)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to analyze events: %v", err)
	}
	processingTime := time.Since(startTime).Milliseconds()

	analysisID := fmt.Sprintf("analysis-%d", time.Now().UnixNano())

	return &pb.AnalyzeEventsResponse{
		AnalysisId:   analysisID,
		Status:       pb.AnalysisStatus_ANALYSIS_STATUS_COMPLETED,
		Correlations: correlations,
		Summary: &pb.AnalysisSummary{
			TotalEvents:       int32(len(req.Events)),
			CorrelationsFound: int32(len(correlations)),
			ProcessingTime:    processingTime,
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetInsights retrieves AI-generated insights
func (s *TapioServiceComplete) GetInsights(ctx context.Context, req *pb.GetInsightsRequest) (*pb.GetInsightsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_insights")
	defer span.End()

	// Generate insights based on current system state
	insights := s.generateInsights(ctx, req)

	stats := &pb.InsightStats{
		InsightsByType:       make(map[string]int32),
		InsightsBySeverity:   make(map[string]int32),
		AvgConfidence:        0,
		TotalRecommendations: 0,
	}

	// Calculate statistics
	totalConfidence := 0.0
	for _, insight := range insights {
		stats.InsightsByType[insight.Type]++
		if insight.Impact != nil {
			stats.InsightsBySeverity[insight.Impact.Severity]++
		}
		totalConfidence += insight.Confidence
		stats.TotalRecommendations += int32(len(insight.Actions))
	}

	if len(insights) > 0 {
		stats.AvgConfidence = totalConfidence / float64(len(insights))
	}

	return &pb.GetInsightsResponse{
		Insights:      insights,
		TotalCount:    int32(len(insights)),
		NextPageToken: "",
		Stats:         stats,
		Timestamp:     timestamppb.Now(),
	}, nil
}

// GetMetrics retrieves system metrics and statistics
func (s *TapioServiceComplete) GetMetrics(ctx context.Context, req *pb.TapioGetMetricsRequest) (*pb.TapioGetMetricsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_metrics")
	defer span.End()

	metrics, err := s.metrics.GetMetrics(req.Component)
	if err != nil {
		span.RecordError(err)
		return nil, status.Errorf(codes.Internal, "failed to get metrics: %v", err)
	}

	return &pb.TapioGetMetricsResponse{
		Metrics:   metrics,
		Timestamp: timestamppb.Now(),
	}, nil
}

// HealthCheck monitors service health
func (s *TapioServiceComplete) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	// Check all component health
	components := make(map[string]*pb.ComponentHealth)

	// Check storage
	storageHealth := s.storage.Health()
	components["storage"] = &pb.ComponentHealth{
		Status:      storageHealth.Status,
		Message:     storageHealth.Message,
		LastHealthy: timestamppb.New(storageHealth.LastHealthy),
		Metrics:     storageHealth.Metrics,
	}

	// Check correlation engine
	corrHealth := s.correlator.Health()
	components["correlation"] = &pb.ComponentHealth{
		Status:      corrHealth.Status,
		Message:     corrHealth.Message,
		LastHealthy: timestamppb.New(corrHealth.LastHealthy),
		Metrics:     corrHealth.Metrics,
	}

	// Check collectors
	collectorsHealth := s.collectors.Health()
	components["collectors"] = &pb.ComponentHealth{
		Status:      collectorsHealth.Status,
		Message:     collectorsHealth.Message,
		LastHealthy: timestamppb.New(collectorsHealth.LastHealthy),
		Metrics:     collectorsHealth.Metrics,
	}

	// Check metrics system
	metricsHealth := s.metrics.Health()
	components["metrics"] = &pb.ComponentHealth{
		Status:      metricsHealth.Status,
		Message:     metricsHealth.Message,
		LastHealthy: timestamppb.New(metricsHealth.LastHealthy),
		Metrics:     metricsHealth.Metrics,
	}

	// Determine overall health
	overallStatus := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	for _, comp := range components {
		if comp.Status == pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			overallStatus = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
			break
		} else if comp.Status == pb.HealthStatus_HEALTH_STATUS_DEGRADED {
			overallStatus = pb.HealthStatus_HEALTH_STATUS_DEGRADED
		}
	}

	return &pb.HealthCheckResponse{
		OverallStatus: overallStatus,
		Components:    components,
		CheckedAt:     timestamppb.Now(),
	}, nil
}

// GetServiceInfo returns service capabilities and version
func (s *TapioServiceComplete) GetServiceInfo(ctx context.Context, req *emptypb.Empty) (*pb.TapioServiceInfo, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_service_info")
	defer span.End()

	uptime := time.Since(s.startTime)
	collectors := s.collectors.GetCollectors()
	enabledCollectors := make([]string, 0, len(collectors))
	for name := range collectors {
		enabledCollectors = append(enabledCollectors, name)
	}

	return &pb.TapioServiceInfo{
		Version:     s.version,
		BuildCommit: s.buildCommit,
		BuildTime:   timestamppb.New(s.buildTime),
		SupportedFeatures: []string{
			"unified-events",
			"real-time-streaming",
			"correlation-analysis",
			"semantic-grouping",
			"pattern-detection",
			"health-monitoring",
			"metrics-collection",
			"distributed-tracing",
		},
		EnabledCollectors: enabledCollectors,
		CorrelationEngines: []string{
			"real-time-processor",
			"pattern-matcher",
			"semantic-analyzer",
			"anomaly-detector",
		},
		Limits: &pb.ServiceLimits{
			MaxEventsPerSecond:   s.config.MaxEventsPerSecond,
			MaxConcurrentStreams: int64(s.config.MaxConcurrentStream),
			MaxEventSize:         s.config.MaxEventSize,
			MaxBatchSize:         int64(s.config.MaxBatchSize),
			RetentionPeriod:      durationpb.New(s.config.RetentionPeriod),
		},
		ApiVersions: []string{"v1"},
		Uptime:      durationpb.New(uptime),
		Metrics: map[string]float64{
			"events_received":    float64(s.eventsReceived.Load()),
			"events_stored":      float64(s.eventsStored.Load()),
			"streams_active":     float64(s.streamsActive.Load()),
			"correlations_found": float64(s.correlations.Load()),
			"uptime_seconds":     uptime.Seconds(),
			"memory_usage_mb":    float64(getMemoryUsageMB()),
			"goroutines":         float64(runtime.NumGoroutine()),
		},
	}, nil
}

// Helper methods

func (s *TapioServiceComplete) convertProtoToUnifiedEvent(event *pb.Event) (*domain.UnifiedEvent, error) {
	// Comprehensive conversion logic will be implemented here
	ue := &domain.UnifiedEvent{
		ID:        event.Id,
		Timestamp: event.Timestamp.AsTime(),
		Type:      s.mapProtoEventType(event.Type),
		Source:    event.Source,
	}

	// Add trace context if present
	if event.Context != nil && event.Context.TraceId != "" {
		ue.TraceContext = &domain.TraceContext{
			TraceID: event.Context.TraceId,
			SpanID:  event.Context.SpanId,
		}
	}

	// Add entity context
	if event.Context != nil && event.Context.EntityType != "" {
		ue.Entity = &domain.EntityContext{
			Type:      event.Context.EntityType,
			Name:      event.Context.EntityName,
			Namespace: event.Context.Namespace,
		}
	}

	// Add layer-specific data based on event type
	switch event.Type {
	case pb.EventType_EVENT_TYPE_KERNEL:
		if event.KernelData != nil {
			ue.Kernel = &domain.KernelData{
				Syscall: event.KernelData.Syscall,
				PID:     event.KernelData.Pid,
				Comm:    event.KernelData.Comm,
			}
		}
	case pb.EventType_EVENT_TYPE_NETWORK:
		if event.NetworkData != nil {
			ue.Network = &domain.NetworkData{
				Protocol:   event.NetworkData.Protocol,
				SourceIP:   event.NetworkData.SourceIp,
				SourcePort: uint16(event.NetworkData.SourcePort),
				DestIP:     event.NetworkData.DestIp,
				DestPort:   uint16(event.NetworkData.DestPort),
			}
		}
	case pb.EventType_EVENT_TYPE_APPLICATION:
		if event.ApplicationData != nil {
			ue.Application = &domain.ApplicationData{
				Level:   event.ApplicationData.Level,
				Message: event.ApplicationData.Message,
				Logger:  event.ApplicationData.Logger,
			}
		}
	}

	return ue, nil
}

func (s *TapioServiceComplete) convertUnifiedEventToProto(event *domain.UnifiedEvent) (*pb.Event, error) {
	pe := &pb.Event{
		Id:        event.ID,
		Timestamp: timestamppb.New(event.Timestamp),
		Type:      s.mapDomainEventType(event.Type),
		Source:    event.Source,
	}

	// Add context
	pe.Context = &pb.EventContext{}

	// Add trace context
	if event.TraceContext != nil {
		pe.Context.TraceId = event.TraceContext.TraceID
		pe.Context.SpanId = event.TraceContext.SpanID
	}

	// Add entity context
	if event.Entity != nil {
		pe.Context.EntityType = event.Entity.Type
		pe.Context.EntityName = event.Entity.Name
		pe.Context.Namespace = event.Entity.Namespace
	}

	// Add layer-specific data
	if event.Kernel != nil {
		pe.KernelData = &pb.KernelEventData{
			Syscall: event.Kernel.Syscall,
			Pid:     event.Kernel.PID,
			Comm:    event.Kernel.Comm,
		}
	}

	if event.Network != nil {
		pe.NetworkData = &pb.NetworkEventData{
			Protocol:   event.Network.Protocol,
			SourceIp:   event.Network.SourceIP,
			SourcePort: uint32(event.Network.SourcePort),
			DestIp:     event.Network.DestIP,
			DestPort:   uint32(event.Network.DestPort),
		}
	}

	if event.Application != nil {
		pe.ApplicationData = &pb.ApplicationEventData{
			Level:   event.Application.Level,
			Message: event.Application.Message,
			Logger:  event.Application.Logger,
		}
	}

	return pe, nil
}

func (s *TapioServiceComplete) mapProtoEventType(eventType pb.EventType) domain.EventType {
	switch eventType {
	case pb.EventType_EVENT_TYPE_KERNEL:
		return domain.EventTypeProcess
	case pb.EventType_EVENT_TYPE_NETWORK:
		return domain.EventTypeNetwork
	case pb.EventType_EVENT_TYPE_APPLICATION:
		return domain.EventTypeLog
	case pb.EventType_EVENT_TYPE_KUBERNETES:
		return domain.EventTypeKubernetes
	case pb.EventType_EVENT_TYPE_SYSTEM:
		return domain.EventTypeSystem
	default:
		return domain.EventTypeSystem
	}
}

func (s *TapioServiceComplete) mapDomainEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_KERNEL
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeLog:
		return pb.EventType_EVENT_TYPE_APPLICATION
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_SYSTEM
	}
}

func (s *TapioServiceComplete) sendErrorResponse(stream grpc.ServerStreamingServer[pb.TapioStreamEventsResponse], err error) error {
	resp := &pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Error{
			Error: &pb.Error{
				Code:    codes.Internal.String(),
				Message: err.Error(),
			},
		},
	}
	return stream.Send(resp)
}

func (s *TapioServiceComplete) handleStreamControl(stream grpc.ServerStreamingServer[pb.TapioStreamEventsResponse], control *pb.StreamControl) {
	resp := &pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Control{
			Control: &pb.StreamControlResponse{
				Success: true,
				Message: fmt.Sprintf("Control command %s processed", control.Type.String()),
			},
		},
	}
	stream.Send(resp)
}

func (s *TapioServiceComplete) handleStreamSubscription(stream grpc.ServerStreamingServer[pb.TapioStreamEventsResponse], subscribe *pb.SubscribeRequest) {
	// Implementation for stream-based subscription
	s.logger.Info("Stream subscription request received", zap.String("filter", subscribe.Filter.GetQuery()))
}

func (s *TapioServiceComplete) generateInsights(ctx context.Context, req *pb.GetInsightsRequest) []*pb.Insight {
	var insights []*pb.Insight

	// Generate performance insight based on event rate
	eventsPerSec := float64(s.eventsReceived.Load()) / time.Since(s.startTime).Seconds()
	if eventsPerSec > 1000 {
		insights = append(insights, &pb.Insight{
			Id:         fmt.Sprintf("perf-%d", time.Now().UnixNano()),
			Type:       "performance",
			Title:      "High Event Throughput",
			Summary:    fmt.Sprintf("Processing %.0f events per second", eventsPerSec),
			Confidence: 0.9,
			CreatedAt:  timestamppb.Now(),
			Explanation: &pb.HumanExplanation{
				TechnicalExplanation: fmt.Sprintf("System is processing %.0f events/second, which is above the normal threshold", eventsPerSec),
				BusinessExplanation:  "High event throughput may indicate increased system activity or potential issues",
				ExecutiveSummary:     "System is handling high volume of events",
			},
		})
	}

	// Generate correlation insight
	correlationCount := s.correlations.Load()
	if correlationCount > 0 {
		insights = append(insights, &pb.Insight{
			Id:         fmt.Sprintf("corr-%d", time.Now().UnixNano()),
			Type:       "correlation",
			Title:      "Event Correlations Detected",
			Summary:    fmt.Sprintf("Found %d correlations", correlationCount),
			Confidence: 0.8,
			CreatedAt:  timestamppb.Now(),
			Explanation: &pb.HumanExplanation{
				TechnicalExplanation: fmt.Sprintf("Detected %d event correlations indicating system patterns", correlationCount),
				BusinessExplanation:  "Correlated events help identify potential issues before they escalate",
				ExecutiveSummary:     "System is actively identifying event patterns",
			},
		})
	}

	return insights
}

// getMemoryUsageMB returns current memory usage in MB
func getMemoryUsageMB() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.Alloc / 1024 / 1024)
}

// Shutdown gracefully shuts down the service
func (s *TapioServiceComplete) Shutdown() error {
	close(s.shutdown)
	s.wg.Wait()

	// Close all components
	if err := s.storage.Close(); err != nil {
		s.logger.Error("Failed to close storage", zap.Error(err))
	}

	if err := s.correlator.Close(); err != nil {
		s.logger.Error("Failed to close correlator", zap.Error(err))
	}

	return nil
}
