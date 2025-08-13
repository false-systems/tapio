package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// BehaviorStore handles persistence of behavior predictions and feedback to Neo4j
type BehaviorStore struct {
	logger *zap.Logger
	driver neo4j.DriverWithContext

	// Configuration
	database     string
	batchSize    int
	writeTimeout time.Duration

	// OTEL instrumentation
	tracer            trace.Tracer
	predictionsStored metric.Int64Counter
	feedbackStored    metric.Int64Counter
	queryDuration     metric.Float64Histogram
	storeErrors       metric.Int64Counter
}

// BehaviorStoreConfig configures the behavior store
type BehaviorStoreConfig struct {
	URI          string
	Username     string
	Password     string
	Database     string
	BatchSize    int
	WriteTimeout time.Duration
}

// NewBehaviorStore creates a new behavior store
func NewBehaviorStore(logger *zap.Logger, config BehaviorStoreConfig) (*BehaviorStore, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Set defaults
	if config.Database == "" {
		config.Database = "neo4j"
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}

	// Create Neo4j driver
	driver, err := neo4j.NewDriverWithContext(
		config.URI,
		neo4j.BasicAuth(config.Username, config.Password, ""),
		func(c *neo4j.Config) {
			c.MaxConnectionLifetime = 5 * time.Minute
			c.MaxConnectionPoolSize = 50
			c.ConnectionAcquisitionTimeout = 30 * time.Second
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	// Initialize OTEL
	tracer := otel.Tracer("neo4j.behavior_store")
	meter := otel.Meter("neo4j.behavior_store")

	predictionsStored, err := meter.Int64Counter(
		"behavior_predictions_stored_total",
		metric.WithDescription("Total number of predictions stored in Neo4j"),
	)
	if err != nil {
		logger.Warn("Failed to create predictions counter", zap.Error(err))
	}

	feedbackStored, err := meter.Int64Counter(
		"behavior_feedback_stored_total",
		metric.WithDescription("Total number of feedback items stored in Neo4j"),
	)
	if err != nil {
		logger.Warn("Failed to create feedback counter", zap.Error(err))
	}

	queryDuration, err := meter.Float64Histogram(
		"behavior_neo4j_query_duration_ms",
		metric.WithDescription("Neo4j query duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create query duration histogram", zap.Error(err))
	}

	storeErrors, err := meter.Int64Counter(
		"behavior_store_errors_total",
		metric.WithDescription("Total number of store errors"),
	)
	if err != nil {
		logger.Warn("Failed to create store errors counter", zap.Error(err))
	}

	store := &BehaviorStore{
		logger:            logger,
		driver:            driver,
		database:          config.Database,
		batchSize:         config.BatchSize,
		writeTimeout:      config.WriteTimeout,
		tracer:            tracer,
		predictionsStored: predictionsStored,
		feedbackStored:    feedbackStored,
		queryDuration:     queryDuration,
		storeErrors:       storeErrors,
	}

	// Initialize schema
	if err := store.initializeSchema(context.Background()); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initializeSchema creates necessary indexes and constraints
func (bs *BehaviorStore) initializeSchema(ctx context.Context) error {
	ctx, span := bs.tracer.Start(ctx, "behavior_store.initialize_schema")
	defer span.End()

	session := bs.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: bs.database,
	})
	defer session.Close(ctx)

	// Create constraints and indexes
	queries := []string{
		// Prediction constraints
		`CREATE CONSTRAINT prediction_id IF NOT EXISTS 
		 FOR (p:BehaviorPrediction) REQUIRE p.id IS UNIQUE`,

		// Feedback constraints
		`CREATE CONSTRAINT feedback_id IF NOT EXISTS 
		 FOR (f:BehaviorFeedback) REQUIRE f.id IS UNIQUE`,

		// Pattern constraints
		`CREATE CONSTRAINT pattern_id IF NOT EXISTS 
		 FOR (p:BehaviorPattern) REQUIRE p.id IS UNIQUE`,

		// Indexes for common queries
		`CREATE INDEX prediction_pattern IF NOT EXISTS 
		 FOR (p:BehaviorPrediction) ON (p.pattern_id)`,

		`CREATE INDEX prediction_time IF NOT EXISTS 
		 FOR (p:BehaviorPrediction) ON (p.generated_at)`,

		`CREATE INDEX prediction_confidence IF NOT EXISTS 
		 FOR (p:BehaviorPrediction) ON (p.confidence)`,

		`CREATE INDEX feedback_prediction IF NOT EXISTS 
		 FOR (f:BehaviorFeedback) ON (f.prediction_id)`,

		`CREATE INDEX feedback_time IF NOT EXISTS 
		 FOR (f:BehaviorFeedback) ON (f.timestamp)`,
	}

	for _, query := range queries {
		if _, err := session.Run(ctx, query, nil); err != nil {
			// Ignore "already exists" errors
			if !isAlreadyExistsError(err) {
				return fmt.Errorf("failed to execute schema query: %w", err)
			}
		}
	}

	bs.logger.Info("Neo4j schema initialized successfully")
	return nil
}

// StorePrediction stores a behavior prediction in Neo4j
func (bs *BehaviorStore) StorePrediction(ctx context.Context, prediction *domain.BehaviorPrediction) error {
	ctx, span := bs.tracer.Start(ctx, "behavior_store.store_prediction")
	defer span.End()

	if prediction == nil {
		return fmt.Errorf("prediction is required")
	}

	start := time.Now()
	defer func() {
		if bs.queryDuration != nil {
			bs.queryDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0,
				metric.WithAttributes(
					attribute.String("operation", "store_prediction"),
				))
		}
	}()

	session := bs.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: bs.database,
	})
	defer session.Close(ctx)

	// Create prediction node with relationships
	query := `
		MERGE (pat:BehaviorPattern {id: $pattern_id})
		ON CREATE SET pat.name = $pattern_name
		
		CREATE (pred:BehaviorPrediction {
			id: $id,
			pattern_id: $pattern_id,
			pattern_name: $pattern_name,
			confidence: $confidence,
			time_horizon: $time_horizon,
			generated_at: $generated_at,
			severity: $severity,
			category: $category
		})
		
		CREATE (pat)-[:GENERATED]->(pred)
		
		// Store affected resources
		WITH pred
		UNWIND $affected_resources AS resource_id
		MERGE (r:Resource {id: resource_id})
		CREATE (pred)-[:AFFECTS]->(r)
		
		// Store evidence
		WITH pred
		UNWIND $evidence AS ev
		CREATE (e:Evidence {
			type: ev.type,
			source: ev.source,
			description: ev.description,
			timestamp: ev.timestamp
		})
		CREATE (pred)-[:HAS_EVIDENCE]->(e)
		
		RETURN pred.id as prediction_id
	`

	params := map[string]interface{}{
		"id":                 prediction.ID,
		"pattern_id":         prediction.PatternID,
		"pattern_name":       prediction.PatternName,
		"confidence":         prediction.Confidence,
		"time_horizon":       prediction.TimeHorizon.Seconds(),
		"generated_at":       prediction.GeneratedAt.Unix(),
		"severity":           prediction.Metadata["severity"],
		"category":           prediction.Metadata["category"],
		"affected_resources": prediction.AffectedResources,
		"evidence":           bs.convertEvidence(prediction.Evidence),
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Consume(ctx)
	})

	if err != nil {
		if bs.storeErrors != nil {
			bs.storeErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "store_prediction"),
				attribute.String("error_type", "write_failed"),
			))
		}
		return fmt.Errorf("failed to store prediction: %w", err)
	}

	if bs.predictionsStored != nil {
		bs.predictionsStored.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pattern_id", prediction.PatternID),
			attribute.Float64("confidence", prediction.Confidence),
		))
	}

	span.SetAttributes(
		attribute.String("prediction.id", prediction.ID),
		attribute.String("pattern.id", prediction.PatternID),
		attribute.Float64("confidence", prediction.Confidence),
	)

	bs.logger.Debug("Stored prediction",
		zap.String("prediction_id", prediction.ID),
		zap.String("pattern", prediction.PatternName))

	return nil
}

