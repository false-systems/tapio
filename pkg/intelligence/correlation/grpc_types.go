package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Manager wraps CollectionManager for gRPC compatibility
type Manager struct {
	*CollectionManager
}

// NewManager creates a new correlation manager
func NewManager() *Manager {
	logger, _ := zap.NewProduction()
	return &Manager{
		CollectionManager: NewCollectionManager(DefaultConfig(), logger, nil), // No K8s client in simple manager
	}
}

// AnalyzeEvent processes a single event and returns correlations
func (m *Manager) AnalyzeEvent(ctx context.Context, event *domain.Event) []*Correlation {
	// Process event and get insights
	insights := m.ProcessEvents([]domain.Event{*event})

	// Convert insights to correlations
	correlations := make([]*Correlation, 0, len(insights))
	for _, insight := range insights {
		correlations = append(correlations, insightToCorrelation(insight))
	}

	return correlations
}

// AnalyzeBatch processes multiple events and returns semantic groups
func (m *Manager) AnalyzeBatch(ctx context.Context, events []*domain.Event) []*SemanticGroup {
	// Convert pointer slice to value slice
	eventValues := make([]domain.Event, 0, len(events))
	for _, e := range events {
		if e != nil {
			eventValues = append(eventValues, *e)
		}
	}

	// Process through correlation system (semantic engine was removed)
	// For now, return empty groups since semantic engine is gone
	groups := []EventGroup{}

	// Convert to semantic groups
	semanticGroups := make([]*SemanticGroup, 0, len(groups))
	for _, group := range groups {
		semanticGroups = append(semanticGroups, eventGroupToSemanticGroup(group))
	}

	return semanticGroups
}

// GetCorrelations retrieves correlations based on filter
func (m *Manager) GetCorrelations(ctx context.Context, filter *Filter) []*Correlation {
	// This is a simplified implementation
	// In production, this would query a correlation store
	return []*Correlation{}
}

// GetSemanticGroups retrieves semantic groups based on filter
func (m *Manager) GetSemanticGroups(ctx context.Context, filter *Filter) []*SemanticGroup {
	// This is a simplified implementation
	// In production, this would query a semantic group store
	return []*SemanticGroup{}
}

// AnalyzeEvents performs comprehensive analysis
func (m *Manager) AnalyzeEvents(ctx context.Context, events []*domain.Event, opts *AnalysisOptions) *AnalysisResult {
	result := &AnalysisResult{
		Correlations:   make([]*Correlation, 0),
		SemanticGroups: make([]*SemanticGroup, 0),
		Predictions:    make([]*PredictedOutcome, 0),
	}

	// Run analysis
	semanticGroups := m.AnalyzeBatch(ctx, events)
	result.SemanticGroups = semanticGroups

	// Generate correlations from semantic groups
	for _, group := range semanticGroups {
		if group.Confidence >= opts.MinConfidence {
			corr := &Correlation{
				ID:          group.ID,
				Type:        CorrelationTypeSemantic,
				Title:       group.Name,
				Description: group.Description,
				Score:       group.Confidence,
				Confidence:  group.Confidence,
				EventIDs:    group.EventIDs,
			}
			result.Correlations = append(result.Correlations, corr)
		}
	}

	return result
}

// GetInsights retrieves AI-generated insights
func (m *Manager) GetInsights(ctx context.Context, query *InsightQuery) []*CorrelationInsight {
	// Get all available insights
	domainInsights := m.CollectionManager.GetInsights()

	// Convert and filter
	insights := make([]*CorrelationInsight, 0, len(domainInsights))
	for _, di := range domainInsights {
		insight := domainInsightToCorrelationInsight(di)
		// Extract confidence from metadata or use default
		confidence := 0.8 // Default confidence
		if conf, ok := di.Metadata["confidence"].(float64); ok {
			confidence = conf
		}
		if confidence >= query.MinConfidence {
			insights = append(insights, insight)
		}
		if len(insights) >= query.Limit {
			break
		}
	}

	return insights
}

