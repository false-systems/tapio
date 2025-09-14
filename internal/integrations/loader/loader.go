package loader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	neo4jint "github.com/yairfalse/tapio/internal/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Loader is a Neo4j loader service that subscribes to NATS JetStream observation events
// and stores them in Neo4j with proper relationships and batching
type Loader struct {
	logger *zap.Logger
	config *Config

	// NATS connection and JetStream
	nc           *nats.Conn
	js           nats.JetStreamContext
	subscription *nats.Subscription

	// Neo4j client
	neo4jClient *neo4jint.Client

	// OTEL instrumentation - REQUIRED fields
	tracer            trace.Tracer
	eventsReceived    metric.Int64Counter
	eventsProcessed   metric.Int64Counter
	eventsFailed      metric.Int64Counter
	batchesProcessed  metric.Int64Counter
	batchesFailed     metric.Int64Counter
	processingLatency metric.Float64Histogram
	storageLatency    metric.Float64Histogram
	backlogSize       metric.Int64UpDownCounter
	activeWorkers     metric.Int64UpDownCounter

	// Worker pool and batching
	batchChannel chan *domain.ObservationEvent
	workerPool   chan struct{}
	jobQueue     chan *BatchJob

	// Lifecycle management
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	shutdownMu sync.Mutex
	isShutdown atomic.Bool

	// Metrics and health
	metrics      atomic.Pointer[LoaderMetrics]
	lastActivity atomic.Pointer[time.Time]

	// Resource cleanup
	resources   []func() error
	resourcesMu sync.Mutex
}

