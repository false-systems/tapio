package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/falseyair/tapio/pkg/correlation/types"
	"github.com/falseyair/tapio/pkg/domain"
	simpleTypes "github.com/falseyair/tapio/pkg/domain"
)

// Service provides correlation analysis for the CLI
type Service struct {
	engine       CorrelationEngine
	insightStore InsightStore
	timeline     *Timeline
}

// NewService creates a new correlation service
func NewService() (*Service, error) {
	// Create components
	insightStore := NewInMemoryInsightStore()

	// Create perfect engine with store
	baseEngine, err := NewPerfectEngine(DefaultPerfectConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create correlation engine: %w", err)
	}
	engineWithStore := NewPerfectEngineWithStore(baseEngine, insightStore)

	// Create timeline for analysis
	timeline := NewTimeline(10000) // Keep last 10k events

	return &Service{
		engine:       engineWithStore,
		insightStore: insightStore,
		timeline:     timeline,
	}, nil
}

// Start starts the correlation service
func (s *Service) Start(ctx context.Context) error {
	return s.engine.Start(ctx)
}

// Stop stops the correlation service
func (s *Service) Stop() error {
	return s.engine.Stop()
}

// AnalyzeCheckResult analyzes problems from a check result and returns insights
func (s *Service) AnalyzeCheckResult(ctx context.Context, result *simpleTypes.CheckResult) (*ServiceCorrelationResult, error) {
	// Convert problems to events for correlation
	events := s.convertProblemsToEvents(result.Problems)

	// Process events through correlation engine
	for _, event := range events {
		if err := s.engine.ProcessEvents(ctx, []*types.Event{event}); err != nil {
			return nil, fmt.Errorf("failed to process event: %w", err)
		}

		// Add to timeline for pattern analysis
		timelineEvent := s.convertToTimelineEvent(event)
		if err := s.timeline.AddEvent(timelineEvent); err != nil {
			// Log but don't fail
			fmt.Printf("Failed to add event to timeline: %v\n", err)
		}
	}

	// Get insights for the resources
	resourceInsights := make(map[string][]*domain.Insight)
	for _, problem := range result.Problems {
		insights := s.insightStore.GetInsights(problem.Resource.Name, problem.Resource.Namespace)
		if len(insights) > 0 {
			key := fmt.Sprintf("%s/%s", problem.Resource.Namespace, problem.Resource.Name)
			resourceInsights[key] = insights
		}
	}

	// Find patterns in timeline
	patterns := s.timeline.FindPatterns()

	// Build correlation result
	return &ServiceCorrelationResult{
		Insights:         s.insightStore.GetAllInsights(),
		ResourceInsights: resourceInsights,
		Patterns:         patterns,
		Timeline:         s.buildTimelineSummary(),
		Statistics:       s.timeline.GetStatistics(),
		EngineStats:      s.engine.GetStats(),
	}, nil
}

// convertProblemsToEvents converts check problems to correlation events
func (s *Service) convertProblemsToEvents(problems []simpleTypes.Problem) []*types.Event {
	events := make([]*types.Event, 0, len(problems))

	for _, problem := range problems {
		event := &types.Event{
			ID:        fmt.Sprintf("problem-%s-%d", problem.Resource.Name, time.Now().UnixNano()),
			Timestamp: time.Now(),
			Source:    "health-check",
			Type:      "problem-detected",
			Severity:  s.convertSeverity(problem.Severity),
			Entity: types.Entity{
				Type:      problem.Resource.Kind,
				Name:      problem.Resource.Name,
				Namespace: problem.Resource.Namespace,
				Container: "", // Not provided in problem
			},
			Data: map[string]interface{}{
				"title":       problem.Title,
				"description": problem.Description,
				"severity":    string(problem.Severity),
			},
		}

		// Add prediction if available
		if problem.Prediction != nil {
			event.Data["prediction"] = map[string]interface{}{
				"time_to_failure": problem.Prediction.TimeToFailure.String(),
				"confidence":      problem.Prediction.Confidence,
				"reason":          problem.Prediction.Reason,
			}
		}

		events = append(events, event)
	}

	return events
}

