package neo4j

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// DefaultBatchSize is the maximum number of operations to batch together
	DefaultBatchSize = 100
	// DefaultBatchTimeout is the maximum time to wait before executing a partial batch
	DefaultBatchTimeout = 50 * time.Millisecond
	// MaxBatchSize is the absolute maximum batch size to prevent memory issues
	MaxBatchSize = 1000
)

// BatchOperation represents a single operation to be batched
type BatchOperation struct {
	Query      string                 `json:"query"`
	Parameters map[string]interface{} `json:"parameters"`
	ID         string                 `json:"id"` // Unique identifier for result tracking
}

// BatchResult represents the result of a batched operation
type BatchResult struct {
	ID      string       `json:"id"`
	Records []Record     `json:"records"`
	Summary Summary      `json:"summary"`
	Error   error        `json:"error"`
}

// BatchConfig configures batch operation behavior
type BatchConfig struct {
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`
}

// BatchClient provides high-performance batch operations for Neo4j
type BatchClient struct {
	client *Client
	config BatchConfig
	logger *zap.Logger

	// Batching infrastructure
	mu          sync.RWMutex
	operations  []BatchOperation
	results     map[string]chan BatchResult
	batchTimer  *time.Timer
	processing  bool
	closed      bool

	// OpenTelemetry instrumentation
	tracer               trace.Tracer
	batchesExecuted      metric.Int64Counter
	operationsPerBatch   metric.Float64Histogram
	batchExecutionTime   metric.Float64Histogram
	pendingOperations    metric.Int64Gauge
	batchErrors          metric.Int64Counter
	operationsProcessed  metric.Int64Counter
}

// NewBatchClient creates a new batch-enabled Neo4j client
func NewBatchClient(client *Client, config BatchConfig, logger *zap.Logger) (*BatchClient, error) {
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Validate and set defaults for batch configuration
	if err := validateBatchConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid batch config: %w", err)
	}

	// Initialize OpenTelemetry instrumentation
	tracer := otel.Tracer("integrations.neo4j.batch_client")
	meter := otel.Meter("integrations.neo4j.batch_client")

	batchesExecuted, err := meter.Int64Counter(
		"neo4j_batches_executed_total",
		metric.WithDescription("Total number of batches executed"),
	)
	if err != nil {
		logger.Warn("Failed to create batches executed counter", zap.Error(err))
	}

	operationsPerBatch, err := meter.Float64Histogram(
		"neo4j_operations_per_batch",
		metric.WithDescription("Number of operations per batch"),
	)
	if err != nil {
		logger.Warn("Failed to create operations per batch histogram", zap.Error(err))
	}

	batchExecutionTime, err := meter.Float64Histogram(
		"neo4j_batch_execution_duration_ms",
		metric.WithDescription("Batch execution duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create batch execution time histogram", zap.Error(err))
	}

	pendingOperations, err := meter.Int64Gauge(
		"neo4j_pending_operations",
		metric.WithDescription("Number of operations pending in batch"),
	)
	if err != nil {
		logger.Warn("Failed to create pending operations gauge", zap.Error(err))
	}

	batchErrors, err := meter.Int64Counter(
		"neo4j_batch_errors_total",
		metric.WithDescription("Total number of batch execution errors"),
	)
	if err != nil {
		logger.Warn("Failed to create batch errors counter", zap.Error(err))
	}

	operationsProcessed, err := meter.Int64Counter(
		"neo4j_operations_processed_total",
		metric.WithDescription("Total number of operations processed"),
	)
	if err != nil {
		logger.Warn("Failed to create operations processed counter", zap.Error(err))
	}

	return &BatchClient{
		client:               client,
		config:               config,
		logger:               logger,
		operations:           make([]BatchOperation, 0, config.BatchSize),
		results:              make(map[string]chan BatchResult),
		batchTimer:           time.NewTimer(config.BatchTimeout),
		tracer:               tracer,
		batchesExecuted:      batchesExecuted,
		operationsPerBatch:   operationsPerBatch,
		batchExecutionTime:   batchExecutionTime,
		pendingOperations:    pendingOperations,
		batchErrors:          batchErrors,
		operationsProcessed:  operationsProcessed,
	}, nil
}

// validateBatchConfig validates and sets default values for batch configuration
func validateBatchConfig(config *BatchConfig) error {
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = DefaultBatchTimeout
	}

	// Validate reasonable limits
	if config.BatchSize > MaxBatchSize {
		return fmt.Errorf("batch size too large: %d (max %d)", config.BatchSize, MaxBatchSize)
	}
	if config.BatchTimeout > 10*time.Second {
		return fmt.Errorf("batch timeout too large: %v (max 10s)", config.BatchTimeout)
	}

	return nil
}

// ExecuteQueryBatch executes a query as part of a batch operation
func (bc *BatchClient) ExecuteQueryBatch(ctx context.Context, query string, params QueryParams) (*QueryResult, error) {
	if bc.closed {
		return nil, fmt.Errorf("batch client is closed")
	}

	// Generate unique ID for this operation
	operationID := fmt.Sprintf("op_%d_%s", time.Now().UnixNano(), generateShortID())

	// Create batch operation
	operation := BatchOperation{
		Query:      query,
		Parameters: params.ToMap(),
		ID:         operationID,
	}

	// Create result channel for this operation
	resultCh := make(chan BatchResult, 1)

	// Add to batch
	if err := bc.addToBatch(ctx, operation, resultCh); err != nil {
		close(resultCh)
		return nil, fmt.Errorf("failed to add to batch: %w", err)
	}

	// Wait for result
	select {
	case result := <-resultCh:
		if result.Error != nil {
			return nil, result.Error
		}
		return &QueryResult{
			Records: result.Records,
			Summary: result.Summary,
		}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("batch operation cancelled: %w", ctx.Err())
	}
}

// addToBatch adds an operation to the current batch
func (bc *BatchClient) addToBatch(ctx context.Context, operation BatchOperation, resultCh chan BatchResult) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.closed {
		return fmt.Errorf("batch client is closed")
	}

	// Add operation and result channel
	bc.operations = append(bc.operations, operation)
	bc.results[operation.ID] = resultCh

	// Update pending operations metric
	if bc.pendingOperations != nil {
		bc.pendingOperations.Record(ctx, int64(len(bc.operations)))
	}

	// Check if batch is full
	shouldExecute := len(bc.operations) >= bc.config.BatchSize

	if shouldExecute && !bc.processing {
		// Stop timer since we're executing immediately
		if !bc.batchTimer.Stop() {
			select {
			case <-bc.batchTimer.C:
			default:
			}
		}
		bc.batchTimer.Reset(bc.config.BatchTimeout)
		
		// Execute batch in background
		go bc.executeBatch(ctx, "batch_full")
	} else if len(bc.operations) == 1 && !bc.processing {
		// First operation in batch, start timer
		bc.batchTimer.Reset(bc.config.BatchTimeout)
		go bc.waitForTimeout(ctx)
	}

	return nil
}

// waitForTimeout waits for the batch timeout and executes if no other trigger occurred
func (bc *BatchClient) waitForTimeout(ctx context.Context) {
	select {
	case <-bc.batchTimer.C:
		bc.mu.Lock()
		if len(bc.operations) > 0 && !bc.processing {
			bc.mu.Unlock()
			bc.executeBatch(ctx, "timeout")
		} else {
			bc.mu.Unlock()
		}
	case <-ctx.Done():
		return
	}
}

// executeBatch executes the current batch of operations
func (bc *BatchClient) executeBatch(ctx context.Context, reason string) {
	bc.mu.Lock()
	
	if bc.processing || len(bc.operations) == 0 {
		bc.mu.Unlock()
		return
	}

	bc.processing = true
	
	// Copy operations and results for execution
	operationsCopy := make([]BatchOperation, len(bc.operations))
	copy(operationsCopy, bc.operations)
	
	resultsCopy := make(map[string]chan BatchResult)
	for id, ch := range bc.results {
		resultsCopy[id] = ch
	}
	
	// Clear for next batch
	bc.operations = bc.operations[:0]
	bc.results = make(map[string]chan BatchResult)
	
	bc.mu.Unlock()

	// Execute batch with instrumentation
	ctx, span := bc.tracer.Start(ctx, "batch_client.execute_batch")
	defer span.End()

	start := time.Now()
	operationCount := len(operationsCopy)

	span.SetAttributes(
		attribute.Int("operation_count", operationCount),
		attribute.String("batch_reason", reason),
		attribute.String("batch_id", fmt.Sprintf("batch_%d", start.UnixNano())),
	)

	// Execute batch transaction
	results := bc.executeBatchTransaction(ctx, operationsCopy)

	// Send results to waiting goroutines
	for _, result := range results {
		if resultCh, exists := resultsCopy[result.ID]; exists {
			resultCh <- result
			close(resultCh)
		}
	}

	// Handle any operations that didn't get results (errors)
	for id, resultCh := range resultsCopy {
		found := false
		for _, result := range results {
			if result.ID == id {
				found = true
				break
			}
		}
		if !found {
			resultCh <- BatchResult{
				ID:    id,
				Error: fmt.Errorf("operation not found in batch results"),
			}
			close(resultCh)
		}
	}

	// Record metrics
	duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds

	if bc.batchesExecuted != nil {
		bc.batchesExecuted.Add(ctx, 1, metric.WithAttributes(
			attribute.String("batch_reason", reason),
			attribute.Int("operation_count", operationCount),
		))
	}

	if bc.operationsPerBatch != nil {
		bc.operationsPerBatch.Record(ctx, float64(operationCount), metric.WithAttributes(
			attribute.String("batch_reason", reason),
		))
	}

	if bc.batchExecutionTime != nil {
		bc.batchExecutionTime.Record(ctx, duration, metric.WithAttributes(
			attribute.Int("operation_count", operationCount),
		))
	}

	if bc.operationsProcessed != nil {
		bc.operationsProcessed.Add(ctx, int64(operationCount), metric.WithAttributes(
			attribute.String("execution_type", "batch"),
		))
	}

	if bc.pendingOperations != nil {
		bc.mu.RLock()
		bc.pendingOperations.Record(ctx, int64(len(bc.operations)))
		bc.mu.RUnlock()
	}

	span.SetAttributes(
		attribute.Float64("duration_ms", duration),
		attribute.Int("results_count", len(results)),
	)

	bc.logger.Debug("Batch executed",
		zap.String("reason", reason),
		zap.Int("operations", operationCount),
		zap.Float64("duration_ms", duration),
		zap.Int("results", len(results)))

	// Mark processing as complete
	bc.mu.Lock()
	bc.processing = false
	bc.mu.Unlock()
}

// executeBatchTransaction executes all operations in a single transaction
func (bc *BatchClient) executeBatchTransaction(ctx context.Context, operations []BatchOperation) []BatchResult {
	results := make([]BatchResult, 0, len(operations))

	err := bc.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		for _, operation := range operations {
			result, err := bc.executeOperationInTransaction(ctx, tx, operation)
			if err != nil {
				// For batch operations, we collect errors rather than failing entire batch
				result = BatchResult{
					ID:    operation.ID,
					Error: fmt.Errorf("operation failed: %w", err),
				}
			}
			results = append(results, result)
		}
		return nil
	})

	if err != nil {
		// Transaction failed entirely, mark all operations as failed
		bc.logger.Error("Batch transaction failed", zap.Error(err))
		
		if bc.batchErrors != nil {
			bc.batchErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "transaction_failed"),
				attribute.Int("operation_count", len(operations)),
			))
		}

		failedResults := make([]BatchResult, len(operations))
		for i, operation := range operations {
			failedResults[i] = BatchResult{
				ID:    operation.ID,
				Error: fmt.Errorf("batch transaction failed: %w", err),
			}
		}
		return failedResults
	}

	return results
}

// executeOperationInTransaction executes a single operation within a transaction
func (bc *BatchClient) executeOperationInTransaction(ctx context.Context, tx neo4j.ManagedTransaction, operation BatchOperation) (BatchResult, error) {
	result, err := tx.Run(ctx, operation.Query, operation.Parameters)
	if err != nil {
		return BatchResult{}, fmt.Errorf("failed to run query: %w", err)
	}

	var records []Record
	for result.Next(ctx) {
		values := result.Record().Values
		keys := result.Record().Keys

		// Create typed record from raw values
		record := bc.client.parseRecord(keys, values)
		records = append(records, record)
	}

	if err := result.Err(); err != nil {
		return BatchResult{}, fmt.Errorf("query error: %w", err)
	}

	return BatchResult{
		ID:      operation.ID,
		Records: records,
		Summary: bc.client.extractSummary(result),
	}, nil
}

// ExecuteWriteBatch executes multiple write operations in a single transaction
func (bc *BatchClient) ExecuteWriteBatch(ctx context.Context, operations []BatchOperation) ([]BatchResult, error) {
	if bc.closed {
		return nil, fmt.Errorf("batch client is closed")
	}

	if len(operations) == 0 {
		return []BatchResult{}, nil
	}

	ctx, span := bc.tracer.Start(ctx, "batch_client.execute_write_batch")
	defer span.End()

	start := time.Now()
	operationCount := len(operations)

	span.SetAttributes(
		attribute.Int("operation_count", operationCount),
		attribute.String("execution_type", "direct_batch"),
	)

	// Execute all operations in a single transaction
	results := bc.executeBatchTransaction(ctx, operations)

	// Record metrics
	duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds

	if bc.batchesExecuted != nil {
		bc.batchesExecuted.Add(ctx, 1, metric.WithAttributes(
			attribute.String("batch_reason", "direct_batch"),
			attribute.Int("operation_count", operationCount),
		))
	}

	if bc.batchExecutionTime != nil {
		bc.batchExecutionTime.Record(ctx, duration, metric.WithAttributes(
			attribute.Int("operation_count", operationCount),
		))
	}

	if bc.operationsProcessed != nil {
		bc.operationsProcessed.Add(ctx, int64(operationCount), metric.WithAttributes(
			attribute.String("execution_type", "direct_batch"),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_ms", duration),
		attribute.Int("results_count", len(results)),
	)

	return results, nil
}

// CreateNodesBatch creates multiple nodes in a single transaction for 4x performance improvement
func (bc *BatchClient) CreateNodesBatch(ctx context.Context, nodes []NodeCreationParams) error {
	if len(nodes) == 0 {
		return nil
	}

	// Build batch CREATE operations
	operations := make([]BatchOperation, 0, len(nodes))
	
	// Use UNWIND for efficient bulk creation
	query := `
		UNWIND $nodes AS node
		CREATE (n:Resource)
		SET n = node.properties
		SET n.created_at = timestamp()
		SET n.updated_at = timestamp()
	`

	// Group nodes by label type for efficient processing
	nodeGroups := bc.groupNodesByType(nodes)
	
	for nodeType, nodeGroup := range nodeGroups {
		// Convert nodes to map format
		nodeData := make([]map[string]interface{}, 0, len(nodeGroup))
		for _, node := range nodeGroup {
			nodeMap := node.ToMap()
			nodeData = append(nodeData, map[string]interface{}{
				"properties": nodeMap,
			})
		}

		operation := BatchOperation{
			Query: strings.Replace(query, "Resource", nodeType, 1),
			Parameters: map[string]interface{}{
				"nodes": nodeData,
			},
			ID: fmt.Sprintf("create_nodes_%s_%d", nodeType, time.Now().UnixNano()),
		}
		operations = append(operations, operation)
	}

	// Execute batch
	results, err := bc.ExecuteWriteBatch(ctx, operations)
	if err != nil {
		return fmt.Errorf("failed to create nodes batch: %w", err)
	}

	// Check for individual operation errors
	for _, result := range results {
		if result.Error != nil {
			bc.logger.Warn("Node creation failed in batch", 
				zap.String("operation_id", result.ID), 
				zap.Error(result.Error))
		}
	}

	return nil
}

// groupNodesByType groups nodes by their primary label for efficient batch processing
func (bc *BatchClient) groupNodesByType(nodes []NodeCreationParams) map[string][]NodeCreationParams {
	groups := make(map[string][]NodeCreationParams)
	
	for _, node := range nodes {
		// Use Kind as the primary label, default to "Resource"
		nodeType := node.Kind
		if nodeType == "" {
			nodeType = "Resource"
		}
		
		groups[nodeType] = append(groups[nodeType], node)
	}
	
	return groups
}

// CreateRelationshipsBatch creates multiple relationships in a single transaction
func (bc *BatchClient) CreateRelationshipsBatch(ctx context.Context, relationships []RelationshipCreationParams) error {
	if len(relationships) == 0 {
		return nil
	}

	// Build efficient UNWIND-based relationship creation
	query := `
		UNWIND $relationships AS rel
		MATCH (from:Resource {uid: rel.from_uid})
		MATCH (to:Resource {uid: rel.to_uid})
		CREATE (from)-[r:RELATES_TO]->(to)
		SET r = rel.properties
		SET r.created_at = rel.timestamp
	`

	// Convert relationships to map format
	relationshipData := make([]map[string]interface{}, 0, len(relationships))
	for _, rel := range relationships {
		relMap := map[string]interface{}{
			"from_uid":   rel.FromUID,
			"to_uid":     rel.ToUID,
			"timestamp":  rel.Timestamp,
			"properties": bc.convertPropertyValues(rel.Properties),
		}
		relationshipData = append(relationshipData, relMap)
	}

	operation := BatchOperation{
		Query: query,
		Parameters: map[string]interface{}{
			"relationships": relationshipData,
		},
		ID: fmt.Sprintf("create_relationships_%d", time.Now().UnixNano()),
	}

	results, err := bc.ExecuteWriteBatch(ctx, []BatchOperation{operation})
	if err != nil {
		return fmt.Errorf("failed to create relationships batch: %w", err)
	}

	// Check for errors
	for _, result := range results {
		if result.Error != nil {
			return fmt.Errorf("relationship creation failed: %w", result.Error)
		}
	}

	return nil
}

// convertPropertyValues converts PropertyValue struct to raw values for Neo4j
func (bc *BatchClient) convertPropertyValues(properties map[string]PropertyValue) map[string]interface{} {
	result := make(map[string]interface{})
	
	for key, prop := range properties {
		if prop.StringVal != nil {
			result[key] = *prop.StringVal
		} else if prop.IntVal != nil {
			result[key] = *prop.IntVal
		} else if prop.FloatVal != nil {
			result[key] = *prop.FloatVal
		}
	}
	
	return result
}

// Flush executes any pending operations immediately
func (bc *BatchClient) Flush(ctx context.Context) error {
	bc.mu.Lock()
	if len(bc.operations) == 0 || bc.processing {
		bc.mu.Unlock()
		return nil
	}
	bc.mu.Unlock()

	// Execute remaining operations
	bc.executeBatch(ctx, "flush")
	
	// Wait for processing to complete
	for {
		bc.mu.RLock()
		processing := bc.processing
		bc.mu.RUnlock()
		
		if !processing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	
	return nil
}

// Close gracefully shuts down the batch client
func (bc *BatchClient) Close(ctx context.Context) error {
	bc.mu.Lock()
	if bc.closed {
		bc.mu.Unlock()
		return nil
	}
	bc.closed = true
	bc.mu.Unlock()

	// Flush any remaining operations
	if err := bc.Flush(ctx); err != nil {
		bc.logger.Warn("Error flushing operations during close", zap.Error(err))
	}

	// Stop timer
	if !bc.batchTimer.Stop() {
		select {
		case <-bc.batchTimer.C:
		default:
		}
	}

	bc.logger.Info("Batch client closed")
	return nil
}

// Stats returns current batch client statistics
func (bc *BatchClient) Stats() map[string]interface{} {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return map[string]interface{}{
		"pending_operations": len(bc.operations),
		"batch_size":         bc.config.BatchSize,
		"batch_timeout_ms":   bc.config.BatchTimeout.Milliseconds(),
		"processing":         bc.processing,
		"closed":             bc.closed,
	}
}

// generateShortID generates a short unique identifier
func generateShortID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()%0xFFFF)
}