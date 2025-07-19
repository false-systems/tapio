package events

import "time"

// OpinionatedEvent represents an OPINIONATED event format
type OpinionatedEvent struct {
	// Core identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	// OPINIONATED classification
	Category   EventCategory `json:"category"`
	Severity   EventSeverity `json:"severity"`
	Confidence float32       `json:"confidence"`

	// Source information
	Source EventSource `json:"source"`

	// AI-ready context
	Context OpinionatedContext `json:"context"`

	// Structured data
	Data       map[string]interface{} `json:"data"`
	Attributes map[string]interface{} `json:"attributes"`

	// Correlation hints
	CorrelationHints []string `json:"correlation_hints"`

	// Correlation data for AI analysis
	Correlation *CorrelationData `json:"correlation,omitempty"`

	// Semantic context for AI analysis
	Semantic *SemanticContext `json:"semantic,omitempty"`

	// Behavioral context for entity tracking
	Behavioral *BehavioralContext `json:"behavioral,omitempty"`

	// Anomaly detection results
	Anomaly *AnomalyInfo `json:"anomaly,omitempty"`

	// Temporal analysis context
	Temporal *TemporalContext `json:"temporal,omitempty"`

	// AI Features for ML processing
	AiFeatures map[string]float32 `json:"ai_features,omitempty"`

	// State information for stateful analysis
	State *StateInfo `json:"state,omitempty"`

	// Impact assessment information
	Impact *ImpactInfo `json:"impact,omitempty"`

	// Causality analysis context
	Causality *CausalityContext `json:"causality,omitempty"`
}

// EventCategory represents OPINIONATED event categories
type EventCategory string

const (
	CategorySystemHealth     EventCategory = "system_health"
	CategoryNetworkHealth    EventCategory = "network_health"
	CategoryAppHealth        EventCategory = "app_health"
	CategorySecurityEvent    EventCategory = "security_event"
	CategoryPerformanceIssue EventCategory = "performance_issue"
)

// EventSeverity represents OPINIONATED severity levels
type EventSeverity string

const (
	SeverityCritical EventSeverity = "critical"
	SeverityHigh     EventSeverity = "high"
	SeverityMedium   EventSeverity = "medium"
	SeverityLow      EventSeverity = "low"
	SeverityInfo     EventSeverity = "info"
)

// EventSource identifies the source of an OPINIONATED event
type EventSource struct {
	Collector string `json:"collector"`
	Component string `json:"component"`
	Node      string `json:"node"`
}

