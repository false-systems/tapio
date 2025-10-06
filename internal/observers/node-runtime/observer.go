package noderuntime

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// ObserverVersion is the version of the node-runtime observer
	ObserverVersion = "1.0.0"
)

// generateEventID creates a unique event ID for node-runtime events
func generateEventID(eventType, source string) string {
	timestamp := time.Now().UnixNano()
	data := fmt.Sprintf("%s-%s-%d", eventType, source, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("node-runtime-%s", hex.EncodeToString(hash[:])[:16])
}

// Observer implements the node-runtime metrics observer
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channel with drop counting
	*base.LifecycleManager    // Manages goroutines and graceful shutdown

	name            string
	config          *Config
	client          *http.Client
	logger          *zap.Logger
	podTraceManager *PodTraceManager

	// Collectors for kubelet endpoints
	statsCollector    *StatsCollector
	podsCollector     *PodsCollector
	healthzCollector  *HealthzCollector
	probesCollector   *ProbesCollector
	syncloopCollector *SyncloopCollector

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// node-runtime-specific metrics (optional)
	apiLatency  metric.Float64Histogram
	pollsActive metric.Int64UpDownCounter
	apiFailures metric.Int64Counter
}

// NewObserver creates a new node-runtime observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if config.Logger == nil {
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		config.Logger = logger
	}

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	apiLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_api_latency_ms", name),
		metric.WithDescription(fmt.Sprintf("API call latency for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create API latency histogram", zap.Error(err))
	}

	pollsActive, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_polls", name),
		metric.WithDescription(fmt.Sprintf("Active polling operations in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create polls active gauge", zap.Error(err))
	}

	apiFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_api_failures_total", name),
		metric.WithDescription(fmt.Sprintf("API failures in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create API failures counter", zap.Error(err))
	}

	// Create HTTP client with proper TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.Insecure,
	}

	if config.ClientCert != "" && config.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(config.ClientCert, config.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificates: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: config.RequestTimeout,
	}

	// Configure advanced BaseObserver with RingBuffer and multi-output
	baseConfig := base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: 30 * time.Second,
		ErrorRateThreshold: 0.1, // 10% error rate threshold

		// Enable RingBuffer for high-performance event processing
		EnableRingBuffer: true,
		RingBufferSize:   8192, // Must be power of 2
		BatchSize:        32,
		BatchTimeout:     10 * time.Millisecond,

		// Enable multi-output targets
		OutputTargets: base.OutputTargets{
			Channel: true,  // Local Go channel (existing behavior)
			OTEL:    true,  // Direct OTEL domain metrics emission
			Stdout:  false, // Disabled in production
		},

		Logger: config.Logger,
	}

	// Create base observer components
	baseObs := base.NewBaseObserverWithConfig(baseConfig)
	eventChanMgr := base.NewEventChannelManager(10000, name, config.Logger)

	// Helper function for extracting trace context
	extractTrace := func(ctx context.Context) (string, string) {
		span := trace.SpanFromContext(ctx)
		if span.SpanContext().IsValid() {
			return span.SpanContext().TraceID().String(), span.SpanContext().SpanID().String()
		}
		return "", ""
	}

	// Initialize collectors
	statsCollector := NewStatsCollector(
		name, config.Address,
		client,
		tracer,
		apiLatency,
		extractTrace,
	)

	podsCollector := NewPodsCollector(
		name, config.Address,
		config.Insecure,
		client,
		tracer,
		apiLatency,
		extractTrace,
	)

	healthzCollector := NewHealthzCollector(
		name, config.Address,
		config.Insecure,
		client,
		tracer,
		apiLatency,
	)

	probesCollector := NewProbesCollector(
		name, config.Address,
		config.Insecure,
		client,
		tracer,
		apiLatency,
		extractTrace,
	)

	syncloopCollector := NewSyncloopCollector(
		name, config.Address,
		config.Insecure,
		client,
		tracer,
		apiLatency,
		extractTrace,
	)

	return &Observer{
		BaseObserver:        baseObs,
		EventChannelManager: eventChanMgr,
		LifecycleManager:    base.NewLifecycleManager(context.Background(), config.Logger),

		name:            name,
		config:          config,
		client:          client,
		logger:          config.Logger,
		podTraceManager: NewPodTraceManager(),

		// Collectors
		statsCollector:    statsCollector,
		podsCollector:     podsCollector,
		healthzCollector:  healthzCollector,
		probesCollector:   probesCollector,
		syncloopCollector: syncloopCollector,

		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
		apiLatency:      apiLatency,
		pollsActive:     pollsActive,
		apiFailures:     apiFailures,
	}, nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start begins collection
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting node-runtime observer")

	// Create new lifecycle manager with provided context
	o.LifecycleManager = base.NewLifecycleManager(ctx, o.logger)

	// Verify connectivity
	if err := o.checkConnectivity(); err != nil {
		return fmt.Errorf("node-runtime connectivity check failed: %w", err)
	}

	// Start collection goroutines using LifecycleManager
	o.LifecycleManager.Start("collect-stats", func() {
		o.collectStats()
	})

	o.LifecycleManager.Start("collect-pod-metrics", func() {
		o.collectPodMetrics()
	})

	o.LifecycleManager.Start("collect-probes", func() {
		o.collectProbes()
	})

	o.LifecycleManager.Start("collect-syncloop", func() {
		o.collectSyncloop()
	})

	o.BaseObserver.SetHealthy(true)

	o.logger.Info("Node-runtime observer started",
		zap.String("address", o.config.Address),
		zap.Duration("stats_interval", o.config.StatsInterval))

	return nil
}