// NewLoader creates a new Neo4j loader service
func NewLoader(logger *zap.Logger, config *Config) (*Loader, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	loader := &Loader{
		logger: logger,
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize OTEL instrumentation
	if err := loader.initOTEL(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize OTEL: %w", err)
	}

	// Initialize worker pool and channels
	loader.initWorkerPool()

	// Initialize metrics
	loader.initMetrics()

	return loader, nil
}

// initOTEL initializes OpenTelemetry instrumentation
func (l *Loader) initOTEL() error {
	// Initialize OTEL components - MANDATORY pattern
	l.tracer = otel.Tracer("integrations.loader")
	meter := otel.Meter("integrations.loader")

	var err error

	// Create metrics with descriptive names and descriptions
	l.eventsReceived, err = meter.Int64Counter(
		"loader_events_received_total",
		metric.WithDescription("Total events received by the Neo4j loader"),
	)
	if err != nil {
		l.logger.Warn("Failed to create events received counter", zap.Error(err))
	}

	l.eventsProcessed, err = meter.Int64Counter(
		"loader_events_processed_total",
		metric.WithDescription("Total events successfully processed by the Neo4j loader"),
	)
	if err != nil {
		l.logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	l.eventsFailed, err = meter.Int64Counter(
		"loader_events_failed_total",
		metric.WithDescription("Total events that failed processing in the Neo4j loader"),
	)
	if err != nil {
		l.logger.Warn("Failed to create events failed counter", zap.Error(err))
	}

	l.batchesProcessed, err = meter.Int64Counter(
		"loader_batches_processed_total",
		metric.WithDescription("Total batches successfully processed by the Neo4j loader"),
	)
	if err != nil {
		l.logger.Warn("Failed to create batches processed counter", zap.Error(err))
	}

	l.batchesFailed, err = meter.Int64Counter(
		"loader_batches_failed_total",
		metric.WithDescription("Total batches that failed processing in the Neo4j loader"),
	)
	if err != nil {
		l.logger.Warn("Failed to create batches failed counter", zap.Error(err))
	}

	l.processingLatency, err = meter.Float64Histogram(
		"loader_processing_duration_ms",
		metric.WithDescription("Processing duration for the Neo4j loader in milliseconds"),
	)
	if err != nil {
		l.logger.Warn("Failed to create processing latency histogram", zap.Error(err))
	}

	l.storageLatency, err = meter.Float64Histogram(
		"loader_storage_duration_ms",
		metric.WithDescription("Storage duration for the Neo4j loader in milliseconds"),
	)
	if err != nil {
		l.logger.Warn("Failed to create storage latency histogram", zap.Error(err))
	}

	l.backlogSize, err = meter.Int64UpDownCounter(
		"loader_backlog_size",
		metric.WithDescription("Current number of events in the processing backlog"),
	)
	if err != nil {
		l.logger.Warn("Failed to create backlog size counter", zap.Error(err))
	}

	l.activeWorkers, err = meter.Int64UpDownCounter(
		"loader_active_workers",
		metric.WithDescription("Current number of active worker goroutines"),
	)
	if err != nil {
		l.logger.Warn("Failed to create active workers counter", zap.Error(err))
	}

	return nil
}

// initWorkerPool initializes the worker pool and channels
func (l *Loader) initWorkerPool() {
	l.batchChannel = make(chan *domain.ObservationEvent, l.config.BatchSize*2)
	l.workerPool = make(chan struct{}, l.config.MaxConcurrency)
	l.jobQueue = make(chan *BatchJob, l.config.MaxConcurrency*2)
}

// initMetrics initializes the metrics structure
func (l *Loader) initMetrics() {
	now := time.Now()
	metrics := &LoaderMetrics{
		HealthStatus:      "initializing",
		LastProcessedTime: now,
	}
	l.metrics.Store(metrics)
	l.lastActivity.Store(&now)
}

// Start initializes connections and begins processing observation events
func (l *Loader) Start(ctx context.Context) error {
	ctx, span := l.tracer.Start(ctx, "loader.start")
	defer span.End()

	l.logger.Info("Starting Neo4j loader service")

	// Connect to NATS
	if err := l.connectNATS(ctx); err != nil {
		span.SetStatus(codes.Error, "Failed to connect to NATS")
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Connect to Neo4j
	if err := l.connectNeo4j(ctx); err != nil {
		span.SetStatus(codes.Error, "Failed to connect to Neo4j")
		return fmt.Errorf("failed to connect to Neo4j: %w", err)
	}

	// Setup schema and indexes
	if err := l.setupNeo4jSchema(ctx); err != nil {
		span.SetStatus(codes.Error, "Failed to setup Neo4j schema")
		return fmt.Errorf("failed to setup Neo4j schema: %w", err)
	}

	// Setup NATS subscriptions
	if err := l.setupSubscriptions(ctx); err != nil {
		span.SetStatus(codes.Error, "Failed to setup NATS subscriptions")
		return fmt.Errorf("failed to setup NATS subscriptions: %w", err)
	}

	// Start worker goroutines
	l.startWorkers(ctx)

	// Start batch aggregator
	l.startBatchAggregator(ctx)

	// Start health monitor
	l.startHealthMonitor(ctx)

	// Update metrics
	l.updateMetrics(func(m *LoaderMetrics) {
		m.HealthStatus = "running"
	})

	l.logger.Info("Neo4j loader service started successfully")
	span.SetStatus(codes.Ok, "Loader started successfully")

	// Wait for context cancellation
	<-ctx.Done()
	return l.Stop()
}

// Stop gracefully shuts down the loader service
func (l *Loader) Stop() error {
	l.shutdownMu.Lock()
	defer l.shutdownMu.Unlock()

	if l.isShutdown.Load() {
		return nil
	}

	_, span := l.tracer.Start(context.Background(), "loader.stop")
	defer span.End()

	l.logger.Info("Stopping Neo4j loader service")

	// Set shutdown flag
	l.isShutdown.Store(true)

	// Cancel context to signal shutdown
	l.cancel()

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), l.config.ShutdownTimeout)
	defer shutdownCancel()

	// Wait for workers to complete with timeout
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		l.logger.Info("All workers stopped gracefully")
	case <-shutdownCtx.Done():
		l.logger.Warn("Timeout waiting for workers to stop")
	}

	// Close channels
	l.closeChannels()

	// Cleanup resources
	l.cleanupResources()

	// Update final metrics
	l.updateMetrics(func(m *LoaderMetrics) {
		m.HealthStatus = "stopped"
	})

	l.logger.Info("Neo4j loader service stopped")
	return nil
}

// connectNATS establishes connection to NATS
func (l *Loader) connectNATS(ctx context.Context) error {
	_, span := l.tracer.Start(ctx, "loader.connect_nats")
	defer span.End()

	opts := []nats.Option{
		nats.Name("neo4j-loader"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(l.config.NATS.MaxReconnects),
		nats.ReconnectWait(l.config.NATS.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			l.logger.Error("NATS disconnected", zap.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			l.logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			l.logger.Error("NATS error", zap.Error(err))
		}),
	}

	nc, err := nats.Connect(l.config.NATS.URL, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	l.nc = nc

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}
	l.js = js

	// Add resource cleanup
	l.addResource(func() error {
		if l.nc != nil && l.nc.IsConnected() {
			l.nc.Close()
		}
		return nil
	})

	span.SetAttributes(
		attribute.String("nats.url", l.config.NATS.URL),
		attribute.Bool("nats.connected", nc.IsConnected()),
	)

	l.logger.Info("Connected to NATS successfully", zap.String("url", l.config.NATS.URL))
	return nil
}

// connectNeo4j establishes connection to Neo4j
func (l *Loader) connectNeo4j(ctx context.Context) error {
	_, span := l.tracer.Start(ctx, "loader.connect_neo4j")
	defer span.End()

	client, err := neo4jint.NewClient(l.config.Neo4j, l.logger)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}

	l.neo4jClient = client

	// Add resource cleanup
	l.addResource(func() error {
		if l.neo4jClient != nil {
			return l.neo4jClient.Close(context.Background())
		}
		return nil
	})

	span.SetAttributes(
		attribute.String("neo4j.uri", l.config.Neo4j.URI),
		attribute.String("neo4j.database", l.config.Neo4j.Database),
	)

	l.logger.Info("Connected to Neo4j successfully", zap.String("uri", l.config.Neo4j.URI))
	return nil
}