// OpinionatedContext provides AI-ready context
type OpinionatedContext struct {
	// Kubernetes context
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`

	// Process context
	PID         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`

	// Network context
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`

	// Custom context
	Custom map[string]string `json:"custom,omitempty"`
}

// CorrelationData contains correlation information for AI analysis
type CorrelationData struct {
	Vectors     []CorrelationVectors `json:"vectors"`
	Groups      []CorrelationGroup   `json:"groups"`
	CausalLinks []CausalLink         `json:"causal_links"`
}

// CorrelationVectors represents correlation vectors for AI processing
type CorrelationVectors struct {
	Temporal []float64 `json:"temporal"`
	Spatial  []float64 `json:"spatial"`
	Semantic []float64 `json:"semantic"`
}

// CorrelationGroup represents a group of correlated events
type CorrelationGroup struct {
	ID     string   `json:"id"`
	Events []string `json:"events"`
	Score  float64  `json:"score"`
	Type   string   `json:"type"`
}

// CausalLink represents a causal relationship between events
type CausalLink struct {
	SourceEvent  string  `json:"source_event"`
	TargetEvent  string  `json:"target_event"`
	Confidence   float64 `json:"confidence"`
	Delay        int64   `json:"delay_ms"`
	Relationship string  `json:"relationship"` // e.g., "causes", "precedes", "enables"
}

// SemanticContext provides semantic context for AI analysis
type SemanticContext struct {
	Domain           string             `json:"domain"`
	Concepts         []string           `json:"concepts"`
	Metadata         map[string]string  `json:"metadata"`
	Embedding        []float32          `json:"embedding"`  // Vector embedding for similarity
	EventType        string             `json:"event_type"` // Semantic classification
	Intent           string             `json:"intent"`     // Intended purpose or goal
	IntentConfidence float32            `json:"intent_confidence"`
	SemanticFeatures map[string]float32 `json:"semantic_features"`
	OntologyTags     []string           `json:"ontology_tags"`
	Description      string             `json:"description"`
}

// TemporalPattern represents a time-based pattern in events
type TemporalPattern struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Pattern     []PatternStep `json:"pattern"`
	Window      time.Duration `json:"window"`
	Confidence  float64       `json:"confidence"`
}

// PatternStep represents a step in a temporal pattern
type PatternStep struct {
	EventType string        `json:"event_type"`
	Condition string        `json:"condition"`
	Delay     time.Duration `json:"delay"`
	Optional  bool          `json:"optional"`
}

// SpatialPattern represents a location-based pattern
type SpatialPattern struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Entities   []string          `json:"entities"`
	Relations  []SpatialRelation `json:"relations"`
	Confidence float64           `json:"confidence"`
}

// SpatialRelation represents a relationship between entities
type SpatialRelation struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Type     string `json:"type"`
	Distance int    `json:"distance"`
}

// BehavioralContext provides behavioral analysis context
type BehavioralContext struct {
	Entity            *EntityContext    `json:"entity"`
	Patterns          []string          `json:"patterns"`
	Anomalies         []Anomaly         `json:"anomalies"`
	Confidence        float64           `json:"confidence"`
	BehaviorDeviation float64           `json:"behavior_deviation"`
	BehaviorTrend     string            `json:"behavior_trend"`
	ChangeIndicators  *ChangeIndicators `json:"change_indicators"`
}

// EntityContext represents an entity involved in the event
type EntityContext struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	Name           string  `json:"name"`
	TrustScore     float64 `json:"trust_score"`
	LifecycleStage string  `json:"lifecycle_stage"`
}

// Anomaly represents detected anomalous behavior
type Anomaly struct {
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// AnomalyInfo provides detailed anomaly information for events
type AnomalyInfo struct {
	Score              float64             `json:"score"`
	Type               string              `json:"type"`
	Description        string              `json:"description"`
	Anomalies          []Anomaly           `json:"anomalies"`
	Confidence         float64             `json:"confidence"`
	AnomalyScore       float32             `json:"anomaly_score"`
	Dimensions         *AnomalyDimensions  `json:"dimensions"`
	BaselineComparison *BaselineComparison `json:"baseline_comparison"`
}

// TemporalContext provides temporal analysis context
type TemporalContext struct {
	Period      time.Duration      `json:"period"`
	Frequency   float64            `json:"frequency"`
	Patterns    []TemporalPattern  `json:"patterns"`
	Seasonality map[string]float64 `json:"seasonality"`
	Duration    time.Duration      `json:"duration"`
	Periodicity float64            `json:"periodicity"`
}

// ChangeIndicators provides metrics about behavioral changes
type ChangeIndicators struct {
	Velocity       float32 `json:"velocity"`
	Acceleration   float32 `json:"acceleration"`
	Jitter         float32 `json:"jitter"`
	Predictability float32 `json:"predictability"`
}

// AnomalyDimensions provides dimensional analysis of anomalies
type AnomalyDimensions struct {
	Temporal    float32 `json:"temporal"`
	Spatial     float32 `json:"spatial"`
	Behavioral  float32 `json:"behavioral"`
	Contextual  float32 `json:"contextual"`
	Statistical float32 `json:"statistical"`
}

// BaselineComparison provides comparison with baseline behavior
type BaselineComparison struct {
	Deviation    float32 `json:"deviation"`
	Significance float32 `json:"significance"`
	Confidence   float32 `json:"confidence"`
	Percentile   float32 `json:"percentile"`
	ZScore       float32 `json:"z_score"`
}

// StateInfo provides state tracking information
type StateInfo struct {
	Current    string            `json:"current"`
	Previous   string            `json:"previous"`
	Transition string            `json:"transition"`
	Duration   time.Duration     `json:"duration"`
	Metadata   map[string]string `json:"metadata"`
	TimeSeries *TimeSeriesData   `json:"time_series,omitempty"`
}

// TimeSeriesData provides time series context
type TimeSeriesData struct {
	Values     []float64     `json:"values"`
	Timestamps []time.Time   `json:"timestamps"`
	Window     time.Duration `json:"window"`
	Trend      string        `json:"trend"`
}

// ImpactInfo provides impact assessment information
type ImpactInfo struct {
	BusinessImpact  float32           `json:"business_impact"`
	TechnicalImpact float32           `json:"technical_impact"`
	UserImpact      float32           `json:"user_impact"`
	SecurityImpact  float32           `json:"security_impact"`
	Severity        string            `json:"severity"`
	Scope           string            `json:"scope"`
	Urgency         float32           `json:"urgency"`
	Metadata        map[string]string `json:"metadata"`
}

// CausalityContext provides causality analysis context for correlation engine
type CausalityContext struct {
	CausalChain []CausalEvent `json:"causal_chain"`
	RootCause   string        `json:"root_cause"`
	Confidence  float64       `json:"confidence"`
	ChainDepth  int           `json:"chain_depth"`
}

// CausalEvent represents an event in a causal chain
type CausalEvent struct {
	EventID     string    `json:"event_id"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Confidence  float64   `json:"confidence"`
}