// Stop gracefully shuts down the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping node-runtime observer")

	// Stop lifecycle manager (waits for goroutines)
	o.LifecycleManager.Stop(5 * time.Second)

	// Stop pod trace manager
	if o.podTraceManager != nil {
		o.podTraceManager.Stop()
	}

	// Close event channel
	o.EventChannelManager.Close()

	// Mark as unhealthy
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("Node-runtime observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// checkConnectivity verifies we can reach the kubelet API
func (o *Observer) checkConnectivity() error {
	url := fmt.Sprintf("https://%s/healthz", o.config.Address)
	if o.config.Insecure {
		url = fmt.Sprintf("http://%s/healthz", o.config.Address)
	}

	resp, err := o.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node-runtime health check failed: %s", resp.Status)
	}

	return nil
}

// collectStats collects node-runtime stats summary
func (o *Observer) collectStats() {
	ticker := time.NewTicker(o.config.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchStats(); err != nil {
				o.logger.Error("Failed to fetch node-runtime stats", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchStats fetches and processes stats from kubelet API using StatsCollector
func (o *Observer) fetchStats() error {
	ctx := o.LifecycleManager.Context()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
	}

	// Use StatsCollector to fetch and build events
	events, err := o.statsCollector.Collect(ctx)
	if err != nil {
		o.BaseObserver.RecordError(err)
		return err
	}

	// Send all collected events
	for i := range events {
		o.sendCollectedEvent(ctx, &events[i])
	}

	return nil
}

// sendCollectedEvent sends a collected event and records metrics
func (o *Observer) sendCollectedEvent(ctx context.Context, event *domain.CollectorEvent) {
	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
		if o.droppedEvents != nil {
			o.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
			))
		}
	}
}

// collectPodMetrics collects pod lifecycle events
func (o *Observer) collectPodMetrics() {
	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchPodLifecycle(); err != nil {
				o.logger.Error("Failed to fetch pod lifecycle", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchPodLifecycle fetches pod status from kubelet

// fetchPodLifecycle fetches pod status from kubelet using PodsCollector
func (o *Observer) fetchPodLifecycle() error {
	ctx := o.LifecycleManager.Context()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
	}

	// Use PodsCollector to fetch and build events
	events, err := o.podsCollector.Collect(ctx)
	if err != nil {
		o.BaseObserver.RecordError(err)
		return err
	}

	// Send all collected events
	for i := range events {
		o.sendCollectedEvent(ctx, &events[i])
	}

	return nil
}

// collectProbes collects probe health metrics
func (o *Observer) collectProbes() {
	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchProbes(); err != nil {
				o.logger.Error("Failed to fetch probe metrics", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchProbes fetches probe metrics from kubelet using ProbesCollector
func (o *Observer) fetchProbes() error {
	ctx := o.LifecycleManager.Context()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_probes"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_probes"),
		))
	}

	// Use ProbesCollector to fetch and build events
	events, err := o.probesCollector.Collect(ctx)
	if err != nil {
		o.BaseObserver.RecordError(err)
		return err
	}

	// Send all collected events
	for i := range events {
		o.sendCollectedEvent(ctx, &events[i])
	}

	return nil
}

// collectSyncloop monitors kubelet syncloop health
func (o *Observer) collectSyncloop() {
	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchSyncloop(); err != nil {
				o.logger.Error("Failed to fetch syncloop health", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchSyncloop fetches syncloop health from kubelet using SyncloopCollector
func (o *Observer) fetchSyncloop() error {
	ctx := o.LifecycleManager.Context()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_syncloop"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_syncloop"),
		))
	}

	// Use SyncloopCollector to fetch and build events
	events, err := o.syncloopCollector.Collect(ctx)
	if err != nil {
		o.BaseObserver.RecordError(err)
		return err
	}

	// Send all collected events
	for i := range events {
		o.sendCollectedEvent(ctx, &events[i])
	}

	return nil
}

