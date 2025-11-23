package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Config configures supervisor behavior
type Config struct {
	// ShutdownTimeout is max time to wait for all observers to exit
	ShutdownTimeout time.Duration

	// HealthCheckInterval is how often to check observer health
	// Default: 5 seconds
	HealthCheckInterval time.Duration

	// ResourceCheckInterval is how often to check resource usage
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
}

// supervisedObserver wraps an observer with supervision metadata
type supervisedObserver struct {
	name   string
	runFn  func(context.Context) error
	config observerConfig
}

// observerConfig holds per-observer configuration
type observerConfig struct {
	// Auto-restart configuration
	maxRestarts     int           // Max restarts in restart window
	restartWindow   time.Duration // Time window for restart counting
	restartCount    int           // Current restart count
	lastRestartTime time.Time     // Last restart timestamp

	// Resource limits
	maxCPU    float64 // Max CPU cores
	maxMemory uint64  // Max memory bytes

	// Dependencies
	dependencies []string // Observer names this one depends on
	optional     bool     // Can run without dependencies?

	// Worker scaling
	minWorkers int // Minimum workers
	maxWorkers int // Maximum workers

	// Health check
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

// New creates a new supervisor with given configuration
func New(cfg Config) *Supervisor {
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

	return &Supervisor{
		config:    cfg,
		observers: make(map[string]*supervisedObserver),
	}
}

// SuperviseFunc adds an observer function to be supervised
func (s *Supervisor) SuperviseFunc(name string, fn func(context.Context) error, opts ...Option) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	attempt := 0
	for {
		// Run observer
		err := obs.runFn(s.ctx)

		// Clean exit (context cancelled)
		if err == nil || s.ctx.Err() != nil {
			log.Info().
				Str("observer", obs.name).
				Msg("observer exited cleanly")
			return
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
			// Outside restart window - reset counter
			obs.config.restartCount = 0
		}

		// Check circuit breaker
		if obs.config.restartCount >= obs.config.maxRestarts {
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

		// Calculate exponential backoff: 2^attempt seconds (1s, 2s, 4s, 8s, ...)
		backoff := time.Duration(1<<attempt) * time.Second

		log.Info().
			Str("observer", obs.name).
			Int("attempt", attempt+1).
			Dur("backoff", backoff).
			Msg("restarting observer after backoff")

		// Wait for backoff (or context cancellation)
		select {
		case <-time.After(backoff):
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

// Option is a functional option for configuring observer supervision
type Option func(*observerConfig)

// WithRestartPolicy configures auto-restart behavior
func WithRestartPolicy(maxRestarts int, window time.Duration) Option {
	return func(cfg *observerConfig) {
		cfg.maxRestarts = maxRestarts
		cfg.restartWindow = window
	}
}

// WithResourceLimits configures CPU and memory limits
func WithResourceLimits(cpuCores float64, memoryBytes uint64) Option {
	return func(cfg *observerConfig) {
		cfg.maxCPU = cpuCores
		cfg.maxMemory = memoryBytes
	}
}

// WithDependencies configures observer dependencies
func WithDependencies(deps ...string) Option {
	return func(cfg *observerConfig) {
		cfg.dependencies = deps
	}
}

// WithOptionalDeps marks dependencies as optional (can run in degraded mode)
func WithOptionalDeps(optional bool) Option {
	return func(cfg *observerConfig) {
		cfg.optional = optional
	}
}

// WithWorkerScaling configures worker pool scaling
func WithWorkerScaling(min, max int) Option {
	return func(cfg *observerConfig) {
		cfg.minWorkers = min
		cfg.maxWorkers = max
	}
}

// WithHealthCheck configures observer health check function
func WithHealthCheck(fn HealthCheckFunc) Option {
	return func(cfg *observerConfig) {
		cfg.healthCheckFn = fn
	}
}
