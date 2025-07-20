package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// CorrelationServer implements the CorrelationService gRPC interface
type CorrelationServer struct {
	pb.UnimplementedCorrelationServiceServer

	// Core dependencies
	logger         *zap.Logger
	tracer         trace.Tracer
	correlationMgr *corrDomain.Manager
	eventStore     EventStore

	// Subscription management
	mu            sync.RWMutex
	subscriptions map[string]*CorrelationSubscription
	subscribers   map[string]chan *pb.CorrelationUpdate

	// Configuration
	config CorrelationServiceConfig

	// Statistics tracking
	stats CorrelationServiceStats
}

// CorrelationServiceConfig configures the correlation service
type CorrelationServiceConfig struct {
	MaxCorrelationsPerQuery    int
	MaxSemanticGroupsPerQuery  int
	MaxEventsPerAnalysis       int
	MaxSubscriptions           int
	SubscriptionBufferSize     int
	AnalysisWorkers            int
	EnableRealTimeAnalysis     bool
	EnableImpactAssessment     bool
	EnablePredictions          bool
	EnableRootCauseAnalysis    bool
	DefaultAnalysisTimeWindow  time.Duration
	MinConfidenceThreshold     float64
	AnalysisTimeout            time.Duration
	CorrelationRetentionPeriod time.Duration
}

// CorrelationServiceStats tracks service metrics
type CorrelationServiceStats struct {
	mu                    sync.RWMutex
	TotalCorrelations     int64
	TotalSemanticGroups   int64
	AnalysisRequests      int64
	ActiveSubscriptions   int32
	AvgAnalysisTime       time.Duration
	CorrelationsPerSecond float64
	SuccessfulAnalyses    int64
	FailedAnalyses        int64
	TotalAnalyses         int64
	PredictionsMade       int64
}

// CorrelationSubscription tracks active correlation subscriptions
type CorrelationSubscription struct {
	ID               string
	Filter           *pb.Filter
	CorrelationTypes []pb.CorrelationType
	MinConfidence    float64
	StartTime        time.Time
	LastActivity     time.Time
	UpdatesSent      int64
}

// NewCorrelationServer creates a new correlation server
func NewCorrelationServer(
	logger *zap.Logger,
	tracer trace.Tracer,
	eventStore EventStore,
) *CorrelationServer {
	config := CorrelationServiceConfig{
		MaxCorrelationsPerQuery:   10000,
		MaxSemanticGroupsPerQuery: 5000,
		MaxEventsPerAnalysis:      100000,
		MaxSubscriptions:          500,
		SubscriptionBufferSize:    5000,
		AnalysisWorkers:           8,
		EnableRealTimeAnalysis:    true,
		EnableImpactAssessment:    true,
		EnablePredictions:         true,
		EnableRootCauseAnalysis:   true,
		DefaultAnalysisTimeWindow: 30 * time.Minute,
		MinConfidenceThreshold:    0.7,
	}

	return &CorrelationServer{
		logger:         logger,
		tracer:         tracer,
		correlationMgr: corrDomain.NewManager(),
		eventStore:     eventStore,
		subscriptions:  make(map[string]*CorrelationSubscription),
		subscribers:    make(map[string]chan *pb.CorrelationUpdate),
		config:         config,
		stats:          CorrelationServiceStats{},
	}
}

