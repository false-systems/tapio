package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// maxBackoff is the maximum backoff duration for exponential backoff
	// Prevents unbounded growth (2^30 seconds ≈ 34 years)
	maxBackoff = 5 * time.Minute
)

// Config configures supervisor behavior
type Config struct {
	// ShutdownTimeout is max time to wait for all observers to exit
	ShutdownTimeout time.Duration

	// HealthCheckInterval is how often to check observer health
	// Reserved for Phase 2 - not yet enforced
	// Default: 5 seconds
	HealthCheckInterval time.Duration

	// ResourceCheckInterval is how often to check resource usage
	// Reserved for Phase 2 - not yet enforced
	// Default: 1 second
	ResourceCheckInterval time.Duration
}

// DefaultConfig returns default supervisor configuration
func DefaultConfig() Config {
	return Config{
		ShutdownTimeout:       2 * time.Second,
		HealthCheckInterval:   5 * time.Second,
		ResourceCheckInterval: 1 * time.Second,
	}
}

// Supervisor manages lifecycle of multiple observers
type Supervisor struct {
	config    Config
	observers map[string]*supervisedObserver
	mu        sync.RWMutex
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	hasRun    bool // Prevents Run() from being called multiple times

	// OTEL metrics (optional - nil = no-op)
	meter            metric.Meter
	observerStarts   metric.Int64Counter
	observerRestarts metric.Int64Counter
	circuitBreakers  metric.Int64Counter
	restartLatency   metric.Float64Histogram
	activeObservers  metric.Int64ObservableGauge
	healthStatus     metric.Int64Gauge
}

// supervisedObserver wraps an observer with supervision metadata
type supervisedObserver struct {
	name   string
	runFn  func(context.Context) error
	config observerConfig
}

// observerConfig holds per-observer configuration
type observerConfig struct {
	// Auto-restart configuration (Phase 1 - active)
	maxRestarts     int           // Max restarts in restart window
	restartWindow   time.Duration // Time window for restart counting
	restartCount    int           // Current restart count
	lastRestartTime time.Time     // Last restart timestamp

	// Resource limits (Phase 2 - reserved, not yet enforced)
	maxCPU    float64 // Max CPU cores
	maxMemory uint64  // Max memory bytes

	// Dependencies (Phase 2 - reserved, not yet enforced)
	dependencies []string // Observer names this one depends on
	optional     bool     // Can run without dependencies?

	// Worker scaling (Phase 2 - reserved, not yet enforced)
	minWorkers int // Minimum workers
	maxWorkers int // Maximum workers

	// Health check (Phase 2 - reserved, not yet enforced)
	healthCheckFn HealthCheckFunc
}

// HealthCheckFunc checks if observer is healthy
type HealthCheckFunc func(ctx context.Context) HealthStatus

// HealthStatus represents observer health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"   // All good
	HealthStatusDegraded  HealthStatus = "degraded"  // Slow but working
	HealthStatusUnhealthy HealthStatus = "unhealthy" // Needs restart
)

// SupervisorOption configures supervisor-level settings
type SupervisorOption func(*Supervisor)

// New creates a new supervisor with given configuration
func New(cfg Config, opts ...SupervisorOption) *Supervisor {
	// Apply defaults
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 2 * time.Second
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 5 * time.Second
	}
	if cfg.ResourceCheckInterval == 0 {
		cfg.ResourceCheckInterval = 1 * time.Second
	}

	s := &Supervisor{
		config:    cfg,
		observers: make(map[string]*supervisedObserver),
	}

	// Apply supervisor options
	for _, opt := range opts {
		opt(s)
	}

	// Initialize metrics if meter provided
	if s.meter != nil {
		if err := s.initMetrics(); err != nil {
			log.Error().Err(err).Msg("failed to initialize metrics")
		}
	}

	return s
}

