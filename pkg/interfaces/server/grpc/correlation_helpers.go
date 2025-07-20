package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper methods for CorrelationServer

// validateAnalyzeEventsRequest validates the analyze events request
func (s *CorrelationServer) validateAnalyzeEventsRequest(req *pb.AnalyzeEventsRequest) error {
	if len(req.EventIds) == 0 && req.EventQuery == nil {
		return fmt.Errorf("either event_ids or event_query must be provided")
	}

	if len(req.EventIds) > s.config.MaxEventsPerAnalysis {
		return fmt.Errorf("too many event IDs provided: %d > %d", len(req.EventIds), s.config.MaxEventsPerAnalysis)
	}

	return nil
}

// validateSubscriptionRequest validates subscription requests
func (s *CorrelationServer) validateSubscriptionRequest(req *pb.SubscribeToCorrelationsRequest) error {
	if req.SubscriptionId == "" {
		return fmt.Errorf("subscription_id is required")
	}

	// Check if subscription already exists
	s.mu.RLock()
	_, exists := s.subscriptions[req.SubscriptionId]
	s.mu.RUnlock()
	if exists {
		return fmt.Errorf("subscription with ID %s already exists", req.SubscriptionId)
	}

	// Check subscription limits
	s.mu.RLock()
	activeCount := len(s.subscriptions)
	s.mu.RUnlock()
	if activeCount >= s.config.MaxSubscriptions {
		return fmt.Errorf("maximum number of subscriptions (%d) reached", s.config.MaxSubscriptions)
	}

	return nil
}

// getCorrelationByID retrieves a correlation by ID
func (s *CorrelationServer) getCorrelationByID(ctx context.Context, correlationID string) *pb.Correlation {
	// In a real implementation, this would query a correlation store
	// For now, return a mock correlation
	return &pb.Correlation{
		Id:          correlationID,
		Type:        pb.CorrelationType_CORRELATION_TYPE_SEMANTIC,
		Title:       "Mock Correlation",
		Description: fmt.Sprintf("Mock correlation for ID: %s", correlationID),
		Score:       0.85,
		Confidence:  0.80,
		EventIds:    []string{},
		CreatedAt:   timestamppb.Now(),
		UpdatedAt:   timestamppb.Now(),
	}
}

// getSemanticGroupByID retrieves a semantic group by ID
func (s *CorrelationServer) getSemanticGroupByID(ctx context.Context, groupID string) *pb.SemanticGroup {
	// In a real implementation, this would query a semantic group store
	// For now, return a mock semantic group
	return &pb.SemanticGroup{
		Id:              groupID,
		Name:            "Mock Semantic Group",
		Description:     fmt.Sprintf("Mock semantic group for ID: %s", groupID),
		SemanticType:    "deployment",
		Intent:          "Application deployment sequence",
		ConfidenceScore: 0.75,
		EventIds:        []string{},
		StartTime:       timestamppb.Now(),
		EndTime:         timestamppb.Now(),
		Duration:        durationpb.New(5 * time.Minute),
	}
}

// convertProtoFilterToDomainFilter converts proto Filter to corrDomain.Filter
func (s *CorrelationServer) convertProtoFilterToDomainFilter(protoFilter *pb.Filter) *corrDomain.Filter {
	if protoFilter == nil {
		return &corrDomain.Filter{}
	}

	filter := &corrDomain.Filter{
		MinConfidence: s.config.MinConfidenceThreshold,
	}

	// Time range
	if protoFilter.TimeRange != nil {
		filter.TimeRange = &corrDomain.TimeRange{
			Start: protoFilter.TimeRange.Start.AsTime(),
			End:   protoFilter.TimeRange.End.AsTime(),
		}
	}

	// Limit
	if protoFilter.Limit > 0 {
		filter.Limit = int(protoFilter.Limit)
	}

	return filter
}

// convertEventQueryToFilter converts EventQuery to domain.Filter
func (s *CorrelationServer) convertEventQueryToFilter(query *pb.EventQuery) (*domain.Filter, error) {
	if query == nil {
		return &domain.Filter{}, nil
	}

	filter := &domain.Filter{}

	// Convert the query's filter
	if query.Filter != nil {
		if query.Filter.TimeRange != nil {
			filter.Since = query.Filter.TimeRange.Start.AsTime()
			filter.Until = query.Filter.TimeRange.End.AsTime()
		}
		if query.Filter.Limit > 0 {
			filter.Limit = int(query.Filter.Limit)
		}
	}

	return filter, nil
}

