package cni

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkFlow_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		flow     *NetworkFlow
		timeout  time.Duration
		expected bool
	}{
		{
			name: "fresh flow",
			flow: &NetworkFlow{
				LastSeen: now.Add(-30 * time.Second),
			},
			timeout:  1 * time.Minute,
			expected: false,
		},
		{
			name: "expired flow",
			flow: &NetworkFlow{
				LastSeen: now.Add(-2 * time.Minute),
			},
			timeout:  1 * time.Minute,
			expected: true,
		},
		{
			name: "just expired flow",
			flow: &NetworkFlow{
				LastSeen: now.Add(-61 * time.Second),
			},
			timeout:  1 * time.Minute,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flow.IsExpired(tt.timeout)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNetworkFlow_GenerateKey(t *testing.T) {
	flow := &NetworkFlow{
		SourceIP:        net.ParseIP("10.244.1.10"),
		DestinationIP:   net.ParseIP("10.244.2.20"),
		SourcePort:      8080,
		DestinationPort: 80,
		Protocol:        6, // TCP
	}

	key := flow.GenerateKey()
	expected := "10.244.1.10:8080->10.244.2.20:80:6"
	assert.Equal(t, expected, key)
}

func TestNetworkFlow_CalculateRTT(t *testing.T) {
	now := time.Now()
	flow := &NetworkFlow{
		StartTime: now.Add(-100 * time.Millisecond),
		LastSeen:  now,
	}

	rtt := flow.CalculateRTT()
	assert.InDelta(t, 100*time.Millisecond, rtt, float64(10*time.Millisecond))
}

func TestNetworkFlow_UpdateStats(t *testing.T) {
	flow := &NetworkFlow{
		BytesTransmitted:   1000,
		PacketsTransmitted: 10,
		LastSeen:           time.Now().Add(-1 * time.Minute),
	}

	newBytes := uint64(500)
	newPackets := uint64(5)
	now := time.Now()

	flow.UpdateStats(newBytes, newPackets, now)

	assert.Equal(t, uint64(1500), flow.BytesTransmitted)
	assert.Equal(t, uint64(15), flow.PacketsTransmitted)
	assert.Equal(t, now, flow.LastSeen)
}

func TestFlowCache_NewFlowCache(t *testing.T) {
	maxSize := 1000
	ttl := 5 * time.Minute

	cache := NewFlowCache(maxSize, ttl)

	assert.NotNil(t, cache)
	assert.Equal(t, maxSize, cache.maxSize)
	assert.Equal(t, ttl, cache.ttl)
	assert.NotNil(t, cache.flows)
	assert.NotNil(t, cache.lastAccess)
}

func TestFlowCache_AddFlow(t *testing.T) {
	cache := NewFlowCache(3, 1*time.Minute)

	flow1 := &NetworkFlow{FlowID: "flow-1", State: FlowStateActive}
	flow2 := &NetworkFlow{FlowID: "flow-2", State: FlowStateActive}
	flow3 := &NetworkFlow{FlowID: "flow-3", State: FlowStateActive}

	cache.AddFlow(flow1)
	cache.AddFlow(flow2)
	cache.AddFlow(flow3)

	// Verify all flows are added
	assert.Equal(t, flow1, cache.GetFlow("flow-1"))
	assert.Equal(t, flow2, cache.GetFlow("flow-2"))
	assert.Equal(t, flow3, cache.GetFlow("flow-3"))

	// Add fourth flow to trigger eviction
	flow4 := &NetworkFlow{FlowID: "flow-4", State: FlowStateActive}
	cache.AddFlow(flow4)

	// Verify cache size is maintained
	cache.mutex.RLock()
	cacheSize := len(cache.flows)
	cache.mutex.RUnlock()
	assert.LessOrEqual(t, cacheSize, 3)

	// Verify newest flow is present
	assert.Equal(t, flow4, cache.GetFlow("flow-4"))
}

func TestFlowCache_GetFlow(t *testing.T) {
	cache := NewFlowCache(10, 1*time.Minute)

	flow := &NetworkFlow{FlowID: "test-flow", State: FlowStateActive}
	cache.AddFlow(flow)

	// Test existing flow
	retrieved := cache.GetFlow("test-flow")
	assert.Equal(t, flow, retrieved)

	// Test non-existent flow
	retrieved = cache.GetFlow("non-existent")
	assert.Nil(t, retrieved)
}

func TestFlowCache_RemoveFlow(t *testing.T) {
	cache := NewFlowCache(10, 1*time.Minute)

	flow := &NetworkFlow{FlowID: "test-flow", State: FlowStateActive}
	cache.AddFlow(flow)

	// Verify flow exists
	retrieved := cache.GetFlow("test-flow")
	assert.Equal(t, flow, retrieved)

	// Remove flow
	cache.RemoveFlow("test-flow")

	// Verify flow is removed
	retrieved = cache.GetFlow("test-flow")
	assert.Nil(t, retrieved)
}

func TestFlowCache_GetActiveFlows(t *testing.T) {
	cache := NewFlowCache(10, 1*time.Minute)

	flow1 := &NetworkFlow{FlowID: "flow-1", State: FlowStateActive}
	flow2 := &NetworkFlow{FlowID: "flow-2", State: FlowStateTerminated}
	flow3 := &NetworkFlow{FlowID: "flow-3", State: FlowStateActive}

	cache.AddFlow(flow1)
	cache.AddFlow(flow2)
	cache.AddFlow(flow3)

	activeFlows := cache.GetActiveFlows()

	// Should only return active flows
	assert.Len(t, activeFlows, 2)

	flowIDs := make([]string, len(activeFlows))
	for i, flow := range activeFlows {
		flowIDs[i] = flow.FlowID
	}

	assert.Contains(t, flowIDs, "flow-1")
	assert.Contains(t, flowIDs, "flow-3")
	assert.NotContains(t, flowIDs, "flow-2")
}

func TestFlowCache_CleanupExpiredFlows(t *testing.T) {
	cache := NewFlowCache(10, 100*time.Millisecond)

	now := time.Now()

	// Add fresh flow
	freshFlow := &NetworkFlow{
		FlowID:   "fresh-flow",
		LastSeen: now,
		State:    FlowStateActive,
	}
	cache.AddFlow(freshFlow)

	// Add expired flow
	expiredFlow := &NetworkFlow{
		FlowID:   "expired-flow",
		LastSeen: now.Add(-200 * time.Millisecond),
		State:    FlowStateActive,
	}
	cache.AddFlow(expiredFlow)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup
	cache.cleanupExpiredFlows()

	// Verify fresh flow still exists
	assert.NotNil(t, cache.GetFlow("fresh-flow"))

	// Verify expired flow is removed
	assert.Nil(t, cache.GetFlow("expired-flow"))
}

func TestFlowCache_GetStats(t *testing.T) {
	cache := NewFlowCache(10, 1*time.Minute)

	flow1 := &NetworkFlow{FlowID: "flow-1", State: FlowStateActive}
	flow2 := &NetworkFlow{FlowID: "flow-2", State: FlowStateTerminated}

	cache.AddFlow(flow1)
	cache.AddFlow(flow2)

	stats := cache.GetStats()

	assert.Equal(t, 2, stats.TotalFlows)
	assert.Equal(t, 1, stats.ActiveFlows)
	assert.Equal(t, 10, stats.MaxSize)
	assert.InDelta(t, 20.0, stats.UtilizationPercent, 1.0) // 2/10 * 100
}

func TestFlowCache_ConcurrentAccess(t *testing.T) {
	cache := NewFlowCache(100, 1*time.Minute)

	// Test concurrent reads and writes
	done := make(chan bool, 20)

	// Start 10 goroutines adding flows
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				flow := &NetworkFlow{
					FlowID: fmt.Sprintf("flow-%d-%d", id, j),
					State:  FlowStateActive,
				}
				cache.AddFlow(flow)
			}
			done <- true
		}(i)
	}

	// Start 10 goroutines reading flows
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				cache.GetFlow(fmt.Sprintf("flow-%d-%d", id, j))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify cache state is consistent
	stats := cache.GetStats()
	assert.LessOrEqual(t, stats.TotalFlows, 100) // Should not exceed max size
}

