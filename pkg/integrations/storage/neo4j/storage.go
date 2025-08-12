package neo4j

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	neo4jclient "github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// Storage implements correlation.Storage using Neo4j
type Storage struct {
	client *neo4jclient.Client
	logger *zap.Logger
}

// NewStorage creates a new Neo4j storage implementation
func NewStorage(config neo4jclient.Config, logger *zap.Logger) (*Storage, error) {
	client, err := neo4jclient.NewClient(config, logger)
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

		// Marshal typed structs to JSON for storage
		detailsJSON, err := json.Marshal(result.Details)
		if err != nil {
			return fmt.Errorf("failed to marshal correlation details: %w", err)
		}

		params := map[string]interface{}{
			"id":         result.ID,
			"type":       result.Type,
			"confidence": result.Confidence,
			"traceId":    result.TraceID,
			"summary":    result.Summary,
			"details":    string(detailsJSON),
			"startTime":  result.StartTime.Unix(),
			"endTime":    result.EndTime.Unix(),
		}

		_, err = tx.Run(ctx, query, params)
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

			// Store evidence - check if Evidence has data
			if result.RootCause.Evidence.EventIDs != nil || result.RootCause.Evidence.ResourceIDs != nil {
				evidenceJSON, err := json.Marshal(result.RootCause.Evidence)
				if err != nil {
					return fmt.Errorf("failed to marshal root cause evidence: %w", err)
				}

				evidenceQuery := `
					MATCH (c:Correlation {id: $correlationId})
					MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
					SET rc.evidence = $evidence
				`

				_, err = tx.Run(ctx, evidenceQuery, map[string]interface{}{
					"correlationId": result.ID,
					"evidence":      string(evidenceJSON),
				})
				if err != nil {
					return fmt.Errorf("failed to store evidence: %w", err)
				}
			}
		}

		// Store impact if present
		if result.Impact != nil {
			// Marshal services as JSON since it's a complex type
			servicesJSON, servicesErr := json.Marshal(result.Impact.Services)
			if servicesErr != nil {
				return fmt.Errorf("failed to marshal impact services: %w", servicesErr)
			}

			impactQuery := `
				MATCH (c:Correlation {id: $correlationId})
				CREATE (i:Impact {
					severity: $severity,
					resources: $resources,
					services: $services,
					scope: $scope,
					userImpact: $userImpact,
					degradation: $degradation
				})
				CREATE (i)-[:IMPACT_OF]->(c)
			`

			_, err = tx.Run(ctx, impactQuery, map[string]interface{}{
				"correlationId": result.ID,
				"severity":      string(result.Impact.Severity),
				"resources":     result.Impact.Resources,
				"services":      string(servicesJSON),
				"scope":         result.Impact.Scope,
				"userImpact":    result.Impact.UserImpact,
				"degradation":   result.Impact.Degradation,
			})
			if err != nil {
				return fmt.Errorf("failed to create impact: %w", err)
			}
		}

		// Store general evidence - check if Evidence has data
		if result.Evidence.EventIDs != nil || result.Evidence.ResourceIDs != nil {
			evidenceJSON, err := json.Marshal(result.Evidence)
			if err != nil {
				return fmt.Errorf("failed to marshal correlation evidence: %w", err)
			}

			evidenceQuery := `
				MATCH (c:Correlation {id: $correlationId})
				SET c.evidence = $evidence
			`

			_, err = tx.Run(ctx, evidenceQuery, map[string]interface{}{
				"correlationId": result.ID,
				"evidence":      string(evidenceJSON),
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

	params := neo4jclient.QueryParams{
		Limit: limit,
	}

	records, err := s.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	return s.queryResultToCorrelations(records), nil
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

	params := neo4jclient.QueryParams{
		StringParams: map[string]string{
			"traceId": traceID,
		},
	}

	records, err := s.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	return s.queryResultToCorrelations(records), nil
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
// queryResultToCorrelations converts QueryResult to correlation results
func (s *Storage) queryResultToCorrelations(result *neo4jclient.QueryResult) []*correlation.CorrelationResult {
	if result == nil || len(result.Records) == 0 {
		return nil
	}

	correlations := make([]*correlation.CorrelationResult, 0, len(result.Records))
	for _, record := range result.Records {
		if record.Correlation != nil {
			corr := s.nodeToCorrelation(record.Correlation)
			if corr != nil {
				correlations = append(correlations, corr)
			}
		}
	}
	return correlations
}

// nodeToCorrelation converts a CorrelationNode to CorrelationResult
func (s *Storage) nodeToCorrelation(node *neo4jclient.CorrelationNode) *correlation.CorrelationResult {
	if node == nil {
		return nil
	}

	return &correlation.CorrelationResult{
		ID:         node.ID,
		Type:       node.Type,
		Confidence: node.Confidence,
		StartTime:  node.StartTime,
		EndTime:    node.EndTime,
		TraceID:    node.TraceID,
		Summary:    node.Summary,
		Message:    node.Summary, // Use summary as message
		Events:     []string{},   // Would need additional query for related events
		Details: correlation.CorrelationDetails{
			Pattern:   node.Type,
			Algorithm: "neo4j_correlation",
		},
		Evidence: correlation.EvidenceData{
			EventIDs:    []string{},
			ResourceIDs: []string{},
			Attributes:  make(map[string]string),
			Metrics:     make(map[string]correlation.MetricValue),
		},
	}
}

func (s *Storage) recordsToResults(records []map[string]any) []*correlation.CorrelationResult {
	var results []*correlation.CorrelationResult

	for _, record := range records {
		if cNode, ok := record["c"].(neo4j.Node); ok {
			result := &correlation.CorrelationResult{
				ID:         getString(cNode.Props, "id"),
				Type:       getString(cNode.Props, "type"),
				Confidence: getFloat64(cNode.Props, "confidence"),
				TraceID:    getString(cNode.Props, "traceId"),
				Summary:    getString(cNode.Props, "summary"),
			}

			// Parse details from JSON
			if detailsStr := getString(cNode.Props, "details"); detailsStr != "" {
				var details correlation.CorrelationDetails
				if err := json.Unmarshal([]byte(detailsStr), &details); err != nil {
					s.logger.Warn("Failed to unmarshal correlation details",
						zap.String("correlation_id", result.ID),
						zap.Error(err))
				} else {
					result.Details = details
				}
			}

			// Parse timestamps
			if startTime := getInt64(cNode.Props, "startTime"); startTime > 0 {
				result.StartTime = time.Unix(startTime, 0)
			}
			if endTime := getInt64(cNode.Props, "endTime"); endTime > 0 {
				result.EndTime = time.Unix(endTime, 0)
			}

			// Parse evidence from JSON
			if evidenceStr, ok := cNode.Props["evidence"].(string); ok && evidenceStr != "" {
				var evidence correlation.EvidenceData
				if err := json.Unmarshal([]byte(evidenceStr), &evidence); err != nil {
					s.logger.Warn("Failed to unmarshal correlation evidence",
						zap.String("correlation_id", result.ID),
						zap.Error(err))
				} else {
					result.Evidence = evidence
				}
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
				// Parse root cause evidence from JSON
				if evidenceStr, ok := rcNode.Props["evidence"].(string); ok && evidenceStr != "" {
					var evidence correlation.EvidenceData
					if err := json.Unmarshal([]byte(evidenceStr), &evidence); err != nil {
						s.logger.Warn("Failed to unmarshal root cause evidence",
							zap.String("correlation_id", result.ID),
							zap.Error(err))
					} else {
						result.RootCause.Evidence = evidence
					}
				}
			}

			// Parse impact
			if iNode, ok := record["i"].(neo4j.Node); ok {
				result.Impact = &correlation.Impact{
					Severity:    domain.EventSeverity(getString(iNode.Props, "severity")),
					Resources:   getStringSlice(iNode.Props, "resources"),
					Scope:       getString(iNode.Props, "scope"),
					UserImpact:  getString(iNode.Props, "userImpact"),
					Degradation: getString(iNode.Props, "degradation"),
				}

				// Parse services from JSON
				if servicesStr := getString(iNode.Props, "services"); servicesStr != "" {
					var services []correlation.ServiceReference
					if err := json.Unmarshal([]byte(servicesStr), &services); err != nil {
						s.logger.Warn("Failed to unmarshal impact services",
							zap.String("correlation_id", result.ID),
							zap.Error(err))
					} else {
						result.Impact.Services = services
					}
				}
			}

			results = append(results, result)
		}
	}

	return results
}

// Helper functions
func getString(props map[string]any, key string) string {
	if val, ok := props[key].(string); ok {
		return val
	}
	return ""
}

func getFloat64(props map[string]any, key string) float64 {
	if val, ok := props[key].(float64); ok {
		return val
	}
	return 0
}

func getInt64(props map[string]any, key string) int64 {
	if val, ok := props[key].(int64); ok {
		return val
	}
	return 0
}

func getStringSlice(props map[string]any, key string) []string {
	if val, ok := props[key].([]any); ok {
		return interfaceSliceToStringSlice(val)
	}
	return nil
}

func interfaceSliceToStringSlice(slice []any) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result
}