// Helper methods

// extractTraceContext extracts trace and span IDs from context
func (o *Observer) extractTraceContext(ctx context.Context) (traceID, spanID string) {
	traceID = observers.GenerateTraceID()
	spanID = observers.GenerateSpanID()
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
		spanID = span.SpanContext().SpanID().String()
	}
	return traceID, spanID
}

// PodTraceEntry holds trace ID with timestamp for TTL cleanup
type PodTraceEntry struct {
	TraceID   string
	Timestamp time.Time
}

// PodTraceManager manages trace IDs with TTL cleanup
type PodTraceManager struct {
	entries map[types.UID]*PodTraceEntry
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewPodTraceManager creates a new pod trace manager with TTL cleanup
func NewPodTraceManager() *PodTraceManager {
	ctx, cancel := context.WithCancel(context.Background())
	ptm := &PodTraceManager{
		entries: make(map[types.UID]*PodTraceEntry),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start cleanup goroutine
	go ptm.cleanup()

	return ptm
}

// GetOrGenerate gets existing trace ID or generates new one
func (ptm *PodTraceManager) GetOrGenerate(podUID types.UID) string {
	ptm.mu.RLock()
	if entry, exists := ptm.entries[podUID]; exists {
		ptm.mu.RUnlock()
		return entry.TraceID
	}
	ptm.mu.RUnlock()

	// Generate new trace ID
	ptm.mu.Lock()
	traceID := observers.GenerateTraceID()
	ptm.entries[podUID] = &PodTraceEntry{
		TraceID:   traceID,
		Timestamp: time.Now(),
	}
	ptm.mu.Unlock()

	return traceID
}

// Count returns the number of tracked pod traces
func (ptm *PodTraceManager) Count() int {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	return len(ptm.entries)
}

// Stop stops the cleanup goroutine
func (ptm *PodTraceManager) Stop() {
	ptm.cancel()
}

// cleanup runs periodic cleanup of expired entries (every 5 minutes)
func (ptm *PodTraceManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ptm.ctx.Done():
			return
		case <-ticker.C:
			ptm.cleanupExpired()
		}
	}
}

// cleanupExpired removes entries older than 1 hour
func (ptm *PodTraceManager) cleanupExpired() {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	expiry := time.Now().Add(-1 * time.Hour)
	for uid, entry := range ptm.entries {
		if entry.Timestamp.Before(expiry) {
			delete(ptm.entries, uid)
		}
	}
}

// Legacy compatibility methods for migration

// Statistics returns observer statistics
func (o *Observer) Statistics() interface{} {
	stats := o.BaseObserver.Statistics()
	// Add kubelet-specific stats as custom metrics
	if stats.CustomMetrics == nil {
		stats.CustomMetrics = make(map[string]string)
	}
	stats.CustomMetrics["pod_traces"] = fmt.Sprintf("%d", o.podTraceManager.Count())
	stats.CustomMetrics["node_runtime_address"] = o.config.Address
	return stats
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	health := o.BaseObserver.Health()
	health.Component = o.name
	// Add error count from statistics
	stats := o.BaseObserver.Statistics()
	if stats != nil {
		health.ErrorCount = stats.ErrorCount
	}
	return health
}