// HealthCheck verifies the correlation manager is healthy
func (m *Manager) HealthCheck(ctx context.Context) error {
	// Check if semantic engine is running
	stats := m.Statistics()
	if _, ok := stats["semantic_engine_stats"]; !ok {
		return fmt.Errorf("semantic engine not running")
	}
	return nil
}

// Additional types for gRPC compatibility

type Correlation struct {
	ID                 string
	Type               CorrelationType
	Title              string
	Description        string
	Score              float64
	Confidence         float64
	EventIDs           []string
	SemanticGroupIDs   []string
	DiscoveredAt       time.Time
	TimeRange          *TimeRange
	Statistics         map[string]float64
	VisualizationType  string
	VisualizationData  map[string]interface{}
	RecommendedActions []RecommendedAction
	Metadata           map[string]string
}

type SemanticGroup struct {
	ID           string
	Name         string
	Description  string
	SemanticType string
	Intent       string
	Confidence   float64
	EventIDs     []string
	Events       []*domain.Event
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	TraceID      string
	SpanIDs      []string
	Impact       *ImpactAssessment
	Prediction   *PredictedOutcome
	RootCause    *RootCauseAnalysis
	Metadata     map[string]string
	Labels       map[string]string
}

// Type aliases for existing types in semantic_types.go and semantic_otel_tracer.go
// These are defined elsewhere in the package

type CausalFactor struct {
	ID                 string
	Description        string
	ContributionWeight float64
	Category           string
}

type Evidence struct {
	Type           string
	Description    string
	Data           map[string]interface{}
	RelevanceScore float64
}

type RecommendedAction struct {
	ID                string
	Title             string
	Description       string
	Type              ActionType
	Priority          ActionPriority
	Commands          []string
	Parameters        map[string]string
	ExpectedResult    string
	EstimatedDuration time.Duration
	RiskLevel         string
	RiskDescription   string
}

// Remove duplicate type - use CorrelationInsight from insight_types.go instead

type Filter struct {
	Query             string
	EventTypes        []EventType
	Severities        []EventSeverity
	Sources           []SourceType
	ResourceTypes     []string
	ResourceIDs       []string
	TraceIDs          []string
	CorrelationIDs    []string
	SemanticGroupIDs  []string
	Labels            map[string]string
	HasCorrelations   bool
	HasSemanticGroups bool
	MinConfidence     float64
	TimeRange         *TimeRange
	Limit             int
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

type AnalysisOptions struct {
	EnableRootCause        bool
	EnablePredictions      bool
	EnableImpactAssessment bool
	MinConfidence          float64
}

type AnalysisResult struct {
	Correlations   []*Correlation
	SemanticGroups []*SemanticGroup
	RootCause      *RootCauseAnalysis
	Predictions    []*PredictedOutcome
	OverallImpact  *ImpactAssessment
}

type InsightQuery struct {
	TimeRange     *TimeRange
	Filter        *Filter
	InsightTypes  []string
	MinConfidence float64
	Limit         int
	Audience      Audience
}

// Enums

type CorrelationType int

const (
	CorrelationTypeUnknown CorrelationType = iota
	CorrelationTypeTemporal
	CorrelationTypeCausal
	CorrelationTypeSemantic
	CorrelationTypeStatistical
)

type ImpactLevel int

const (
	ImpactLevelUnspecified ImpactLevel = iota
	ImpactLevelMinimal
	ImpactLevelLow
	ImpactLevelMedium
	ImpactLevelHigh
	ImpactLevelCritical
)

type ActionType int

const (
	ActionTypeUnspecified ActionType = iota
	ActionTypeInvestigate
	ActionTypeMitigate
	ActionTypePrevent
	ActionTypeEscalate
)

type ActionPriority int

const (
	ActionPriorityUnspecified ActionPriority = iota
	ActionPriorityLow
	ActionPriorityMedium
	ActionPriorityHigh
	ActionPriorityCritical
)

type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeSystem
	EventTypeApplication
	EventTypeNetwork
	EventTypeSecurity
	EventTypeCustom
)

