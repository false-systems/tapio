package neo4j

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// QueryResult represents the result of a Neo4j query with typed data
type QueryResult struct {
	Records []Record `json:"records"`
	Summary Summary  `json:"summary"`
}

// Record represents a single record from a query result
type Record struct {
	// Core node types
	Resource    *ResourceNode    `json:"resource,omitempty"`
	Event       *EventNode       `json:"event,omitempty"`
	Correlation *CorrelationNode `json:"correlation,omitempty"`

	// Relationships
	Relationships []Relationship `json:"relationships,omitempty"`

	// Aggregated data
	Count      int64    `json:"count,omitempty"`
	StringList []string `json:"string_list,omitempty"`

	// Strongly typed values
	StringValues map[string]string  `json:"string_values,omitempty"`
	IntValues    map[string]int64   `json:"int_values,omitempty"`
	FloatValues  map[string]float64 `json:"float_values,omitempty"`
	BoolValues   map[string]bool    `json:"bool_values,omitempty"`
}

// Summary contains query execution metadata
type Summary struct {
	NodesCreated         int64         `json:"nodes_created"`
	NodesDeleted         int64         `json:"nodes_deleted"`
	RelationshipsCreated int64         `json:"relationships_created"`
	RelationshipsDeleted int64         `json:"relationships_deleted"`
	PropertiesSet        int64         `json:"properties_set"`
	ExecutionTime        time.Duration `json:"execution_time"`
}

// ResourceNode represents a Kubernetes resource in the graph
type ResourceNode struct {
	// Core properties
	UID        string `json:"uid"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	APIVersion string `json:"api_version,omitempty"`

	// Status
	Status    string    `json:"status,omitempty"`
	Phase     string    `json:"phase,omitempty"`
	Ready     bool      `json:"ready"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Resource-specific
	Replicas      int32  `json:"replicas,omitempty"`
	ReadyReplicas int32  `json:"ready_replicas,omitempty"`
	NodeName      string `json:"node_name,omitempty"`
	ClusterIP     string `json:"cluster_ip,omitempty"`
	PodIP         string `json:"pod_ip,omitempty"`
}