func TestFlowState_String(t *testing.T) {
	tests := []struct {
		state    FlowState
		expected string
	}{
		{FlowStateActive, "active"},
		{FlowStateTerminated, "terminated"},
		{FlowStateTimeout, "timeout"},
		{FlowState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyDecision_String(t *testing.T) {
	tests := []struct {
		decision PolicyDecision
		expected string
	}{
		{PolicyAllow, "allow"},
		{PolicyDeny, "deny"},
		{PolicyLog, "log"},
		{PolicyDecision(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.decision.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNetworkFlow_CalculateBandwidth(t *testing.T) {
	flow := &NetworkFlow{
		BytesTransmitted: 1024 * 1024, // 1MB
		StartTime:        time.Now().Add(-1 * time.Second),
		LastSeen:         time.Now(),
	}

	bandwidth := flow.CalculateBandwidth()

	// Should be approximately 1MB/s = 1048576 bytes/s
	assert.InDelta(t, 1048576.0, bandwidth, 100000.0) // Allow some variance
}

func TestNetworkFlow_IsInternalTraffic(t *testing.T) {
	tests := []struct {
		name     string
		flow     *NetworkFlow
		expected bool
	}{
		{
			name: "internal cluster traffic",
			flow: &NetworkFlow{
				SourceIP:      net.ParseIP("10.244.1.10"),
				DestinationIP: net.ParseIP("10.244.2.20"),
			},
			expected: true,
		},
		{
			name: "external traffic",
			flow: &NetworkFlow{
				SourceIP:      net.ParseIP("10.244.1.10"),
				DestinationIP: net.ParseIP("8.8.8.8"),
			},
			expected: false,
		},
		{
			name: "loopback traffic",
			flow: &NetworkFlow{
				SourceIP:      net.ParseIP("127.0.0.1"),
				DestinationIP: net.ParseIP("127.0.0.1"),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flow.IsInternalTraffic()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests for flow cache performance
func BenchmarkFlowCache_AddFlow(b *testing.B) {
	cache := NewFlowCache(1000, 5*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow := &NetworkFlow{
			FlowID: fmt.Sprintf("flow-%d", i),
			State:  FlowStateActive,
		}
		cache.AddFlow(flow)
	}
}

func BenchmarkFlowCache_GetFlow(b *testing.B) {
	cache := NewFlowCache(1000, 5*time.Minute)

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		flow := &NetworkFlow{
			FlowID: fmt.Sprintf("flow-%d", i),
			State:  FlowStateActive,
		}
		cache.AddFlow(flow)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.GetFlow(fmt.Sprintf("flow-%d", i%1000))
	}
}

func BenchmarkFlowCache_GetActiveFlows(b *testing.B) {
	cache := NewFlowCache(1000, 5*time.Minute)

	// Pre-populate cache with mix of active and terminated flows
	for i := 0; i < 500; i++ {
		flow := &NetworkFlow{
			FlowID: fmt.Sprintf("active-flow-%d", i),
			State:  FlowStateActive,
		}
		cache.AddFlow(flow)
	}
	for i := 0; i < 500; i++ {
		flow := &NetworkFlow{
			FlowID: fmt.Sprintf("terminated-flow-%d", i),
			State:  FlowStateTerminated,
		}
		cache.AddFlow(flow)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.GetActiveFlows()
	}
}

func BenchmarkNetworkFlow_GenerateKey(b *testing.B) {
	flow := &NetworkFlow{
		SourceIP:        net.ParseIP("10.244.1.10"),
		DestinationIP:   net.ParseIP("10.244.2.20"),
		SourcePort:      8080,
		DestinationPort: 80,
		Protocol:        6,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flow.GenerateKey()
	}
}
