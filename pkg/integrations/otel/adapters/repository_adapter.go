package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
	"github.com/yairfalse/tapio/pkg/integrations/otel/ports"
)

// PostgreSQLTraceRepositoryAdapter implements the TraceRepositoryPort using PostgreSQL
// This adapter demonstrates hexagonal architecture by implementing the port interface
// and handling all database-specific concerns outside the domain core
type PostgreSQLTraceRepositoryAdapter[T domain.TraceData] struct {
	db            *sql.DB
	config        RepositoryConfig
	schemaManager *SchemaManager
	queryBuilder  *QueryBuilder
	encoder       BinaryEncoder[T]
	metrics       *RepositoryMetrics

	// Connection pooling and management
	connectionPool *ConnectionPool
	healthChecker  *HealthChecker

	// Query optimization
	preparedStmts map[string]*sql.Stmt
	stmtMutex     sync.RWMutex

	// Event sourcing support
	eventStore  *EventStore
	projections map[string]*Projection

	// Performance optimization
	batchProcessor *BatchProcessor[T]
	asyncWriter    *AsyncWriter[T]
	cacheLayer     *CacheLayer[T]

	// Migration and versioning
	migrator       *Migrator
	versionManager *VersionManager
}

// RepositoryConfig configures the repository adapter behavior
type RepositoryConfig struct {
	// Database connection
	ConnectionString   string
	MaxConnections     int
	MaxIdleConnections int
	ConnectionTimeout  time.Duration
	QueryTimeout       time.Duration

	// Performance settings
	BatchSize         int
	AsyncBufferSize   int
	EnableBatching    bool
	EnableAsyncWrites bool
	EnableCaching     bool
	CacheTTL          time.Duration

	// Reliability settings
	RetryAttempts       int
	RetryBackoff        time.Duration
	EnableDeadlockRetry bool
	HealthCheckInterval time.Duration

	// Event sourcing
	EnableEventSourcing bool
	EventTableName      string
	SnapshotInterval    int64

	// Schema management
	SchemaName          string
	TablePrefix         string
	EnableAutoMigration bool

	// Monitoring
	EnableMetrics      bool
	SlowQueryThreshold time.Duration
	EnableQueryLogging bool
}

