package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer implements DNS problem detection (negative observer - only tracks problems)
type Observer struct {
	*base.BaseObserver        // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// DNS-specific fields
	config *Config
	logger *zap.Logger
	name   string

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Problem tracking
	mu             sync.RWMutex
	recentProblems map[string]*ProblemTracker // Track repeated problems
	stats          QueryStats                 // Overall statistics

	// OpenTelemetry instrumentation
	tracer           trace.Tracer
	problemsDetected metric.Int64Counter
	slowQueries      metric.Int64Counter
	timeouts         metric.Int64Counter
	nxdomains        metric.Int64Counter
	servfails        metric.Int64Counter
	queryLatency     metric.Float64Histogram
	eventsProcessed  metric.Int64Counter
	errorsTotal      metric.Int64Counter
}

// ProblemTracker tracks repeated DNS problems for a specific query
type ProblemTracker struct {
	QueryName    string
	FirstSeen    time.Time
	LastSeen     time.Time
	Count        int
	ProblemTypes map[DNSProblemType]int
}

// NewObserver creates a new DNS problem observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize OpenTelemetry
	meter := otel.Meter("tapio.observers.dns")
	tracer := otel.Tracer("tapio.observers.dns")

	// Create metrics
	problemsDetected, _ := meter.Int64Counter(
		fmt.Sprintf("%s_problems_total", name),
		metric.WithDescription("Total DNS problems detected"),
	)
	slowQueries, _ := meter.Int64Counter(
		fmt.Sprintf("%s_slow_queries_total", name),
		metric.WithDescription("Total slow DNS queries"),
	)
	timeouts, _ := meter.Int64Counter(
		fmt.Sprintf("%s_timeouts_total", name),
		metric.WithDescription("Total DNS timeouts"),
	)
	nxdomains, _ := meter.Int64Counter(
		fmt.Sprintf("%s_nxdomains_total", name),
		metric.WithDescription("Total NXDOMAIN responses"),
	)
	servfails, _ := meter.Int64Counter(
		fmt.Sprintf("%s_servfails_total", name),
		metric.WithDescription("Total SERVFAIL responses"),
	)
	queryLatency, _ := meter.Float64Histogram(
		fmt.Sprintf("%s_query_latency_ms", name),
		metric.WithDescription("DNS query latency in milliseconds"),
	)
	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	errorsTotal, _ := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in observer"),
	)

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger.Named(name)),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger.Named(name)),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		recentProblems:      make(map[string]*ProblemTracker),
		tracer:              tracer,
		problemsDetected:    problemsDetected,
		slowQueries:         slowQueries,
		timeouts:            timeouts,
		nxdomains:           nxdomains,
		servfails:           servfails,
		queryLatency:        queryLatency,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
	}

	o.logger.Info("DNS problem observer created",
		zap.String("name", name),
		zap.Int("slow_threshold_ms", config.SlowQueryThresholdMs),
		zap.Int("timeout_ms", config.TimeoutMs))

	return o, nil
}

// Start begins DNS problem detection
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting DNS problem observer")

	// Platform-specific start (eBPF on Linux, fallback otherwise)
	if err := o.startPlatform(); err != nil {
		return fmt.Errorf("starting platform: %w", err)
	}

	// Start problem cleanup goroutine
	o.LifecycleManager.Start("cleanup", func() {
		o.cleanupOldProblems(ctx)
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("DNS problem observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping DNS problem observer")

	o.BaseObserver.SetHealthy(false)

	// Stop platform-specific components
	o.stopPlatform()

	// Stop lifecycle
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Error("Error stopping lifecycle", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.logger.Info("DNS problem observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// cleanupOldProblems removes old problem trackers
func (o *Observer) cleanupOldProblems(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.doCleanup()
		}
	}
}

// doCleanup performs the actual cleanup
func (o *Observer) doCleanup() {
	o.mu.Lock()
	defer o.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(o.config.RepeatWindowSec) * time.Second)
	for query, tracker := range o.recentProblems {
		if tracker.LastSeen.Before(cutoff) {
			delete(o.recentProblems, query)
		}
	}
}

// trackProblem records a DNS problem for repeat detection
func (o *Observer) trackProblem(event *DNSEvent) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	queryName := event.GetQueryName()
	tracker, exists := o.recentProblems[queryName]

	if !exists {
		tracker = &ProblemTracker{
			QueryName:    queryName,
			FirstSeen:    time.Now(),
			LastSeen:     time.Now(),
			Count:        1,
			ProblemTypes: make(map[DNSProblemType]int),
		}
		tracker.ProblemTypes[event.ProblemType] = 1
		o.recentProblems[queryName] = tracker
		return false // First occurrence
	}

	// Update existing tracker
	tracker.LastSeen = time.Now()
	tracker.Count++
	tracker.ProblemTypes[event.ProblemType]++

	// Check if this is a repeated problem
	return tracker.Count >= o.config.RepeatThreshold
}

// GetStats returns current statistics
func (o *Observer) GetStats() QueryStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.stats
}

// Statistics returns observer statistics (implements base.Observer interface)
func (o *Observer) Statistics() *domain.CollectorStats {
	return o.BaseObserver.Statistics()
}

// IsHealthy returns true if the observer is healthy
func (o *Observer) IsHealthy() bool {
	return o.BaseObserver.IsHealthy()
}

// Health returns the health status of the observer
func (o *Observer) Health() *domain.HealthStatus {
	return o.BaseObserver.Health()
}
