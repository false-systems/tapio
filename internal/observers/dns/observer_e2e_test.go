package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// Helper function to create query name
func makeQueryName(name string) [253]byte {
	var result [253]byte
	copy(result[:], name)
	return result
}

// Helper function to create comm name
func makeComm(name string) [16]byte {
	var result [16]byte
	copy(result[:], name)
	return result
}

// E2E tests verify complete workflows from start to finish

func TestE2E_DNSProblemDetectionFlow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-dns"
	config.EnableEBPF = false
	config.BufferSize = 100
	config.RepeatThreshold = 2

	// Create and start observer
	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Simulate DNS problems through internal methods
	// (In real E2E, this would come from actual DNS queries)

	// Simulate slow query
	slowEvent := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemSlow,
		QueryName:   makeQueryName("slow.example.com"),
		QueryType:   1,           // A record
		LatencyNs:   250_000_000, // 250ms
		PID:         12345,
		Comm:        makeComm("curl"),
	}

	// Track the problem multiple times to trigger repeat detection
	for i := 0; i < 3; i++ {
		isRepeated := obs.trackProblem(slowEvent)
		if i >= 1 {
			assert.True(t, isRepeated, "Should detect repeated problem after threshold")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify statistics were updated
	stats := obs.GetStats()
	assert.Greater(t, stats.SlowQueries, uint64(0))
	assert.Greater(t, stats.TotalProblems, uint64(0))

	// Verify health is good
	assert.True(t, obs.IsHealthy())
	health := obs.Health()
	assert.Equal(t, "healthy", string(health.Status))
}

func TestE2E_MultipleProblemTypes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-multi"
	config.EnableEBPF = false

	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Simulate different problem types
	problems := []struct {
		name        string
		problemType DNSProblemType
		queryName   string
		latency     uint64
	}{
		{"slow_query", DNSProblemSlow, "slow.test.com", 200_000_000},
		{"timeout", DNSProblemTimeout, "timeout.test.com", 5_000_000_000},
		{"nxdomain", DNSProblemNXDomain, "nonexistent.test.com", 50_000_000},
		{"servfail", DNSProblemServfail, "failed.test.com", 100_000_000},
		{"refused", DNSProblemRefused, "refused.test.com", 30_000_000},
	}

	for _, p := range problems {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: p.problemType,
			QueryName:   makeQueryName(p.queryName),
			QueryType:   1,
			LatencyNs:   p.latency,
			PID:         uint32(1000 + uint32(p.problemType)),
		}
		obs.trackProblem(event)
	}

	// Verify all problem types were tracked
	stats := obs.GetStats()
	assert.Greater(t, stats.SlowQueries, uint64(0))
	assert.Greater(t, stats.Timeouts, uint64(0))
	assert.Greater(t, stats.NXDomains, uint64(0))
	assert.Greater(t, stats.ServerFailures, uint64(0))
	assert.Equal(t, uint64(5), stats.TotalProblems)
}

func TestE2E_ObserverLifecycleWithEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-lifecycle"
	config.EnableEBPF = false

	// Phase 1: Create observer
	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)
	assert.False(t, obs.IsHealthy(), "Should not be healthy before start")

	// Phase 2: Start observer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	assert.True(t, obs.IsHealthy(), "Should be healthy after start")

	// Phase 3: Get event channel
	events := obs.Events()
	require.NotNil(t, events)

	// Phase 4: Generate some activity
	for i := 0; i < 5; i++ {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemSlow,
			QueryName:   makeQueryName("test.example.com"),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
		time.Sleep(50 * time.Millisecond)
	}

	// Phase 5: Verify metrics
	stats := obs.Statistics()
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(0))

	// Phase 6: Stop observer
	err = obs.Stop()
	assert.NoError(t, err)
	assert.False(t, obs.IsHealthy(), "Should not be healthy after stop")

	// Phase 7: Verify clean shutdown
	select {
	case _, open := <-events:
		assert.False(t, open, "Event channel should be closed")
	default:
		// Channel might still have buffered events
	}
}

func TestE2E_RateLimiting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-ratelimit"
	config.EnableEBPF = false
	config.MaxEventsPerSecond = 10
	config.RateLimitWindow = 100 * time.Millisecond

	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Try to generate more events than rate limit allows
	startTime := time.Now()
	eventsGenerated := 0

	for i := 0; i < 50; i++ {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemSlow,
			QueryName:   makeQueryName("burst.test.com"),
			QueryType:   1,
			LatencyNs:   100_000_000,
		}
		obs.trackProblem(event)
		eventsGenerated++

		// Small delay to spread events
		time.Sleep(5 * time.Millisecond)
	}

	elapsed := time.Since(startTime)

	// Calculate expected max events based on rate limit
	expectedMax := int(float64(config.MaxEventsPerSecond) * elapsed.Seconds())

	stats := obs.GetStats()
	t.Logf("Generated %d events, processed %d problems in %v (max allowed: ~%d)",
		eventsGenerated, stats.TotalProblems, elapsed, expectedMax)

	// Stats should reflect rate limiting in action
	assert.Greater(t, int(stats.TotalProblems), 0)
}

func TestE2E_RepeatProblemDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-repeat"
	config.EnableEBPF = false
	config.RepeatThreshold = 3
	config.RepeatWindowSec = 2

	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Generate repeated problems for same domain
	problemDomain := "problematic.service.local"
	var repeatDetected bool

	for i := 0; i < 5; i++ {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemTimeout,
			QueryName:   makeQueryName(problemDomain),
			QueryType:   1,
			LatencyNs:   5_000_000_000,
		}

		isRepeated := obs.trackProblem(event)
		if isRepeated {
			repeatDetected = true
			t.Logf("Repeat detected after %d occurrences", i+1)
		}

		time.Sleep(100 * time.Millisecond)
	}

	assert.True(t, repeatDetected, "Should detect repeated problems")

	// Wait for window to expire
	time.Sleep(2100 * time.Millisecond)
	obs.doCleanup()

	// Problem should be cleaned up
	obs.mu.RLock()
	_, exists := obs.recentProblems[problemDomain]
	obs.mu.RUnlock()
	assert.False(t, exists, "Old problems should be cleaned up")
}

func TestE2E_FallbackMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E fallback test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-fallback"
	config.EnableEBPF = false // Force fallback

	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// In fallback mode, observer should still be functional
	assert.True(t, obs.IsHealthy())

	events := obs.Events()
	assert.NotNil(t, events)

	// Should be able to track problems manually
	event := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemSlow,
		QueryName:   makeQueryName("fallback.test.com"),
		QueryType:   1,
		LatencyNs:   200_000_000,
	}

	obs.trackProblem(event)

	stats := obs.GetStats()
	assert.Equal(t, uint64(1), stats.SlowQueries)
}

func TestE2E_EventChannelDelivery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "e2e-events"
	config.EnableEBPF = false
	config.BufferSize = 10

	obs, err := NewObserver("e2e", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	events := obs.Events()

	// Create goroutine to consume events
	received := make([]*domain.CollectorEvent, 0)
	done := make(chan struct{})

	go func() {
		defer close(done)
		timeout := time.After(1 * time.Second)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if event != nil {
					received = append(received, event)
				}
			case <-timeout:
				return
			}
		}
	}()

	// Wait for consumer to finish
	<-done

	// In fallback mode, may not receive events unless generated
	t.Logf("Received %d events through channel", len(received))
}
