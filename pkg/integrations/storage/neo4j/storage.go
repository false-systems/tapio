package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/intelligence/graph"
	"go.uber.org/zap"
)

// Storage implements correlation.Storage using Neo4j
type Storage struct {
	client *graph.Client
	logger *zap.Logger
}

// NewStorage creates a new Neo4j storage implementation
func NewStorage(config graph.Config, logger *zap.Logger) (*Storage, error) {
	client, err := graph.NewClient(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph client: %w", err)
	}

	storage := &Storage{
		client: client,
		logger: logger,
	}

	// Initialize schema
	if err := storage.initSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initSchema creates necessary indexes and constraints
func (s *Storage) initSchema(ctx context.Context) error {
	// Create indexes for correlation storage
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.id)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.traceId)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.type)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.startTime)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Correlation) ON (c.confidence)",
		"CREATE INDEX IF NOT EXISTS FOR (rc:RootCause) ON (rc.eventId)",
		"CREATE INDEX IF NOT EXISTS FOR (i:Impact) ON (i.severity)",
	}

	for _, index := range indexes {
		if err := s.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
			_, err := tx.Run(ctx, index, nil)
			return err
		}); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Create constraints
	constraints := []string{
		"CREATE CONSTRAINT IF NOT EXISTS FOR (c:Correlation) REQUIRE c.id IS UNIQUE",
	}

	for _, constraint := range constraints {
		if err := s.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
			_, err := tx.Run(ctx, constraint, nil)
			return err
		}); err != nil {
			// Constraints might already exist, log but don't fail
			s.logger.Warn("Failed to create constraint",
				zap.String("constraint", constraint),
				zap.Error(err))
		}
	}

	return nil
}

// Store saves a correlation result in Neo4j
func (s *Storage) Store(ctx context.Context, result *correlation.CorrelationResult) error {
	return s.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		// Create correlation node
		query := `
			CREATE (c:Correlation {
				id: $id,
				type: $type,
				confidence: $confidence,
				traceId: $traceId,
				summary: $summary,
				details: $details,
				startTime: $startTime,
				endTime: $endTime,
				createdAt: datetime()
			})
		`

		params := map[string]interface{}{
			"id":         result.ID,
			"type":       result.Type,
			"confidence": result.Confidence,
			"traceId":    result.TraceID,
			"summary":    result.Summary,
			"details":    result.Details,
			"startTime":  result.StartTime.Unix(),
			"endTime":    result.EndTime.Unix(),
		}

		_, err := tx.Run(ctx, query, params)
		if err != nil {
			return fmt.Errorf("failed to create correlation node: %w", err)
		}

		// Store event relationships
		if len(result.Events) > 0 {
			eventQuery := `
				MATCH (c:Correlation {id: $correlationId})
				WITH c
				UNWIND $eventIds AS eventId
				MERGE (e:Event {id: eventId})
				CREATE (e)-[:PART_OF]->(c)
			`

			_, err = tx.Run(ctx, eventQuery, map[string]interface{}{
				"correlationId": result.ID,
				"eventIds":      result.Events,
			})
			if err != nil {
				return fmt.Errorf("failed to create event relationships: %w", err)
			}
		}

		// Store root cause if present
		if result.RootCause != nil {
			rcQuery := `
				MATCH (c:Correlation {id: $correlationId})
				CREATE (rc:RootCause {
					eventId: $eventId,
					confidence: $confidence,
					description: $description
				})
				CREATE (rc)-[:ROOT_CAUSE_OF]->(c)
			`

			_, err = tx.Run(ctx, rcQuery, map[string]interface{}{
				"correlationId": result.ID,
				"eventId":       result.RootCause.EventID,
				"confidence":    result.RootCause.Confidence,
				"description":   result.RootCause.Description,
			})
			if err != nil {
				return fmt.Errorf("failed to create root cause: %w", err)
			}

			// Store evidence
			if len(result.RootCause.Evidence) > 0 {
				evidenceQuery := `
					MATCH (c:Correlation {id: $correlationId})
					MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
					SET rc.evidence = $evidence
				`

				_, err = tx.Run(ctx, evidenceQuery, map[string]interface{}{
					"correlationId": result.ID,
					"evidence":      result.RootCause.Evidence,
				})
				if err != nil {
					return fmt.Errorf("failed to store evidence: %w", err)
				}
			}
		}

		// Store impact if present
		if result.Impact != nil {
			impactQuery := `
				MATCH (c:Correlation {id: $correlationId})
				CREATE (i:Impact {
					severity: $severity,
					resources: $resources,
					services: $services
				})
				CREATE (i)-[:IMPACT_OF]->(c)
			`

			_, err = tx.Run(ctx, impactQuery, map[string]interface{}{
				"correlationId": result.ID,
				"severity":      string(result.Impact.Severity),
				"resources":     result.Impact.Resources,
				"services":      result.Impact.Services,
			})
			if err != nil {
				return fmt.Errorf("failed to create impact: %w", err)
			}
		}

		// Store general evidence
		if len(result.Evidence) > 0 {
			evidenceQuery := `
				MATCH (c:Correlation {id: $correlationId})
				SET c.evidence = $evidence
			`

			_, err = tx.Run(ctx, evidenceQuery, map[string]interface{}{
				"correlationId": result.ID,
				"evidence":      result.Evidence,
			})
			if err != nil {
				return fmt.Errorf("failed to store correlation evidence: %w", err)
			}
		}

		return nil
	})
}

