package integrations

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/neo4j"
)

// GraphStorageAdapter implements domain.GraphStorage using neo4j.Client
type GraphStorageAdapter struct {
	client *neo4j.Client
}

// NewGraphStorageAdapter creates a new adapter
func NewGraphStorageAdapter(client *neo4j.Client) *GraphStorageAdapter {
	return &GraphStorageAdapter{client: client}
}

// CreateOrUpdateNode creates or updates a node from a unified event
func (a *GraphStorageAdapter) CreateOrUpdateNode(ctx context.Context, event *domain.UnifiedEvent) error {
	// Convert UnifiedEvent to node properties
	params := neo4j.QueryParams{
		StringParams: map[string]string{
			"uid":       event.Entity.UID,
			"name":      event.Entity.Name,
			"namespace": event.Entity.Namespace,
			"kind":      event.Entity.Type,
		},
	}

	query := `
		MERGE (n:Entity {uid: $uid})
		SET n.name = $name,
		    n.namespace = $namespace,
		    n.kind = $kind,
		    n.updated = datetime()
	`

	_, err := a.client.ExecuteQuery(ctx, query, params)
	return err
}

// CreateEvent creates an event node
func (a *GraphStorageAdapter) CreateEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	params := neo4j.QueryParams{
		StringParams: map[string]string{
			"id":      event.ID,
			"type":    string(event.Type),
			"source":  event.Source,
			"traceId": event.TraceContext.TraceID,
		},
	}

	query := `
		CREATE (e:Event {
			id: $id,
			type: $type,
			source: $source,
			traceId: $traceId,
			timestamp: datetime()
		})
	`

	_, err := a.client.ExecuteQuery(ctx, query, params)
	return err
}

// CreateRelationship creates a relationship between nodes
func (a *GraphStorageAdapter) CreateRelationship(ctx context.Context, fromUID, toUID, relType string, properties map[string]interface{}) error {
	// Convert properties to PropertyValue map - CLAUDE.md compliant, no interface{} abuse
	propValues := make(map[string]neo4j.PropertyValue)
	for key, value := range properties {
		pv := neo4j.PropertyValue{}
		switch v := value.(type) {
		case string:
			pv.StringVal = &v
		case int:
			i64 := int64(v)
			pv.IntVal = &i64
		case int64:
			pv.IntVal = &v
		case float64:
			pv.FloatVal = &v
		case bool:
			pv.BoolVal = &v
		default:
			// Convert to string for unknown types
			s := fmt.Sprintf("%v", v)
			pv.StringVal = &s
		}
		propValues[key] = pv
	}

	return a.client.CreateRelationship(ctx, fromUID, toUID, neo4j.RelationType(relType), propValues)
}

// CreateEventRelationship creates event-entity relationship
func (a *GraphStorageAdapter) CreateEventRelationship(ctx context.Context, eventID, entityUID, relType string) error {
	params := neo4j.QueryParams{
		StringParams: map[string]string{
			"eventId":   eventID,
			"entityUid": entityUID,
		},
	}

	query := fmt.Sprintf(`
		MATCH (e:Event {id: $eventId})
		MATCH (n:Entity {uid: $entityUid})
		CREATE (e)-[:%s]->(n)
	`, relType)

	_, err := a.client.ExecuteQuery(ctx, query, params)
	return err
}

// ExecuteQuery executes a query with generic params
func (a *GraphStorageAdapter) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	// Convert map[string]interface{} to QueryParams
	queryParams := neo4j.QueryParams{
		StringParams: make(map[string]string),
		IntParams:    make(map[string]int64),
		FloatParams:  make(map[string]float64),
		BoolParams:   make(map[string]bool),
	}

	// Sort params into typed maps
	for key, value := range params {
		switch v := value.(type) {
		case string:
			queryParams.StringParams[key] = v
		case int:
			queryParams.IntParams[key] = int64(v)
		case int64:
			queryParams.IntParams[key] = v
		case float64:
			queryParams.FloatParams[key] = v
		case bool:
			queryParams.BoolParams[key] = v
		default:
			queryParams.StringParams[key] = fmt.Sprintf("%v", v)
		}
	}

	// Execute query
	result, err := a.client.ExecuteQuery(ctx, query, queryParams)
	if err != nil {
		return nil, err
	}

	// Convert QueryResult to []map[string]interface{} - properly handle typed fields
	var results []map[string]interface{}
	for _, record := range result.Records {
		recordMap := make(map[string]interface{})

		// Extract values from typed fields (CLAUDE.md: no raw interface{})
		for k, v := range record.StringValues {
			recordMap[k] = v
		}
		for k, v := range record.IntValues {
			recordMap[k] = v
		}
		for k, v := range record.FloatValues {
			recordMap[k] = v
		}
		for k, v := range record.BoolValues {
			recordMap[k] = v
		}

		results = append(results, recordMap)
	}

	return results, nil
}

// CreateIndexes creates database indexes
func (a *GraphStorageAdapter) CreateIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.uid)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.type)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.traceId)",
	}

	for _, index := range indexes {
		if _, err := a.client.ExecuteQuery(ctx, index, neo4j.QueryParams{}); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// Health checks database connectivity
func (a *GraphStorageAdapter) Health(ctx context.Context) error {
	_, err := a.client.ExecuteQuery(ctx, "RETURN 1", neo4j.QueryParams{})
	return err
}

// Close closes the database connection
func (a *GraphStorageAdapter) Close(ctx context.Context) error {
	return a.client.Close(ctx)
}