// convertToTimelineEvent converts a correlation event to timeline event
func (s *Service) convertToTimelineEvent(event *types.Event) TimelineEvent {
	return TimelineEvent{
		ID:        event.ID,
		Timestamp: event.Timestamp,
		Source:    SourceType(event.Source),
		EventType: event.Type,
		Severity:  string(event.Severity),
		Message:   fmt.Sprintf("%v", event.Data["description"]),
		Entity: EntityReference{
			Type:      event.Entity.Type,
			Name:      event.Entity.Name,
			Namespace: event.Entity.Namespace,
		},
		Metadata: event.Data,
	}
}

// convertSeverity converts simple severity to correlation severity
func (s *Service) convertSeverity(sev simpleTypes.Severity) types.Severity {
	switch sev {
	case simpleTypes.SeverityCritical:
		return types.SeverityCritical
	case simpleTypes.SeverityWarning:
		return types.SeverityMedium
	default:
		return types.SeverityLow
	}
}

// buildTimelineSummary builds a summary of timeline events
func (s *Service) buildTimelineSummary() *TimelineSummary {
	stats := s.timeline.GetStatistics()

	return &TimelineSummary{
		TotalEvents:      stats.TotalEvents,
		TimeRange:        stats.TimeRange,
		EventsBySeverity: stats.EventsBySeverity,
		EventsByType:     stats.EventsByType,
		EventsBySource:   stats.EventsBySource,
	}
}

// ServiceCorrelationResult contains the results of correlation analysis from the service
type ServiceCorrelationResult struct {
	Insights         []*domain.Insight            `json:"insights"`
	ResourceInsights map[string][]*domain.Insight `json:"resource_insights"`
	Patterns         []EventPattern        `json:"patterns"`
	Timeline         *TimelineSummary      `json:"timeline"`
	Statistics       TimelineStatistics    `json:"statistics"`
	EngineStats      *EngineStats          `json:"engine_stats"`
}

// TimelineSummary provides a summary of timeline data
type TimelineSummary struct {
	TotalEvents      int            `json:"total_events"`
	TimeRange        TimeRange      `json:"time_range"`
	EventsBySeverity map[string]int `json:"events_by_severity"`
	EventsByType     map[string]int `json:"events_by_type"`
	EventsBySource   map[string]int `json:"events_by_source"`
}

// GetMostCriticalInsights returns the most critical insights
func (r *ServiceCorrelationResult) GetMostCriticalInsights(limit int) []*domain.Insight {
	// Sort insights by severity and return top ones
	critical := make([]*domain.Insight, 0)
	high := make([]*domain.Insight, 0)
	medium := make([]*domain.Insight, 0)

	for _, insight := range r.Insights {
		switch insight.Severity {
		case "critical":
			critical = append(critical, insight)
		case "high":
			high = append(high, insight)
		case "medium":
			medium = append(medium, insight)
		}
	}

	// Combine in priority order
	result := append(critical, high...)
	result = append(result, medium...)

	if len(result) > limit {
		result = result[:limit]
	}

	return result
}

// GetActionableRecommendations returns all actionable recommendations
func (r *ServiceCorrelationResult) GetActionableRecommendations() []domain.ActionItem {
	recommendations := make([]domain.ActionItem, 0)

	for _, insight := range r.Insights {
		recommendations = append(recommendations, insight.ActionableItems...)
	}

	return recommendations
}

// HasCriticalPatterns returns true if critical patterns were detected
func (r *ServiceCorrelationResult) HasCriticalPatterns() bool {
	for _, pattern := range r.Patterns {
		if pattern.Type == "cascade" || pattern.Type == "burst" {
			if pattern.Confidence > 0.7 {
				return true
			}
		}
	}
	return false
}
