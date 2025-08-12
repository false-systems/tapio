package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"
)

// Client wraps Neo4j operations for correlation queries
type Client struct {
	driver neo4j.DriverWithContext
	logger *zap.Logger
	config Config
}

// Config holds Neo4j configuration
type Config struct {
	URI      string
	Username string
	Password string
	Database string
}

// NewClient creates a new Neo4j client
func NewClient(config Config, logger *zap.Logger) (*Client, error) {
	driver, err := neo4j.NewDriverWithContext(
		config.URI,
		neo4j.BasicAuth(config.Username, config.Password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify connectivity: %w", err)
	}

	return &Client{
		driver: driver,
		logger: logger,
		config: config,
	}, nil
}

// Close closes the driver
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// ExecuteQuery runs a Cypher query with typed parameters
func (c *Client) ExecuteQuery(ctx context.Context, query string, params QueryParams) (*QueryResult, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: c.config.Database,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, params.ToMap())
	if err != nil {
		return nil, fmt.Errorf("failed to run query: %w", err)
	}

	var records []Record
	for result.Next(ctx) {
		values := result.Record().Values
		keys := result.Record().Keys

		// Create typed record from raw values
		record := c.parseRecord(keys, values)
		records = append(records, record)
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return &QueryResult{
		Records: records,
		Summary: c.extractSummary(result),
	}, nil
}

// ExecuteWrite runs a write transaction
func (c *Client) ExecuteWrite(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
	if c.driver == nil {
		return fmt.Errorf("neo4j driver not initialized")
	}

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: c.config.Database,
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return nil, fn(tx)
	})

	return err
}

// CreateIndexes creates required indexes for performance
func (c *Client) CreateIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.uid)",
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.namespace, p.name)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Service) ON (s.namespace, s.name)",
		"CREATE INDEX IF NOT EXISTS FOR (d:Deployment) ON (d.namespace, d.name)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.timestamp)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.traceId)",
	}

	for _, index := range indexes {
		if err := c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
			_, err := tx.Run(ctx, index, nil)
			return err
		}); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// parseRecord converts raw Neo4j record values to typed Record
func (c *Client) parseRecord(keys []string, values []any) Record {
	record := Record{
		StringValues: make(map[string]string),
		IntValues:    make(map[string]int64),
		FloatValues:  make(map[string]float64),
		BoolValues:   make(map[string]bool),
	}

	for i, key := range keys {
		if i >= len(values) {
			continue
		}

		value := values[i]
		switch v := value.(type) {
		case string:
			record.StringValues[key] = v
		case int64:
			record.IntValues[key] = v
		case int:
			record.IntValues[key] = int64(v)
		case float64:
			record.FloatValues[key] = v
		case bool:
			record.BoolValues[key] = v
		case neo4j.Node:
			// Handle Neo4j node - extract properties and parse by labels
			c.parseNodeToRecord(&record, v)
		case neo4j.Relationship:
			// Handle Neo4j relationship
			c.parseRelationshipToRecord(&record, v)
		default:
			// For complex types, convert to string
			if v != nil {
				record.StringValues[key] = fmt.Sprintf("%v", v)
			}
		}
	}

	return record
}

// parseNodeToRecord extracts typed data from Neo4j node
func (c *Client) parseNodeToRecord(record *Record, node neo4j.Node) {
	props := node.Props
	labels := node.Labels

	// Determine node type and parse accordingly
	for _, label := range labels {
		switch label {
		case "Resource", "Pod", "Service", "Deployment", "ConfigMap":
			if record.Resource == nil {
				record.Resource = c.parseResourceNode(props, labels)
			}
		case "Event":
			if record.Event == nil {
				record.Event = c.parseEventNode(props)
			}
		case "Correlation":
			if record.Correlation == nil {
				record.Correlation = c.parseCorrelationNode(props)
			}
		}
	}
}