// NewPostgreSQLTraceRepositoryAdapter creates a new PostgreSQL repository adapter
func NewPostgreSQLTraceRepositoryAdapter[T domain.TraceData](
	db *sql.DB,
	config RepositoryConfig,
) (*PostgreSQLTraceRepositoryAdapter[T], error) {

	// Apply configuration defaults
	applyRepositoryDefaults(&config)

	// Initialize components
	schemaManager := NewSchemaManager(db, config.SchemaName, config.TablePrefix)
	queryBuilder := NewQueryBuilder(config.SchemaName, config.TablePrefix)
	encoder := NewBinaryEncoder[T](BinaryEncoderConfig{})

	adapter := &PostgreSQLTraceRepositoryAdapter[T]{
		db:            db,
		config:        config,
		schemaManager: schemaManager,
		queryBuilder:  queryBuilder,
		encoder:       encoder,
		preparedStmts: make(map[string]*sql.Stmt),
		projections:   make(map[string]*Projection),
	}

	// Initialize connection pool
	if err := adapter.initializeConnectionPool(); err != nil {
		return nil, fmt.Errorf("failed to initialize connection pool: %w", err)
	}

	// Initialize schema
	if config.EnableAutoMigration {
		if err := adapter.initializeSchema(); err != nil {
			return nil, fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	// Initialize event sourcing components
	if config.EnableEventSourcing {
		if err := adapter.initializeEventSourcing(); err != nil {
			return nil, fmt.Errorf("failed to initialize event sourcing: %w", err)
		}
	}

	// Initialize performance components
	if err := adapter.initializePerformanceComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize performance components: %w", err)
	}

	// Start health monitoring
	go adapter.startHealthMonitoring()

	return adapter, nil
}

// SaveSpan implements TraceRepositoryPort.SaveSpan
func (r *PostgreSQLTraceRepositoryAdapter[T]) SaveSpan(
	ctx context.Context,
	span domain.SpanSnapshot[T],
) error {
	startTime := time.Now()
	defer func() {
		r.metrics.RecordOperation("save_span", time.Since(startTime), true)
	}()

	// Use async writer if enabled
	if r.config.EnableAsyncWrites {
		return r.asyncWriter.WriteSpan(ctx, span)
	}

	// Synchronous write
	return r.saveSpanSync(ctx, span)
}

// SaveSpanBatch implements TraceRepositoryPort.SaveSpanBatch with optimized batch processing
func (r *PostgreSQLTraceRepositoryAdapter[T]) SaveSpanBatch(
	ctx context.Context,
	spans []domain.SpanSnapshot[T],
) error {
	startTime := time.Now()
	defer func() {
		r.metrics.RecordBatchOperation("save_span_batch", len(spans), time.Since(startTime), true)
	}()

	if len(spans) == 0 {
		return nil
	}

	// Use batch processor for large batches
	if len(spans) > r.config.BatchSize {
		return r.batchProcessor.ProcessSpanBatch(ctx, spans)
	}

	// Direct batch processing for smaller batches
	return r.saveSpanBatchSync(ctx, spans)
}

// GetSpan implements TraceRepositoryPort.GetSpan with caching
func (r *PostgreSQLTraceRepositoryAdapter[T]) GetSpan(
	ctx context.Context,
	traceID domain.TraceID,
	spanID domain.SpanID,
) (domain.SpanSnapshot[T], error) {
	startTime := time.Now()
	defer func() {
		r.metrics.RecordOperation("get_span", time.Since(startTime), true)
	}()

	// Check cache first
	if r.config.EnableCaching {
		if span, err := r.cacheLayer.GetSpan(ctx, traceID, spanID); err == nil {
			r.metrics.RecordCacheHit("get_span")
			return span, nil
		}
		r.metrics.RecordCacheMiss("get_span")
	}

	// Database query
	span, err := r.getSpanFromDB(ctx, traceID, spanID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if r.config.EnableCaching {
		r.cacheLayer.SetSpan(ctx, traceID, spanID, span, r.config.CacheTTL)
	}

	return span, nil
}

// FindSpans implements TraceRepositoryPort.FindSpans with optimized queries
func (r *PostgreSQLTraceRepositoryAdapter[T]) FindSpans(
	ctx context.Context,
	query ports.SpanQuery,
) ([]domain.SpanSnapshot[T], error) {
	startTime := time.Now()
	defer func() {
		r.metrics.RecordOperation("find_spans", time.Since(startTime), true)
	}()

	// Build optimized SQL query
	sqlQuery, args, err := r.queryBuilder.BuildSpanQuery(query)
	if err != nil {
		return nil, fmt.Errorf("failed to build span query: %w", err)
	}

	// Execute query with timeout
	queryCtx, cancel := context.WithTimeout(ctx, r.config.QueryTimeout)
	defer cancel()

	rows, err := r.db.QueryContext(queryCtx, sqlQuery, args...)
	if err != nil {
		r.metrics.RecordError("find_spans", err)
		return nil, fmt.Errorf("failed to execute span query: %w", err)
	}
	defer rows.Close()

	// Parse results
	var spans []domain.SpanSnapshot[T]
	for rows.Next() {
		span, err := r.scanSpanRow(rows)
		if err != nil {
			r.metrics.RecordError("find_spans_scan", err)
			return nil, fmt.Errorf("failed to scan span row: %w", err)
		}
		spans = append(spans, span)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating span rows: %w", err)
	}

	return spans, nil
}

// StreamSpans implements TraceRepositoryPort.StreamSpans for real-time data access
func (r *PostgreSQLTraceRepositoryAdapter[T]) StreamSpans(
	ctx context.Context,
	query ports.SpanQuery,
) (<-chan domain.SpanSnapshot[T], error) {
	spanChan := make(chan domain.SpanSnapshot[T], r.config.AsyncBufferSize)

	go func() {
		defer close(spanChan)

		// Build streaming query
		sqlQuery, args, err := r.queryBuilder.BuildStreamingSpanQuery(query)
		if err != nil {
			r.metrics.RecordError("stream_spans_build", err)
			return
		}

		// Execute streaming query
		rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
		if err != nil {
			r.metrics.RecordError("stream_spans_execute", err)
			return
		}
		defer rows.Close()

		// Stream results
		for rows.Next() {
			span, err := r.scanSpanRow(rows)
			if err != nil {
				r.metrics.RecordError("stream_spans_scan", err)
				continue
			}

			select {
			case spanChan <- span:
			case <-ctx.Done():
				return
			}
		}
	}()

	return spanChan, nil
}

// Event sourcing methods

// AppendTraceEvents implements TraceRepositoryPort.AppendTraceEvents
func (r *PostgreSQLTraceRepositoryAdapter[T]) AppendTraceEvents(
	ctx context.Context,
	traceID domain.TraceID,
	events []domain.TraceEvent,
) error {
	if !r.config.EnableEventSourcing {
		return fmt.Errorf("event sourcing not enabled")
	}

	return r.eventStore.AppendEvents(ctx, traceID.String(), events)
}

// GetTraceEvents implements TraceRepositoryPort.GetTraceEvents
func (r *PostgreSQLTraceRepositoryAdapter[T]) GetTraceEvents(
	ctx context.Context,
	traceID domain.TraceID,
	fromVersion int64,
) ([]domain.TraceEvent, error) {
	if !r.config.EnableEventSourcing {
		return nil, fmt.Errorf("event sourcing not enabled")
	}

	return r.eventStore.GetEvents(ctx, traceID.String(), fromVersion)
}

// Private implementation methods

func (r *PostgreSQLTraceRepositoryAdapter[T]) saveSpanSync(
	ctx context.Context,
	span domain.SpanSnapshot[T],
) error {
	// Encode span data
	spanData, err := r.encoder.EncodeSpan(span)
	if err != nil {
		return fmt.Errorf("failed to encode span: %w", err)
	}

	// Prepare insert statement
	stmt, err := r.getPreparedStatement("insert_span")
	if err != nil {
		return fmt.Errorf("failed to get prepared statement: %w", err)
	}

	// Execute insert with retry logic
	return r.executeWithRetry(ctx, func(ctx context.Context) error {
		_, err := stmt.ExecContext(ctx,
			span.GetTraceID(),
			span.GetSpanID(),
			span.GetParentSpanID(),
			span.GetName(),
			span.GetKind(),
			span.GetStartTime(),
			span.GetEndTime(),
			spanData,
			time.Now(),
		)
		return err
	})
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) saveSpanBatchSync(
	ctx context.Context,
	spans []domain.SpanSnapshot[T],
) error {
	// Begin transaction
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare batch insert
	stmt, err := tx.PrepareContext(ctx, r.queryBuilder.GetBatchInsertSQL())
	if err != nil {
		return fmt.Errorf("failed to prepare batch insert: %w", err)
	}
	defer stmt.Close()

	// Insert spans in batch
	for _, span := range spans {
		spanData, err := r.encoder.EncodeSpan(span)
		if err != nil {
			return fmt.Errorf("failed to encode span: %w", err)
		}

		_, err = stmt.ExecContext(ctx,
			span.GetTraceID(),
			span.GetSpanID(),
			span.GetParentSpanID(),
			span.GetName(),
			span.GetKind(),
			span.GetStartTime(),
			span.GetEndTime(),
			spanData,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to insert span: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch insert: %w", err)
	}

	return nil
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) getSpanFromDB(
	ctx context.Context,
	traceID domain.TraceID,
	spanID domain.SpanID,
) (domain.SpanSnapshot[T], error) {
	stmt, err := r.getPreparedStatement("select_span")
	if err != nil {
		return nil, fmt.Errorf("failed to get prepared statement: %w", err)
	}

	row := stmt.QueryRowContext(ctx, traceID, spanID)
	return r.scanSpanRow(row)
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) scanSpanRow(scanner interface{}) (domain.SpanSnapshot[T], error) {
	var (
		traceID   domain.TraceID
		spanID    domain.SpanID
		parentID  domain.SpanID
		name      string
		kind      domain.SpanKind
		startTime time.Time
		endTime   time.Time
		spanData  []byte
		createdAt time.Time
	)

	// Type assertion for different scanner types
	switch s := scanner.(type) {
	case *sql.Row:
		err := s.Scan(&traceID, &spanID, &parentID, &name, &kind, &startTime, &endTime, &spanData, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan span row: %w", err)
		}
	case *sql.Rows:
		err := s.Scan(&traceID, &spanID, &parentID, &name, &kind, &startTime, &endTime, &spanData, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan span rows: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported scanner type")
	}

	// Decode span data
	span, err := r.encoder.DecodeSpan(spanData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode span data: %w", err)
	}

	return span, nil
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) getPreparedStatement(name string) (*sql.Stmt, error) {
	r.stmtMutex.RLock()
	stmt, exists := r.preparedStmts[name]
	r.stmtMutex.RUnlock()

	if exists {
		return stmt, nil
	}

	// Prepare statement if not exists
	r.stmtMutex.Lock()
	defer r.stmtMutex.Unlock()

	// Double-check after acquiring write lock
	if stmt, exists := r.preparedStmts[name]; exists {
		return stmt, nil
	}

	// Get SQL for statement
	sqlQuery, err := r.queryBuilder.GetPreparedSQL(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get SQL for statement %s: %w", name, err)
	}

	// Prepare statement
	stmt, err = r.db.Prepare(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement %s: %w", name, err)
	}

	r.preparedStmts[name] = stmt
	return stmt, nil
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) executeWithRetry(
	ctx context.Context,
	operation func(context.Context) error,
) error {
	var lastErr error

	for attempt := 0; attempt <= r.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * r.config.RetryBackoff
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := operation(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !r.isRetryableError(err) {
			break
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", r.config.RetryAttempts, lastErr)
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) isRetryableError(err error) bool {
	// Implement PostgreSQL-specific error analysis
	// Check for deadlocks, connection errors, temporary failures, etc.
	// This is a simplified implementation
	errStr := err.Error()
	retryableErrors := []string{
		"deadlock detected",
		"connection lost",
		"temporary failure",
		"timeout",
	}

	for _, retryable := range retryableErrors {
		if contains(errStr, retryable) {
			return true
		}
	}

	return false
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) initializeConnectionPool() error {
	r.connectionPool = NewConnectionPool(ConnectionPoolConfig{
		MaxConnections:     r.config.MaxConnections,
		MaxIdleConnections: r.config.MaxIdleConnections,
		ConnectionTimeout:  r.config.ConnectionTimeout,
	})

	return r.connectionPool.Initialize(r.db)
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) initializeSchema() error {
	return r.schemaManager.InitializeSchema()
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) initializeEventSourcing() error {
	r.eventStore = NewEventStore(r.db, EventStoreConfig{
		TableName:        r.config.EventTableName,
		SchemaName:       r.config.SchemaName,
		SnapshotInterval: r.config.SnapshotInterval,
	})

	return r.eventStore.Initialize()
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) initializePerformanceComponents() error {
	// Initialize metrics
	if r.config.EnableMetrics {
		r.metrics = NewRepositoryMetrics()
	}

	// Initialize batch processor
	if r.config.EnableBatching {
		r.batchProcessor = NewBatchProcessor[T](BatchProcessorConfig{
			BatchSize:    r.config.BatchSize,
			FlushTimeout: time.Second * 5,
		})
	}

	// Initialize async writer
	if r.config.EnableAsyncWrites {
		r.asyncWriter = NewAsyncWriter[T](AsyncWriterConfig{
			BufferSize:   r.config.AsyncBufferSize,
			FlushTimeout: time.Second * 1,
		})
	}

	// Initialize cache layer
	if r.config.EnableCaching {
		r.cacheLayer = NewCacheLayer[T](CacheLayerConfig{
			TTL:     r.config.CacheTTL,
			MaxSize: 10000,
		})
	}

	return nil
}

func (r *PostgreSQLTraceRepositoryAdapter[T]) startHealthMonitoring() {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := r.healthChecker.CheckHealth(r.db); err != nil {
			r.metrics.RecordHealthCheckFailure(err)
		}
	}
}

// DeleteSpan implements TraceRepositoryPort.DeleteSpan
func (r *PostgreSQLTraceRepositoryAdapter[T]) DeleteSpan(
	ctx context.Context,
	traceID domain.TraceID,
	spanID domain.SpanID,
) error {
	stmt, err := r.getPreparedStatement("delete_span")
	if err != nil {
		return fmt.Errorf("failed to get prepared statement: %w", err)
	}

	_, err = stmt.ExecContext(ctx, traceID, spanID)
	if err != nil {
		return fmt.Errorf("failed to delete span: %w", err)
	}

	// Invalidate cache
	if r.config.EnableCaching {
		r.cacheLayer.InvalidateSpan(ctx, traceID, spanID)
	}

	return nil
}

// Additional methods would implement the remaining TraceRepositoryPort interface...

// Helper functions
func applyRepositoryDefaults(config *RepositoryConfig) {
	if config.MaxConnections == 0 {
		config.MaxConnections = 25
	}
	if config.MaxIdleConnections == 0 {
		config.MaxIdleConnections = 5
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 30 * time.Second
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 30 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.AsyncBufferSize == 0 {
		config.AsyncBufferSize = 1000
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}
	if config.SlowQueryThreshold == 0 {
		config.SlowQueryThreshold = time.Second
	}
}

func contains(s, substr string) bool {
	// Simple string contains check
	return len(s) >= len(substr) && s[:len(substr)] == substr
}

// Supporting types for the adapter implementation would be defined here...
// These include BinaryEncoder, ConnectionPool, QueryBuilder, etc.