// StoreFeedback stores behavior feedback in Neo4j
func (bs *BehaviorStore) StoreFeedback(ctx context.Context, feedback *domain.BehaviorFeedback) error {
	ctx, span := bs.tracer.Start(ctx, "behavior_store.store_feedback")
	defer span.End()

	if feedback == nil {
		return fmt.Errorf("feedback is required")
	}

	start := time.Now()
	defer func() {
		if bs.queryDuration != nil {
			bs.queryDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0,
				metric.WithAttributes(
					attribute.String("operation", "store_feedback"),
				))
		}
	}()

	session := bs.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: bs.database,
	})
	defer session.Close(ctx)

	query := `
		MATCH (pred:BehaviorPrediction {id: $prediction_id})
		CREATE (feedback:BehaviorFeedback {
			id: $id,
			prediction_id: $prediction_id,
			pattern_id: $pattern_id,
			feedback_type: $feedback_type,
			prediction_occurred: $prediction_occurred,
			accuracy_score: $accuracy_score,
			timestamp: $timestamp,
			user_id: $user_id,
			comments: $comments
		})
		CREATE (pred)-[:HAS_FEEDBACK]->(feedback)
		
		// Update pattern statistics
		WITH feedback
		MATCH (pat:BehaviorPattern {id: $pattern_id})
		SET pat.feedback_count = COALESCE(pat.feedback_count, 0) + 1,
		    pat.last_feedback = $timestamp
		
		RETURN feedback.id as feedback_id
	`

	params := map[string]interface{}{
		"id":                  feedback.ID,
		"prediction_id":       feedback.PredictionID,
		"pattern_id":          feedback.PatternID,
		"feedback_type":       string(feedback.FeedbackType),
		"prediction_occurred": feedback.PredictionOccurred,
		"accuracy_score":      feedback.AccuracyScore,
		"timestamp":           feedback.Timestamp.Unix(),
		"user_id":             feedback.UserID,
		"comments":            feedback.Comments,
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Consume(ctx)
	})

	if err != nil {
		if bs.storeErrors != nil {
			bs.storeErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "store_feedback"),
				attribute.String("error_type", "write_failed"),
			))
		}
		return fmt.Errorf("failed to store feedback: %w", err)
	}

	if bs.feedbackStored != nil {
		bs.feedbackStored.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pattern_id", feedback.PatternID),
			attribute.String("feedback_type", string(feedback.FeedbackType)),
		))
	}

	span.SetAttributes(
		attribute.String("feedback.id", feedback.ID),
		attribute.String("prediction.id", feedback.PredictionID),
		attribute.String("feedback.type", string(feedback.FeedbackType)),
	)

	bs.logger.Debug("Stored feedback",
		zap.String("feedback_id", feedback.ID),
		zap.String("type", string(feedback.FeedbackType)))

	return nil
}

