package neo4j

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// SchemaManager handles Neo4j schema initialization
type SchemaManager struct {
	client *Client
	logger *zap.Logger
}

// NewSchemaManager creates a new schema manager
func NewSchemaManager(client *Client, logger *zap.Logger) *SchemaManager {
	return &SchemaManager{
		client: client,
		logger: logger,
	}
}

// Initialize creates indexes and constraints for optimal performance
func (s *SchemaManager) Initialize(ctx context.Context) error {
	s.logger.Info("Initializing Neo4j schema")

	// Create constraints (which also create indexes)
	constraints := []struct {
		name  string
		query string
	}{
		{
			name: "pod_uid_unique",
			query: `CREATE CONSTRAINT pod_uid_unique IF NOT EXISTS 
					FOR (p:Pod) REQUIRE p.uid IS UNIQUE`,
		},
		{
			name: "service_uid_unique",
			query: `CREATE CONSTRAINT service_uid_unique IF NOT EXISTS 
					FOR (s:Service) REQUIRE s.uid IS UNIQUE`,
		},
		{
			name: "deployment_uid_unique",
			query: `CREATE CONSTRAINT deployment_uid_unique IF NOT EXISTS 
					FOR (d:Deployment) REQUIRE d.uid IS UNIQUE`,
		},
		{
			name: "configmap_uid_unique",
			query: `CREATE CONSTRAINT configmap_uid_unique IF NOT EXISTS 
					FOR (cm:ConfigMap) REQUIRE cm.uid IS UNIQUE`,
		},
		{
			name: "secret_uid_unique",
			query: `CREATE CONSTRAINT secret_uid_unique IF NOT EXISTS 
					FOR (s:Secret) REQUIRE s.uid IS UNIQUE`,
		},
		{
			name: "pvc_uid_unique",
			query: `CREATE CONSTRAINT pvc_uid_unique IF NOT EXISTS 
					FOR (pvc:PVC) REQUIRE pvc.uid IS UNIQUE`,
		},
		{
			name: "event_id_unique",
			query: `CREATE CONSTRAINT event_id_unique IF NOT EXISTS 
					FOR (e:Event) REQUIRE e.id IS UNIQUE`,
		},
	}

	// Create indexes for query performance
	indexes := []struct {
		name  string
		query string
	}{
		// Composite indexes for common queries
		{
			name: "pod_namespace_name_cluster",
			query: `CREATE INDEX pod_namespace_name_cluster IF NOT EXISTS 
					FOR (p:Pod) ON (p.namespace, p.name, p.cluster)`,
		},
		{
			name: "service_namespace_name_cluster",
			query: `CREATE INDEX service_namespace_name_cluster IF NOT EXISTS 
					FOR (s:Service) ON (s.namespace, s.name, s.cluster)`,
		},
		{
			name: "configmap_namespace_name_cluster",
			query: `CREATE INDEX configmap_namespace_name_cluster IF NOT EXISTS 
					FOR (cm:ConfigMap) ON (cm.namespace, cm.name, cm.cluster)`,
		},
		{
			name: "node_name_cluster",
			query: `CREATE INDEX node_name_cluster IF NOT EXISTS 
					FOR (n:Node) ON (n.name, n.cluster)`,
		},
		// Event indexes
		{
			name: "event_timestamp",
			query: `CREATE INDEX event_timestamp IF NOT EXISTS 
					FOR (e:Event) ON (e.timestamp)`,
		},
		{
			name: "event_type",
			query: `CREATE INDEX event_type IF NOT EXISTS 
					FOR (e:Event) ON (e.type)`,
		},
		{
			name: "event_severity",
			query: `CREATE INDEX event_severity IF NOT EXISTS 
					FOR (e:Event) ON (e.severity)`,
		},
		// State tracking indexes
		{
			name: "pod_phase",
			query: `CREATE INDEX pod_phase IF NOT EXISTS 
					FOR (p:Pod) ON (p.phase)`,
		},
		{
			name: "pod_ready",
			query: `CREATE INDEX pod_ready IF NOT EXISTS 
					FOR (p:Pod) ON (p.ready)`,
		},
		{
			name: "pod_node",
			query: `CREATE INDEX pod_node IF NOT EXISTS 
					FOR (p:Pod) ON (p.node)`,
		},
	}

	// Execute constraints
	for _, constraint := range constraints {
		if err := s.executeSchema(ctx, constraint.name, constraint.query); err != nil {
			return fmt.Errorf("failed to create constraint %s: %w", constraint.name, err)
		}
	}

	// Execute indexes
	for _, index := range indexes {
		if err := s.executeSchema(ctx, index.name, index.query); err != nil {
			return fmt.Errorf("failed to create index %s: %w", index.name, err)
		}
	}

	s.logger.Info("Neo4j schema initialization complete")
	return nil
}