// SuperviseFunc adds an observer function to be supervised
func (s *Supervisor) SuperviseFunc(name string, fn func(context.Context) error, opts ...Option) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Warn if overwriting existing observer
	if _, exists := s.observers[name]; exists {
		log.Warn().
			Str("observer", name).
			Msg("overwriting existing observer with same name")
	}

	// Create observer config
	cfg := observerConfig{
		maxRestarts:   5,               // Default: max 5 restarts
		restartWindow: 1 * time.Minute, // Default: in 1 minute window
		minWorkers:    1,               // Default: 1 worker min
		maxWorkers:    10,              // Default: 10 workers max
	}

	// Apply options
	for _, opt := range opts {
		opt(&cfg)
	}

	s.observers[name] = &supervisedObserver{
		name:   name,
		runFn:  fn,
		config: cfg,
	}

	log.Info().
		Str("observer", name).
		Int("total_observers", len(s.observers)).
		Msg("added observer to supervisor")
}

// Run starts supervising all observers and blocks until context is cancelled
func (s *Supervisor) Run(ctx context.Context) error {
	// Check if already run
	s.mu.Lock()
	if s.hasRun {
		s.mu.Unlock()
		return fmt.Errorf("supervisor has already been run")
	}
	s.hasRun = true
	s.mu.Unlock()

	s.mu.RLock()
	observers := make([]*supervisedObserver, 0, len(s.observers))
	for _, obs := range s.observers {
		observers = append(observers, obs)
	}
	s.mu.RUnlock()

	if len(observers) == 0 {
		return fmt.Errorf("no observers registered")
	}

	// Create supervisor context
	s.ctx, s.cancel = context.WithCancel(ctx)
	defer s.cancel()

	log.Info().
		Int("count", len(observers)).
		Msg("starting supervisor")

	// Start all observers
	for _, obs := range observers {
		s.wg.Add(1)
		go s.superviseObserver(obs)
	}

	// Wait for context cancellation
	<-s.ctx.Done()
	log.Info().Msg("supervisor shutdown initiated")

	// Wait for all observers to exit with timeout
	return s.waitForShutdown()
}

// superviseObserver runs a single observer with supervision and auto-restart
func (s *Supervisor) superviseObserver(obs *supervisedObserver) {
	defer s.wg.Done()

	log.Info().
		Str("observer", obs.name).
		Msg("starting observer")

	// Record observer start
	if s.observerStarts != nil {
		s.observerStarts.Add(s.ctx, 1, metric.WithAttributes(
			attribute.String("observer", obs.name),
			attribute.String("result", "success"),
		))
	}

	attempt := 0
	for {
		// Create cancelable context for this attempt (allows health check to trigger restart)
		observerCtx, observerCancel := context.WithCancel(s.ctx)

		// Start health check loop (if health check provided)
		if obs.config.healthCheckFn != nil {
			healthCtx, healthCancel := context.WithCancel(s.ctx)

			s.wg.Add(1)
			go func() {
				s.healthCheckLoop(healthCtx, obs, observerCancel)
				healthCancel()
			}()
			defer healthCancel()
		}

		// Run observer
		err := obs.runFn(observerCtx)

		// Clean up observer context
		observerCancel()

		// Check if supervisor is shutting down
		if s.ctx.Err() != nil {
			log.Info().
				Str("observer", obs.name).
				Msg("observer exited cleanly")
			return
		}

		// If observer exited with nil error but supervisor is still running,
		// it was likely cancelled by health check - treat as failure to trigger restart
		if err == nil {
			err = context.Canceled // Treat as cancellation for restart logic
		}

		// Observer failed - check if we should restart
		log.Error().
			Str("observer", obs.name).
			Err(err).
			Int("attempt", attempt+1).
			Msg("observer failed")

		// Update restart tracking
		now := time.Now()
		if !obs.config.lastRestartTime.IsZero() && now.Sub(obs.config.lastRestartTime) > obs.config.restartWindow {
			// Outside restart window - reset counter AND attempt
			obs.config.restartCount = 0
			attempt = 0 // Reset backoff attempt counter
		}

		// Check circuit breaker
		if obs.config.restartCount >= obs.config.maxRestarts {
			// Record circuit breaker trigger
			if s.circuitBreakers != nil {
				s.circuitBreakers.Add(s.ctx, 1, metric.WithAttributes(
					attribute.String("observer", obs.name),
				))
			}

			log.Error().
				Str("observer", obs.name).
				Int("restarts", obs.config.restartCount).
				Dur("window", obs.config.restartWindow).
				Msg("circuit breaker triggered - observer disabled")
			return
		}

		// Increment restart count
		obs.config.restartCount++
		obs.config.lastRestartTime = now

		// Record restart start time for latency measurement
		restartStart := time.Now()

		// Calculate exponential backoff: 2^attempt seconds (1s, 2s, 4s, 8s, ...)
		// Cap at maxBackoff to prevent overflow and unbounded growth
		backoff := time.Duration(1<<attempt) * time.Second
		if backoff > maxBackoff || attempt >= 30 {
			backoff = maxBackoff
		}

		log.Info().
			Str("observer", obs.name).
			Int("attempt", attempt+1).
			Dur("backoff", backoff).
			Msg("restarting observer after backoff")

		// Wait for backoff (or context cancellation)
		select {
		case <-time.After(backoff):
			// Record restart counter
			if s.observerRestarts != nil {
				s.observerRestarts.Add(s.ctx, 1, metric.WithAttributes(
					attribute.String("observer", obs.name),
					attribute.String("reason", "crash"),
				))
			}

			// Record restart latency
			if s.restartLatency != nil {
				latency := time.Since(restartStart).Milliseconds()
				s.restartLatency.Record(s.ctx, float64(latency), metric.WithAttributes(
					attribute.String("observer", obs.name),
				))
			}

			attempt++
			// Continue loop to restart
		case <-s.ctx.Done():
			log.Info().
				Str("observer", obs.name).
				Msg("restart cancelled due to supervisor shutdown")
			return
		}
	}
}