// passesCorrelationFilters checks if a correlation passes the query filters
func (s *CorrelationServer) passesCorrelationFilters(correlation *corrDomain.Correlation, query *pb.CorrelationQuery) bool {
	// Check confidence threshold
	if query.MinConfidence > 0 && correlation.Confidence < query.MinConfidence {
		return false
	}

	// Check correlation score threshold
	if query.MinCorrelationScore > 0 && correlation.Score < query.MinCorrelationScore {
		return false
	}

	// Check correlation types
	if len(query.CorrelationTypes) > 0 {
		typeMatch := false
		corrType := s.convertCorrelationTypeToProto(correlation.Type)
		for _, queryType := range query.CorrelationTypes {
			if corrType == queryType {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}

	return true
}

// enrichCorrelationsWithEvents adds event data to correlations
func (s *CorrelationServer) enrichCorrelationsWithEvents(ctx context.Context, correlations []*pb.Correlation) {
	for _, correlation := range correlations {
		if len(correlation.EventIds) > 0 {
			events, err := s.eventStore.Get(ctx, correlation.EventIds)
			if err != nil {
				s.logger.Warn("Failed to enrich correlation with events",
					zap.String("correlation_id", correlation.Id),
					zap.Error(err),
				)
				continue
			}

			// Convert domain events to proto events and add to correlation
			correlation.Events = make([]*pb.Event, len(events))
			for i, event := range events {
				correlation.Events[i] = s.convertDomainEventToProto(event)
			}
		}
	}
}

// enrichCorrelationsWithSemanticGroups adds semantic group data to correlations
func (s *CorrelationServer) enrichCorrelationsWithSemanticGroups(ctx context.Context, correlations []*pb.Correlation) {
	for _, correlation := range correlations {
		if len(correlation.SemanticGroupIds) > 0 {
			// In a real implementation, this would fetch semantic groups from storage
			// For now, just log that enrichment was requested
			s.logger.Debug("Enriching correlation with semantic groups",
				zap.String("correlation_id", correlation.Id),
				zap.Strings("group_ids", correlation.SemanticGroupIds),
			)
		}
	}
}

// enrichCorrelationsWithRootCause adds root cause analysis to correlations
func (s *CorrelationServer) enrichCorrelationsWithRootCause(ctx context.Context, correlations []*pb.Correlation) {
	for _, correlation := range correlations {
		if correlation.RootCause == nil && s.config.EnableRootCauseAnalysis {
			// Generate mock root cause analysis
			correlation.RootCause = &pb.RootCauseAnalysis{
				PrimaryFactor: &pb.CausalFactor{
					Id:                 fmt.Sprintf("factor_%s", correlation.Id),
					Description:        "Primary root cause factor",
					ContributionWeight: 0.8,
					Category:           "configuration",
				},
				ContributingFactors: []*pb.CausalFactor{
					{
						Id:                 fmt.Sprintf("factor2_%s", correlation.Id),
						Description:        "Secondary contributing factor",
						ContributionWeight: 0.2,
						Category:           "performance",
					},
				},
				Confidence: correlation.Confidence,
				Evidence: []*pb.Evidence{
					{
						Type:           "log_pattern",
						Description:    "Error pattern detected in logs",
						Data:           map[string]string{"pattern": "connection_timeout"},
						RelevanceScore: 0.9,
					},
				},
			}
		}
	}
}

// enrichCorrelationsWithPredictions adds prediction data to correlations
func (s *CorrelationServer) enrichCorrelationsWithPredictions(ctx context.Context, correlations []*pb.Correlation) {
	for _, correlation := range correlations {
		if correlation.Prediction == nil && s.config.EnablePredictions {
			// Generate mock prediction
			correlation.Prediction = &pb.PredictedOutcome{
				Scenario:      "Service degradation likely to continue",
				Probability:   0.75,
				TimeToOutcome: durationpb.New(15 * time.Minute),
				Confidence:    0.70,
				PreventionActions: []string{
					"Restart affected service",
					"Check resource allocation",
					"Review recent configuration changes",
				},
				PredictedEvents: []*pb.PredictedEvent{
					{
						EventType:   pb.EventType_EVENT_TYPE_SERVICE,
						Probability: 0.8,
						TimeWindow:  durationpb.New(10 * time.Minute),
						Description: "Service restart event predicted",
					},
				},
			}
		}
	}
}

// enrichSemanticGroupsWithEvents adds event data to semantic groups
func (s *CorrelationServer) enrichSemanticGroupsWithEvents(ctx context.Context, groups []*pb.SemanticGroup) {
	for _, group := range groups {
		if len(group.EventIds) > 0 {
			events, err := s.eventStore.Get(ctx, group.EventIds)
			if err != nil {
				s.logger.Warn("Failed to enrich semantic group with events",
					zap.String("group_id", group.Id),
					zap.Error(err),
				)
				continue
			}

			// Convert domain events to proto events and add to group
			group.Events = make([]*pb.Event, len(events))
			for i, event := range events {
				group.Events[i] = s.convertDomainEventToProto(event)
			}
		}
	}
}

// enrichSemanticGroupsWithAnalysis adds analysis data to semantic groups
func (s *CorrelationServer) enrichSemanticGroupsWithAnalysis(ctx context.Context, groups []*pb.SemanticGroup) {
	for _, group := range groups {
		// Add impact assessment if missing
		if group.Impact == nil && s.config.EnableImpactAssessment {
			group.Impact = &pb.ImpactAssessment{
				Level:                   pb.ImpactAssessment_IMPACT_LEVEL_MEDIUM,
				BusinessImpactScore:     0.6,
				TechnicalImpactScore:    0.7,
				AffectedServices:        []string{"web-service", "api-gateway"},
				EstimatedDuration:       durationpb.New(30 * time.Minute),
				AffectedUsers:           1000,
				AffectedRequests:        5000,
				EstimatedCost:           2500.0,
				Currency:                "USD",
				CascadeProbability:      0.3,
				PotentialCascadeTargets: []string{"database", "cache-service"},
			}
		}

		// Add prediction if missing
		if group.Prediction == nil && s.config.EnablePredictions {
			group.Prediction = &pb.PredictedOutcome{
				Scenario:      "Issue resolution expected",
				Probability:   0.85,
				TimeToOutcome: durationpb.New(20 * time.Minute),
				Confidence:    0.75,
				PreventionActions: []string{
					"Monitor resource usage",
					"Check service dependencies",
				},
			}
		}

		// Add root cause analysis if missing
		if group.RootCause == nil && s.config.EnableRootCauseAnalysis {
			group.RootCause = &pb.RootCauseAnalysis{
				PrimaryFactor: &pb.CausalFactor{
					Id:                 fmt.Sprintf("root_cause_%s", group.Id),
					Description:        "Configuration change triggered cascade",
					ContributionWeight: 0.9,
					Category:           "configuration",
				},
				Confidence: group.ConfidenceScore,
			}
		}
	}
}

// notifyCorrelationSubscribers sends updates to active subscribers
func (s *CorrelationServer) notifyCorrelationSubscribers(result *corrDomain.AnalysisResult) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create correlation updates for new correlations
	for _, correlation := range result.Correlations {
		protoCorr := s.convertCorrelationToProto(correlation)
		update := &pb.CorrelationUpdate{
			Type:        pb.CorrelationUpdate_UPDATE_TYPE_NEW_CORRELATION,
			Correlation: protoCorr,
			Timestamp:   timestamppb.Now(),
			AnalysisInfo: &pb.AnalysisInfo{
				AnalysisId:      fmt.Sprintf("realtime_%d", time.Now().UnixNano()),
				TriggerType:     "real_time_analysis",
				ConfidenceScore: correlation.Confidence,
			},
		}

		// Send to matching subscribers
		for subID, subscription := range s.subscriptions {
			if s.correlationMatchesSubscription(correlation, subscription) {
				if updateChan, exists := s.subscribers[subID]; exists {
					select {
					case updateChan <- update:
						// Successfully sent
					default:
						// Channel full, log warning
						s.logger.Warn("Correlation subscription channel full, dropping update",
							zap.String("subscription_id", subID),
							zap.String("correlation_id", correlation.ID),
						)
					}
				}
			}
		}
	}

	// Create semantic group updates
	for _, group := range result.SemanticGroups {
		protoGroup := s.convertSemanticGroupToProto(group)
		update := &pb.CorrelationUpdate{
			Type:          pb.CorrelationUpdate_UPDATE_TYPE_NEW_SEMANTIC_GROUP,
			SemanticGroup: protoGroup,
			Timestamp:     timestamppb.Now(),
		}

		// Send to all subscribers (semantic groups are generally of interest)
		for subID := range s.subscribers {
			if updateChan, exists := s.subscribers[subID]; exists {
				select {
				case updateChan <- update:
					// Successfully sent
				default:
					s.logger.Warn("Semantic group subscription channel full, dropping update",
						zap.String("subscription_id", subID),
						zap.String("group_id", group.ID),
					)
				}
			}
		}
	}
}

// correlationMatchesSubscription checks if a correlation matches subscription criteria
func (s *CorrelationServer) correlationMatchesSubscription(correlation *corrDomain.Correlation, subscription *CorrelationSubscription) bool {
	// Check confidence threshold
	if correlation.Confidence < subscription.MinConfidence {
		return false
	}

	// Check correlation types
	if len(subscription.CorrelationTypes) > 0 {
		corrType := s.convertCorrelationTypeToProto(correlation.Type)
		typeMatch := false
		for _, subType := range subscription.CorrelationTypes {
			if corrType == subType {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}

	// Additional filter checks could be added here

	return true
}

// generateRecommendedActions creates actionable recommendations for a correlation
func (s *CorrelationServer) generateRecommendedActions(correlation *pb.Correlation) []*pb.RecommendedAction {
	var actions []*pb.RecommendedAction

	// Generate actions based on correlation type and severity
	switch correlation.Type {
	case pb.CorrelationType_CORRELATION_TYPE_SEMANTIC:
		actions = append(actions, &pb.RecommendedAction{
			Id:          fmt.Sprintf("action_investigate_%s", correlation.Id),
			Title:       "Investigate Semantic Pattern",
			Description: "Analyze the semantic relationship between correlated events",
			Type:        pb.RecommendedAction_ACTION_TYPE_INVESTIGATE,
			Priority:    pb.RecommendedAction_PRIORITY_MEDIUM,
			Commands:    []string{"kubectl logs", "kubectl describe"},
			Parameters: map[string]string{
				"correlation_id": correlation.Id,
				"confidence":     fmt.Sprintf("%.2f", correlation.Confidence),
			},
			ExpectedResult:    "Better understanding of event relationships",
			EstimatedDuration: durationpb.New(10 * time.Minute),
			RiskLevel:         "low",
			RiskDescription:   "Read-only investigation poses minimal risk",
		})

	case pb.CorrelationType_CORRELATION_TYPE_CAUSAL:
		actions = append(actions, &pb.RecommendedAction{
			Id:          fmt.Sprintf("action_mitigate_%s", correlation.Id),
			Title:       "Address Root Cause",
			Description: "Take action to address the identified causal relationship",
			Type:        pb.RecommendedAction_ACTION_TYPE_MITIGATE,
			Priority:    pb.RecommendedAction_PRIORITY_HIGH,
			Commands:    []string{"kubectl scale", "kubectl restart"},
			Parameters: map[string]string{
				"correlation_id": correlation.Id,
				"severity":       "high",
			},
			ExpectedResult:    "Resolution of underlying cause",
			EstimatedDuration: durationpb.New(20 * time.Minute),
			RiskLevel:         "medium",
			RiskDescription:   "Service restart may cause brief downtime",
		})

	case pb.CorrelationType_CORRELATION_TYPE_TEMPORAL:
		actions = append(actions, &pb.RecommendedAction{
			Id:          fmt.Sprintf("action_monitor_%s", correlation.Id),
			Title:       "Monitor Temporal Pattern",
			Description: "Set up monitoring for the identified temporal correlation",
			Type:        pb.RecommendedAction_ACTION_TYPE_PREVENT,
			Priority:    pb.RecommendedAction_PRIORITY_MEDIUM,
			Commands:    []string{"kubectl create", "kubectl apply"},
			Parameters: map[string]string{
				"correlation_id": correlation.Id,
				"pattern_type":   "temporal",
			},
			ExpectedResult:    "Early detection of similar patterns",
			EstimatedDuration: durationpb.New(15 * time.Minute),
			RiskLevel:         "low",
			RiskDescription:   "Monitoring setup poses minimal risk",
		})
	}

	// Add escalation action for high-confidence correlations
	if correlation.Confidence > 0.9 {
		actions = append(actions, &pb.RecommendedAction{
			Id:          fmt.Sprintf("action_escalate_%s", correlation.Id),
			Title:       "Escalate to Operations Team",
			Description: "High-confidence correlation requires immediate attention",
			Type:        pb.RecommendedAction_ACTION_TYPE_ESCALATE,
			Priority:    pb.RecommendedAction_PRIORITY_CRITICAL,
			Commands:    []string{"notify", "alert"},
			Parameters: map[string]string{
				"correlation_id": correlation.Id,
				"confidence":     fmt.Sprintf("%.2f", correlation.Confidence),
				"urgency":        "high",
			},
			ExpectedResult:    "Operations team engagement",
			EstimatedDuration: durationpb.New(5 * time.Minute),
			RiskLevel:         "low",
			RiskDescription:   "Notification poses no operational risk",
		})
	}

	return actions
}

// deduplicateAndPrioritizeActions removes duplicates and sorts by priority
func (s *CorrelationServer) deduplicateAndPrioritizeActions(actions []*pb.RecommendedAction) []*pb.RecommendedAction {
	// Use a map to deduplicate by title
	actionMap := make(map[string]*pb.RecommendedAction)
	for _, action := range actions {
		key := strings.ToLower(action.Title)
		if existing, exists := actionMap[key]; exists {
			// Keep the higher priority action
			if action.Priority > existing.Priority {
				actionMap[key] = action
			}
		} else {
			actionMap[key] = action
		}
	}

	// Convert back to slice and sort by priority
	var result []*pb.RecommendedAction
	for _, action := range actionMap {
		result = append(result, action)
	}

	// Sort by priority (highest first)
	// Critical = 4, High = 3, Medium = 2, Low = 1
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Priority < result[j].Priority {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// Type conversion methods

// convertCorrelationToProto converts corrDomain.Correlation to pb.Correlation
func (s *CorrelationServer) convertCorrelationToProto(correlation *corrDomain.Correlation) *pb.Correlation {
	return &pb.Correlation{
		Id:          correlation.ID,
		Type:        s.convertCorrelationTypeToProto(correlation.Type),
		Title:       correlation.Title,
		Description: correlation.Description,
		Score:       correlation.Score,
		Confidence:  correlation.Confidence,
		EventIds:    correlation.EventIDs,
		CreatedAt:   timestamppb.New(correlation.DiscoveredAt),
		UpdatedAt:   timestamppb.New(time.Now()),
		Metadata:    correlation.Metadata,
	}
}

// convertSemanticGroupToProto converts corrDomain.SemanticGroup to pb.SemanticGroup
func (s *CorrelationServer) convertSemanticGroupToProto(group *corrDomain.SemanticGroup) *pb.SemanticGroup {
	return &pb.SemanticGroup{
		Id:              group.ID,
		Name:            group.Name,
		Description:     group.Description,
		SemanticType:    group.SemanticType,
		Intent:          group.Intent,
		ConfidenceScore: group.Confidence,
		EventIds:        group.EventIDs,
		StartTime:       timestamppb.New(group.StartTime),
		EndTime:         timestamppb.New(group.EndTime),
		Duration:        durationpb.New(group.Duration),
		TraceId:         group.TraceID,
		SpanIds:         group.SpanIDs,
		Metadata:        group.Metadata,
		Labels:          group.Labels,
	}
}

// convertPredictedOutcomeToProto converts corrDomain.PredictedOutcome to pb.PredictedOutcome
func (s *CorrelationServer) convertPredictedOutcomeToProto(prediction *corrDomain.PredictedOutcome) *pb.PredictedOutcome {
	// Mock conversion since the exact structure might differ
	return &pb.PredictedOutcome{
		Scenario:          "Predicted scenario",
		Probability:       0.75,
		TimeToOutcome:     durationpb.New(15 * time.Minute),
		Confidence:        0.70,
		PreventionActions: []string{"Monitor system", "Check resources"},
	}
}

// convertRootCauseAnalysisToProto converts corrDomain.RootCauseAnalysis to pb.RootCauseAnalysis
func (s *CorrelationServer) convertRootCauseAnalysisToProto(rootCause *corrDomain.RootCauseAnalysis) *pb.RootCauseAnalysis {
	// Mock conversion since the exact structure might differ
	return &pb.RootCauseAnalysis{
		PrimaryFactor: &pb.CausalFactor{
			Id:                 "primary_factor",
			Description:        "Primary root cause identified",
			ContributionWeight: 0.85,
			Category:           "system",
		},
		Confidence: 0.80,
	}
}

// convertImpactAssessmentToProto converts corrDomain.ImpactAssessment to pb.ImpactAssessment
func (s *CorrelationServer) convertImpactAssessmentToProto(impact *corrDomain.ImpactAssessment) *pb.ImpactAssessment {
	return &pb.ImpactAssessment{
		Level:                pb.ImpactAssessment_IMPACT_LEVEL_MEDIUM,
		BusinessImpactScore:  float64(impact.BusinessImpact),
		TechnicalImpactScore: 0.7,
		AffectedServices:     []string{"service1", "service2"},
		EstimatedDuration:    durationpb.New(30 * time.Minute),
		AffectedUsers:        1000,
		EstimatedCost:        5000.0,
		Currency:             "USD",
	}
}

// convertCorrelationTypeToProto converts correlation type to proto enum
func (s *CorrelationServer) convertCorrelationTypeToProto(corrType corrDomain.CorrelationType) pb.CorrelationType {
	switch corrType {
	case corrDomain.CorrelationTypeTemporal:
		return pb.CorrelationType_CORRELATION_TYPE_TEMPORAL
	case corrDomain.CorrelationTypeCausal:
		return pb.CorrelationType_CORRELATION_TYPE_CAUSAL
	case corrDomain.CorrelationTypeSemantic:
		return pb.CorrelationType_CORRELATION_TYPE_SEMANTIC
	case corrDomain.CorrelationTypeStatistical:
		return pb.CorrelationType_CORRELATION_TYPE_STATISTICAL
	default:
		return pb.CorrelationType_CORRELATION_TYPE_UNSPECIFIED
	}
}

// convertDomainEventToProto converts domain.Event to pb.Event (reuse from event service)
func (s *CorrelationServer) convertDomainEventToProto(domainEvent domain.Event) *pb.Event {
	return &pb.Event{
		Id:         string(domainEvent.ID),
		Type:       s.convertEventType(domainEvent.Type),
		Severity:   s.convertEventSeverity(domainEvent.Severity),
		Source:     s.convertSourceType(domainEvent.Source),
		Message:    domainEvent.Message,
		Timestamp:  timestamppb.New(domainEvent.Timestamp),
		TraceId:    domainEvent.Context.TraceID,
		SpanId:     domainEvent.Context.SpanID,
		Tags:       domainEvent.Tags,
		Confidence: domainEvent.Confidence,
	}
}

// Helper conversion methods (reused from event service)
func (s *CorrelationServer) convertEventType(domainType domain.EventType) pb.EventType {
	switch domainType {
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSTEM
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_SERVICE
	case domain.EventTypeLog:
		return pb.EventType_EVENT_TYPE_LOG
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func (s *CorrelationServer) convertEventSeverity(domainSeverity domain.EventSeverity) pb.EventSeverity {
	switch domainSeverity {
	case domain.EventSeverityDebug:
		return pb.EventSeverity_EVENT_SEVERITY_DEBUG
	case domain.EventSeverityInfo:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case domain.EventSeverityWarning:
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case domain.EventSeverityError:
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case domain.EventSeverityCritical:
		return pb.EventSeverity_EVENT_SEVERITY_CRITICAL
	default:
		return pb.EventSeverity_EVENT_SEVERITY_UNSPECIFIED
	}
}

func (s *CorrelationServer) convertSourceType(domainSource domain.SourceType) pb.SourceType {
	switch domainSource {
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_SYSTEMD
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}
