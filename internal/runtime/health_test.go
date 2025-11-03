package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test health checker creation
func TestNewHealthChecker(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	checker := NewHealthChecker(config, "test-observer")
	assert.NotNil(t, checker)
}

// RED: Test initial state is healthy
func TestHealthChecker_InitiallyHealthy(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")
	assert.True(t, checker.IsHealthy())
}

// RED: Test MarkUnhealthy
func TestHealthChecker_MarkUnhealthy(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")
	assert.True(t, checker.IsHealthy())

	checker.MarkUnhealthy("test failure")
	assert.False(t, checker.IsHealthy())
}

// RED: Test MarkHealthy
func TestHealthChecker_MarkHealthy(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")

	// Mark unhealthy
	checker.MarkUnhealthy("test failure")
	assert.False(t, checker.IsHealthy())

	// Mark healthy again
	checker.MarkHealthy()
	assert.True(t, checker.IsHealthy())
}

// RED: Test Run with context cancellation
func TestHealthChecker_Run(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 50 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run should block until context cancelled
	done := make(chan struct{})
	go func() {
		checker.Run(ctx)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Run did not complete after context cancellation")
	}
}

// RED: Test LastCheck updates
func TestHealthChecker_LastCheck(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 50 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")

	// Get initial last check time
	lastCheck1 := checker.LastCheck()
	require.False(t, lastCheck1.IsZero())

	// Mark unhealthy
	checker.MarkUnhealthy("test")

	// Last check should be updated
	lastCheck2 := checker.LastCheck()
	assert.True(t, lastCheck2.After(lastCheck1) || lastCheck2.Equal(lastCheck1))
}

// RED: Test GetStatus
func TestHealthChecker_GetStatus(t *testing.T) {
	config := HealthConfig{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
	}

	checker := NewHealthChecker(config, "test-observer")

	// Initially healthy
	status := checker.GetStatus()
	assert.True(t, status.Healthy)
	assert.Equal(t, "", status.Reason)
	assert.Equal(t, "test-observer", status.ObserverName)

	// Mark unhealthy
	checker.MarkUnhealthy("connection failed")

	status = checker.GetStatus()
	assert.False(t, status.Healthy)
	assert.Equal(t, "connection failed", status.Reason)
	assert.Equal(t, "test-observer", status.ObserverName)
	assert.False(t, status.LastCheck.IsZero())
}
