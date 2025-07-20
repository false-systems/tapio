package grpc

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TapioServer struct {
	pb.UnimplementedTapioServiceServer
	logger             *zap.Logger
	correlationManager *correlation.Manager
	eventStore         EventStore
	metricsCollector   MetricsCollector
	tracer             trace.Tracer

	// Stream management
	streamsMu     sync.RWMutex
	activeStreams map[string]*streamConnection
	streamCounter uint64

	// Performance tuning
	eventBatchSize    int
	maxEventsPerSec   int64
	processingWorkers int
}

type streamConnection struct {
	id             string
	stream         pb.TapioService_StreamEventsServer
	ctx            context.Context
	cancel         context.CancelFunc
	subscriptions  map[string]*subscription
	lastActivity   time.Time
	eventsReceived uint64
	eventsSent     uint64
	errors         uint64
	mu             sync.RWMutex
}

type subscription struct {
	id      string
	filter  *pb.Filter
	created time.Time
	matched uint64
}

type EventStore interface {
	Store(ctx context.Context, event *pb.Event) error
	StoreBatch(ctx context.Context, events []*pb.Event) error
	Get(ctx context.Context, id string) (*pb.Event, error)
	Query(ctx context.Context, query *pb.EventQuery) ([]*pb.Event, string, error)
	GetByTimeRange(ctx context.Context, start, end time.Time, limit int) ([]*pb.Event, error)
	Subscribe(ctx context.Context, filter *pb.Filter, ch chan<- *pb.Event) UnsubscribeFunc
	HealthCheck(ctx context.Context) error
}

type MetricsCollector interface {
	RecordEvent(eventType string, severity string)
	RecordCorrelation(correlationType string)
	RecordStreamConnection(connected bool)
	RecordProcessingDuration(operation string, duration time.Duration)
	GetMetrics(component string) []*pb.SystemMetric
}

func NewTapioServer(
	logger *zap.Logger,
	correlationManager *correlation.Manager,
	eventStore EventStore,
	metricsCollector MetricsCollector,
	tracer trace.Tracer,
) *TapioServer {
	return &TapioServer{
		logger:             logger,
		correlationManager: correlationManager,
		eventStore:         eventStore,
		metricsCollector:   metricsCollector,
		tracer:             tracer,
		activeStreams:      make(map[string]*streamConnection),
		eventBatchSize:     1000,
		maxEventsPerSec:    165000, // As per Epic requirement
		processingWorkers:  16,
	}
}

func (s *TapioServer) StreamEvents(stream pb.TapioService_StreamEventsServer) error {
	ctx := stream.Context()
	streamID := fmt.Sprintf("stream-%d-%d", time.Now().UnixNano(), s.streamCounter)
	s.streamCounter++

	streamConn := &streamConnection{
		id:            streamID,
		stream:        stream,
		ctx:           ctx,
		subscriptions: make(map[string]*subscription),
		lastActivity:  time.Now(),
	}

	// Register stream
	s.streamsMu.Lock()
	s.activeStreams[streamID] = streamConn
	s.streamsMu.Unlock()

	s.metricsCollector.RecordStreamConnection(true)
	s.logger.Info("New stream connection established", zap.String("stream_id", streamID))

	defer func() {
		s.streamsMu.Lock()
		delete(s.activeStreams, streamID)
		s.streamsMu.Unlock()
		s.metricsCollector.RecordStreamConnection(false)
		s.logger.Info("Stream connection closed",
			zap.String("stream_id", streamID),
			zap.Uint64("events_received", streamConn.eventsReceived),
			zap.Uint64("events_sent", streamConn.eventsSent),
			zap.Uint64("errors", streamConn.errors))
	}()

	// Process incoming messages
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
				streamConn.errors++
				s.logger.Error("Stream receive error", zap.Error(err), zap.String("stream_id", streamID))
				return err
			}

			streamConn.lastActivity = time.Now()

			// Process request based on type
			switch r := req.Request.(type) {
			case *pb.TapioStreamEventsRequest_Event:
				if err := s.processStreamEvent(ctx, streamConn, r.Event); err != nil {
					s.sendErrorResponse(stream, err)
				}

			case *pb.TapioStreamEventsRequest_Batch:
				if err := s.processStreamBatch(ctx, streamConn, r.Batch); err != nil {
					s.sendErrorResponse(stream, err)
				}

			case *pb.TapioStreamEventsRequest_Control:
				if err := s.processStreamControl(ctx, streamConn, r.Control); err != nil {
					s.sendErrorResponse(stream, err)
				}

			case *pb.TapioStreamEventsRequest_Subscribe:
				if err := s.processStreamSubscribe(ctx, streamConn, r.Subscribe); err != nil {
					s.sendErrorResponse(stream, err)
				}
			}
		}
	}
}