type EventSeverity int

const (
	EventSeverityUnknown EventSeverity = iota
	EventSeverityDebug
	EventSeverityInfo
	EventSeverityWarning
	EventSeverityError
	EventSeverityCritical
)

type SourceType int

const (
	SourceTypeUnknown SourceType = iota
	SourceTypeEBPF
	SourceTypeJournald
	SourceTypeKubernetes
	SourceTypeSystemd
)

// Audience is defined in semantic_types.go

// Conversion helpers

func insightToCorrelation(insight domain.Insight) *Correlation {
	return &Correlation{
		ID:           insight.ID,
		Type:         CorrelationTypeSemantic,
		Title:        insight.Title,
		Description:  insight.Description,
		Confidence:   0.8, // Default confidence
		DiscoveredAt: insight.Timestamp,
		Metadata:     convertMetadataToStringMap(insight.Metadata),
	}
}

func eventGroupToSemanticGroup(group EventGroup) *SemanticGroup {
	eventIDs := make([]string, 0, len(group.Events))
	events := make([]*domain.Event, 0, len(group.Events))

	for _, e := range group.Events {
		eventIDs = append(eventIDs, string(e.ID))
		eventCopy := e
		events = append(events, &eventCopy)
	}

	// Calculate time range from events
	var startTime, endTime time.Time
	if len(group.Events) > 0 {
		startTime = group.Events[0].Timestamp
		endTime = group.Events[0].Timestamp
		for _, e := range group.Events {
			if e.Timestamp.Before(startTime) {
				startTime = e.Timestamp
			}
			if e.Timestamp.After(endTime) {
				endTime = e.Timestamp
			}
		}
	}

	return &SemanticGroup{
		ID:           group.ID,
		Name:         group.Type,
		Description:  group.Description,
		SemanticType: group.Type,
		Confidence:   group.Confidence,
		EventIDs:     eventIDs,
		Events:       events,
		StartTime:    startTime,
		EndTime:      endTime,
		Duration:     group.TimeSpan,
		TraceID:      group.TraceID,
		Impact: &ImpactAssessment{
			InfrastructureImpact: float32(group.InfrastructureImpact),
		},
		Metadata: make(map[string]string),
		Labels:   make(map[string]string),
	}
}

func domainInsightToCorrelationInsight(di domain.Insight) *CorrelationInsight {
	return &CorrelationInsight{
		Insight: di,
		// Additional fields can be populated from metadata if available
		RelatedEvents: extractRelatedEventsFromMetadata(di.Metadata),
		Resources:     extractResourcesFromMetadata(di.Metadata),
		Actions:       extractActionsFromMetadata(di.Metadata),
		Prediction:    extractPredictionFromMetadata(di.Metadata),
	}
}

func convertMetadataToStringMap(metadata map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range metadata {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

func extractRelatedEventsFromMetadata(metadata map[string]interface{}) []string {
	if events, ok := metadata["related_events"].([]string); ok {
		return events
	}
	if eventsInterface, ok := metadata["related_events"].([]interface{}); ok {
		events := make([]string, 0, len(eventsInterface))
		for _, e := range eventsInterface {
			if eventID, ok := e.(string); ok {
				events = append(events, eventID)
			}
		}
		return events
	}
	return nil
}

func extractResourcesFromMetadata(metadata map[string]interface{}) []AffectedResource {
	if resources, ok := metadata["affected_resources"].([]AffectedResource); ok {
		return resources
	}
	// More complex extraction logic could be added here
	return nil
}

func extractActionsFromMetadata(metadata map[string]interface{}) []ActionableItem {
	if actions, ok := metadata["recommended_actions"].([]ActionableItem); ok {
		return actions
	}
	// More complex extraction logic could be added here
	return nil
}

func extractPredictionFromMetadata(metadata map[string]interface{}) *Prediction {
	if pred, ok := metadata["prediction"].(*Prediction); ok {
		return pred
	}
	// More complex extraction logic could be added here
	return nil
}