// GetCorrelations retrieves correlations based on query
func (s *CorrelationServer) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "correlation.get_correlations")
	defer span.End()

	start := time.Now()
	defer func() {
		s.stats.mu.Lock()
		s.stats.AnalysisRequests++
		s.stats.AvgAnalysisTime = time.Since(start)
		s.stats.mu.Unlock()
	}()

	var correlations []*pb.Correlation

	// Handle specific correlation IDs
	if len(req.CorrelationIds) > 0 {
		correlations = make([]*pb.Correlation, 0, len(req.CorrelationIds))
		for _, corrID := range req.CorrelationIds {
			corr := s.getCorrelationByID(ctx, corrID)
			if corr != nil {
				correlations = append(correlations, corr)
			}
		}
	} else if req.Query != nil {
		// Query-based retrieval
		filter := s.convertProtoFilterToDomainFilter(req.Query.Filter)
		domainCorrelations := s.correlationMgr.GetCorrelations(ctx, filter)

		// Convert to proto correlations
		correlations = make([]*pb.Correlation, 0, len(domainCorrelations))
		for _, domainCorr := range domainCorrelations {
			if s.passesCorrelationFilters(domainCorr, req.Query) {
				protoCorr := s.convertCorrelationToProto(domainCorr)
				correlations = append(correlations, protoCorr)
			}
		}

		// Apply limit
		if len(correlations) > s.config.MaxCorrelationsPerQuery {
			correlations = correlations[:s.config.MaxCorrelationsPerQuery]
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "either correlation_ids or query must be provided")
	}

	// Enrich correlations if requested
	if req.Query != nil {
		if req.Query.IncludeEvents {
			s.enrichCorrelationsWithEvents(ctx, correlations)
		}
		if req.Query.IncludeSemanticGroups {
			s.enrichCorrelationsWithSemanticGroups(ctx, correlations)
		}
		if req.Query.IncludeRootCause {
			s.enrichCorrelationsWithRootCause(ctx, correlations)
		}
		if req.Query.IncludePredictions {
			s.enrichCorrelationsWithPredictions(ctx, correlations)
		}
	}

	metadata := map[string]string{
		"query_duration_ms": fmt.Sprintf("%.2f", time.Since(start).Seconds()*1000),
		"result_count":      fmt.Sprintf("%d", len(correlations)),
		"enrichment_applied": fmt.Sprintf("%t", req.Query != nil &&
			(req.Query.IncludeEvents || req.Query.IncludeSemanticGroups ||
				req.Query.IncludeRootCause || req.Query.IncludePredictions)),
	}

	s.stats.mu.Lock()
	s.stats.TotalCorrelations += int64(len(correlations))
	s.stats.SuccessfulAnalyses++
	s.stats.mu.Unlock()

	return &pb.GetCorrelationsResponse{
		Correlations:  correlations,
		TotalCount:    int64(len(correlations)),
		NextPageToken: "", // TODO: Implement pagination
		Metadata:      metadata,
	}, nil
}

// GetSemanticGroups retrieves semantic groups of related events
func (s *CorrelationServer) GetSemanticGroups(ctx context.Context, req *pb.GetSemanticGroupsRequest) (*pb.GetSemanticGroupsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "correlation.get_semantic_groups")
	defer span.End()

	start := time.Now()

	var semanticGroups []*pb.SemanticGroup

	// Handle specific group IDs
	if len(req.GroupIds) > 0 {
		semanticGroups = make([]*pb.SemanticGroup, 0, len(req.GroupIds))
		for _, groupID := range req.GroupIds {
			group := s.getSemanticGroupByID(ctx, groupID)
			if group != nil {
				semanticGroups = append(semanticGroups, group)
			}
		}
	} else if req.Filter != nil {
		// Query-based retrieval
		filter := s.convertProtoFilterToDomainFilter(req.Filter)
		domainGroups := s.correlationMgr.GetSemanticGroups(ctx, filter)

		// Convert to proto semantic groups
		semanticGroups = make([]*pb.SemanticGroup, 0, len(domainGroups))
		for _, domainGroup := range domainGroups {
			protoGroup := s.convertSemanticGroupToProto(domainGroup)
			semanticGroups = append(semanticGroups, protoGroup)
		}

		// Apply limit
		if len(semanticGroups) > s.config.MaxSemanticGroupsPerQuery {
			semanticGroups = semanticGroups[:s.config.MaxSemanticGroupsPerQuery]
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "either group_ids or filter must be provided")
	}

	// Enrich semantic groups if requested
	if req.IncludeEvents {
		s.enrichSemanticGroupsWithEvents(ctx, semanticGroups)
	}
	if req.IncludeAnalysis {
		s.enrichSemanticGroupsWithAnalysis(ctx, semanticGroups)
	}

	s.stats.mu.Lock()
	s.stats.TotalSemanticGroups += int64(len(semanticGroups))
	s.stats.mu.Unlock()

	s.logger.Debug("Retrieved semantic groups",
		zap.Int("count", len(semanticGroups)),
		zap.Duration("duration", time.Since(start)),
		zap.Bool("include_events", req.IncludeEvents),
		zap.Bool("include_analysis", req.IncludeAnalysis),
	)

	return &pb.GetSemanticGroupsResponse{
		Groups:        semanticGroups,
		TotalCount:    int64(len(semanticGroups)),
		NextPageToken: "", // TODO: Implement pagination
	}, nil
}

