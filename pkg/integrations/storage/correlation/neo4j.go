package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// Neo4jStorage implements correlation.Storage using Neo4j
type Neo4jStorage struct {
	driver neo4j.DriverWithContext
	logger *zap.Logger
	config Neo4jConfig
}

// Neo4jConfig holds Neo4j configuration
type Neo4jConfig struct {
	URI      string
	Username string
	Password string
	Database string
}

// NewNeo4jStorage creates a new Neo4j storage adapter
func NewNeo4jStorage(logger *zap.Logger, config Neo4jConfig) (*Neo4jStorage, error) {
	driver, err := neo4j.NewDriverWithContext(
		config.URI,
		neo4j.BasicAuth(config.Username, config.Password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	storage := &Neo4jStorage{
		driver: driver,
		logger: logger,
		config: config,
	}

	// Create indexes
	if err := storage.createIndexes(ctx); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	return storage, nil
}

// createIndexes creates necessary indexes for performance
func (s *Neo4jStorage) createIndexes(ctx context.Context) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.id)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.traceId)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.startTime)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.type)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.id)",
		"CREATE INDEX IF NOT EXISTS FOR (r:Resource) ON (r.type, r.namespace, r.name)",
	}

	for _, index := range indexes {
		if _, err := session.Run(ctx, index, nil); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// Store saves a correlation result
func (s *Neo4jStorage) Store(ctx context.Context, result *correlation.CorrelationResult) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	// Serialize evidence and details for storage
	evidenceJSON, _ := json.Marshal(result.Evidence)

	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Create the correlation node
	query := `
		CREATE (c:Correlation {
			id: $id,
			type: $type,
			confidence: $confidence,
			summary: $summary,
			details: $details,
			evidence: $evidence,
			startTime: datetime($startTime),
			endTime: datetime($endTime),
			traceId: $traceId,
			createdAt: datetime()
		})
	`

	params := map[string]interface{}{
		"id":         result.ID,
		"type":       result.Type,
		"confidence": result.Confidence,
		"summary":    result.Summary,
		"details":    result.Details,
		"evidence":   string(evidenceJSON),
		"startTime":  result.StartTime.Format(time.RFC3339),
		"endTime":    result.EndTime.Format(time.RFC3339),
		"traceId":    result.TraceID,
	}

	if _, err := tx.Run(ctx, query, params); err != nil {
		return fmt.Errorf("failed to create correlation node: %w", err)
	}

	// Create root cause if present
	if result.RootCause != nil {
		rootCauseQuery := `
			MATCH (c:Correlation {id: $correlationId})
			CREATE (rc:RootCause {
				eventId: $eventId,
				confidence: $confidence,
				description: $description,
				evidence: $evidence
			})
			CREATE (c)-[:HAS_ROOT_CAUSE]->(rc)
		`

		evidenceJSON, _ := json.Marshal(result.RootCause.Evidence)
		params := map[string]interface{}{
			"correlationId": result.ID,
			"eventId":       result.RootCause.EventID,
			"confidence":    result.RootCause.Confidence,
			"description":   result.RootCause.Description,
			"evidence":      string(evidenceJSON),
		}

		if _, err := tx.Run(ctx, rootCauseQuery, params); err != nil {
			return fmt.Errorf("failed to create root cause: %w", err)
		}
	}

	// Create impact relationships
	if result.Impact != nil && len(result.Impact.Resources) > 0 {
		for _, resourceName := range result.Impact.Resources {
			// Parse resource format (namespace/name)
			var namespace, name string
			if n, err := fmt.Sscanf(resourceName, "%s/%s", &namespace, &name); err == nil && n == 2 {
				resourceQuery := `
					MATCH (c:Correlation {id: $correlationId})
					MERGE (r:Resource {name: $name, namespace: $namespace})
					CREATE (c)-[:AFFECTS {severity: $severity}]->(r)
				`

				params := map[string]interface{}{
					"correlationId": result.ID,
					"name":          name,
					"namespace":     namespace,
					"severity":      string(result.Impact.Severity),
				}

				if _, err := tx.Run(ctx, resourceQuery, params); err != nil {
					s.logger.Warn("Failed to create resource relationship",
						zap.String("resource", resourceName),
						zap.Error(err))
				}
			}
		}
	}

	// Link correlated events
	for _, eventID := range result.Events {
		eventQuery := `
			MATCH (c:Correlation {id: $correlationId})
			MERGE (e:Event {id: $eventId})
			CREATE (c)-[:INCLUDES_EVENT]->(e)
		`

		params := map[string]interface{}{
			"correlationId": result.ID,
			"eventId":       eventID,
		}

		if _, err := tx.Run(ctx, eventQuery, params); err != nil {
			s.logger.Warn("Failed to link event",
				zap.String("event", eventID),
				zap.Error(err))
		}
	}

	return tx.Commit(ctx)
}

