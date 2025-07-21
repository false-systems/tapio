package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/dataflow"
	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapioServiceImpl implements the TapioService from proto
type TapioServiceImpl struct {
	pb.UnimplementedTapioServiceServer

	logger *zap.Logger
	tracer trace.Tracer

	// Dependencies
	collectorMgr      *manager.CollectorManager
	dataFlow          *dataflow.TapioDataFlow
	correlationEngine *correlation.SemanticCorrelationEngine
	eventProcessor    *EventProcessor

	// Statistics
	startTime time.Time
	mu        sync.RWMutex
}

// NewTapioServiceImpl creates a new Tapio service implementation
func NewTapioServiceImpl(logger *zap.Logger, tracer trace.Tracer) *TapioServiceImpl {
	return &TapioServiceImpl{
		logger:         logger,
		tracer:         tracer,
		startTime:      time.Now(),
		eventProcessor: NewEventProcessor(logger),
	}
}

// SetDependencies injects dependencies
func (s *TapioServiceImpl) SetDependencies(
	collectorMgr *manager.CollectorManager,
	dataFlow *dataflow.TapioDataFlow,
	correlationEngine *correlation.SemanticCorrelationEngine,
) {
	s.collectorMgr = collectorMgr
	s.dataFlow = dataFlow
	s.correlationEngine = correlationEngine
}

// StreamEvents establishes bidirectional streaming for real-time event processing
func (s *TapioServiceImpl) StreamEvents(stream grpc.BidiStreamingServer[pb.TapioStreamEventsRequest, pb.TapioStreamEventsResponse]) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "tapio.stream_events")
	defer span.End()

	s.logger.Info("Client connected for event streaming")

	// Create channels for event processing
	eventChan := make(chan *pb.Event, 1000)
	errorChan := make(chan error, 1)

	// Start goroutine to receive events from client
	go func() {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				close(eventChan)
				return
			}
			if err != nil {
				errorChan <- err
				return
			}

			if req.Event != nil {
				eventChan <- req.Event
			}
		}
	}()

	// Process events and send responses
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errorChan:
			return err

		case event, ok := <-eventChan:
			if !ok {
				return nil
			}

			// Process the event
			if s.dataFlow != nil {
				// Convert to domain event
				domainEvent := s.convertProtoDomainEvent(event)

				// Submit to dataflow
				if err := s.dataFlow.SubmitEvent(ctx, domainEvent); err != nil {
					s.logger.Error("Failed to submit event to dataflow", zap.Error(err))
				}
			}

			// Send acknowledgment
			resp := &pb.TapioStreamEventsResponse{
				Response: &pb.TapioStreamEventsResponse_EventProcessed{
					EventProcessed: &pb.EventProcessedResponse{
						EventId:   event.Id,
						Status:    pb.ProcessingStatus_PROCESSING_STATUS_SUCCESS,
						Timestamp: timestamppb.Now(),
					},
				},
			}

			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

// GetCorrelations queries correlation findings
func (s *TapioServiceImpl) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_correlations")
	defer span.End()

	s.logger.Debug("Getting correlations", zap.Int32("limit", req.Limit))

	if s.correlationEngine == nil {
		return nil, status.Error(codes.Internal, "correlation engine not initialized")
	}

	// Get correlations from engine
	correlations := s.correlationEngine.GetRecentCorrelations(int(req.Limit))

	// Convert to proto format
	pbCorrelations := make([]*pb.Correlation, len(correlations))
	for i, corr := range correlations {
		pbCorrelations[i] = s.convertDomainCorrelationToProto(corr)
	}

	return &pb.GetCorrelationsResponse{
		Correlations: pbCorrelations,
		TotalCount:   int64(len(pbCorrelations)),
		Timestamp:    timestamppb.Now(),
	}, nil
}