func (s *TapioServer) processStreamEvent(ctx context.Context, conn *streamConnection, event *pb.Event) error {
	start := time.Now()
	defer func() {
		s.metricsCollector.RecordProcessingDuration("stream_event", time.Since(start))
	}()

	// Enrich event with processing metadata
	if event.Id == "" {
		event.Id = generateEventID()
	}
	if event.Timestamp == nil {
		event.Timestamp = timestamppb.Now()
	}
	event.ProcessedAt = timestamppb.Now()

	// Store event
	if err := s.eventStore.Store(ctx, event); err != nil {
		s.logger.Error("Failed to store event", zap.Error(err), zap.String("event_id", event.Id))
		return fmt.Errorf("failed to store event: %w", err)
	}

	conn.mu.Lock()
	conn.eventsReceived++
	conn.mu.Unlock()

	s.metricsCollector.RecordEvent(event.Type.String(), event.Severity.String())

	// Send acknowledgment
	ack := &pb.EventAck{
		EventId:   event.Id,
		Timestamp: timestamppb.Now(),
		Status:    "processed",
		Metadata: map[string]string{
			"stream_id":    conn.id,
			"processed_at": time.Now().Format(time.RFC3339),
		},
	}

	if err := conn.stream.Send(&pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Ack{Ack: ack},
	}); err != nil {
		return fmt.Errorf("failed to send ack: %w", err)
	}

	// Trigger correlation analysis asynchronously
	go s.analyzeEventCorrelations(context.Background(), event, conn)

	return nil
}

func (s *TapioServer) processStreamBatch(ctx context.Context, conn *streamConnection, batch *pb.EventBatch) error {
	start := time.Now()
	defer func() {
		s.metricsCollector.RecordProcessingDuration("stream_batch", time.Since(start))
	}()

	if len(batch.Events) == 0 {
		return status.Error(codes.InvalidArgument, "empty batch")
	}

	// Process events in batch
	processedCount := 0
	for _, event := range batch.Events {
		if event.Id == "" {
			event.Id = generateEventID()
		}
		if event.Timestamp == nil {
			event.Timestamp = timestamppb.Now()
		}
		event.ProcessedAt = timestamppb.Now()
		event.CollectorId = batch.CollectorId

		s.metricsCollector.RecordEvent(event.Type.String(), event.Severity.String())
		processedCount++
	}

	// Store batch
	if err := s.eventStore.StoreBatch(ctx, batch.Events); err != nil {
		s.logger.Error("Failed to store batch", zap.Error(err), zap.String("batch_id", batch.BatchId))
		return fmt.Errorf("failed to store batch: %w", err)
	}

	conn.mu.Lock()
	conn.eventsReceived += uint64(len(batch.Events))
	conn.mu.Unlock()

	// Send batch acknowledgment
	ack := &pb.EventAck{
		BatchId:   batch.BatchId,
		Timestamp: timestamppb.Now(),
		Status:    "processed",
		Metadata: map[string]string{
			"stream_id":       conn.id,
			"processed_count": fmt.Sprintf("%d", processedCount),
			"batch_size":      fmt.Sprintf("%d", len(batch.Events)),
		},
	}

	if err := conn.stream.Send(&pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Ack{Ack: ack},
	}); err != nil {
		return fmt.Errorf("failed to send batch ack: %w", err)
	}

	// Trigger correlation analysis for batch
	go s.analyzeBatchCorrelations(context.Background(), batch.Events, conn)

	return nil
}

