package dns

import (
	"context"
	"fmt"
	"strings"
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
	*base.BaseObserver                               // Embed for stats/health
	*base.EventChannelManager                        // Embed for events
	lifecycleManager          *base.LifecycleManager // Not embedded to avoid method conflicts

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
	started        bool                       // Prevents double start

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
		lifecycleManager:    base.NewLifecycleManager(context.Background(), logger.Named(name)),
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

	// Start as unhealthy until explicitly started
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("DNS problem observer created",
		zap.String("name", name),
		zap.Int("slow_threshold_ms", config.SlowQueryThresholdMs),
		zap.Int("timeout_ms", config.TimeoutMs))

	return o, nil
}

// Start begins DNS problem detection
func (o *Observer) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.started {
		o.logger.Debug("Observer already started, ignoring")
		return nil // Idempotent
	}

	o.logger.Info("Starting DNS problem observer")

	// Platform-specific start (eBPF on Linux, fallback otherwise)
	if err := o.startPlatform(); err != nil {
		return fmt.Errorf("starting platform: %w", err)
	}

	// Start problem cleanup goroutine
	o.lifecycleManager.Start("cleanup", func() {
		o.cleanupOldProblems(o.lifecycleManager.Context())
	})

	o.started = true
	o.BaseObserver.SetHealthy(true)
	o.logger.Info("DNS problem observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.logger.Info("Stopping DNS problem observer")

	o.BaseObserver.SetHealthy(false)

	// Stop lifecycle first to shutdown goroutines cleanly
	if err := o.lifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Error("Error stopping lifecycle", zap.Error(err))
	}

	// Then stop platform-specific components
	o.stopPlatform()

	// Close event channel
	o.EventChannelManager.Close()

	o.started = false

	o.logger.Info("DNS problem observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// cleanupOldProblems removes old problem trackers
func (o *Observer) cleanupOldProblems(ctx context.Context) {
	// Use shorter intervals in test mode for faster shutdown
	interval := 30 * time.Second
	if o.name == "test" || strings.Contains(o.name, "test") || strings.Contains(o.config.Name, "test") || strings.Contains(o.config.Name, "negative") {
		interval = 100 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Don't log after context is cancelled to avoid logging after test completes
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

	// Update statistics
	switch event.ProblemType {
	case DNSProblemSlow:
		o.stats.SlowQueries++
	case DNSProblemTimeout:
		o.stats.Timeouts++
	case DNSProblemNXDomain:
		o.stats.NXDomains++
	case DNSProblemServfail:
		o.stats.ServerFailures++
	}
	o.stats.TotalProblems++
	o.stats.LastProblemTime = time.Now()

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

// startFallback generates simulated DNS problems for testing
func (o *Observer) startFallback() error {
	o.logger.Info("Starting DNS observer in fallback mode (simulated problems)")

	o.lifecycleManager.Start("mock-generator", func() {
		o.generateMockProblems(o.lifecycleManager.Context())
	})

	return nil
}

// generateMockProblems generates fake DNS problems for testing
func (o *Observer) generateMockProblems(ctx context.Context) {
	defer o.logger.Debug("Mock DNS problem generator stopped")

	// Use shorter ticker for faster shutdown in tests
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	problems := []struct {
		query       string
		problemType DNSProblemType
		latencyMs   float64
	}{
		{"slow-service.example.com", DNSProblemSlow, 250},
		{"nonexistent.domain.local", DNSProblemNXDomain, 5},
		{"timeout.service.cluster.local", DNSProblemTimeout, 5000},
		{"broken-dns.internal", DNSProblemServfail, 10},
	}

	eventCount := 0

	// Use shorter loops to be more responsive to context cancellation
	checkInterval := time.NewTicker(100 * time.Millisecond)
	defer checkInterval.Stop()

	for {
		select {
		case <-ctx.Done():
			o.logger.Debug("Mock generator context cancelled")
			return
		case <-checkInterval.C:
			// Check for cancellation frequently
			select {
			case <-ctx.Done():
				return
			default:
			}
		case <-ticker.C:
			// Only generate events if context is still valid
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Generate a mock problem
			problem := problems[eventCount%len(problems)]
			eventCount++

			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("mock-dns-problem-%d", eventCount),
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    o.name,
				Severity:  domain.EventSeverityWarning,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName:    problem.query,
						QueryType:    "A",
						Duration:     time.Duration(problem.latencyMs * float64(time.Millisecond)),
						ResponseCode: getResponseCode(problem.problemType),
						Error:        true,
						ErrorMessage: getMockErrorMessage(problem.problemType),
						ClientIP:     "10.0.1.5",
						ServerIP:     "10.0.0.53",
					},
					Process: &domain.ProcessData{
						PID:     12345,
						Command: "mock-app",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer":     o.name,
						"version":      "1.0.0",
						"mode":         "fallback",
						"problem_type": problem.problemType.String(),
					},
				},
			}

			if o.EventChannelManager.SendEvent(event) {
				o.BaseObserver.RecordEvent()
				o.logger.Debug("Sent mock DNS problem event",
					zap.String("query", problem.query),
					zap.String("problem", problem.problemType.String()))
			} else {
				o.BaseObserver.RecordDrop()
			}
		}
	}
}

// Helper functions for fallback mode
func getResponseCode(problemType DNSProblemType) int {
	switch problemType {
	case DNSProblemNXDomain:
		return 3 // NXDOMAIN
	case DNSProblemServfail:
		return 2 // SERVFAIL
	case DNSProblemRefused:
		return 5 // REFUSED
	default:
		return 0 // NOERROR (but slow/timeout)
	}
}

func getMockErrorMessage(problemType DNSProblemType) string {
	switch problemType {
	case DNSProblemSlow:
		return "Query exceeded latency threshold"
	case DNSProblemTimeout:
		return "DNS query timed out"
	case DNSProblemNXDomain:
		return "Domain does not exist"
	case DNSProblemServfail:
		return "DNS server failure"
	case DNSProblemRefused:
		return "Query refused by server"
	default:
		return "Unknown DNS problem"
	}
}