// setupNeo4jSchema creates necessary indexes and constraints
func (l *Loader) setupNeo4jSchema(ctx context.Context) error {
	_, span := l.tracer.Start(ctx, "loader.setup_neo4j_schema")
	defer span.End()

	// Create indexes for observation nodes
	indexes := []string{
		// Observation node indexes
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.id)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.timestamp)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.source)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.type)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.pid)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.container_id)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.pod_name)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.namespace)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.service_name)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.node_name)",

		// Pod node indexes
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.name, p.namespace)",
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.uid)",

		// Service node indexes
		"CREATE INDEX IF NOT EXISTS FOR (s:Service) ON (s.name, s.namespace)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Service) ON (s.uid)",

		// Node indexes
		"CREATE INDEX IF NOT EXISTS FOR (n:Node) ON (n.name)",

		// Composite indexes for common queries
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.source, o.timestamp)",
		"CREATE INDEX IF NOT EXISTS FOR (o:Observation) ON (o.namespace, o.pod_name, o.timestamp)",
	}

	for _, index := range indexes {
		if err := l.neo4jClient.ExecuteTypedWrite(ctx, func(ctx context.Context, tx *neo4jint.TypedTransaction) error {
			// No parameters needed for DDL statements
			_, err := tx.Run(ctx, index, nil)
			return err
		}); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Create constraints
	constraints := []string{
		"CREATE CONSTRAINT IF NOT EXISTS FOR (o:Observation) REQUIRE o.id IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (p:Pod) REQUIRE p.uid IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (s:Service) REQUIRE s.uid IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (n:Node) REQUIRE n.name IS UNIQUE",
	}

	for _, constraint := range constraints {
		if err := l.neo4jClient.ExecuteTypedWrite(ctx, func(ctx context.Context, tx *neo4jint.TypedTransaction) error {
			// No parameters needed for DDL statements
			_, err := tx.Run(ctx, constraint, nil)
			return err
		}); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("failed to create constraint: %w", err)
		}
	}

	span.SetAttributes(
		attribute.Int("indexes_created", len(indexes)),
		attribute.Int("constraints_created", len(constraints)),
	)

	l.logger.Info("Neo4j schema setup completed",
		zap.Int("indexes", len(indexes)),
		zap.Int("constraints", len(constraints)))
	return nil
}

// generateBatchID generates a unique batch ID
func (l *Loader) generateBatchID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("batch_%d", time.Now().UnixNano())
	}
	return "batch_" + hex.EncodeToString(bytes)
}

// updateMetrics safely updates the metrics using atomic operations
func (l *Loader) updateMetrics(updateFunc func(*LoaderMetrics)) {
	current := l.metrics.Load()
	if current == nil {
		current = &LoaderMetrics{}
	}

	// Make a copy to avoid race conditions
	updated := *current
	updateFunc(&updated)
	l.metrics.Store(&updated)

	// Update last activity
	now := time.Now()
	l.lastActivity.Store(&now)
}

// addResource adds a cleanup function to the resource list
func (l *Loader) addResource(cleanup func() error) {
	l.resourcesMu.Lock()
	defer l.resourcesMu.Unlock()
	l.resources = append(l.resources, cleanup)
}

// cleanupResources calls all registered cleanup functions
func (l *Loader) cleanupResources() {
	l.resourcesMu.Lock()
	defer l.resourcesMu.Unlock()

	for i := len(l.resources) - 1; i >= 0; i-- {
		if err := l.resources[i](); err != nil {
			l.logger.Error("Failed to cleanup resource",
				zap.Int("resource_index", i), zap.Error(err))
		}
	}
	l.resources = nil
}

