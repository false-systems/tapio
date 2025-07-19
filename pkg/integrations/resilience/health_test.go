package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthChecker_RegisterComponent(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 5*time.Second)

	// Test valid component registration
	err := hc.RegisterComponent(Component{
		Name:        "test-component",
		Description: "Test component",
		HealthCheck: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Errorf("unexpected error registering component: %v", err)
	}

	// Test missing name
	err = hc.RegisterComponent(Component{
		HealthCheck: func(ctx context.Context) error { return nil },
	})
	if err == nil {
		t.Error("expected error for missing component name")
	}

	// Test missing health check
	err = hc.RegisterComponent(Component{
		Name: "no-check",
	})
	if err == nil {
		t.Error("expected error for missing health check")
	}
}

func TestHealthChecker_CheckComponent(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 5*time.Second)

	// Register healthy component
	hc.RegisterComponent(Component{
		Name: "healthy",
		HealthCheck: func(ctx context.Context) error {
			return nil
		},
	})

	// Register unhealthy component
	hc.RegisterComponent(Component{
		Name: "unhealthy",
		HealthCheck: func(ctx context.Context) error {
			return errors.New("component failure")
		},
	})

	// Check healthy component
	result, err := hc.CheckComponent(context.Background(), "healthy")
	if err != nil {
		t.Errorf("unexpected error checking healthy component: %v", err)
	}
	if result.Status != HealthStatusHealthy {
		t.Errorf("expected healthy status, got %v", result.Status)
	}

	// Check unhealthy component
	result, err = hc.CheckComponent(context.Background(), "unhealthy")
	if err != nil {
		t.Errorf("unexpected error checking unhealthy component: %v", err)
	}
	if result.Status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy status, got %v", result.Status)
	}

	// Check non-existent component
	_, err = hc.CheckComponent(context.Background(), "non-existent")
	if err == nil {
		t.Error("expected error for non-existent component")
	}
}

func TestHealthChecker_Timeout(t *testing.T) {
	hc := NewHealthChecker(100*time.Millisecond, 5*time.Second)

	hc.RegisterComponent(Component{
		Name:    "slow-component",
		Timeout: 50 * time.Millisecond,
		HealthCheck: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return nil
			}
		},
	})

	result, err := hc.CheckComponent(context.Background(), "slow-component")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result.Status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy status due to timeout, got %v", result.Status)
	}

	if result.Error != "Health check timed out" {
		t.Errorf("expected timeout error message, got %v", result.Error)
	}
}

func TestHealthChecker_Caching(t *testing.T) {
	callCount := 0
	hc := NewHealthChecker(1*time.Second, 100*time.Millisecond)

	hc.RegisterComponent(Component{
		Name:     "cached-component",
		Interval: 200 * time.Millisecond,
		HealthCheck: func(ctx context.Context) error {
			callCount++
			return nil
		},
	})

	// First check
	_, err := hc.CheckComponent(context.Background(), "cached-component")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Immediate second check should use cache
	_, err = hc.CheckComponent(context.Background(), "cached-component")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call (cached), got %d", callCount)
	}

	// Wait for cache to expire
	time.Sleep(250 * time.Millisecond)

	// Third check should trigger new health check
	_, err = hc.CheckComponent(context.Background(), "cached-component")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestHealthChecker_CheckAll(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 5*time.Second)

	components := []string{"comp1", "comp2", "comp3"}
	statuses := map[string]HealthStatus{
		"comp1": HealthStatusHealthy,
		"comp2": HealthStatusUnhealthy,
		"comp3": HealthStatusHealthy,
	}

	for _, name := range components {
		compName := name // Capture for closure
		status := statuses[compName]

		hc.RegisterComponent(Component{
			Name: compName,
			HealthCheck: func(ctx context.Context) error {
				if status == HealthStatusUnhealthy {
					return errors.New("unhealthy")
				}
				return nil
			},
		})
	}

	results := hc.CheckAll(context.Background())

	if len(results) != len(components) {
		t.Errorf("expected %d results, got %d", len(components), len(results))
	}

	// Verify results
	resultMap := make(map[string]HealthCheckResult)
	for _, result := range results {
		resultMap[result.Name] = result
	}

	for name, expectedStatus := range statuses {
		result, exists := resultMap[name]
		if !exists {
			t.Errorf("missing result for component %s", name)
			continue
		}

		if result.Status != expectedStatus {
			t.Errorf("component %s: expected status %v, got %v", name, expectedStatus, result.Status)
		}
	}
}