// GetRecent retrieves recent correlations
func (s *Storage) GetRecent(ctx context.Context, limit int) ([]*correlation.CorrelationResult, error) {
	query := `
		MATCH (c:Correlation)
		OPTIONAL MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
		OPTIONAL MATCH (i:Impact)-[:IMPACT_OF]->(c)
		OPTIONAL MATCH (e:Event)-[:PART_OF]->(c)
		WITH c, rc, i, collect(DISTINCT e.id) as eventIds
		RETURN c, rc, i, eventIds
		ORDER BY c.startTime DESC
		LIMIT $limit
	`

	params := map[string]interface{}{
		"limit": limit,
	}

	records, err := s.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	return s.recordsToResults(records), nil
}

// GetByTraceID retrieves correlations by trace ID
func (s *Storage) GetByTraceID(ctx context.Context, traceID string) ([]*correlation.CorrelationResult, error) {
	query := `
		MATCH (c:Correlation {traceId: $traceId})
		OPTIONAL MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
		OPTIONAL MATCH (i:Impact)-[:IMPACT_OF]->(c)
		OPTIONAL MATCH (e:Event)-[:PART_OF]->(c)
		WITH c, rc, i, collect(DISTINCT e.id) as eventIds
		RETURN c, rc, i, eventIds
		ORDER BY c.startTime DESC
	`

	params := map[string]interface{}{
		"traceId": traceID,
	}

	records, err := s.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	return s.recordsToResults(records), nil
}

// Cleanup removes old correlations
func (s *Storage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan).Unix()

	return s.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		query := `
			MATCH (c:Correlation)
			WHERE c.startTime < $cutoff
			OPTIONAL MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
			OPTIONAL MATCH (i:Impact)-[:IMPACT_OF]->(c)
			DETACH DELETE c, rc, i
			RETURN count(c) as deletedCount
		`

		result, err := tx.Run(ctx, query, map[string]interface{}{
			"cutoff": cutoff,
		})
		if err != nil {
			return err
		}

		if result.Next(ctx) {
			deletedCount := result.Record().Values[0].(int64)
			s.logger.Info("Cleaned up old correlations",
				zap.Int64("deleted", deletedCount),
				zap.Duration("olderThan", olderThan))
		}

		return nil
	})
}

// Close closes the underlying Neo4j client
func (s *Storage) Close(ctx context.Context) error {
	return s.client.Close(ctx)
}

// recordsToResults converts Neo4j records to CorrelationResult structs
func (s *Storage) recordsToResults(records []map[string]interface{}) []*correlation.CorrelationResult {
	var results []*correlation.CorrelationResult

	for _, record := range records {
		if cNode, ok := record["c"].(neo4j.Node); ok {
			result := &correlation.CorrelationResult{
				ID:         getString(cNode.Props, "id"),
				Type:       getString(cNode.Props, "type"),
				Confidence: getFloat64(cNode.Props, "confidence"),
				TraceID:    getString(cNode.Props, "traceId"),
				Summary:    getString(cNode.Props, "summary"),
				Details:    getString(cNode.Props, "details"),
			}

			// Parse timestamps
			if startTime := getInt64(cNode.Props, "startTime"); startTime > 0 {
				result.StartTime = time.Unix(startTime, 0)
			}
			if endTime := getInt64(cNode.Props, "endTime"); endTime > 0 {
				result.EndTime = time.Unix(endTime, 0)
			}

			// Parse evidence
			if evidence, ok := cNode.Props["evidence"].([]interface{}); ok {
				result.Evidence = interfaceSliceToStringSlice(evidence)
			}

			// Parse event IDs
			if eventIds, ok := record["eventIds"].([]interface{}); ok {
				result.Events = interfaceSliceToStringSlice(eventIds)
			}

			// Parse root cause
			if rcNode, ok := record["rc"].(neo4j.Node); ok {
				result.RootCause = &correlation.RootCause{
					EventID:     getString(rcNode.Props, "eventId"),
					Confidence:  getFloat64(rcNode.Props, "confidence"),
					Description: getString(rcNode.Props, "description"),
				}
				if evidence, ok := rcNode.Props["evidence"].([]interface{}); ok {
					result.RootCause.Evidence = interfaceSliceToStringSlice(evidence)
				}
			}

			// Parse impact
			if iNode, ok := record["i"].(neo4j.Node); ok {
				result.Impact = &correlation.Impact{
					Severity:  domain.EventSeverity(getString(iNode.Props, "severity")),
					Resources: getStringSlice(iNode.Props, "resources"),
					Services:  getStringSlice(iNode.Props, "services"),
				}
			}

			results = append(results, result)
		}
	}

	return results
}

// Helper functions
func getString(props map[string]interface{}, key string) string {
	if val, ok := props[key].(string); ok {
		return val
	}
	return ""
}

func getFloat64(props map[string]interface{}, key string) float64 {
	if val, ok := props[key].(float64); ok {
		return val
	}
	return 0
}

func getInt64(props map[string]interface{}, key string) int64 {
	if val, ok := props[key].(int64); ok {
		return val
	}
	return 0
}

func getStringSlice(props map[string]interface{}, key string) []string {
	if val, ok := props[key].([]interface{}); ok {
		return interfaceSliceToStringSlice(val)
	}
	return nil
}

func interfaceSliceToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result
}