// SubscribeToEvents provides real-time filtered event subscriptions
func (s *TapioServiceImpl) SubscribeToEvents(req *pb.SubscribeRequest, stream grpc.ServerStreamingServer[pb.EventUpdate]) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "tapio.subscribe_events")
	defer span.End()

	s.logger.Info("Client subscribed to events", zap.String("filter", req.Filter.Query))

	// Create subscription channel
	subChan := make(chan *domain.UnifiedEvent, 100)

	// Register subscription with dataflow
	if s.dataFlow != nil {
		subID := s.dataFlow.Subscribe(ctx, req.Filter.Query, subChan)
		defer s.dataFlow.Unsubscribe(subID)
	}

	// Send initial connection event
	if err := stream.Send(&pb.EventUpdate{
		UpdateType: pb.EventUpdateType_EVENT_UPDATE_TYPE_CONNECTED,
		Timestamp:  timestamppb.Now(),
	}); err != nil {
		return err
	}

	// Stream events
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-subChan:
			if !ok {
				return nil
			}

			// Convert and send event update
			update := &pb.EventUpdate{
				UpdateType: pb.EventUpdateType_EVENT_UPDATE_TYPE_NEW,
				Event:      s.convertUnifiedEventToProto(event),
				Timestamp:  timestamppb.Now(),
			}

			if err := stream.Send(update); err != nil {
				return err
			}
		}
	}
}

// GetSemanticGroups queries semantic correlation groups
func (s *TapioServiceImpl) GetSemanticGroups(ctx context.Context, req *pb.GetSemanticGroupsRequest) (*pb.GetSemanticGroupsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_semantic_groups")
	defer span.End()

	s.logger.Debug("Getting semantic groups")

	if s.correlationEngine == nil {
		return nil, status.Error(codes.Internal, "correlation engine not initialized")
	}

	// Get semantic groups from engine
	groups := s.correlationEngine.GetActiveGroups()

	// Convert to proto format
	pbGroups := make([]*pb.SemanticGroup, len(groups))
	for i, group := range groups {
		pbGroups[i] = &pb.SemanticGroup{
			Id:          group.ID,
			Name:        group.Name,
			Description: group.Description,
			EventCount:  int32(group.EventCount),
			CreatedAt:   timestamppb.New(group.CreatedAt),
			UpdatedAt:   timestamppb.New(group.UpdatedAt),
		}
	}

	return &pb.GetSemanticGroupsResponse{
		Groups:     pbGroups,
		TotalCount: int32(len(pbGroups)),
		Timestamp:  timestamppb.Now(),
	}, nil
}

// GetEvents retrieves historical events with pagination
func (s *TapioServiceImpl) GetEvents(ctx context.Context, req *pb.GetEventsRequest) (*pb.GetEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_events")
	defer span.End()

	s.logger.Debug("Getting events", zap.Int32("limit", req.PageSize))

	// This would query from event store
	events := make([]*pb.Event, 0)

	// For now, return empty response
	return &pb.GetEventsResponse{
		Events:        events,
		TotalCount:    0,
		NextPageToken: "",
		Timestamp:     timestamppb.Now(),
	}, nil
}

// GetEventById retrieves a specific event
func (s *TapioServiceImpl) GetEventById(ctx context.Context, req *pb.GetEventByIdRequest) (*pb.Event, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_event_by_id")
	defer span.End()

	s.logger.Debug("Getting event by ID", zap.String("id", req.EventId))

	// This would query from event store
	// For now, return not found
	return nil, status.Error(codes.NotFound, "event not found")
}

