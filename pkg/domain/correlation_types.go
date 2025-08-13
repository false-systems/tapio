package domain

import (
	"time"
)

// CorrelationRef references a correlation this event is part of
type CorrelationRef struct {
	CorrelationID string  `json:"correlation_id"`
	Type          string  `json:"type"`
	Confidence    float64 `json:"confidence"`
	Role          string  `json:"role"` // "root", "trigger", "effect", "related"
}

// PatternMatch represents a matched pattern
type PatternMatch struct {
	PatternID   string           `json:"pattern_id"`
	PatternName string           `json:"pattern_name"`
	Type        string           `json:"type"`
	Confidence  float64          `json:"confidence"`
	MatchTime   time.Time        `json:"match_time"`
	Timestamp   time.Time        `json:"timestamp"`
	Evidence    []string         `json:"evidence,omitempty"`
	Conditions  []ConditionMatch `json:"conditions,omitempty"`
}

// AnomalyRef references an anomaly detection
type AnomalyRef struct {
	AnomalyID   string    `json:"anomaly_id"`
	Type        string    `json:"type"`
	Score       float64   `json:"score"`
	Severity    string    `json:"severity"`
	DetectedAt  time.Time `json:"detected_at"`
	Description string    `json:"description,omitempty"`
}

// EnhancedCorrelation represents multi-dimensional event correlation
type EnhancedCorrelation struct {
	ID     string                `json:"id"`
	Type   CorrelationType       `json:"type"`
	Events []CorrelationEventRef `json:"events"`

	// Multi-dimensional correlation scores
	Dimensions   map[string]DimensionScore `json:"dimensions"`
	OverallScore float64                   `json:"overall_score"`

	// Correlation metadata
	TimeWindow    TimeWindow     `json:"time_window"`
	CommonFactors []CommonFactor `json:"common_factors"`
	CausalChain   []CausalLink   `json:"causal_chain,omitempty"`

	// Analysis results
	RootEvents  []CorrelationEventRef   `json:"root_events,omitempty"`
	ImpactScope *CorrelationImpactScope `json:"impact_scope,omitempty"`

	Metadata *CorrelationMetadata `json:"metadata,omitempty"`
}

// Additional correlation types not in types.go
const (
	CorrelationTypeOwnership   CorrelationType = "ownership"
	CorrelationTypeSemantic    CorrelationType = "semantic"
	CorrelationTypeDependency  CorrelationType = "dependency"
	CorrelationTypeStatistical CorrelationType = "statistical"
)

// CorrelationEventRef references an event in a correlation
type CorrelationEventRef struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Source     string    `json:"source"`
	Timestamp  time.Time `json:"timestamp"`
	Role       string    `json:"role"`   // "root", "trigger", "cascade", "recovery"
	Impact     string    `json:"impact"` // "critical", "high", "medium", "low", "info"
	Confidence float64   `json:"confidence"`
}

// DimensionScore represents correlation strength in one dimension
type DimensionScore struct {
	Dimension  string   `json:"dimension"` // "temporal", "spatial", "causal", "semantic"
	Score      float64  `json:"score"`     // 0.0 - 1.0
	Evidence   []string `json:"evidence"`
	Confidence float64  `json:"confidence"`
}

// CommonFactor represents shared characteristics
type CommonFactor struct {
	Type         string      `json:"type"` // "namespace", "node", "service", "workload"
	Value        interface{} `json:"value"`
	Events       []string    `json:"events"` // Event IDs sharing this factor
	Significance float64     `json:"significance"`
}

// CausalLink represents causality between events
type CausalLink struct {
	CauseEvent  string        `json:"cause_event"`
	EffectEvent string        `json:"effect_event"`
	LinkType    string        `json:"link_type"` // "triggers", "causes", "correlates"
	Confidence  float64       `json:"confidence"`
	TimeDelta   time.Duration `json:"time_delta"`
	Evidence    string        `json:"evidence,omitempty"`
}

// CorrelationImpactScope defines the scope of impact
type CorrelationImpactScope struct {
	DirectlyAffected   []K8sResourceRef `json:"directly_affected"`
	IndirectlyAffected []K8sResourceRef `json:"indirectly_affected"`
	BusinessImpact     string           `json:"business_impact"`
	EstimatedDuration  time.Duration    `json:"estimated_duration"`
	AffectedUsers      int              `json:"affected_users,omitempty"`
	SeverityLevel      string           `json:"severity_level"`
}

// BehavioralPattern represents a detected behavioral pattern
type BehavioralPattern struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`

	// Pattern characteristics
	Signature   BehavioralPatternSignature `json:"signature"`
	Frequency   float64                    `json:"frequency"`
	Periodicity *time.Duration             `json:"periodicity,omitempty"`

	// Matching events
	Matches  []PatternMatch `json:"matches"`
	Coverage float64        `json:"coverage"` // % of events explained

	// Pattern metadata
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	OccurrenceCount int       `json:"occurrence_count"`

	// Predictive capability
	Predictability float64    `json:"predictability"`
	NextOccurrence *time.Time `json:"next_occurrence,omitempty"`
}

// BehavioralPatternSignature defines pattern matching rules
type BehavioralPatternSignature struct {
	EventSequence   []EventCriteria   `json:"event_sequence"`
	TimeConstraints []TimeConstraint  `json:"time_constraints"`
	RequiredContext []ContextCriteria `json:"required_context"`
}

// EventCriteriaConditions represents event matching conditions
type EventCriteriaConditions struct {
	// Field-based conditions
	FieldEquals   map[string]string  `json:"field_equals,omitempty"`
	FieldContains map[string]string  `json:"field_contains,omitempty"`
	FieldGreater  map[string]float64 `json:"field_greater,omitempty"`
	FieldLess     map[string]float64 `json:"field_less,omitempty"`
	FieldExists   []string           `json:"field_exists,omitempty"`
	FieldRegex    map[string]string  `json:"field_regex,omitempty"`

	// Label-based conditions
	LabelsMatch map[string]string `json:"labels_match,omitempty"`
	LabelsExist []string          `json:"labels_exist,omitempty"`

	// Complex conditions
	LogicOperator    string   `json:"logic_operator,omitempty"` // "AND", "OR", "NOT"
	NestedConditions []string `json:"nested_conditions,omitempty"`

	// Additional flexible matching
	CustomMatch interface{} `json:"custom_match,omitempty"`
}

// EventCriteria defines criteria for matching events
type EventCriteria struct {
	Type       string                   `json:"type,omitempty"`
	Source     string                   `json:"source,omitempty"`
	Severity   string                   `json:"severity,omitempty"`
	Conditions *EventCriteriaConditions `json:"conditions,omitempty"`
	MinOccur   int                      `json:"min_occur,omitempty"`
}

// TimeConstraint defines temporal constraints
type TimeConstraint struct {
	Between  string        `json:"between"` // "event1,event2"
	MinDelta time.Duration `json:"min_delta,omitempty"`
	MaxDelta time.Duration `json:"max_delta,omitempty"`
}

// ContextCriteria defines required context
type ContextCriteria struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // "equals", "contains", "exists"
	Value    interface{} `json:"value,omitempty"`
}
