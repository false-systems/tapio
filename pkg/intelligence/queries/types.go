package queries

import "time"

// RootCauseAnalysis represents the result of a root cause query
type RootCauseAnalysis struct {
	FailedEntity   EntityInfo  `json:"failed_entity"`
	RootCauses     []CauseInfo `json:"root_causes"`
	CausalChain    []CauseInfo `json:"causal_chain"`
	RelatedEvents  []EventInfo `json:"related_events"`
	Recommendation string      `json:"recommendation"`
	Confidence     float64     `json:"confidence"`
	Timestamp      time.Time   `json:"timestamp"`
}

// ImpactAnalysis shows what resources are affected
type ImpactAnalysis struct {
	Service             EntityInfo   `json:"service"`
	AffectedPods        []EntityInfo `json:"affected_pods"`
	AffectedDeployments []EntityInfo `json:"affected_deployments"`
	DependentServices   []EntityInfo `json:"dependent_services"`
	EstimatedImpact     string       `json:"estimated_impact"`
	Timestamp           time.Time    `json:"timestamp"`
}

// CascadePattern represents a cascading failure pattern
type CascadePattern struct {
	TriggerEvent      EventInfo    `json:"trigger_event"`
	AffectedResources []EntityInfo `json:"affected_resources"`
	PropagationPath   []EventInfo  `json:"propagation_path"`
	Pattern           string       `json:"pattern"`
	Severity          string       `json:"severity"`
	DetectedAt        time.Time    `json:"detected_at"`
}

// EventSequence represents temporal sequence of events
type EventSequence struct {
	Entity       EntityInfo  `json:"entity"`
	Events       []EventInfo `json:"events"`
	Duration     string      `json:"duration"`
	EventCount   int         `json:"event_count"`
	AnomalyScore float64     `json:"anomaly_score"`
}

// ServiceDependencyMap maps service dependencies
type ServiceDependencyMap struct {
	Dependencies map[string][]string `json:"dependencies"`
	Timestamp    time.Time           `json:"timestamp"`
}

// EntityInfo represents a K8s entity
type EntityInfo struct {
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	UID             string            `json:"uid"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	ResourceVersion string            `json:"resource_version,omitempty"`
}

// EventInfo represents an event
type EventInfo struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Message   string            `json:"message"`
	Severity  string            `json:"severity"`
	Source    string            `json:"source"`
	Timestamp time.Time         `json:"timestamp"`
	TraceID   string            `json:"trace_id,omitempty"`
	SpanID    string            `json:"span_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// CauseInfo represents a root cause
type CauseInfo struct {
	Type       string    `json:"type"`
	Entity     string    `json:"entity"`
	Message    string    `json:"message"`
	Timestamp  time.Time `json:"timestamp"`
	Confidence float64   `json:"confidence"`
	Evidence   []string  `json:"evidence,omitempty"`
}