// healthCheckLoop periodically checks observer health and triggers restart if unhealthy
func (s *Supervisor) healthCheckLoop(ctx context.Context, obs *supervisedObserver, cancelObserver context.CancelFunc) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.HealthCheckInterval)
	defer ticker.Stop()

	// Helper to convert health status to int for metrics
	statusToInt := func(status HealthStatus) int64 {
		switch status {
		case HealthStatusHealthy:
			return 1
		case HealthStatusDegraded:
			return 2
		case HealthStatusUnhealthy:
			return 3
		default:
			return 0
		}
	}

	for {
		select {
		case <-ticker.C:
			// Call health check with panic recovery
			var status HealthStatus
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error().
							Str("observer", obs.name).
							Interface("panic", r).
							Msg("health check panicked")
						status = HealthStatusHealthy // Treat panic as healthy (don't restart)
					}
				}()
				status = obs.config.healthCheckFn(ctx)
			}()

			// Record health status metric
			if s.healthStatus != nil {
				s.healthStatus.Record(ctx, statusToInt(status), metric.WithAttributes(
					attribute.String("observer", obs.name),
					attribute.String("status", string(status)),
				))
			}

			switch status {
			case HealthStatusHealthy:
				// All good, continue
				log.Debug().Str("observer", obs.name).Msg("health check passed")

			case HealthStatusDegraded:
				// Warn but continue
				log.Warn().Str("observer", obs.name).Msg("observer degraded")

			case HealthStatusUnhealthy:
				// Restart observer
				log.Error().Str("observer", obs.name).Msg("observer unhealthy - triggering restart")
				cancelObserver() // This will cause runFn to exit and restart
				return

			default:
				log.Error().Str("observer", obs.name).Str("status", string(status)).Msg("unknown health status")
			}

		case <-ctx.Done():
			return
		}
	}
}

// waitForShutdown waits for all observers to exit with timeout
func (s *Supervisor) waitForShutdown() error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("all observers shut down cleanly")
		return nil

	case <-time.After(s.config.ShutdownTimeout):
		// Timeout - some observers are zombies
		log.Error().
			Dur("timeout", s.config.ShutdownTimeout).
			Msg("shutdown timeout exceeded - some observers are zombies")
		return fmt.Errorf("shutdown timeout exceeded (%v)", s.config.ShutdownTimeout)
	}
}

// ObserverNames returns list of registered observer names
func (s *Supervisor) ObserverNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.observers))
	for name := range s.observers {
		names = append(names, name)
	}
	return names
}

// WithMeter configures OTEL metrics collection
func WithMeter(meter metric.Meter) SupervisorOption {
	return func(s *Supervisor) {
		s.meter = meter
	}
}