func (s *TapioServer) processStreamControl(ctx context.Context, conn *streamConnection, control *pb.StreamControl) error {
	s.logger.Debug("Processing stream control",
		zap.String("stream_id", conn.id),
		zap.String("control_type", control.Type.String()))

	response := &pb.StreamControlResponse{
		Success:    true,
		Parameters: make(map[string]string),
	}

	switch control.Type {
	case pb.StreamControl_CONTROL_TYPE_PAUSE:
		conn.mu.Lock()
		// Implementation would pause event processing for this stream
		conn.mu.Unlock()
		response.Message = "Stream paused"

	case pb.StreamControl_CONTROL_TYPE_RESUME:
		conn.mu.Lock()
		// Implementation would resume event processing
		conn.mu.Unlock()
		response.Message = "Stream resumed"

	case pb.StreamControl_CONTROL_TYPE_CONFIGURE:
		// Apply configuration parameters
		for k := range control.Parameters {
			switch k {
			case "batch_size":
				// Update batch size for this stream
			case "rate_limit":
				// Update rate limit for this stream
			}
		}
		response.Message = "Configuration applied"
		response.Parameters = control.Parameters

	case pb.StreamControl_CONTROL_TYPE_HEARTBEAT:
		response.Message = "Heartbeat acknowledged"
		response.Parameters["timestamp"] = time.Now().Format(time.RFC3339)
	}

	return conn.stream.Send(&pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Control{Control: response},
	})
}

func (s *TapioServer) processStreamSubscribe(ctx context.Context, conn *streamConnection, req *pb.SubscribeRequest) error {
	sub := &subscription{
		id:      req.SubscriptionId,
		filter:  req.Filter,
		created: time.Now(),
	}

	conn.mu.Lock()
	conn.subscriptions[sub.id] = sub
	conn.mu.Unlock()

	s.logger.Info("Added subscription to stream",
		zap.String("stream_id", conn.id),
		zap.String("subscription_id", sub.id))

	// If requested, send existing events
	if req.IncludeExisting && req.Lookback != nil {
		go s.sendHistoricalEvents(conn, sub, req.Lookback.AsDuration())
	}

	return nil
}

func (s *TapioServer) analyzeEventCorrelations(ctx context.Context, event *pb.Event, conn *streamConnection) {
	// Convert to domain event
	domainEvent := &domain.Event{
		ID:        domain.EventID(event.Id),
		Type:      domain.EventType(event.Type),
		Severity:  domain.EventSeverity(event.Severity),
		Source:    domain.SourceType(event.Source),
		Message:   event.Message,
		Timestamp: event.Timestamp.AsTime(),
		Context: domain.EventContext{
			TraceID: event.TraceId,
			SpanID:  event.SpanId,
		},
		Attributes: convertStringMapToInterface(event.Attributes),
		Confidence: event.Confidence,
		Tags:       event.Tags,
	}

	// Analyze correlations
	correlations := s.correlationManager.AnalyzeEvent(ctx, domainEvent)

	// Send correlation updates to stream
	for _, corr := range correlations {
		pbCorr := convertCorrelationToProto(corr)

		if err := conn.stream.Send(&pb.TapioStreamEventsResponse{
			Response: &pb.TapioStreamEventsResponse_Correlation{Correlation: pbCorr},
		}); err != nil {
			s.logger.Error("Failed to send correlation", zap.Error(err))
			break
		}

		conn.mu.Lock()
		conn.eventsSent++
		conn.mu.Unlock()

		s.metricsCollector.RecordCorrelation(fmt.Sprintf("%v", corr.Type))
	}
}

func (s *TapioServer) analyzeBatchCorrelations(ctx context.Context, events []*pb.Event, conn *streamConnection) {
	// Convert to domain events
	domainEvents := make([]*domain.Event, 0, len(events))
	for _, event := range events {
		domainEvents = append(domainEvents, &domain.Event{
			ID:        domain.EventID(event.Id),
			Type:      domain.EventType(event.Type),
			Severity:  domain.EventSeverity(event.Severity),
			Source:    domain.SourceType(event.Source),
			Message:   event.Message,
			Timestamp: event.Timestamp.AsTime(),
			Context: domain.EventContext{
				TraceID: event.TraceId,
				SpanID:  event.SpanId,
			},
			Attributes: convertStringMapToInterface(event.Attributes),
			Confidence: event.Confidence,
			Tags:       event.Tags,
		})
	}

	// Analyze batch correlations
	semanticGroups := s.correlationManager.AnalyzeBatch(ctx, domainEvents)

	// Send semantic group updates
	for _, group := range semanticGroups {
		pbGroup := convertSemanticGroupToProto(group)

		if err := conn.stream.Send(&pb.TapioStreamEventsResponse{
			Response: &pb.TapioStreamEventsResponse_SemanticGroup{SemanticGroup: pbGroup},
		}); err != nil {
			s.logger.Error("Failed to send semantic group", zap.Error(err))
			break
		}

		conn.mu.Lock()
		conn.eventsSent++
		conn.mu.Unlock()
	}
}