// BatchStorePredictions stores multiple predictions efficiently
func (bs *BehaviorStore) BatchStorePredictions(ctx context.Context, predictions []*domain.BehaviorPrediction) error {
	ctx, span := bs.tracer.Start(ctx, "behavior_store.batch_store_predictions")
	defer span.End()

	if len(predictions) == 0 {
		return nil
	}

	start := time.Now()
	defer func() {
		if bs.queryDuration != nil {
			bs.queryDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0,
				metric.WithAttributes(
					attribute.String("operation", "batch_store_predictions"),
					attribute.Int("batch_size", len(predictions)),
				))
		}
	}()

	session := bs.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: bs.database,
	})
	defer session.Close(ctx)

	// Use UNWIND for efficient batch insert
	query := `
		UNWIND $predictions AS pred
		
		MERGE (pat:BehaviorPattern {id: pred.pattern_id})
		ON CREATE SET pat.name = pred.pattern_name
		
		CREATE (p:BehaviorPrediction {
			id: pred.id,
			pattern_id: pred.pattern_id,
			pattern_name: pred.pattern_name,
			confidence: pred.confidence,
			time_horizon: pred.time_horizon,
			generated_at: pred.generated_at,
			severity: pred.severity,
			category: pred.category
		})
		
		CREATE (pat)-[:GENERATED]->(p)
		
		WITH p, pred
		UNWIND pred.affected_resources AS resource_id
		MERGE (r:Resource {id: resource_id})
		CREATE (p)-[:AFFECTS]->(r)
		
		RETURN count(p) as stored_count
	`

	// Convert predictions to params
	predData := make([]map[string]interface{}, 0, len(predictions))
	for _, pred := range predictions {
		predData = append(predData, map[string]interface{}{
			"id":                 pred.ID,
			"pattern_id":         pred.PatternID,
			"pattern_name":       pred.PatternName,
			"confidence":         pred.Confidence,
			"time_horizon":       pred.TimeHorizon.Seconds(),
			"generated_at":       pred.GeneratedAt.Unix(),
			"severity":           pred.Metadata["severity"],
			"category":           pred.Metadata["category"],
			"affected_resources": pred.AffectedResources,
		})
	}

	params := map[string]interface{}{
		"predictions": predData,
	}

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}

		if result.Next(ctx) {
			record := result.Record()
			count, _ := record.Get("stored_count")
			return count, nil
		}

		return 0, result.Err()
	})

	if err != nil {
		if bs.storeErrors != nil {
			bs.storeErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "batch_store_predictions"),
				attribute.String("error_type", "write_failed"),
			))
		}
		return fmt.Errorf("failed to batch store predictions: %w", err)
	}

	storedCount := result.(int64)

	if bs.predictionsStored != nil {
		bs.predictionsStored.Add(ctx, storedCount)
	}

	span.SetAttributes(
		attribute.Int("batch_size", len(predictions)),
		attribute.Int64("stored_count", storedCount),
	)

	bs.logger.Info("Batch stored predictions",
		zap.Int("batch_size", len(predictions)),
		zap.Int64("stored_count", storedCount))

	return nil
}

