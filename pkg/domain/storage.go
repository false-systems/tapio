package domain

import "context"

// GraphStorage defines the interface for graph-based storage operations
// This interface abstracts away the specific graph database implementation
type GraphStorage interface {
	// CRUD operations for nodes and relationships
	CreateOrUpdateNode(ctx context.Context, event *ObservationEvent) error
	CreateEvent(ctx context.Context, event *ObservationEvent) error
	CreateRelationship(ctx context.Context, fromUID, toUID, relType string, properties RelationshipProperties) error
	CreateEventRelationship(ctx context.Context, eventID, entityUID, relType string) error

	// Query operations
	ExecuteQuery(ctx context.Context, query string, params QueryParams) ([]QueryResult, error)

	// Lifecycle management
	CreateIndexes(ctx context.Context) error
	Health(ctx context.Context) error
	Close(ctx context.Context) error
}

// GraphStorageConfig defines configuration for graph storage
type GraphStorageConfig struct {
	URI      string
	Username string
	Password string
	Database string
}

// RelationshipProperties represents typed properties for graph relationships
type RelationshipProperties struct {
	Timestamp  int64              `json:"timestamp,omitempty"`
	Confidence float64            `json:"confidence,omitempty"`
	Weight     float64            `json:"weight,omitempty"`
	Type       string             `json:"type,omitempty"`
	Source     string             `json:"source,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Labels     map[string]string  `json:"labels,omitempty"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Custom     map[string]string  `json:"custom,omitempty"`
}

// QueryParams represents typed query parameters
type QueryParams struct {
	StringParams map[string]string   `json:"string_params,omitempty"`
	IntParams    map[string]int64    `json:"int_params,omitempty"`
	FloatParams  map[string]float64  `json:"float_params,omitempty"`
	BoolParams   map[string]bool     `json:"bool_params,omitempty"`
	ListParams   map[string][]string `json:"list_params,omitempty"`
}

// QueryResult represents a typed result from a graph query
type QueryResult struct {
	NodeID     string             `json:"node_id,omitempty"`
	NodeType   string             `json:"node_type,omitempty"`
	Properties map[string]string  `json:"properties,omitempty"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Labels     []string           `json:"labels,omitempty"`
	Relations  []RelationInfo     `json:"relations,omitempty"`
}

// RelationInfo represents information about a relationship
type RelationInfo struct {
	Type       string            `json:"type"`
	TargetID   string            `json:"target_id"`
	TargetType string            `json:"target_type,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}