func TestHealthChecker_GetStatus(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 5*time.Second)

	// All healthy
	hc.RegisterComponent(Component{
		Name:        "critical1",
		Critical:    true,
		HealthCheck: func(ctx context.Context) error { return nil },
	})
	hc.RegisterComponent(Component{
		Name:        "non-critical1",
		Critical:    false,
		HealthCheck: func(ctx context.Context) error { return nil },
	})

	status := hc.GetStatus(context.Background())
	if status != HealthStatusHealthy {
		t.Errorf("expected healthy status, got %v", status)
	}

	// Non-critical component unhealthy
	hc.RegisterComponent(Component{
		Name:        "non-critical2",
		Critical:    false,
		HealthCheck: func(ctx context.Context) error { return errors.New("error") },
	})

	status = hc.GetStatus(context.Background())
	if status != HealthStatusDegraded {
		t.Errorf("expected degraded status, got %v", status)
	}

	// Critical component unhealthy
	hc.RegisterComponent(Component{
		Name:        "critical2",
		Critical:    true,
		HealthCheck: func(ctx context.Context) error { return errors.New("critical error") },
	})

	status = hc.GetStatus(context.Background())
	if status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy status, got %v", status)
	}
}

func TestHealthChecker_StatusChangeCallback(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 5*time.Second)

	var (
		callbackCalled atomic.Bool
		oldStatus      HealthStatus
		newStatus      HealthStatus
		mu             sync.Mutex
	)

	hc.SetStatusChangeCallback(func(component string, old, new HealthStatus) {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled.Store(true)
		oldStatus = old
		newStatus = new
	})

	// Variable to control health status
	isHealthy := true

	hc.RegisterComponent(Component{
		Name: "status-change",
		HealthCheck: func(ctx context.Context) error {
			if isHealthy {
				return nil
			}
			return errors.New("unhealthy")
		},
	})

	// First check - establish baseline
	hc.CheckComponent(context.Background(), "status-change")

	// Change status
	isHealthy = false

	// Force new check (bypass cache)
	time.Sleep(10 * time.Millisecond)
	comp, _ := hc.getComponent("status-change")
	comp.mu.Lock()
	comp.lastCheckTime = time.Time{} // Reset cache
	comp.mu.Unlock()

	hc.CheckComponent(context.Background(), "status-change")

	// Verify callback was called
	if !callbackCalled.Load() {
		t.Error("status change callback was not called")
	}

	mu.Lock()
	if oldStatus != HealthStatusHealthy || newStatus != HealthStatusUnhealthy {
		t.Errorf("expected status change from healthy to unhealthy, got %v to %v", oldStatus, newStatus)
	}
	mu.Unlock()
}

func TestHealthChecker_BackgroundChecks(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 100*time.Millisecond)

	checkCount := atomic.Int32{}

	hc.RegisterComponent(Component{
		Name:     "background-check",
		Interval: 50 * time.Millisecond,
		HealthCheck: func(ctx context.Context) error {
			checkCount.Add(1)
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.StartBackgroundChecks(ctx)

	// Wait for multiple checks
	time.Sleep(300 * time.Millisecond)

	count := checkCount.Load()
	if count < 3 {
		t.Errorf("expected at least 3 background checks, got %d", count)
	}

	// Cancel and verify checks stop
	cancel()
	time.Sleep(100 * time.Millisecond)

	finalCount := checkCount.Load()
	time.Sleep(100 * time.Millisecond)

	if checkCount.Load() != finalCount {
		t.Error("background checks continued after context cancellation")
	}
}

func TestDependencyHealthCheck(t *testing.T) {
	deps := map[string]func() error{
		"database": func() error { return nil },
		"cache":    func() error { return nil },
		"api":      func() error { return errors.New("API unavailable") },
	}

	check := DependencyHealthCheck(deps)
	err := check(context.Background())

	if err == nil {
		t.Error("expected error when dependency fails")
	}

	// All dependencies healthy
	deps["api"] = func() error { return nil }
	check = DependencyHealthCheck(deps)
	err = check(context.Background())

	if err != nil {
		t.Errorf("unexpected error when all dependencies are healthy: %v", err)
	}
}

func TestAggregateHealth(t *testing.T) {
	results := []HealthCheckResult{
		{Name: "comp1", Status: HealthStatusHealthy},
		{Name: "comp2", Status: HealthStatusDegraded},
		{Name: "comp3", Status: HealthStatusHealthy},
		{Name: "critical1", Status: HealthStatusHealthy},
	}

	criticalComponents := []string{"critical1"}

	// With degraded component
	status := AggregateHealth(results, criticalComponents)
	if status != HealthStatusDegraded {
		t.Errorf("expected degraded status, got %v", status)
	}

	// With critical component failure
	results[3].Status = HealthStatusUnhealthy
	status = AggregateHealth(results, criticalComponents)
	if status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy status, got %v", status)
	}

	// All healthy
	results = []HealthCheckResult{
		{Name: "comp1", Status: HealthStatusHealthy},
		{Name: "comp2", Status: HealthStatusHealthy},
		{Name: "critical1", Status: HealthStatusHealthy},
	}
	status = AggregateHealth(results, criticalComponents)
	if status != HealthStatusHealthy {
		t.Errorf("expected healthy status, got %v", status)
	}
}