// GetRecent retrieves recent correlations
func (s *Neo4jStorage) GetRecent(ctx context.Context, limit int) ([]*correlation.CorrelationResult, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	query := `
		MATCH (c:Correlation)
		OPTIONAL MATCH (c)-[:HAS_ROOT_CAUSE]->(rc:RootCause)
		OPTIONAL MATCH (c)-[:AFFECTS]->(r:Resource)
		OPTIONAL MATCH (c)-[:INCLUDES_EVENT]->(e:Event)
		RETURN c, rc, collect(DISTINCT r) as resources, collect(DISTINCT e.id) as events
		ORDER BY c.startTime DESC
		LIMIT $limit
	`

	result, err := session.Run(ctx, query, map[string]interface{}{"limit": limit})
	if err != nil {
		return nil, fmt.Errorf("failed to query correlations: %w", err)
	}

	return s.parseResults(result)
}

// GetByTraceID retrieves correlations for a specific trace
func (s *Neo4jStorage) GetByTraceID(ctx context.Context, traceID string) ([]*correlation.CorrelationResult, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	query := `
		MATCH (c:Correlation {traceId: $traceId})
		OPTIONAL MATCH (c)-[:HAS_ROOT_CAUSE]->(rc:RootCause)
		OPTIONAL MATCH (c)-[:AFFECTS]->(r:Resource)
		OPTIONAL MATCH (c)-[:INCLUDES_EVENT]->(e:Event)
		RETURN c, rc, collect(DISTINCT r) as resources, collect(DISTINCT e.id) as events
		ORDER BY c.startTime DESC
	`

	result, err := session.Run(ctx, query, map[string]interface{}{"traceId": traceID})
	if err != nil {
		return nil, fmt.Errorf("failed to query by trace ID: %w", err)
	}

	return s.parseResults(result)
}

// GetByTimeRange retrieves correlations within a time range
func (s *Neo4jStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*correlation.CorrelationResult, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	query := `
		MATCH (c:Correlation)
		WHERE c.startTime >= datetime($start) AND c.startTime <= datetime($end)
		OPTIONAL MATCH (c)-[:HAS_ROOT_CAUSE]->(rc:RootCause)
		OPTIONAL MATCH (c)-[:AFFECTS]->(r:Resource)
		OPTIONAL MATCH (c)-[:INCLUDES_EVENT]->(e:Event)
		RETURN c, rc, collect(DISTINCT r) as resources, collect(DISTINCT e.id) as events
		ORDER BY c.startTime DESC
	`

	params := map[string]interface{}{
		"start": start.Format(time.RFC3339),
		"end":   end.Format(time.RFC3339),
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query by time range: %w", err)
	}

	return s.parseResults(result)
}

// GetByResource retrieves correlations affecting a specific resource
func (s *Neo4jStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*correlation.CorrelationResult, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	query := `
		MATCH (r:Resource {name: $name, namespace: $namespace})
		MATCH (c:Correlation)-[:AFFECTS]->(r)
		OPTIONAL MATCH (c)-[:HAS_ROOT_CAUSE]->(rc:RootCause)
		OPTIONAL MATCH (c)-[:AFFECTS]->(r2:Resource)
		OPTIONAL MATCH (c)-[:INCLUDES_EVENT]->(e:Event)
		RETURN c, rc, collect(DISTINCT r2) as resources, collect(DISTINCT e.id) as events
		ORDER BY c.startTime DESC
	`

	params := map[string]interface{}{
		"name":      name,
		"namespace": namespace,
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query by resource: %w", err)
	}

	return s.parseResults(result)
}