// initMetrics initializes all OTEL metrics for the supervisor
func (s *Supervisor) initMetrics() error {
	var err error

	// Counter: Observer starts
	s.observerStarts, err = s.meter.Int64Counter(
		"supervisor_observer_starts_total",
		metric.WithDescription("Total number of observer starts"),
	)
	if err != nil {
		return fmt.Errorf("failed to create observerStarts counter: %w", err)
	}

	// Counter: Observer restarts
	s.observerRestarts, err = s.meter.Int64Counter(
		"supervisor_observer_restarts_total",
		metric.WithDescription("Total number of observer restarts"),
	)
	if err != nil {
		return fmt.Errorf("failed to create observerRestarts counter: %w", err)
	}

	// Counter: Circuit breaker triggers
	s.circuitBreakers, err = s.meter.Int64Counter(
		"supervisor_circuit_breaker_triggers_total",
		metric.WithDescription("Total number of circuit breaker triggers"),
	)
	if err != nil {
		return fmt.Errorf("failed to create circuitBreakers counter: %w", err)
	}

	// Histogram: Restart latency
	s.restartLatency, err = s.meter.Float64Histogram(
		"supervisor_restart_latency_ms",
		metric.WithDescription("Observer restart latency in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return fmt.Errorf("failed to create restartLatency histogram: %w", err)
	}

	// Gauge: Active observers (observable)
	s.activeObservers, err = s.meter.Int64ObservableGauge(
		"supervisor_active_observers",
		metric.WithDescription("Number of currently active observers"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			s.mu.RLock()
			defer s.mu.RUnlock()
			o.Observe(int64(len(s.observers)))
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create activeObservers gauge: %w", err)
	}

	// Gauge: Health status
	s.healthStatus, err = s.meter.Int64Gauge(
		"supervisor_observer_health_status",
		metric.WithDescription("Observer health status (1=healthy, 2=degraded, 3=unhealthy)"),
	)
	if err != nil {
		return fmt.Errorf("failed to create healthStatus gauge: %w", err)
	}

	return nil
}

// Option is a functional option for configuring observer supervision
type Option func(*observerConfig)

// WithRestartPolicy configures auto-restart behavior
func WithRestartPolicy(maxRestarts int, window time.Duration) Option {
	return func(cfg *observerConfig) {
		// Validate maxRestarts (negative = 0 = no restarts)
		if maxRestarts < 0 {
			maxRestarts = 0
		}
		// Validate window (zero or negative defaults to 1 minute)
		if window <= 0 {
			window = 1 * time.Minute
		}
		cfg.maxRestarts = maxRestarts
		cfg.restartWindow = window
	}
}

// WithResourceLimits configures CPU and memory limits
// Reserved for Phase 2 - not yet enforced
func WithResourceLimits(cpuCores float64, memoryBytes uint64) Option {
	return func(cfg *observerConfig) {
		// Validate cpuCores (negative defaults to 0 = unlimited)
		if cpuCores < 0 {
			cpuCores = 0
		}
		cfg.maxCPU = cpuCores
		cfg.maxMemory = memoryBytes
	}
}

// WithDependencies configures observer dependencies
// Reserved for Phase 2 - not yet enforced
func WithDependencies(deps ...string) Option {
	return func(cfg *observerConfig) {
		cfg.dependencies = deps
	}
}

// WithOptionalDeps marks dependencies as optional (can run in degraded mode)
// Reserved for Phase 2 - not yet enforced
func WithOptionalDeps(optional bool) Option {
	return func(cfg *observerConfig) {
		cfg.optional = optional
	}
}

// WithWorkerScaling configures worker pool scaling
// Reserved for Phase 2 - not yet enforced
func WithWorkerScaling(min, max int) Option {
	return func(cfg *observerConfig) {
		// Validate min >= 1
		if min < 1 {
			min = 1
		}
		// Validate max >= min
		if max < min {
			max = min
		}
		cfg.minWorkers = min
		cfg.maxWorkers = max
	}
}

// WithHealthCheck configures observer health check function
// Reserved for Phase 2 - not yet enforced
func WithHealthCheck(fn HealthCheckFunc) Option {
	return func(cfg *observerConfig) {
		cfg.healthCheckFn = fn
	}
}
