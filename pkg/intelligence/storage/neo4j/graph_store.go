package neo4j

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// GraphStore implements the correlation.GraphStore interface for Neo4j
type GraphStore struct {
	driver neo4j.DriverWithContext
}

// NewGraphStore creates a new Neo4j graph store implementation
func NewGraphStore(driver neo4j.DriverWithContext) (*GraphStore, error) {
	if driver == nil {
		return nil, fmt.Errorf("neo4j driver is required")
	}
	return &GraphStore{
		driver: driver,
	}, nil
}

// ExecuteQuery runs a graph query with parameters and returns results
func (g *GraphStore) ExecuteQuery(ctx context.Context, query string, params correlation.QueryParams) (correlation.ResultIterator, error) {
	if params != nil {
		if err := params.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	session := g.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	result, err := session.Run(ctx, query, paramMap)
	if err != nil {
		session.Close(ctx)
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return &neo4jResultIterator{
		result:  result,
		session: session,
	}, nil
}

// ExecuteWrite runs a write query (create, update, delete) with parameters
func (g *GraphStore) ExecuteWrite(ctx context.Context, query string, params correlation.QueryParams) error {
	if params != nil {
		if err := params.Validate(); err != nil {
			return fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	session := g.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	_, err := session.Run(ctx, query, paramMap)
	if err != nil {
		return fmt.Errorf("failed to execute write: %w", err)
	}

	return nil
}

// ExecuteTypedQuery runs a query and returns typed results
func (g *GraphStore) ExecuteTypedQuery(ctx context.Context, query string, params correlation.QueryParams) (*correlation.QueryResult, error) {
	if params != nil {
		if err := params.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	session := g.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	result, err := session.Run(ctx, query, paramMap)
	if err != nil {
		return nil, fmt.Errorf("failed to execute typed query: %w", err)
	}

	// Collect all results into typed format
	queryResult := &correlation.QueryResult{
		Nodes:         []correlation.GraphNode{},
		Relationships: []correlation.GraphRelationship{},
		Paths:         []correlation.GraphPath{},
		Scalars:       make(map[string]interface{}),
	}

	for result.Next(ctx) {
		record := result.Record()
		for _, key := range record.Keys {
			value, _ := record.Get(key)
			// Parse based on type
			switch v := value.(type) {
			case neo4j.Node:
				node := g.parseNode(v)
				if node != nil {
					queryResult.Nodes = append(queryResult.Nodes, *node)
				}
			case []interface{}:
				// Could be a list of nodes, relationships, or paths
				for _, item := range v {
					if n, ok := item.(neo4j.Node); ok {
						node := g.parseNode(n)
						if node != nil {
							queryResult.Nodes = append(queryResult.Nodes, *node)
						}
					}
				}
			default:
				// Store as scalar value
				queryResult.Scalars[key] = value
			}
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error processing query results: %w", err)
	}

	return queryResult, nil
}

// parseNode converts a Neo4j node to GraphNode
func (g *GraphStore) parseNode(node neo4j.Node) *correlation.GraphNode {
	graphNode := &correlation.GraphNode{
		ID:     node.GetId(),
		Labels: node.Labels,
	}

	// Parse properties
	props := correlation.NodeProperties{
		Metadata: make(map[string]string),
	}

	for k, v := range node.Props {
		switch k {
		case "uid":
			if uid, ok := v.(string); ok {
				graphNode.UID = uid
			}
		case "name":
			if name, ok := v.(string); ok {
				props.Name = name
			}
		case "namespace":
			if ns, ok := v.(string); ok {
				props.Namespace = ns
			}
		case "cluster":
			if cluster, ok := v.(string); ok {
				props.Cluster = cluster
			}
		case "ready":
			if ready, ok := v.(bool); ok {
				props.Ready = ready
			}
		case "phase":
			if phase, ok := v.(string); ok {
				props.Phase = phase
			}
		default:
			// Store other properties as metadata
			if str, ok := v.(string); ok {
				props.Metadata[k] = str
			}
		}
	}

	graphNode.Properties = props

	// Set node type from first label
	if len(node.Labels) > 0 {
		graphNode.Type = correlation.NodeType(node.Labels[0])
	}

	return graphNode
}

// HealthCheck verifies the graph store connection is healthy
func (g *GraphStore) HealthCheck(ctx context.Context) error {
	return g.driver.VerifyConnectivity(ctx)
}

// BeginTransaction starts a new transaction
func (g *GraphStore) BeginTransaction(ctx context.Context) (correlation.Transaction, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})

	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		session.Close(ctx)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &neo4jTransaction{
		tx:      tx,
		session: session,
	}, nil
}

// neo4jResultIterator wraps Neo4j result for the ResultIterator interface
type neo4jResultIterator struct {
	result  neo4j.ResultWithContext
	session neo4j.SessionWithContext
	current *correlation.GraphRecord
}

// Next advances to the next record, returns false when no more records
func (r *neo4jResultIterator) Next(ctx context.Context) bool {
	if !r.result.Next(ctx) {
		return false
	}

	// Convert Neo4j record to GraphRecord
	record := r.result.Record()
	r.current = correlation.NewGraphRecord(r.convertRecord(record))
	return true
}

// Record returns the current record wrapped in GraphRecord
func (r *neo4jResultIterator) Record() *correlation.GraphRecord {
	return r.current
}

// Node retrieves a node from the current record by key
func (r *neo4jResultIterator) Node(key string) (*correlation.GraphNode, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetNode(key)
}

// Relationship retrieves a relationship from the current record by key
func (r *neo4jResultIterator) Relationship(key string) (*correlation.GraphRelationship, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetRelationship(key)
}

// Path retrieves a path from the current record by key
func (r *neo4jResultIterator) Path(key string) (*correlation.GraphPath, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetPath(key)
}

// Err returns any error that occurred during iteration
func (r *neo4jResultIterator) Err() error {
	return r.result.Err()
}

// Close releases resources associated with the iterator
func (r *neo4jResultIterator) Close(ctx context.Context) error {
	// Close the session which will also close the result
	return r.session.Close(ctx)
}

// convertRecord converts a Neo4j record to map[string]interface{}
func (r *neo4jResultIterator) convertRecord(record *neo4j.Record) map[string]interface{} {
	result := make(map[string]interface{})

	for _, key := range record.Keys {
		value, _ := record.Get(key)
		result[key] = r.convertValue(value)
	}

	return result
}

// convertValue converts Neo4j values to standard Go types
func (r *neo4jResultIterator) convertValue(value interface{}) interface{} {
	switch v := value.(type) {
	case neo4j.Node:
		// Convert Neo4j Node to map with properties
		return map[string]interface{}{
			"id":         v.GetId(),
			"labels":     v.Labels,
			"properties": v.Props,
		}
	case neo4j.Relationship:
		// Convert Neo4j Relationship to map
		return map[string]interface{}{
			"id":         v.GetId(),
			"type":       v.Type,
			"startId":    v.StartId,
			"endId":      v.EndId,
			"properties": v.Props,
		}
	case neo4j.Path:
		// Convert Neo4j Path to map
		nodes := make([]interface{}, len(v.Nodes))
		for i, node := range v.Nodes {
			nodes[i] = r.convertValue(node)
		}
		relationships := make([]interface{}, len(v.Relationships))
		for i, rel := range v.Relationships {
			relationships[i] = r.convertValue(rel)
		}
		return map[string]interface{}{
			"nodes":         nodes,
			"relationships": relationships,
		}
	case []interface{}:
		// Recursively convert slice elements
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = r.convertValue(item)
		}
		return result
	case map[string]interface{}:
		// Recursively convert map values
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = r.convertValue(val)
		}
		return result
	default:
		// Return primitive values as-is
		return value
	}
}

// neo4jTransaction wraps Neo4j transaction for the Transaction interface
type neo4jTransaction struct {
	tx      neo4j.ExplicitTransaction
	session neo4j.SessionWithContext
}

// ExecuteQuery runs a query within the transaction
func (t *neo4jTransaction) ExecuteQuery(ctx context.Context, query string, params correlation.QueryParams) (correlation.ResultIterator, error) {
	if params != nil {
		if err := params.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	result, err := t.tx.Run(ctx, query, paramMap)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query in transaction: %w", err)
	}

	return &neo4jTransactionResultIterator{
		result:  result,
		tx:      t.tx,
		session: t.session,
	}, nil
}

// ExecuteWrite runs a write operation within the transaction
func (t *neo4jTransaction) ExecuteWrite(ctx context.Context, query string, params correlation.QueryParams) error {
	if params != nil {
		if err := params.Validate(); err != nil {
			return fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	_, err := t.tx.Run(ctx, query, paramMap)
	if err != nil {
		return fmt.Errorf("failed to execute write in transaction: %w", err)
	}
	return nil
}

// ExecuteTypedQuery runs a query and returns typed results within the transaction
func (t *neo4jTransaction) ExecuteTypedQuery(ctx context.Context, query string, params correlation.QueryParams) (*correlation.QueryResult, error) {
	if params != nil {
		if err := params.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query parameters: %w", err)
		}
	}

	// Convert typed params to map for Neo4j driver
	var paramMap map[string]interface{}
	if params != nil {
		paramMap = params.ToMap()
	} else {
		paramMap = make(map[string]interface{})
	}

	result, err := t.tx.Run(ctx, query, paramMap)
	if err != nil {
		return nil, fmt.Errorf("failed to execute typed query in transaction: %w", err)
	}

	// Collect all results into typed format
	queryResult := &correlation.QueryResult{
		Nodes:         []correlation.GraphNode{},
		Relationships: []correlation.GraphRelationship{},
		Paths:         []correlation.GraphPath{},
		Scalars:       make(map[string]interface{}),
	}

	for result.Next(ctx) {
		record := result.Record()
		for _, key := range record.Keys {
			value, _ := record.Get(key)
			// Parse based on type
			switch v := value.(type) {
			case neo4j.Node:
				node := t.parseNode(v)
				if node != nil {
					queryResult.Nodes = append(queryResult.Nodes, *node)
				}
			case []interface{}:
				// Could be a list of nodes, relationships, or paths
				for _, item := range v {
					if n, ok := item.(neo4j.Node); ok {
						node := t.parseNode(n)
						if node != nil {
							queryResult.Nodes = append(queryResult.Nodes, *node)
						}
					}
				}
			default:
				// Store as scalar value
				queryResult.Scalars[key] = value
			}
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error processing query results: %w", err)
	}

	return queryResult, nil
}

// parseNode converts a Neo4j node to GraphNode (helper for transaction)
func (t *neo4jTransaction) parseNode(node neo4j.Node) *correlation.GraphNode {
	graphNode := &correlation.GraphNode{
		ID:     node.GetId(),
		Labels: node.Labels,
	}

	// Parse properties
	props := correlation.NodeProperties{
		Metadata: make(map[string]string),
	}

	for k, v := range node.Props {
		switch k {
		case "uid":
			if uid, ok := v.(string); ok {
				graphNode.UID = uid
			}
		case "name":
			if name, ok := v.(string); ok {
				props.Name = name
			}
		case "namespace":
			if ns, ok := v.(string); ok {
				props.Namespace = ns
			}
		case "cluster":
			if cluster, ok := v.(string); ok {
				props.Cluster = cluster
			}
		case "ready":
			if ready, ok := v.(bool); ok {
				props.Ready = ready
			}
		case "phase":
			if phase, ok := v.(string); ok {
				props.Phase = phase
			}
		default:
			// Store other properties as metadata
			if str, ok := v.(string); ok {
				props.Metadata[k] = str
			}
		}
	}

	graphNode.Properties = props

	// Set node type from first label
	if len(node.Labels) > 0 {
		graphNode.Type = correlation.NodeType(node.Labels[0])
	}

	return graphNode
}

// Commit commits the transaction
func (t *neo4jTransaction) Commit(ctx context.Context) error {
	err := t.tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	// Close session after commit
	return t.session.Close(ctx)
}

// Rollback rolls back the transaction
func (t *neo4jTransaction) Rollback(ctx context.Context) error {
	err := t.tx.Rollback(ctx)
	if err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	// Close session after rollback
	return t.session.Close(ctx)
}

// neo4jTransactionResultIterator wraps Neo4j result within a transaction
type neo4jTransactionResultIterator struct {
	result  neo4j.ResultWithContext
	tx      neo4j.ExplicitTransaction
	session neo4j.SessionWithContext
	current *correlation.GraphRecord
}

// Next advances to the next record, returns false when no more records
func (r *neo4jTransactionResultIterator) Next(ctx context.Context) bool {
	if !r.result.Next(ctx) {
		return false
	}

	// Convert Neo4j record to GraphRecord
	record := r.result.Record()
	r.current = correlation.NewGraphRecord(r.convertRecord(record))
	return true
}

// Record returns the current record wrapped in GraphRecord
func (r *neo4jTransactionResultIterator) Record() *correlation.GraphRecord {
	return r.current
}

// Node retrieves a node from the current record by key
func (r *neo4jTransactionResultIterator) Node(key string) (*correlation.GraphNode, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetNode(key)
}

// Relationship retrieves a relationship from the current record by key
func (r *neo4jTransactionResultIterator) Relationship(key string) (*correlation.GraphRelationship, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetRelationship(key)
}

// Path retrieves a path from the current record by key
func (r *neo4jTransactionResultIterator) Path(key string) (*correlation.GraphPath, error) {
	if r.current == nil {
		return nil, fmt.Errorf("no current record")
	}
	return r.current.GetPath(key)
}

// Err returns any error that occurred during iteration
func (r *neo4jTransactionResultIterator) Err() error {
	return r.result.Err()
}

// Close releases resources associated with the iterator
func (r *neo4jTransactionResultIterator) Close(ctx context.Context) error {
	// In transaction context, we don't close the session/tx here
	// They will be closed when the transaction is committed/rolled back
	return nil
}

// convertRecord converts a Neo4j record to map[string]interface{}
func (r *neo4jTransactionResultIterator) convertRecord(record *neo4j.Record) map[string]interface{} {
	result := make(map[string]interface{})

	for _, key := range record.Keys {
		value, _ := record.Get(key)
		result[key] = r.convertValue(value)
	}

	return result
}

// convertValue converts Neo4j values to standard Go types
func (r *neo4jTransactionResultIterator) convertValue(value interface{}) interface{} {
	switch v := value.(type) {
	case neo4j.Node:
		// Convert Neo4j Node to map with properties
		return map[string]interface{}{
			"id":         v.GetId(),
			"labels":     v.Labels,
			"properties": v.Props,
		}
	case neo4j.Relationship:
		// Convert Neo4j Relationship to map
		return map[string]interface{}{
			"id":         v.GetId(),
			"type":       v.Type,
			"startId":    v.StartId,
			"endId":      v.EndId,
			"properties": v.Props,
		}
	case neo4j.Path:
		// Convert Neo4j Path to map
		nodes := make([]interface{}, len(v.Nodes))
		for i, node := range v.Nodes {
			nodes[i] = r.convertValue(node)
		}
		relationships := make([]interface{}, len(v.Relationships))
		for i, rel := range v.Relationships {
			relationships[i] = r.convertValue(rel)
		}
		return map[string]interface{}{
			"nodes":         nodes,
			"relationships": relationships,
		}
	case []interface{}:
		// Recursively convert slice elements
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = r.convertValue(item)
		}
		return result
	case map[string]interface{}:
		// Recursively convert map values
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = r.convertValue(val)
		}
		return result
	default:
		// Return primitive values as-is
		return value
	}
}