// AnalyzeEvents performs on-demand analysis of events to find correlations
func (s *CorrelationServer) AnalyzeEvents(ctx context.Context, req *pb.AnalyzeEventsRequest) (*pb.AnalyzeEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "correlation.analyze_events")
	defer span.End()

	start := time.Now()

	// Validate request
	if err := s.validateAnalyzeEventsRequest(req); err != nil {
		s.stats.mu.Lock()
		s.stats.FailedAnalyses++
		s.stats.mu.Unlock()
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Get events for analysis
	var events []*domain.Event

	if len(req.EventIds) > 0 {
		// Get specific events by ID
		domainEvents, err := s.eventStore.Get(ctx, req.EventIds)
		if err != nil {
			s.logger.Error("Failed to get events by IDs", zap.Error(err))
			s.stats.mu.Lock()
			s.stats.FailedAnalyses++
			s.stats.mu.Unlock()
			return nil, status.Error(codes.Internal, "failed to retrieve events")
		}

		events = make([]*domain.Event, len(domainEvents))
		for i, event := range domainEvents {
			eventCopy := event
			events[i] = &eventCopy
		}
	} else if req.EventQuery != nil {
		// Query events based on criteria
		filter, err := s.convertEventQueryToFilter(req.EventQuery)
		if err != nil {
			s.stats.mu.Lock()
			s.stats.FailedAnalyses++
			s.stats.mu.Unlock()
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid event query: %v", err))
		}

		domainEvents, err := s.eventStore.Query(ctx, *filter)
		if err != nil {
			s.logger.Error("Failed to query events", zap.Error(err))
			s.stats.mu.Lock()
			s.stats.FailedAnalyses++
			s.stats.mu.Unlock()
			return nil, status.Error(codes.Internal, "failed to query events")
		}

		events = make([]*domain.Event, len(domainEvents))
		for i, event := range domainEvents {
			eventCopy := event
			events[i] = &eventCopy
		}
	} else {
		s.stats.mu.Lock()
		s.stats.FailedAnalyses++
		s.stats.mu.Unlock()
		return nil, status.Error(codes.InvalidArgument, "either event_ids or event_query must be provided")
	}

	// Apply event limit
	if len(events) > s.config.MaxEventsPerAnalysis {
		events = events[:s.config.MaxEventsPerAnalysis]
		s.logger.Warn("Truncated events for analysis due to limit",
			zap.Int("limit", s.config.MaxEventsPerAnalysis),
			zap.Int("requested", len(events)),
		)
	}

	// Perform analysis
	analysisOptions := &corrDomain.AnalysisOptions{
		EnableRootCause:        req.EnableRootCause && s.config.EnableRootCauseAnalysis,
		EnablePredictions:      req.EnablePredictions && s.config.EnablePredictions,
		EnableImpactAssessment: req.EnableImpactAssessment && s.config.EnableImpactAssessment,
		MinConfidence:          s.config.MinConfidenceThreshold,
	}

	result := s.correlationMgr.AnalyzeEvents(ctx, events, analysisOptions)

	// Convert result to proto
	response := &pb.AnalyzeEventsResponse{
		Correlations:     make([]*pb.Correlation, len(result.Correlations)),
		SemanticGroups:   make([]*pb.SemanticGroup, len(result.SemanticGroups)),
		Predictions:      make([]*pb.PredictedOutcome, len(result.Predictions)),
		AnalysisDuration: durationpb.New(time.Since(start)),
		EventsAnalyzed:   int32(len(events)),
		Metadata: map[string]string{
			"analysis_id":               fmt.Sprintf("analysis_%d", time.Now().UnixNano()),
			"root_cause_enabled":        fmt.Sprintf("%t", req.EnableRootCause),
			"predictions_enabled":       fmt.Sprintf("%t", req.EnablePredictions),
			"impact_assessment_enabled": fmt.Sprintf("%t", req.EnableImpactAssessment),
			"min_confidence":            fmt.Sprintf("%.2f", s.config.MinConfidenceThreshold),
			"analysis_duration_ms":      fmt.Sprintf("%.2f", time.Since(start).Seconds()*1000),
		},
	}

	// Convert correlations
	for i, corr := range result.Correlations {
		response.Correlations[i] = s.convertCorrelationToProto(corr)
	}

	// Convert semantic groups
	for i, group := range result.SemanticGroups {
		response.SemanticGroups[i] = s.convertSemanticGroupToProto(group)
	}

	// Convert predictions
	for i, pred := range result.Predictions {
		response.Predictions[i] = s.convertPredictedOutcomeToProto(pred)
	}

	// Set root cause analysis if available
	if result.RootCause != nil {
		response.RootCause = s.convertRootCauseAnalysisToProto(result.RootCause)
	}

	// Set overall impact if available
	if result.OverallImpact != nil {
		response.OverallImpact = s.convertImpactAssessmentToProto(result.OverallImpact)
	}

	s.stats.mu.Lock()
	s.stats.AnalysisRequests++
	s.stats.SuccessfulAnalyses++
	s.stats.AvgAnalysisTime = time.Since(start)
	s.stats.mu.Unlock()

	// Notify subscribers if real-time analysis is enabled
	if s.config.EnableRealTimeAnalysis {
		s.notifyCorrelationSubscribers(result)
	}

	s.logger.Info("Event analysis completed",
		zap.String("analysis_id", response.Metadata["analysis_id"]),
		zap.Int("events_analyzed", len(events)),
		zap.Int("correlations_found", len(response.Correlations)),
		zap.Int("semantic_groups_found", len(response.SemanticGroups)),
		zap.Int("predictions_made", len(response.Predictions)),
		zap.Duration("analysis_time", time.Since(start)),
	)

	return response, nil
}