// GetPredictionContext retrieves context for a prediction from the graph
func (bs *BehaviorStore) GetPredictionContext(ctx context.Context, predictionID string) (map[string]interface{}, error) {
	ctx, span := bs.tracer.Start(ctx, "behavior_store.get_prediction_context")
	defer span.End()

	start := time.Now()
	defer func() {
		if bs.queryDuration != nil {
			bs.queryDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0,
				metric.WithAttributes(
					attribute.String("operation", "get_prediction_context"),
				))
		}
	}()

	session := bs.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: bs.database,
	})
	defer session.Close(ctx)

	// Query to get prediction with related context
	query := `
		MATCH (pred:BehaviorPrediction {id: $prediction_id})
		OPTIONAL MATCH (pred)<-[:GENERATED]-(pat:BehaviorPattern)
		OPTIONAL MATCH (pred)-[:AFFECTS]->(r:Resource)
		OPTIONAL MATCH (pred)-[:HAS_EVIDENCE]->(e:Evidence)
		OPTIONAL MATCH (pred)-[:HAS_FEEDBACK]->(f:BehaviorFeedback)
		
		RETURN pred,
		       pat,
		       collect(DISTINCT r.id) as affected_resources,
		       collect(DISTINCT e) as evidence,
		       collect(DISTINCT f) as feedback
	`

	params := map[string]interface{}{
		"prediction_id": predictionID,
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}

		if result.Next(ctx) {
			record := result.Record()

			context := make(map[string]interface{})

			// Get prediction node
			if pred, ok := record.Get("pred"); ok {
				context["prediction"] = pred
			}

			// Get pattern
			if pat, ok := record.Get("pat"); ok {
				context["pattern"] = pat
			}

			// Get affected resources
			if resources, ok := record.Get("affected_resources"); ok {
				context["affected_resources"] = resources
			}

			// Get evidence
			if evidence, ok := record.Get("evidence"); ok {
				context["evidence"] = evidence
			}

			// Get feedback
			if feedback, ok := record.Get("feedback"); ok {
				context["feedback"] = feedback
			}

			return context, nil
		}

		return nil, fmt.Errorf("prediction not found: %s", predictionID)
	})

	if err != nil {
		if bs.storeErrors != nil {
			bs.storeErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "get_prediction_context"),
				attribute.String("error_type", "read_failed"),
			))
		}
		return nil, fmt.Errorf("failed to get prediction context: %w", err)
	}

	span.SetAttributes(
		attribute.String("prediction.id", predictionID),
	)

	return result.(map[string]interface{}), nil
}

// convertEvidence converts domain evidence to Neo4j format
func (bs *BehaviorStore) convertEvidence(evidence []domain.Evidence) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(evidence))

	for _, e := range evidence {
		result = append(result, map[string]interface{}{
			"type":        e.Type,
			"source":      e.Source,
			"description": e.Description,
			"timestamp":   e.Timestamp.Unix(),
		})
	}

	return result
}

// Close closes the Neo4j driver
func (bs *BehaviorStore) Close(ctx context.Context) error {
	return bs.driver.Close(ctx)
}

// isAlreadyExistsError checks if an error is due to already existing constraint/index
func isAlreadyExistsError(err error) bool {
	// Neo4j returns specific error codes for already existing constraints
	// This is a simplified check - in production, check the actual Neo4j error code
	errStr := err.Error()
	return contains(errStr, "already exists") || contains(errStr, "EquivalentSchemaRuleAlreadyExists")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) > len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