// EventNode represents an event in the graph
type EventNode struct {
	// Core properties
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`

	// Event details
	Severity  domain.EventSeverity `json:"severity"`
	Message   string               `json:"message"`
	Reason    string               `json:"reason,omitempty"`
	Category  string               `json:"category,omitempty"`
	Component string               `json:"component,omitempty"`

	// Context
	ResourceUID string `json:"resource_uid,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`

	// Metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CorrelationNode represents a correlation in the graph
type CorrelationNode struct {
	// Core properties
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	TraceID   string    `json:"trace_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// Correlation details
	Confidence float64   `json:"confidence"`
	Summary    string    `json:"summary"`
	Details    string    `json:"details,omitempty"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`

	// Analysis results
	RootCause string `json:"root_cause,omitempty"`
	Impact    string `json:"impact,omitempty"`
	Severity  string `json:"severity,omitempty"`

	// Evidence
	Evidence []string `json:"evidence,omitempty"`
}

// Relationship represents an edge in the graph
type Relationship struct {
	// Core properties
	Type        RelationType `json:"type"`
	StartNodeID string       `json:"start_node_id"`
	EndNodeID   string       `json:"end_node_id"`

	// Relationship properties
	Properties RelationshipProperties `json:"properties"`
}

// Additional relationship types not in schema.go - extend RelationType from schema.go
const (
	// Additional resource relationships
	RelOwns      RelationType = "OWNS"
	RelSelects   RelationType = "SELECTS"
	RelUses      RelationType = "USES"
	RelExposedBy RelationType = "EXPOSED_BY"

	// Additional event relationships
	RelPartOf RelationType = "PART_OF"

	// Correlation relationships
	RelCorrelatedWith RelationType = "CORRELATED_WITH"
	RelRootCauseOf    RelationType = "ROOT_CAUSE_OF"
	RelImpactOf       RelationType = "IMPACT_OF"

	// Additional network relationships
	RelRoutesTo RelationType = "ROUTES_TO"
)

// RelationshipProperties contains properties for relationships
type RelationshipProperties struct {
	// Common properties
	CreatedAt  time.Time `json:"created_at"`
	Weight     float64   `json:"weight,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`

	// Ownership properties
	Controller         bool `json:"controller,omitempty"`
	BlockOwnerDeletion bool `json:"block_owner_deletion,omitempty"`

	// Connection properties
	Port      int32  `json:"port,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Direction string `json:"direction,omitempty"`

	// Event relationship properties
	Latency time.Duration `json:"latency,omitempty"`
	Count   int64         `json:"count,omitempty"`
}

// NodeProperties is a generic container for node properties
// NodeProperty represents a strongly typed node property
type NodeProperty struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueType string `json:"value_type"` // "string", "int", "float", "bool", "time"
}

// Used during migration from strongly typed properties
type NodeProperties struct {
	Type       string         `json:"type"`
	Properties []NodeProperty `json:"properties"`
}

// WriteNode represents a node for write operations
type WriteNode struct {
	Labels     []string       `json:"labels"`
	Properties []NodeProperty `json:"properties"`
}

// WriteParams contains parameters for write operations
type WriteParams struct {
	Node         *WriteNode     `json:"node,omitempty"`
	Relationship *Relationship  `json:"relationship,omitempty"`
	Properties   []NodeProperty `json:"properties,omitempty"`
}

// QueryParams wraps query parameters with type safety
type QueryParams struct {
	// Resource filters
	ResourceType string `json:"resource_type,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name,omitempty"`
	UID          string `json:"uid,omitempty"`

	// Time filters
	StartTime  time.Time     `json:"start_time,omitempty"`
	EndTime    time.Time     `json:"end_time,omitempty"`
	TimeWindow time.Duration `json:"time_window,omitempty"`

	// Query controls
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`

	// Strongly typed custom parameters
	StringParams map[string]string  `json:"string_params,omitempty"`
	IntParams    map[string]int64   `json:"int_params,omitempty"`
	FloatParams  map[string]float64 `json:"float_params,omitempty"`
	BoolParams   map[string]bool    `json:"bool_params,omitempty"`
}

// ParameterValue represents a strongly typed query parameter
type ParameterValue struct {
	StringVal *string  `json:"string_val,omitempty"`
	IntVal    *int64   `json:"int_val,omitempty"`
	FloatVal  *float64 `json:"float_val,omitempty"`
	BoolVal   *bool    `json:"bool_val,omitempty"`
}

// ToMap converts QueryParams to map for Neo4j driver
func (q QueryParams) ToMap() map[string]any {
	params := make(map[string]any)

	if q.ResourceType != "" {
		params["resourceType"] = q.ResourceType
	}
	if q.Namespace != "" {
		params["namespace"] = q.Namespace
	}
	if q.Name != "" {
		params["name"] = q.Name
	}
	if q.UID != "" {
		params["uid"] = q.UID
	}

	if !q.StartTime.IsZero() {
		params["startTime"] = q.StartTime.Unix()
	}
	if !q.EndTime.IsZero() {
		params["endTime"] = q.EndTime.Unix()
	}
	if q.TimeWindow > 0 {
		params["timeWindow"] = q.TimeWindow.Seconds()
	}

	if q.Limit > 0 {
		params["limit"] = q.Limit
	}
	if q.Offset > 0 {
		params["offset"] = q.Offset
	}

	// Add strongly typed custom parameters
	for k, v := range q.StringParams {
		params[k] = v
	}
	for k, v := range q.IntParams {
		params[k] = v
	}
	for k, v := range q.FloatParams {
		params[k] = v
	}
	for k, v := range q.BoolParams {
		params[k] = v
	}

	return params
}

// NodeCreationParams parameters for creating/updating nodes
type NodeCreationParams struct {
	UID             string            `json:"uid"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Kind            string            `json:"kind"`
	Timestamp       int64             `json:"timestamp"`
	Labels          []string          `json:"labels"`
	Annotations     []string          `json:"annotations"`
	ResourceVersion string            `json:"resource_version"`
}

// ToMap converts NodeCreationParams to map for Neo4j driver
func (p NodeCreationParams) ToMap() map[string]any {
	return map[string]any{
		"uid":             p.UID,
		"name":            p.Name,
		"namespace":       p.Namespace,
		"kind":            p.Kind,
		"timestamp":       p.Timestamp,
		"labels":          p.Labels,
		"annotations":     p.Annotations,
		"resourceVersion": p.ResourceVersion,
	}
}

// EventCreationParams parameters for creating events
type EventCreationParams struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
}

// ToMap converts EventCreationParams to map for Neo4j driver
func (p EventCreationParams) ToMap() map[string]any {
	return map[string]any{
		"id":        p.ID,
		"timestamp": p.Timestamp,
		"type":      p.Type,
		"severity":  p.Severity,
		"message":   p.Message,
		"source":    p.Source,
		"traceId":   p.TraceID,
		"spanId":    p.SpanID,
	}
}

// RelationshipCreationParams parameters for creating relationships
type RelationshipCreationParams struct {
	FromUID    string                 `json:"from_uid"`
	ToUID      string                 `json:"to_uid"`
	Timestamp  int64                  `json:"timestamp"`
	Properties map[string]PropertyValue `json:"properties"`
}

// PropertyValue represents a strongly typed property value
type PropertyValue struct {
	StringVal *string  `json:"string_val,omitempty"`
	IntVal    *int64   `json:"int_val,omitempty"`
	FloatVal  *float64 `json:"float_val,omitempty"`
	BoolVal   *bool    `json:"bool_val,omitempty"`
}

// ToMap converts RelationshipCreationParams to map for Neo4j driver
func (p RelationshipCreationParams) ToMap() map[string]any {
	params := map[string]any{
		"fromUID":   p.FromUID,
		"toUID":     p.ToUID,
		"timestamp": p.Timestamp,
	}

	// Add typed properties with safe parameter names
	for k, v := range p.Properties {
		key := fmt.Sprintf("prop_%s", k)
		switch {
		case v.StringVal != nil:
			params[key] = *v.StringVal
		case v.IntVal != nil:
			params[key] = *v.IntVal
		case v.FloatVal != nil:
			params[key] = *v.FloatVal
		case v.BoolVal != nil:
			params[key] = *v.BoolVal
		}
	}

	return params
}

// EventRelationshipParams parameters for creating event relationships
type EventRelationshipParams struct {
	EventID   string `json:"event_id"`
	EntityUID string `json:"entity_uid"`
}

// ToMap converts EventRelationshipParams to map for Neo4j driver
func (p EventRelationshipParams) ToMap() map[string]any {
	return map[string]any{
		"eventID":   p.EventID,
		"entityUID": p.EntityUID,
	}
}

// CausalityParams parameters for linking event causality
type CausalityParams struct {
	EffectID   string  `json:"effect_id"`
	CauseID    string  `json:"cause_id"`
	Confidence float64 `json:"confidence"`
	Timestamp  int64   `json:"timestamp"`
}

// ToMap converts CausalityParams to map for Neo4j driver
func (p CausalityParams) ToMap() map[string]any {
	return map[string]any{
		"effectID":   p.EffectID,
		"causeID":    p.CauseID,
		"confidence": p.Confidence,
		"timestamp":  p.Timestamp,
	}
}