// parseRelationshipToRecord extracts typed data from Neo4j relationship
func (c *Client) parseRelationshipToRecord(record *Record, rel neo4j.Relationship) {
	relationship := Relationship{
		Type:        RelationType(rel.Type),
		StartNodeID: fmt.Sprintf("%d", rel.StartId),
		EndNodeID:   fmt.Sprintf("%d", rel.EndId),
		Properties:  c.parseRelationshipProperties(rel.Props),
	}
	record.Relationships = append(record.Relationships, relationship)
}

// parseResourceNode creates ResourceNode from properties
func (c *Client) parseResourceNode(props map[string]any, labels []string) *ResourceNode {
	resource := &ResourceNode{
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	if uid, ok := props["uid"].(string); ok {
		resource.UID = uid
	}
	if name, ok := props["name"].(string); ok {
		resource.Name = name
	}
	if namespace, ok := props["namespace"].(string); ok {
		resource.Namespace = namespace
	}
	if resourceType, ok := props["type"].(string); ok {
		resource.Type = resourceType
	}
	if ready, ok := props["ready"].(bool); ok {
		resource.Ready = ready
	}

	// Parse time fields
	if createdAt, ok := props["created_at"].(int64); ok {
		resource.CreatedAt = time.Unix(createdAt, 0)
	}
	if updatedAt, ok := props["updated_at"].(int64); ok {
		resource.UpdatedAt = time.Unix(updatedAt, 0)
	}

	// Determine kind from labels
	for _, label := range labels {
		if label != "Resource" {
			resource.Kind = label
			break
		}
	}

	return resource
}

// parseEventNode creates EventNode from properties
func (c *Client) parseEventNode(props map[string]any) *EventNode {
	event := &EventNode{
		Metadata: make(map[string]string),
	}

	if id, ok := props["id"].(string); ok {
		event.ID = id
	}
	if eventType, ok := props["type"].(string); ok {
		event.Type = eventType
	}
	if source, ok := props["source"].(string); ok {
		event.Source = source
	}
	if message, ok := props["message"].(string); ok {
		event.Message = message
	}
	if traceID, ok := props["trace_id"].(string); ok {
		event.TraceID = traceID
	}

	// Parse timestamp
	if timestamp, ok := props["timestamp"].(int64); ok {
		event.Timestamp = time.Unix(timestamp, 0)
	}

	return event
}

// parseCorrelationNode creates CorrelationNode from properties
func (c *Client) parseCorrelationNode(props map[string]any) *CorrelationNode {
	corr := &CorrelationNode{
		Evidence: make([]string, 0),
	}

	if id, ok := props["id"].(string); ok {
		corr.ID = id
	}
	if corrType, ok := props["type"].(string); ok {
		corr.Type = corrType
	}
	if confidence, ok := props["confidence"].(float64); ok {
		corr.Confidence = confidence
	}
	if summary, ok := props["summary"].(string); ok {
		corr.Summary = summary
	}

	// Parse time fields
	if createdAt, ok := props["created_at"].(int64); ok {
		corr.CreatedAt = time.Unix(createdAt, 0)
	}
	if startTime, ok := props["start_time"].(int64); ok {
		corr.StartTime = time.Unix(startTime, 0)
	}
	if endTime, ok := props["end_time"].(int64); ok {
		corr.EndTime = time.Unix(endTime, 0)
	}

	return corr
}

// parseRelationshipProperties creates RelationshipProperties from raw properties
func (c *Client) parseRelationshipProperties(props map[string]any) RelationshipProperties {
	relProps := RelationshipProperties{}

	if createdAt, ok := props["created_at"].(int64); ok {
		relProps.CreatedAt = time.Unix(createdAt, 0)
	}
	if weight, ok := props["weight"].(float64); ok {
		relProps.Weight = weight
	}
	if confidence, ok := props["confidence"].(float64); ok {
		relProps.Confidence = confidence
	}
	if port, ok := props["port"].(int64); ok {
		relProps.Port = int32(port)
	}

	return relProps
}

// extractSummary creates Summary from Neo4j result
func (c *Client) extractSummary(result neo4j.ResultWithContext) Summary {
	// Neo4j driver doesn't expose summary for read transactions
	// This would be populated for write transactions
	return Summary{}
}