// SubscribeToCorrelations provides real-time correlation updates
func (s *CorrelationServer) SubscribeToCorrelations(req *pb.SubscribeToCorrelationsRequest, stream pb.CorrelationService_SubscribeToCorrelationsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "correlation.subscribe")
	defer span.End()

	// Validate subscription request
	if err := s.validateSubscriptionRequest(req); err != nil {
		return err
	}

	// Generate unique subscription ID
	subscriptionID := fmt.Sprintf("sub_%d_%d", time.Now().UnixNano(), stream.Context().Value("request-id"))

	// Create subscription
	subscription := &CorrelationSubscription{
		ID:               subscriptionID,
		Filter:           req.Filter,
		CorrelationTypes: req.CorrelationTypes,
		MinConfidence:    req.MinConfidence,
		StartTime:        time.Now(),
		LastActivity:     time.Now(),
	}

	// Register subscription
	s.mu.Lock()
	s.subscriptions[subscription.ID] = subscription
	updateChan := make(chan *pb.CorrelationUpdate, s.config.SubscriptionBufferSize)
	s.subscribers[subscription.ID] = updateChan
	s.mu.Unlock()

	// Update active subscriptions counter
	s.stats.mu.Lock()
	s.stats.ActiveSubscriptions++
	s.stats.mu.Unlock()

	defer func() {
		// Cleanup subscription
		s.mu.Lock()
		delete(s.subscriptions, subscription.ID)
		close(updateChan)
		delete(s.subscribers, subscription.ID)
		s.mu.Unlock()

		s.stats.mu.Lock()
		s.stats.ActiveSubscriptions--
		s.stats.mu.Unlock()

		s.logger.Info("Correlation subscription closed",
			zap.String("subscription_id", subscription.ID),
			zap.Int64("updates_sent", subscription.UpdatesSent),
		)
	}()

	s.logger.Info("Correlation subscription started",
		zap.String("subscription_id", subscription.ID),
		zap.Any("filter", req.Filter),
		zap.Float64("min_confidence", req.MinConfidence),
	)

	// Stream real-time updates
	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updateChan:
			if update == nil {
				return nil
			}

			// Send update
			if err := stream.Send(update); err != nil {
				s.logger.Error("Failed to send correlation update", zap.Error(err))
				return err
			}

			// Update subscription stats
			subscription.UpdatesSent++
			subscription.LastActivity = time.Now()
		}
	}
}

// GetRecommendedActions provides actionable recommendations for correlations
func (s *CorrelationServer) GetRecommendedActions(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetRecommendedActionsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "correlation.get_recommended_actions")
	defer span.End()

	// Get correlations first
	correlationsResp, err := s.GetCorrelations(ctx, req)
	if err != nil {
		return nil, err
	}

	var allActions []*pb.RecommendedAction
	var primaryCorrelationID string

	// Generate actions for each correlation
	for i, correlation := range correlationsResp.Correlations {
		if i == 0 {
			primaryCorrelationID = correlation.Id
		}

		actions := s.generateRecommendedActions(correlation)
		allActions = append(allActions, actions...)
	}

	// Deduplicate and prioritize actions
	finalActions := s.deduplicateAndPrioritizeActions(allActions)

	s.logger.Debug("Generated recommended actions",
		zap.String("correlation_id", primaryCorrelationID),
		zap.Int("correlations_count", len(correlationsResp.Correlations)),
		zap.Int("actions_generated", len(finalActions)),
	)

	return &pb.GetRecommendedActionsResponse{
		Actions:       finalActions,
		CorrelationId: primaryCorrelationID,
		Metadata: map[string]string{
			"correlations_analyzed": fmt.Sprintf("%d", len(correlationsResp.Correlations)),
			"actions_generated":     fmt.Sprintf("%d", len(finalActions)),
			"generation_time_ms":    fmt.Sprintf("%.2f", time.Since(time.Now()).Seconds()*1000),
		},
	}, nil
}
