package correlation

import (
	"context"
	"fmt"
)

// GraphStore defines the interface for graph database operations
// This abstraction allows the intelligence layer to work with graph data
// without depending on specific database implementations
type GraphStore interface {
	// ExecuteQuery runs a graph query with parameters and returns results
	// The params parameter uses the QueryParams interface for type safety
	ExecuteQuery(ctx context.Context, query string, params QueryParams) (ResultIterator, error)

	// ExecuteWrite runs a write query (create, update, delete) with parameters
	// The params parameter uses the QueryParams interface for type safety
	ExecuteWrite(ctx context.Context, query string, params QueryParams) error

	// ExecuteTypedQuery runs a query and returns typed results
	ExecuteTypedQuery(ctx context.Context, query string, params QueryParams) (*QueryResult, error)

	// HealthCheck verifies the graph store connection is healthy
	HealthCheck(ctx context.Context) error

	// BeginTransaction starts a new transaction
	BeginTransaction(ctx context.Context) (Transaction, error)
}

// ResultIterator provides iteration over query results
type ResultIterator interface {
	// Next advances to the next record, returns false when no more records
	Next(ctx context.Context) bool

	// Record returns the current record wrapped in GraphRecord for type-safe access
	Record() *GraphRecord

	// Node retrieves a node from the current record by key
	Node(key string) (*GraphNode, error)

	// Relationship retrieves a relationship from the current record by key
	Relationship(key string) (*GraphRelationship, error)

	// Path retrieves a path from the current record by key
	Path(key string) (*GraphPath, error)

	// Err returns any error that occurred during iteration
	Err() error

	// Close releases resources associated with the iterator
	Close(ctx context.Context) error
}

// Transaction represents a graph database transaction
type Transaction interface {
	// ExecuteQuery runs a query within the transaction
	ExecuteQuery(ctx context.Context, query string, params QueryParams) (ResultIterator, error)

	// ExecuteWrite runs a write operation within the transaction
	ExecuteWrite(ctx context.Context, query string, params QueryParams) error

	// ExecuteTypedQuery runs a query and returns typed results within the transaction
	ExecuteTypedQuery(ctx context.Context, query string, params QueryParams) (*QueryResult, error)

	// Commit commits the transaction
	Commit(ctx context.Context) error

	// Rollback rolls back the transaction
	Rollback(ctx context.Context) error
}

// GraphRecord represents a single record from a graph query
// It provides type-safe access to graph data while maintaining internal flexibility
type GraphRecord struct {
	// data holds the raw record data - internal use only
	data map[string]interface{}
}

// NewGraphRecord creates a new GraphRecord from raw data
func NewGraphRecord(data map[string]interface{}) *GraphRecord {
	return &GraphRecord{data: data}
}

// Get retrieves a value from the record
func (r *GraphRecord) Get(key string) (interface{}, bool) {
	if r == nil || r.data == nil {
		return nil, false
	}
	val, ok := r.data[key]
	return val, ok
}

// GetString retrieves a string value from the record
func (r *GraphRecord) GetString(key string) (string, bool) {
	val, ok := r.Get(key)
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetInt retrieves an int value from the record
func (r *GraphRecord) GetInt(key string) (int64, bool) {
	val, ok := r.Get(key)
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	default:
		return 0, false
	}
}

// GetBool retrieves a bool value from the record
func (r *GraphRecord) GetBool(key string) (bool, bool) {
	val, ok := r.Get(key)
	if !ok {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}

// GetNode retrieves a node from the record
func (r *GraphRecord) GetNode(key string) (*GraphNode, error) {
	val, ok := r.Get(key)
	if !ok {
		return nil, ErrNodeNotFound("GraphNode", key)
	}

	// Handle the case where the value is already a map
	if nodeMap, ok := val.(map[string]interface{}); ok {
		return ParseNodeFromRecord(map[string]interface{}{key: nodeMap}, key)
	}

	return nil, ErrParsingFailed("GraphNode", nil)
}

// GetNodes retrieves a slice of nodes from the record
func (r *GraphRecord) GetNodes(key string) ([]GraphNode, error) {
	val, ok := r.Get(key)
	if !ok {
		return nil, nil // Return empty slice if key doesn't exist
	}

	slice, ok := val.([]interface{})
	if !ok {
		return nil, ErrParsingFailed("[]GraphNode", nil)
	}

	nodes := make([]GraphNode, 0, len(slice))
	for i, item := range slice {
		if nodeMap, ok := item.(map[string]interface{}); ok {
			node, err := ParseNodeFromRecord(map[string]interface{}{
				"item": nodeMap,
			}, "item")
			if err != nil {
				return nil, fmt.Errorf("failed to parse node at index %d: %w", i, err)
			}
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

// GetRelationship retrieves a relationship from the record
func (r *GraphRecord) GetRelationship(key string) (*GraphRelationship, error) {
	val, ok := r.Get(key)
	if !ok {
		return nil, ErrNodeNotFound("GraphRelationship", key)
	}

	relMap, ok := val.(map[string]interface{})
	if !ok {
		return nil, ErrParsingFailed("GraphRelationship", nil)
	}

	rel := &GraphRelationship{
		Properties: make(map[string]interface{}),
	}

	if id, ok := relMap["id"].(int64); ok {
		rel.ID = id
	}
	if relType, ok := relMap["type"].(string); ok {
		rel.Type = RelationshipType(relType)
	}
	if startNode, ok := relMap["startNode"].(int64); ok {
		rel.StartNode = startNode
	}
	if endNode, ok := relMap["endNode"].(int64); ok {
		rel.EndNode = endNode
	}
	if props, ok := relMap["properties"].(map[string]interface{}); ok {
		rel.Properties = props
	}

	return rel, nil
}

// GetPath retrieves a path from the record
func (r *GraphRecord) GetPath(key string) (*GraphPath, error) {
	val, ok := r.Get(key)
	if !ok {
		return nil, ErrNodeNotFound("GraphPath", key)
	}

	pathMap, ok := val.(map[string]interface{})
	if !ok {
		return nil, ErrParsingFailed("GraphPath", nil)
	}

	path := &GraphPath{}

	// Parse nodes
	if nodesVal, ok := pathMap["nodes"]; ok {
		if nodeSlice, ok := nodesVal.([]interface{}); ok {
			for _, nodeData := range nodeSlice {
				if nodeMap, ok := nodeData.(map[string]interface{}); ok {
					node, err := ParseNodeFromRecord(map[string]interface{}{"node": nodeMap}, "node")
					if err == nil && node != nil {
						path.Nodes = append(path.Nodes, *node)
					}
				}
			}
		}
	}

	// Parse relationships
	if relsVal, ok := pathMap["relationships"]; ok {
		if relSlice, ok := relsVal.([]interface{}); ok {
			for _, relData := range relSlice {
				if relMap, ok := relData.(map[string]interface{}); ok {
					rel := GraphRelationship{
						Properties: make(map[string]interface{}),
					}
					if id, ok := relMap["id"].(int64); ok {
						rel.ID = id
					}
					if relType, ok := relMap["type"].(string); ok {
						rel.Type = RelationshipType(relType)
					}
					path.Relationships = append(path.Relationships, rel)
				}
			}
		}
	}

	path.Length = len(path.Nodes)
	return path, nil
}

// RawData returns the underlying raw data for backward compatibility
// This should only be used during migration and should be removed in future versions
func (r *GraphRecord) RawData() map[string]interface{} {
	if r == nil {
		return nil
	}
	return r.data
}