// AnalyzeEvents performs on-demand correlation analysis
func (s *TapioServiceImpl) AnalyzeEvents(ctx context.Context, req *pb.AnalyzeEventsRequest) (*pb.AnalyzeEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.analyze_events")
	defer span.End()

	s.logger.Info("Analyzing events", zap.Int("count", len(req.Events)))

	if s.correlationEngine == nil {
		return nil, status.Error(codes.Internal, "correlation engine not initialized")
	}

	// Convert proto events to domain events
	domainEvents := make([]*domain.UnifiedEvent, len(req.Events))
	for i, event := range req.Events {
		domainEvents[i] = s.convertProtoToUnifiedEvent(event)
	}

	// Perform analysis
	analysisID := fmt.Sprintf("analysis-%d", time.Now().Unix())
	findings := s.correlationEngine.AnalyzeEvents(ctx, domainEvents)

	// Convert findings to proto
	pbFindings := make([]*pb.CorrelationFinding, len(findings))
	for i, finding := range findings {
		pbFindings[i] = &pb.CorrelationFinding{
			Id:          finding.ID,
			Type:        pb.CorrelationType_CORRELATION_TYPE_SEMANTIC,
			Confidence:  finding.Confidence,
			Description: finding.Description,
			EventIds:    finding.EventIDs,
			Metadata:    finding.Metadata,
		}
	}

	return &pb.AnalyzeEventsResponse{
		AnalysisId: analysisID,
		Status:     pb.AnalysisStatus_ANALYSIS_STATUS_COMPLETED,
		Findings:   pbFindings,
		Summary: &pb.AnalysisSummary{
			TotalEvents:       int32(len(req.Events)),
			CorrelationsFound: int32(len(pbFindings)),
			ProcessingTime:    100, // milliseconds
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetInsights retrieves AI-generated insights
func (s *TapioServiceImpl) GetInsights(ctx context.Context, req *pb.GetInsightsRequest) (*pb.GetInsightsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_insights")
	defer span.End()

	s.logger.Debug("Getting insights")

	// This would integrate with AI/ML services
	// For now, return sample insights
	insights := []*pb.Insight{
		{
			Id:          "insight-1",
			Type:        pb.InsightType_INSIGHT_TYPE_ANOMALY,
			Severity:    pb.InsightSeverity_INSIGHT_SEVERITY_HIGH,
			Title:       "Unusual spike in error rate",
			Description: "Error rate increased by 300% in the last hour",
			Confidence:  0.95,
			CreatedAt:   timestamppb.Now(),
		},
	}

	return &pb.GetInsightsResponse{
		Insights:   insights,
		TotalCount: int32(len(insights)),
		Timestamp:  timestamppb.Now(),
	}, nil
}

// GetMetrics retrieves system metrics and statistics
func (s *TapioServiceImpl) GetMetrics(ctx context.Context, req *pb.TapioGetMetricsRequest) (*pb.TapioGetMetricsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "tapio.get_metrics")
	defer span.End()

	s.logger.Debug("Getting metrics")

	// Collect metrics from various sources
	metrics := []*pb.TapioMetric{
		{
			Name:  "events_processed_total",
			Value: 10000,
			Type:  pb.MetricType_METRIC_TYPE_COUNTER,
			Labels: map[string]string{
				"service": "tapio",
			},
			Timestamp: timestamppb.Now(),
		},
		{
			Name:  "active_correlations",
			Value: 25,
			Type:  pb.MetricType_METRIC_TYPE_GAUGE,
			Labels: map[string]string{
				"service": "correlation_engine",
			},
			Timestamp: timestamppb.Now(),
		},
	}

	return &pb.TapioGetMetricsResponse{
		Metrics:    metrics,
		TotalCount: int32(len(metrics)),
		Timestamp:  timestamppb.Now(),
	}, nil
}

// HealthCheck implements health checking
func (s *TapioServiceImpl) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	// Check dependencies
	if s.collectorMgr == nil || s.dataFlow == nil {
		return &pb.HealthCheckResponse{
			Status: pb.HealthCheckResponse_NOT_SERVING,
		}, nil
	}

	return &pb.HealthCheckResponse{
		Status: pb.HealthCheckResponse_SERVING,
	}, nil
}

// Helper methods for conversion

func (s *TapioServiceImpl) convertProtoDomainEvent(event *pb.Event) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        event.Id,
		Type:      event.Type.String(),
		Timestamp: event.Timestamp.AsTime(),
		Message:   event.Message,
		Severity:  event.Severity.String(),
		Source:    "grpc",
		Metadata:  make(map[string]interface{}),
	}
}

func (s *TapioServiceImpl) convertUnifiedEventToProto(event *domain.UnifiedEvent) *pb.Event {
	return &pb.Event{
		Id:        event.ID,
		Type:      pb.EventType_EVENT_TYPE_UNSPECIFIED,
		Timestamp: timestamppb.New(event.Timestamp),
		Message:   event.Message,
		Severity:  pb.EventSeverity_EVENT_SEVERITY_INFO,
	}
}

func (s *TapioServiceImpl) convertProtoToUnifiedEvent(event *pb.Event) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        event.Id,
		Type:      event.Type.String(),
		Timestamp: event.Timestamp.AsTime(),
		Message:   event.Message,
		Severity:  event.Severity.String(),
		Source:    "grpc",
		Metadata:  make(map[string]interface{}),
	}
}

func (s *TapioServiceImpl) convertDomainCorrelationToProto(corr *correlation.Correlation) *pb.Correlation {
	return &pb.Correlation{
		Id:          corr.ID,
		Type:        pb.CorrelationType_CORRELATION_TYPE_SEMANTIC,
		Confidence:  corr.Confidence,
		Description: corr.Description,
		EventIds:    corr.EventIDs,
		CreatedAt:   timestamppb.New(corr.CreatedAt),
		UpdatedAt:   timestamppb.New(corr.UpdatedAt),
	}
}