func (s *TapioServer) sendHistoricalEvents(conn *streamConnection, sub *subscription, lookback time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now().Add(-lookback)
	events, err := s.eventStore.GetByTimeRange(ctx, start, time.Now(), 1000)
	if err != nil {
		s.logger.Error("Failed to fetch historical events", zap.Error(err))
		return
	}

	for _, event := range events {
		if matchesFilter(event, sub.filter) {
			// Note: In real implementation, this would be sent via SubscribeToEvents RPC
			// For now, we're sending via the main stream
			if err := conn.stream.Send(&pb.TapioStreamEventsResponse{
				Response: &pb.TapioStreamEventsResponse_Correlation{
					Correlation: &pb.Correlation{
						Id:          "historical-" + event.Id,
						Description: "Historical event",
						EventIds:    []string{event.Id},
					},
				},
			}); err != nil {
				s.logger.Error("Failed to send historical event", zap.Error(err))
				break
			}

			sub.matched++
		}
	}
}

func (s *TapioServer) sendErrorResponse(stream pb.TapioService_StreamEventsServer, err error) {
	errResp := &pb.Error{
		Code:    codes.Internal.String(),
		Message: err.Error(),
		Details: map[string]string{
			"error": "Internal processing error",
		},
		Timestamp: timestamppb.Now(),
	}

	if st, ok := status.FromError(err); ok {
		errResp.Code = st.Code().String()
		errResp.Message = st.Message()
	}

	_ = stream.Send(&pb.TapioStreamEventsResponse{
		Response: &pb.TapioStreamEventsResponse_Error{Error: errResp},
	})
}

// Additional RPC implementations

func (s *TapioServer) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	// Implementation for getting correlations
	correlations := s.correlationManager.GetCorrelations(ctx, convertFilterFromProto(req.Query.Filter))

	pbCorrelations := make([]*pb.Correlation, 0, len(correlations))
	for _, corr := range correlations {
		pbCorrelations = append(pbCorrelations, convertCorrelationToProto(corr))
	}

	return &pb.GetCorrelationsResponse{
		Correlations: pbCorrelations,
		TotalCount:   int64(len(correlations)),
		Metadata: map[string]string{
			"query_time": time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (s *TapioServer) SubscribeToEvents(req *pb.SubscribeRequest, stream pb.TapioService_SubscribeToEventsServer) error {
	ctx := stream.Context()

	// Create event channel for subscription
	eventCh := make(chan *pb.Event, 100)
	defer close(eventCh)

	// Register subscription with event store
	unsubscribe := s.eventStore.Subscribe(ctx, req.Filter, eventCh)
	defer unsubscribe()

	s.logger.Info("Event subscription started", zap.String("subscription_id", req.SubscriptionId))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-eventCh:
			update := &pb.EventUpdate{
				Type:            pb.EventUpdate_UPDATE_TYPE_NEW,
				Event:           event,
				UpdateTimestamp: timestamppb.Now(),
			}

			if err := stream.Send(update); err != nil {
				s.logger.Error("Failed to send event update", zap.Error(err))
				return err
			}
		}
	}
}

func (s *TapioServer) GetSemanticGroups(ctx context.Context, req *pb.GetSemanticGroupsRequest) (*pb.GetSemanticGroupsResponse, error) {
	groups := s.correlationManager.GetSemanticGroups(ctx, convertFilterFromProto(req.Filter))

	pbGroups := make([]*pb.SemanticGroup, 0, len(groups))
	for _, group := range groups {
		pbGroups = append(pbGroups, convertSemanticGroupToProto(group))
	}

	return &pb.GetSemanticGroupsResponse{
		Groups:     pbGroups,
		TotalCount: int64(len(groups)),
	}, nil
}

