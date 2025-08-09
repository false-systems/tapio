package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EvidenceData represents strongly-typed evidence for correlations
type EvidenceData struct {
	EventIDs      []string               `json:"event_ids"`
	ResourceIDs   []string               `json:"resource_ids"`
	Timestamps    []time.Time            `json:"timestamps"`
	Metrics       map[string]MetricValue `json:"metrics"`
	Relationships []ResourceRelationship `json:"relationships"`
	Attributes    map[string]string      `json:"attributes"`
	ConfigMaps    []string               `json:"config_maps,omitempty"`
	Secrets       []string               `json:"secrets,omitempty"`
	Pods          []string               `json:"pods,omitempty"`
	Services      []string               `json:"services,omitempty"`
	Deployments   []string               `json:"deployments,omitempty"`
}

// MetricValue represents a typed metric with metadata
type MetricValue struct {
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// ResourceRelationship represents typed relationships between resources
type ResourceRelationship struct {
	SourceID   string  `json:"source_id"`
	TargetID   string  `json:"target_id"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

// CorrelationDetails provides typed correlation information
type CorrelationDetails struct {
	Pattern          string             `json:"pattern"`
	Algorithm        string             `json:"algorithm"`
	ProcessingTime   time.Duration      `json:"processing_time"`
	DataPoints       int                `json:"data_points"`
	SourceEvents     []EventReference   `json:"source_events"`
	ImpactedServices []ServiceReference `json:"impacted_services"`
}

// EventReference provides typed event information
type EventReference struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"`
}

// ServiceReference provides typed service information
type ServiceReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Version   string `json:"version"`
}

// ConfigChangeData represents typed configuration change data
type ConfigChangeData struct {
	ResourceType  string            `json:"resource_type"`
	ResourceName  string            `json:"resource_name"`
	Namespace     string            `json:"namespace"`
	ChangeType    string            `json:"change_type"`
	OldValue      string            `json:"old_value"`
	NewValue      string            `json:"new_value"`
	ChangedFields map[string]string `json:"changed_fields"`
}

// DependencyData represents typed dependency information
type DependencyData struct {
	SourceService   ServiceReference `json:"source_service"`
	TargetService   ServiceReference `json:"target_service"`
	DependencyType  string           `json:"dependency_type"`
	Direction       string           `json:"direction"`
	Strength        float64          `json:"strength"`
	ObservedLatency time.Duration    `json:"observed_latency"`
}

// TemporalData represents typed temporal correlation data
type TemporalData struct {
	TimeWindow    time.Duration    `json:"time_window"`
	EventSequence []EventReference `json:"event_sequence"`
	Pattern       string           `json:"pattern"`
	Periodicity   time.Duration    `json:"periodicity"`
	NextPredicted time.Time        `json:"next_predicted"`
}

// OwnershipData represents typed ownership information
type OwnershipData struct {
	Owner       string            `json:"owner"`
	Team        string            `json:"team"`
	Environment string            `json:"environment"`
	CostCenter  string            `json:"cost_center"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// CorrelationResult represents a discovered correlation between events
type CorrelationResult struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"` // k8s_ownership, temporal_pattern, sequence_match
	Confidence float64                `json:"confidence"`
	Events     []string               `json:"events"`             // Event IDs involved
	Related    []*domain.UnifiedEvent `json:"related,omitempty"`  // Related events for correlation
	Message    string                 `json:"message"`            // Human-readable message about the correlation
	TraceID    string                 `json:"trace_id,omitempty"` // Trace ID if available
	RootCause  *RootCause             `json:"root_cause,omitempty"`
	Impact     *Impact                `json:"impact,omitempty"`
	Summary    string                 `json:"summary"`
	Details    CorrelationDetails     `json:"details"`  // Changed from string to typed
	Evidence   EvidenceData           `json:"evidence"` // Changed from []string to typed
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`

	// Type-specific data based on correlation type
	ConfigData     *ConfigChangeData `json:"config_data,omitempty"`
	DependencyData *DependencyData   `json:"dependency_data,omitempty"`
	TemporalData   *TemporalData     `json:"temporal_data,omitempty"`
	OwnershipData  *OwnershipData    `json:"ownership_data,omitempty"`
}

// RootCause identifies the source of the issue
type RootCause struct {
	EventID     string       `json:"event_id"`
	Confidence  float64      `json:"confidence"`
	Description string       `json:"description"`
	Evidence    EvidenceData `json:"evidence"` // Changed from []string to typed
}

// Impact describes what's affected
type Impact struct {
	Severity    domain.EventSeverity `json:"severity"`
	Resources   []string             `json:"resources"` // Affected K8s resources
	Services    []ServiceReference   `json:"services"`  // Changed from []string to typed
	Scope       string               `json:"scope"`
	UserImpact  string               `json:"user_impact"`
	Degradation string               `json:"degradation"`
}

// Engine orchestrates all correlators
type IEngine interface {
	// Process an event through all correlators
	Process(ctx context.Context, event *domain.UnifiedEvent) error

	// Get correlation results channel
	Results() <-chan *CorrelationResult

	// Start the engine
	Start(ctx context.Context) error

	// Stop the engine
	Stop() error
}

// Dependency represents a correlator dependency
type Dependency struct {
	Name        string
	Type        string
	Description string
	Required    bool
	HealthCheck func(context.Context) error
}

// Helper function to extract event IDs from unified events
func getEventIDs(events []*domain.UnifiedEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event != nil {
			ids = append(ids, event.ID)
		}
	}
	return ids
}

// SafeGetString safely extracts string from map
func SafeGetString(m map[string]interface{}, key string) (string, bool) {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// SafeGetFloat64 safely extracts float64 from map
func SafeGetFloat64(m map[string]interface{}, key string) (float64, bool) {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case float64:
			return v, true
		case float32:
			return float64(v), true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		}
	}
	return 0, false
}

// SafeGetStringSlice safely extracts string slice from interface slice
func SafeGetStringSlice(input interface{}) []string {
	if slice, ok := input.([]interface{}); ok {
		result := make([]string, 0, len(slice))
		for _, item := range slice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return nil
}

// ConvertMapToAttributes converts untyped map to typed attributes map
func ConvertMapToAttributes(data map[string]interface{}) map[string]string {
	attributes := make(map[string]string)
	for key, val := range data {
		if str, ok := val.(string); ok {
			attributes[key] = str
		} else {
			attributes[key] = fmt.Sprintf("%v", val)
		}
	}
	return attributes
}

// CreateEvidenceData creates typed evidence from legacy interface{} usage
func CreateEvidenceData(eventIDs, resourceIDs []string, attrs map[string]string) EvidenceData {
	return EvidenceData{
		EventIDs:    eventIDs,
		ResourceIDs: resourceIDs,
		Timestamps:  []time.Time{time.Now()},
		Metrics:     make(map[string]MetricValue),
		Attributes:  attrs,
	}
}
