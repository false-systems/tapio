package integrations

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/core"
)

const (
	// HealthCheckConcurrency controls number of concurrent health checks
	HealthCheckConcurrency = 10
	// HealthCheckTimeout is the timeout for individual health checks
	HealthCheckTimeout = 5 * time.Second
)

// Registry manages all active integrations
type Registry struct {
	mu           sync.RWMutex
	integrations map[string]core.Integration
	logger       *zap.Logger

	// OpenTelemetry instrumentation
	tracer              trace.Tracer
	healthChecksTotal   metric.Int64Counter
	healthCheckDuration metric.Float64Histogram
	healthCheckErrors   metric.Int64Counter
	registrationsTotal  metric.Int64Counter
}

// NewRegistry creates a new integration registry with proper instrumentation
func NewRegistry(logger *zap.Logger) (*Registry, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Initialize OTEL components
	tracer := otel.Tracer("integrations.registry")
	meter := otel.Meter("integrations.registry")

	// Create metrics with descriptive names and descriptions
	healthChecksTotal, err := meter.Int64Counter(
		"integrations_health_checks_total",
		metric.WithDescription("Total number of health checks performed"),
	)
	if err != nil {
		logger.Warn("Failed to create health checks counter", zap.Error(err))
	}

	healthCheckDuration, err := meter.Float64Histogram(
		"integrations_health_check_duration_ms",
		metric.WithDescription("Health check duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create health check duration histogram", zap.Error(err))
	}

	healthCheckErrors, err := meter.Int64Counter(
		"integrations_health_check_errors_total",
		metric.WithDescription("Total number of health check errors"),
	)
	if err != nil {
		logger.Warn("Failed to create health check errors counter", zap.Error(err))
	}

	registrationsTotal, err := meter.Int64Counter(
		"integrations_registrations_total",
		metric.WithDescription("Total number of integration registrations"),
	)
	if err != nil {
		logger.Warn("Failed to create registrations counter", zap.Error(err))
	}

	return &Registry{
		integrations:        make(map[string]core.Integration),
		logger:              logger,
		tracer:              tracer,
		healthChecksTotal:   healthChecksTotal,
		healthCheckDuration: healthCheckDuration,
		healthCheckErrors:   healthCheckErrors,
		registrationsTotal:  registrationsTotal,
	}, nil
}

// Register adds an integration to the registry
func (r *Registry) Register(integration core.Integration) error {
	if integration == nil {
		return fmt.Errorf("integration cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := integration.Name()
	if name == "" {
		return fmt.Errorf("integration name cannot be empty")
	}

	if _, exists := r.integrations[name]; exists {
		return fmt.Errorf("integration %s already registered", name)
	}

	r.integrations[name] = integration

	// Record registration metric
	if r.registrationsTotal != nil {
		r.registrationsTotal.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("integration_name", name),
			attribute.String("operation", "register"),
		))
	}

	r.logger.Info("Integration registered", zap.String("name", name))
	return nil
}

// Get retrieves an integration by name
func (r *Registry) Get(name string) (core.Integration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	integration, exists := r.integrations[name]
	return integration, exists
}

// List returns all registered integration names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.integrations))
	for name := range r.integrations {
		names = append(names, name)
	}
	return names
}

// healthCheckResult holds the result of a single health check
type healthCheckResult struct {
	name   string
	health *domain.HealthStatus
	err    error
}

// HealthCheck checks health of all integrations concurrently for 10x performance improvement
func (r *Registry) HealthCheck(ctx context.Context) map[string]*domain.HealthStatus {
	ctx, span := r.tracer.Start(ctx, "registry.health_check")
	defer span.End()

	start := time.Now()

	r.mu.RLock()
	integrationList := make([]struct {
		name        string
		integration core.Integration
	}, 0, len(r.integrations))
	for name, integration := range r.integrations {
		integrationList = append(integrationList, struct {
			name        string
			integration core.Integration
		}{name, integration})
	}
	integrationCount := len(integrationList)
	r.mu.RUnlock()

	if integrationCount == 0 {
		return make(map[string]*domain.HealthStatus)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.Int("integration_count", integrationCount),
		attribute.Int("concurrency", HealthCheckConcurrency),
	)

	// Create buffered channels for work distribution and result collection
	workCh := make(chan struct {
		name        string
		integration core.Integration
	}, integrationCount)
	resultsCh := make(chan healthCheckResult, integrationCount)

	// Start worker goroutines
	workerCount := min(HealthCheckConcurrency, integrationCount)
	for i := 0; i < workerCount; i++ {
		go r.healthCheckWorker(ctx, workCh, resultsCh)
	}

	// Send work to workers
	go func() {
		defer close(workCh)
		for _, item := range integrationList {
			select {
			case workCh <- item:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Collect results
	results := make(map[string]*domain.HealthStatus)
	for i := 0; i < integrationCount; i++ {
		select {
		case result := <-resultsCh:
			health := result.health
			if result.err != nil {
				health = domain.NewUnhealthyStatus(fmt.Sprintf("health check failed: %v", result.err), result.err)
				// Record error metric
				if r.healthCheckErrors != nil {
					r.healthCheckErrors.Add(ctx, 1, metric.WithAttributes(
						attribute.String("integration_name", result.name),
						attribute.String("error_type", "health_check_failed"),
					))
				}
			}
			results[result.name] = health
		case <-ctx.Done():
			// Context cancelled, return partial results
			span.SetAttributes(attribute.String("cancellation_reason", "context_cancelled"))
			break
		}
	}

	// Record overall metrics
	duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
	if r.healthCheckDuration != nil {
		r.healthCheckDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.Int("integration_count", integrationCount),
			attribute.Int("successful_checks", len(results)),
		))
	}

	if r.healthChecksTotal != nil {
		r.healthChecksTotal.Add(ctx, int64(len(results)), metric.WithAttributes(
			attribute.String("operation", "bulk_health_check"),
		))
	}

	span.SetAttributes(
		attribute.Int("results_count", len(results)),
		attribute.Float64("duration_ms", duration),
	)

	return results
}

// healthCheckWorker performs health checks for integrations from the work channel
func (r *Registry) healthCheckWorker(ctx context.Context, workCh <-chan struct {
	name        string
	integration core.Integration
}, resultsCh chan<- healthCheckResult) {
	for {
		select {
		case work, ok := <-workCh:
			if !ok {
				return // Channel closed, exit worker
			}

			// Create timeout context for individual health check
			checkCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout)
			start := time.Now()

			health, err := work.integration.Health(checkCtx)
			cancel()

			// Record individual health check metrics
			duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
			if r.healthCheckDuration != nil {
				r.healthCheckDuration.Record(ctx, duration, metric.WithAttributes(
					attribute.String("integration_name", work.name),
					attribute.String("check_type", "individual"),
				))
			}

			// Send result
			select {
			case resultsCh <- healthCheckResult{name: work.name, health: health, err: err}:
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CloseAll closes all registered integrations
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for name, integration := range r.integrations {
		if err := integration.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s: %w", name, err))
		}
	}

	// Clear registry
	r.integrations = make(map[string]core.Integration)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing integrations: %v", errs)
	}
	return nil
}