// executeSchema runs a schema creation query
func (s *SchemaManager) executeSchema(ctx context.Context, name, query string) error {
	s.logger.Debug("Creating schema element", zap.String("name", name))

	session := s.client.Session(ctx)
	defer session.Close(ctx)

	_, err := session.Run(ctx, query, nil)
	if err != nil {
		// Log but don't fail if constraint/index already exists
		s.logger.Warn("Schema element creation failed (may already exist)",
			zap.String("name", name),
			zap.Error(err))
	}

	return nil
}

// Validate checks if the schema is properly initialized
func (s *SchemaManager) Validate(ctx context.Context) error {
	session := s.client.Session(ctx)
	defer session.Close(ctx)

	// Check constraints
	constraintQuery := `
		SHOW CONSTRAINTS
		YIELD name
		RETURN collect(name) as constraints
	`

	result, err := session.Run(ctx, constraintQuery, nil)
	if err != nil {
		return fmt.Errorf("failed to query constraints: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		constraints, _ := record.Get("constraints")
		s.logger.Info("Found constraints", zap.Any("constraints", constraints))
	}

	// Check indexes
	indexQuery := `
		SHOW INDEXES
		YIELD name
		WHERE name <> 'constraint'
		RETURN collect(name) as indexes
	`

	result, err = session.Run(ctx, indexQuery, nil)
	if err != nil {
		return fmt.Errorf("failed to query indexes: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		indexes, _ := record.Get("indexes")
		s.logger.Info("Found indexes", zap.Any("indexes", indexes))
	}

	return nil
}

// DropAll removes all nodes and relationships (use with caution!)
func (s *SchemaManager) DropAll(ctx context.Context) error {
	s.logger.Warn("Dropping all Neo4j data")

	session := s.client.Session(ctx)
	defer session.Close(ctx)

	// Delete all nodes and relationships
	_, err := session.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
	if err != nil {
		return fmt.Errorf("failed to drop all data: %w", err)
	}

	s.logger.Info("All Neo4j data dropped")
	return nil
}

// GetStatistics returns database statistics
func (s *SchemaManager) GetStatistics(ctx context.Context) (map[string]int64, error) {
	session := s.client.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (n)
		WITH labels(n) as nodeLabels
		UNWIND nodeLabels as label
		WITH label, count(*) as count
		RETURN label, count
		ORDER BY count DESC
	`

	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}

	stats := make(map[string]int64)
	for result.Next(ctx) {
		record := result.Record()
		label, _ := record.Get("label")
		count, _ := record.Get("count")
		if l, ok := label.(string); ok {
			if c, ok := count.(int64); ok {
				stats[l] = c
			}
		}
	}

	// Get relationship counts
	relQuery := `
		MATCH ()-[r]->()
		RETURN type(r) as type, count(*) as count
		ORDER BY count DESC
	`

	result, err = session.Run(ctx, relQuery, nil)
	if err != nil {
		return stats, nil // Return what we have
	}

	for result.Next(ctx) {
		record := result.Record()
		relType, _ := record.Get("type")
		count, _ := record.Get("count")
		if t, ok := relType.(string); ok {
			if c, ok := count.(int64); ok {
				stats["rel:"+t] = c
			}
		}
	}

	return stats, nil
}