// Cleanup removes old correlations
func (s *Neo4jStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: s.config.Database,
	})
	defer session.Close(ctx)

	cutoff := time.Now().Add(-olderThan)

	query := `
		MATCH (c:Correlation)
		WHERE c.createdAt < datetime($cutoff)
		OPTIONAL MATCH (c)-[r]-()
		DELETE r, c
		RETURN count(c) as deleted
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"cutoff": cutoff.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("failed to cleanup old correlations: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		deleted, _ := record.Get("deleted")
		s.logger.Info("Cleaned up old correlations",
			zap.Int64("deleted", deleted.(int64)),
			zap.Duration("older_than", olderThan))
	}

	return nil
}

// parseResults converts Neo4j results to correlation results
func (s *Neo4jStorage) parseResults(result neo4j.ResultWithContext) ([]*correlation.CorrelationResult, error) {
	var correlations []*correlation.CorrelationResult

	for result.Next(context.Background()) {
		record := result.Record()

		// Parse correlation node
		cNode, _ := record.Get("c")
		if cNode == nil {
			continue
		}

		c := cNode.(neo4j.Node)
		props := c.Props

		corr := &correlation.CorrelationResult{
			ID:         props["id"].(string),
			Type:       props["type"].(string),
			Confidence: props["confidence"].(float64),
			Summary:    props["summary"].(string),
			Details:    props["details"].(string),
			TraceID:    props["traceId"].(string),
		}

		// Parse timestamps
		if startTime, ok := props["startTime"].(time.Time); ok {
			corr.StartTime = startTime
		}
		if endTime, ok := props["endTime"].(time.Time); ok {
			corr.EndTime = endTime
		}

		// Parse evidence
		if evidenceStr, ok := props["evidence"].(string); ok {
			if err := json.Unmarshal([]byte(evidenceStr), &corr.Evidence); err != nil {
				s.logger.Warn("Failed to unmarshal correlation evidence",
					zap.String("correlation_id", corr.ID), zap.Error(err))
			}
		}

		// Parse root cause
		if rcNode, ok := record.Get("rc"); ok && rcNode != nil {
			rc := rcNode.(neo4j.Node)
			rcProps := rc.Props

			rootCause := &correlation.RootCause{
				EventID:     rcProps["eventId"].(string),
				Confidence:  rcProps["confidence"].(float64),
				Description: rcProps["description"].(string),
			}

			if evidenceStr, ok := rcProps["evidence"].(string); ok {
				if err := json.Unmarshal([]byte(evidenceStr), &rootCause.Evidence); err != nil {
					s.logger.Warn("Failed to unmarshal root cause evidence",
						zap.String("correlation_id", corr.ID),
						zap.String("event_id", rootCause.EventID), zap.Error(err))
				}
			}

			corr.RootCause = rootCause
		}

		// Parse resources
		if resources, ok := record.Get("resources"); ok && resources != nil {
			resourceList := resources.([]interface{})
			var resourceNames []string

			for _, r := range resourceList {
				if node, ok := r.(neo4j.Node); ok {
					props := node.Props
					if namespace, ok := props["namespace"].(string); ok {
						if name, ok := props["name"].(string); ok {
							resourceNames = append(resourceNames, fmt.Sprintf("%s/%s", namespace, name))
						}
					}
				}
			}

			if len(resourceNames) > 0 {
				corr.Impact = &correlation.Impact{
					Resources: resourceNames,
				}
			}
		}

		// Parse events
		if events, ok := record.Get("events"); ok && events != nil {
			eventList := events.([]interface{})
			for _, e := range eventList {
				if eventID, ok := e.(string); ok {
					corr.Events = append(corr.Events, eventID)
				}
			}
		}

		correlations = append(correlations, corr)
	}

	return correlations, result.Err()
}

// Close closes the Neo4j driver
func (s *Neo4jStorage) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}