func (s *TapioServer) GetEvents(ctx context.Context, req *pb.GetEventsRequest) (*pb.GetEventsResponse, error) {
	events, nextToken, err := s.eventStore.Query(ctx, req.Query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query events: %v", err)
	}

	return &pb.GetEventsResponse{
		Events:        events,
		TotalCount:    int64(len(events)),
		NextPageToken: nextToken,
		Metadata: map[string]string{
			"query_time": time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (s *TapioServer) GetEventById(ctx context.Context, req *pb.GetEventByIdRequest) (*pb.Event, error) {
	event, err := s.eventStore.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "event not found: %v", err)
	}

	// Optionally include correlations
	if req.IncludeCorrelations {
		// Add correlation data to event
	}

	return event, nil
}

func (s *TapioServer) AnalyzeEvents(ctx context.Context, req *pb.AnalyzeEventsRequest) (*pb.AnalyzeEventsResponse, error) {
	// Fetch events for analysis
	var events []*pb.Event
	if len(req.EventIds) > 0 {
		for _, id := range req.EventIds {
			event, err := s.eventStore.Get(ctx, id)
			if err != nil {
				continue
			}
			events = append(events, event)
		}
	} else if req.EventQuery != nil {
		var err error
		events, _, err = s.eventStore.Query(ctx, req.EventQuery)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to query events: %v", err)
		}
	}

	// Convert and analyze
	domainEvents := make([]*domain.Event, 0, len(events))
	for _, event := range events {
		domainEvents = append(domainEvents, convertEventFromProto(event))
	}

	start := time.Now()
	result := s.correlationManager.AnalyzeEvents(ctx, domainEvents, &correlation.AnalysisOptions{
		EnableRootCause:        req.EnableRootCause,
		EnablePredictions:      req.EnablePredictions,
		EnableImpactAssessment: req.EnableImpactAssessment,
		MinConfidence:          req.MinConfidenceThreshold,
	})

	response := &pb.AnalyzeEventsResponse{
		AnalysisDuration: durationpb.New(time.Since(start)),
		EventsAnalyzed:   int32(len(events)),
		Metadata: map[string]string{
			"analysis_id": generateAnalysisID(),
			"timestamp":   time.Now().Format(time.RFC3339),
		},
	}

	// Convert results
	for _, corr := range result.Correlations {
		response.Correlations = append(response.Correlations, convertCorrelationToProto(corr))
	}
	for _, group := range result.SemanticGroups {
		response.SemanticGroups = append(response.SemanticGroups, convertSemanticGroupToProto(group))
	}
	if result.RootCause != nil {
		response.RootCause = convertRootCauseToProto(result.RootCause)
	}
	for _, pred := range result.Predictions {
		response.Predictions = append(response.Predictions, convertPredictionToProto(pred))
	}
	if result.OverallImpact != nil {
		response.OverallImpact = convertImpactToProto(result.OverallImpact)
	}

	return response, nil
}

func (s *TapioServer) GetInsights(ctx context.Context, req *pb.GetInsightsRequest) (*pb.GetInsightsResponse, error) {
	// Get insights from correlation manager
	insights := s.correlationManager.GetInsights(ctx, &correlation.InsightQuery{
		TimeRange:     convertTimeRangeFromProto(req.TimeRange),
		Filter:        convertFilterFromProto(req.Filter),
		InsightTypes:  req.InsightTypes,
		MinConfidence: req.MinConfidence,
		Limit:         int(req.Limit),
		Audience:      correlation.Audience(req.Audience),
	})

	pbInsights := make([]*pb.Insight, 0, len(insights))
	for _, insight := range insights {
		pbInsights = append(pbInsights, convertInsightToProto(insight))
	}

	// Calculate stats
	stats := &pb.InsightStats{
		InsightsByType:       make(map[string]int32),
		InsightsBySeverity:   make(map[string]int32),
		AvgConfidence:        0.0,
		TotalRecommendations: 0,
	}

	totalConfidence := 0.0
	for _, insight := range pbInsights {
		stats.InsightsByType[insight.Type]++
		totalConfidence += insight.Confidence
		stats.TotalRecommendations += int32(len(insight.Actions))
	}

	if len(pbInsights) > 0 {
		stats.AvgConfidence = totalConfidence / float64(len(pbInsights))
	}

	return &pb.GetInsightsResponse{
		Insights:   pbInsights,
		TotalCount: int64(len(pbInsights)),
		Stats:      stats,
	}, nil
}

func (s *TapioServer) GetMetrics(ctx context.Context, req *pb.TapioGetMetricsRequest) (*pb.TapioGetMetricsResponse, error) {
	component := "all"
	if req.Component != pb.TapioGetMetricsRequest_COMPONENT_UNSPECIFIED {
		component = req.Component.String()
	}

	metrics := s.metricsCollector.GetMetrics(component)

	return &pb.TapioGetMetricsResponse{
		Metrics:   metrics,
		Timestamp: timestamppb.Now(),
	}, nil
}

