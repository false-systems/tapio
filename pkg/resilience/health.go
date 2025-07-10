package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthCheck represents a health check function
type HealthCheck func(ctx context.Context) error

// HealthCheckResult contains the result of a health check
type HealthCheckResult struct {
	Name      string        `json:"name"`
	Status    HealthStatus  `json:"status"`
	Message   string        `json:"message,omitempty"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
	Metadata  interface{}   `json:"metadata,omitempty"`
}

// Component represents a component with health checks
type Component struct {
	Name        string
	Description string
	Critical    bool
	HealthCheck HealthCheck
	Timeout     time.Duration
	Interval    time.Duration

	// Internal state
	mu            sync.RWMutex
	lastResult    *HealthCheckResult
	lastCheckTime time.Time

	// Metrics
	totalChecks   atomic.Uint64
	failedChecks  atomic.Uint64
	checkDuration atomic.Int64
}

// HealthChecker manages health checks for multiple components
type HealthChecker struct {
	components sync.Map // map[string]*Component

	// Global settings
	defaultTimeout   time.Duration
	defaultInterval  time.Duration
	aggregateTimeout time.Duration

	// Callbacks
	onStatusChange func(component string, oldStatus, newStatus HealthStatus)

	// Metrics
	totalHealthChecks  atomic.Uint64
	failedHealthChecks atomic.Uint64
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(defaultTimeout, defaultInterval time.Duration) *HealthChecker {
	if defaultTimeout == 0 {
		defaultTimeout = 5 * time.Second
	}
	if defaultInterval == 0 {
		defaultInterval = 30 * time.Second
	}

	return &HealthChecker{
		defaultTimeout:   defaultTimeout,
		defaultInterval:  defaultInterval,
		aggregateTimeout: 10 * time.Second,
	}
}

// RegisterComponent registers a component for health checking
func (hc *HealthChecker) RegisterComponent(component Component) error {
	if component.Name == "" {
		return errors.New("component name is required")
	}
	if component.HealthCheck == nil {
		return errors.New("health check function is required")
	}

	if component.Timeout == 0 {
		component.Timeout = hc.defaultTimeout
	}
	if component.Interval == 0 {
		component.Interval = hc.defaultInterval
	}

	hc.components.Store(component.Name, &component)
	return nil
}

// UnregisterComponent removes a component from health checking
func (hc *HealthChecker) UnregisterComponent(name string) {
	hc.components.Delete(name)
}

// SetStatusChangeCallback sets the callback for status changes
func (hc *HealthChecker) SetStatusChangeCallback(callback func(component string, oldStatus, newStatus HealthStatus)) {
	hc.onStatusChange = callback
}

// CheckComponent performs a health check on a specific component
func (hc *HealthChecker) CheckComponent(ctx context.Context, name string) (*HealthCheckResult, error) {
	value, exists := hc.components.Load(name)
	if !exists {
		return nil, fmt.Errorf("component %s not found", name)
	}

	component := value.(*Component)
	return hc.performHealthCheck(ctx, component)
}

// CheckAll performs health checks on all components
func (hc *HealthChecker) CheckAll(ctx context.Context) []HealthCheckResult {
	ctx, cancel := context.WithTimeout(ctx, hc.aggregateTimeout)
	defer cancel()

	var (
		results []HealthCheckResult
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	hc.components.Range(func(key, value interface{}) bool {
		component := value.(*Component)

		wg.Add(1)
		go func() {
			defer wg.Done()

			result, _ := hc.performHealthCheck(ctx, component)

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()
		}()

		return true
	})

	wg.Wait()
	return results
}

// GetStatus returns the overall system health status
func (hc *HealthChecker) GetStatus(ctx context.Context) HealthStatus {
	results := hc.CheckAll(ctx)

	hasUnhealthy := false
	hasDegraded := false
	hasCriticalFailure := false

	for _, result := range results {
		component, _ := hc.getComponent(result.Name)

		switch result.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
			if component != nil && component.Critical {
				hasCriticalFailure = true
			}
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	if hasCriticalFailure {
		return HealthStatusUnhealthy
	}
	if hasUnhealthy || hasDegraded {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

// performHealthCheck executes a health check for a component
func (hc *HealthChecker) performHealthCheck(ctx context.Context, component *Component) (*HealthCheckResult, error) {
	// Check if we should use cached result
	component.mu.RLock()
	lastCheck := component.lastCheckTime
	lastResult := component.lastResult
	component.mu.RUnlock()

	if time.Since(lastCheck) < component.Interval && lastResult != nil {
		return lastResult, nil
	}

	// Perform health check
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, component.Timeout)
	defer cancel()

	hc.totalHealthChecks.Add(1)
	component.totalChecks.Add(1)

	result := &HealthCheckResult{
		Name:      component.Name,
		Timestamp: start,
	}

	err := component.HealthCheck(ctx)
	result.Duration = time.Since(start)
	component.checkDuration.Add(int64(result.Duration))

	// Determine status
	oldStatus := HealthStatusUnknown
	if lastResult != nil {
		oldStatus = lastResult.Status
	}

	if err == nil {
		result.Status = HealthStatusHealthy
		result.Message = "Component is healthy"
	} else if errors.Is(err, context.DeadlineExceeded) {
		result.Status = HealthStatusUnhealthy
		result.Error = "Health check timed out"
		hc.failedHealthChecks.Add(1)
		component.failedChecks.Add(1)
	} else {
		result.Status = HealthStatusUnhealthy
		result.Error = err.Error()
		hc.failedHealthChecks.Add(1)
		component.failedChecks.Add(1)
	}

	// Update component state
	component.mu.Lock()
	component.lastResult = result
	component.lastCheckTime = time.Now()
	component.mu.Unlock()

	// Notify status change
	if oldStatus != result.Status && hc.onStatusChange != nil {
		hc.onStatusChange(component.Name, oldStatus, result.Status)
	}

	return result, nil
}

// getComponent retrieves a component by name
func (hc *HealthChecker) getComponent(name string) (*Component, bool) {
	value, exists := hc.components.Load(name)
	if !exists {
		return nil, false
	}
	return value.(*Component), true
}

// StartBackgroundChecks starts periodic health checks in the background
func (hc *HealthChecker) StartBackgroundChecks(ctx context.Context) {
	hc.components.Range(func(key, value interface{}) bool {
		component := value.(*Component)

		go func() {
			ticker := time.NewTicker(component.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_, _ = hc.performHealthCheck(ctx, component)
				}
			}
		}()

		return true
	})
}

// GetMetrics returns health checker metrics
func (hc *HealthChecker) GetMetrics() HealthCheckerMetrics {
	var components []ComponentMetrics

	hc.components.Range(func(key, value interface{}) bool {
		component := value.(*Component)

		totalChecks := component.totalChecks.Load()
		avgDuration := time.Duration(0)
		if totalChecks > 0 {
			avgDuration = time.Duration(component.checkDuration.Load()) / time.Duration(totalChecks)
		}

		component.mu.RLock()
		lastResult := component.lastResult
		component.mu.RUnlock()

		status := HealthStatusUnknown
		if lastResult != nil {
			status = lastResult.Status
		}

		components = append(components, ComponentMetrics{
			Name:         component.Name,
			Status:       string(status),
			TotalChecks:  totalChecks,
			FailedChecks: component.failedChecks.Load(),
			AvgDuration:  avgDuration,
		})

		return true
	})

	return HealthCheckerMetrics{
		TotalHealthChecks:  hc.totalHealthChecks.Load(),
		FailedHealthChecks: hc.failedHealthChecks.Load(),
		Components:         components,
	}
}

// HealthCheckerMetrics represents health checker metrics
type HealthCheckerMetrics struct {
	TotalHealthChecks  uint64
	FailedHealthChecks uint64
	Components         []ComponentMetrics
}

// ComponentMetrics represents metrics for a single component
type ComponentMetrics struct {
	Name         string
	Status       string
	TotalChecks  uint64
	FailedChecks uint64
	AvgDuration  time.Duration
}

// DependencyHealthCheck creates a health check that verifies dependencies
func DependencyHealthCheck(dependencies map[string]func() error) HealthCheck {
	return func(ctx context.Context) error {
		var wg sync.WaitGroup
		errChan := make(chan error, len(dependencies))

		for name, check := range dependencies {
			wg.Add(1)
			go func(depName string, depCheck func() error) {
				defer wg.Done()

				if err := depCheck(); err != nil {
					errChan <- fmt.Errorf("dependency %s failed: %w", depName, err)
				}
			}(name, check)
		}

		wg.Wait()
		close(errChan)

		var errors []string
		for err := range errChan {
			errors = append(errors, err.Error())
		}

		if len(errors) > 0 {
			return fmt.Errorf("dependency checks failed: %v", errors)
		}

		return nil
	}
}

// AggregateHealth combines multiple health results into a single status
func AggregateHealth(results []HealthCheckResult, criticalComponents []string) HealthStatus {
	criticalSet := make(map[string]bool)
	for _, name := range criticalComponents {
		criticalSet[name] = true
	}

	hasUnhealthy := false
	hasDegraded := false
	hasCriticalFailure := false

	for _, result := range results {
		switch result.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
			if criticalSet[result.Name] {
				hasCriticalFailure = true
			}
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	if hasCriticalFailure {
		return HealthStatusUnhealthy
	}
	if hasUnhealthy || hasDegraded {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}
