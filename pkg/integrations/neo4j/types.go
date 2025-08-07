package neo4j

import (
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

	// Raw values for backward compatibility during migration
	Values map[string]interface{} `json:"values,omitempty"`
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

// Additional relationship types not in schema.go
type RelationshipType string

const (
	// Additional resource relationships
	RelOwns      RelationshipType = "OWNS"
	RelSelects   RelationshipType = "SELECTS"
	RelUses      RelationshipType = "USES"
	RelExposedBy RelationshipType = "EXPOSED_BY"

	// Additional event relationships
	RelPartOf RelationshipType = "PART_OF"

	// Correlation relationships
	RelCorrelatedWith RelationshipType = "CORRELATED_WITH"
	RelRootCauseOf    RelationshipType = "ROOT_CAUSE_OF"
	RelImpactOf       RelationshipType = "IMPACT_OF"

	// Additional network relationships
	RelRoutesTo RelationshipType = "ROUTES_TO"
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
// Used during migration from map[string]interface{}
type NodeProperties struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

// WriteParams contains parameters for write operations
type WriteParams struct {
	Node         interface{}            `json:"node,omitempty"`
	Relationship *Relationship          `json:"relationship,omitempty"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
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

	// Generic parameters for complex queries
	Custom map[string]interface{} `json:"custom,omitempty"`
}

// ToMap converts QueryParams to map for Neo4j driver
func (q QueryParams) ToMap() map[string]interface{} {
	params := make(map[string]interface{})

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

	// Add custom parameters
	for k, v := range q.Custom {
		params[k] = v
	}

	return params
}