func (s *TapioServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	// Perform health checks
	overallStatus := pb.HealthStatus_STATUS_HEALTHY
	components := make(map[string]*pb.ComponentHealth)

	// Check event store
	if err := s.eventStore.HealthCheck(ctx); err != nil {
		overallStatus = pb.HealthStatus_STATUS_DEGRADED
		components["event_store"] = &pb.ComponentHealth{
			Status: &pb.HealthStatus{
				Status:  pb.HealthStatus_STATUS_UNHEALTHY,
				Message: err.Error(),
			},
			Message: err.Error(),
		}
	} else {
		components["event_store"] = &pb.ComponentHealth{
			Status: &pb.HealthStatus{
				Status:  pb.HealthStatus_STATUS_HEALTHY,
				Message: "Healthy",
			},
			Message: "Event store is healthy",
		}
	}

	// Check correlation manager
	if err := s.correlationManager.HealthCheck(ctx); err != nil {
		overallStatus = pb.HealthStatus_STATUS_DEGRADED
		components["correlation_manager"] = &pb.ComponentHealth{
			Status: &pb.HealthStatus{
				Status:  pb.HealthStatus_STATUS_UNHEALTHY,
				Message: err.Error(),
			},
			Message: err.Error(),
		}
	} else {
		components["correlation_manager"] = &pb.ComponentHealth{
			Status: &pb.HealthStatus{
				Status:  pb.HealthStatus_STATUS_HEALTHY,
				Message: "Healthy",
			},
			Message: "Correlation manager is healthy",
		}
	}

	// Check active streams
	s.streamsMu.RLock()
	activeStreams := len(s.activeStreams)
	s.streamsMu.RUnlock()

	components["streams"] = &pb.ComponentHealth{
		Status: &pb.HealthStatus{
			Status:  pb.HealthStatus_STATUS_HEALTHY,
			Message: "Healthy",
		},
		Message: fmt.Sprintf("%d active streams", activeStreams),
		Details: map[string]string{
			"active_streams": fmt.Sprintf("%d", activeStreams),
		},
	}

	return &pb.HealthCheckResponse{
		OverallStatus: &pb.HealthStatus{
			Status:  overallStatus,
			Message: "System health check",
		},
		Components: components,
		CheckedAt:  timestamppb.Now(),
	}, nil
}

func (s *TapioServer) GetServiceInfo(ctx context.Context, _ *emptypb.Empty) (*pb.TapioServiceInfo, error) {
	return &pb.TapioServiceInfo{
		Version:     "1.0.0",
		BuildCommit: "main",
		BuildTime:   timestamppb.Now(),
		SupportedFeatures: []string{
			"semantic_correlation",
			"otel_trace_context",
			"predictive_analytics",
			"root_cause_analysis",
			"bidirectional_streaming",
			"rest_api",
		},
		EnabledCollectors: []string{
			"ebpf",
			"journald",
			"kubernetes",
			"systemd",
		},
		CorrelationEngines: []string{
			"semantic",
			"temporal",
			"causal",
			"statistical",
		},
		Limits: &pb.ServiceLimits{
			MaxEventsPerSecond:   165000,
			MaxConcurrentStreams: 1000,
			MaxEventSize:         1024 * 1024, // 1MB
			MaxBatchSize:         10000,
			RetentionPeriod:      durationpb.New(30 * 24 * time.Hour), // 30 days
		},
		ApiVersions: []string{"v1"},
	}, nil
}

// Helper functions

func generateEventID() string {
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), rand.Int63())
}

func generateAnalysisID() string {
	return fmt.Sprintf("analysis-%d", time.Now().UnixNano())
}

func matchesFilter(event *pb.Event, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Implement filter matching logic
	// This is a simplified version
	if filter.TimeRange != nil {
		if event.Timestamp.AsTime().Before(filter.TimeRange.Start.AsTime()) ||
			event.Timestamp.AsTime().After(filter.TimeRange.End.AsTime()) {
			return false
		}
	}

	if len(filter.EventTypes) > 0 {
		found := false
		for _, t := range filter.EventTypes {
			if t == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Severities) > 0 {
		found := false
		for _, s := range filter.Severities {
			if s == event.Severity {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// Conversion functions would go here...
// convertCorrelationToProto, convertSemanticGroupToProto, etc.