// closeChannels safely closes all channels
func (l *Loader) closeChannels() {
	defer func() {
		if r := recover(); r != nil {
			l.logger.Warn("Panic during channel close", zap.Any("recover", r))
		}
	}()

	if l.batchChannel != nil {
		close(l.batchChannel)
	}
	if l.jobQueue != nil {
		close(l.jobQueue)
	}
	if l.workerPool != nil {
		close(l.workerPool)
	}
}

// GetMetrics returns current loader metrics
func (l *Loader) GetMetrics() LoaderMetrics {
	metrics := l.metrics.Load()
	if metrics == nil {
		return LoaderMetrics{HealthStatus: "unknown"}
	}
	return *metrics
}

// GetHealthStatus returns current health status
func (l *Loader) GetHealthStatus() HealthStatus {
	metrics := l.GetMetrics()

	status := HealthStatus{
		LastCheck:      time.Now(),
		NATSConnected:  l.nc != nil && l.nc.IsConnected(),
		Neo4jConnected: l.neo4jClient != nil,
		Metrics:        metrics,
		Details:        make(map[string]string),
	}

	// Determine overall health status
	if status.NATSConnected && status.Neo4jConnected && metrics.HealthStatus == "running" {
		if metrics.ErrorRate < 0.1 { // Less than 10% error rate
			status.Status = "healthy"
		} else {
			status.Status = "degraded"
			status.Warnings = append(status.Warnings, fmt.Sprintf("High error rate: %.2f%%", metrics.ErrorRate*100))
		}
	} else {
		status.Status = "unhealthy"
		if !status.NATSConnected {
			status.Errors = append(status.Errors, "NATS not connected")
		}
		if !status.Neo4jConnected {
			status.Errors = append(status.Errors, "Neo4j not connected")
		}
	}

	// Add performance details
	status.Details["throughput"] = fmt.Sprintf("%.2f events/sec", metrics.ThroughputPerSecond)
	status.Details["processing_latency"] = fmt.Sprintf("%.2f ms", metrics.ProcessingLatency)
	status.Details["storage_latency"] = fmt.Sprintf("%.2f ms", metrics.StorageLatency)
	status.Details["backlog_size"] = fmt.Sprintf("%d", metrics.BacklogSize)

	return status
}

// storeObservationEvents stores a batch of observation events in Neo4j
func (l *Loader) storeObservationEvents(ctx context.Context, events []*domain.ObservationEvent) (*StorageStats, error) {
	if len(events) == 0 {
		return &StorageStats{}, nil
	}

	startTime := time.Now()
	stats := &StorageStats{
		BatchSize: len(events),
	}

	// Use Neo4j client to store events
	if l.neo4jClient == nil {
		return nil, fmt.Errorf("neo4j client not initialized")
	}

	// Use ExecuteTypedWrite for transaction management with type-safe parameters
	err := l.neo4jClient.ExecuteTypedWrite(ctx, func(ctx context.Context, tx *neo4jint.TypedTransaction) error {
		// Store each event as a node
		for _, event := range events {
			if event == nil {
				continue
			}

			// Create node for observation event using type-safe parameters
			query := `
				CREATE (e:ObservationEvent {
					id: $id,
					timestamp: $timestamp,
					source: $source,
					type: $type
				})
				RETURN id(e) as nodeId
			`

			// Build type-safe parameters
			params := neo4jint.NewQueryParams().
				SetString("id", event.ID).
				SetTime("timestamp", event.Timestamp).
				SetString("source", event.Source).
				SetString("type", event.Type)

			result, err := tx.Run(ctx, query, params)
			if err != nil {
				return fmt.Errorf("failed to create node: %w", err)
			}

			// Consume the result
			if result.Next(ctx) {
				stats.NodesCreated++
			}

			// Create relationships based on correlation keys
			if event.PodName != nil && *event.PodName != "" {
				relQuery := `
					MERGE (p:Pod {name: $podName, namespace: $namespace})
					WITH p
					MATCH (e:ObservationEvent {id: $eventId})
					CREATE (e)-[:OBSERVED_IN]->(p)
				`

				namespace := ""
				if event.Namespace != nil {
					namespace = *event.Namespace
				}

				// Build type-safe relationship parameters
				relParams := neo4jint.NewQueryParams().
					SetString("eventId", event.ID).
					SetString("podName", *event.PodName).
					SetString("namespace", namespace)

				_, err = tx.Run(ctx, relQuery, relParams)
				if err == nil {
					stats.RelationshipsCreated++
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store events: %w", err)
	}

	stats.StorageTime = time.Since(startTime)
	return stats, nil
}
