package domain

import "context"

// GraphStorage defines the interface for graph-based storage operations
// This interface abstracts away the specific graph database implementation
type GraphStorage interface {
	// CRUD operations for nodes and relationships
	CreateOrUpdateNode(ctx context.Context, event *UnifiedEvent) error
	CreateEvent(ctx context.Context, event *UnifiedEvent) error
	CreateRelationship(ctx context.Context, fromUID, toUID, relType string, properties map[string]interface{}) error
	CreateEventRelationship(ctx context.Context, eventID, entityUID, relType string) error

	// Query operations
	ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error)

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
