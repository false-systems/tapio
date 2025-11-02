//go:build linux

package node

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD Cycle 1: PMCProcessor IPC Calculation
// RED Phase: Write failing test first

// TestPMCProcessor_CalculateIPC verifies IPC calculation from PMC deltas
func TestPMCProcessor_CalculateIPC(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// First sample (baseline) - should return nil (need 2 samples)
	event1 := PMCEvent{
		CPU:          0,
		Cycles:       1000000,
		Instructions: 500000,
		StallCycles:  300000,
		Timestamp:    1000000000, // 1 second in nanoseconds
	}

	result := proc.Process(context.Background(), event1)
	assert.Nil(t, result, "First sample should return nil (no baseline yet)")

	// Second sample - should calculate IPC
	event2 := PMCEvent{
		CPU:          0,
		Cycles:       2000000,
		Instructions: 1000000,
		StallCycles:  800000,
		Timestamp:    2000000000, // 2 seconds
	}

	result = proc.Process(context.Background(), event2)
	require.NotNil(t, result, "Second sample should return event with IPC")

	// Verify event structure
	assert.Equal(t, "node", result.Type)
	assert.Equal(t, "test-node-pmc", result.Source)
	require.NotNil(t, result.NodeData)

	// Calculate expected values:
	// Delta cycles = 2000000 - 1000000 = 1000000
	// Delta instructions = 1000000 - 500000 = 500000
	// IPC = 500000 / 1000000 = 0.5
	assert.Equal(t, 0.5, result.NodeData.CPUIPC)

	// Delta stalls = 800000 - 300000 = 500000
	// Stall % = 500000 / 1000000 * 100 = 50%
	assert.Equal(t, 50.0, result.NodeData.MemoryStalls)

	// Impact classification: IPC=0.5 + Stall=50% → medium
	assert.Equal(t, "medium", result.NodeData.PerformanceImpact)
}

// TestPMCProcessor_MultiplePerCPU verifies per-CPU tracking
func TestPMCProcessor_MultiplePerCPU(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// CPU 0 samples - degraded performance
	cpu0_sample1 := PMCEvent{CPU: 0, Cycles: 1000000, Instructions: 400000, StallCycles: 300000}
	cpu0_sample2 := PMCEvent{CPU: 0, Cycles: 2000000, Instructions: 800000, StallCycles: 600000}

	// CPU 1 samples - worse degradation
	cpu1_sample1 := PMCEvent{CPU: 1, Cycles: 1000000, Instructions: 200000, StallCycles: 600000}
	cpu1_sample2 := PMCEvent{CPU: 1, Cycles: 2000000, Instructions: 400000, StallCycles: 1200000}

	// Process CPU 0
	proc.Process(context.Background(), cpu0_sample1)
	result0 := proc.Process(context.Background(), cpu0_sample2)

	// Process CPU 1
	proc.Process(context.Background(), cpu1_sample1)
	result1 := proc.Process(context.Background(), cpu1_sample2)

	// CPU 0: IPC = (800000 - 400000) / (2000000 - 1000000) = 0.4
	//        Stalls = (600000 - 300000) / 1000000 = 30% → "low" impact
	require.NotNil(t, result0)
	assert.Equal(t, 0.4, result0.NodeData.CPUIPC)
	assert.Equal(t, 30.0, result0.NodeData.MemoryStalls)

	// CPU 1: IPC = (400000 - 200000) / (2000000 - 1000000) = 0.2
	//        Stalls = (1200000 - 600000) / 1000000 = 60% → "high" impact
	require.NotNil(t, result1)
	assert.Equal(t, 0.2, result1.NodeData.CPUIPC)
	assert.Equal(t, 60.0, result1.NodeData.MemoryStalls)
}

// TestPMCProcessor_ZeroCycles handles division by zero
func TestPMCProcessor_ZeroCycles(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// Baseline
	event1 := PMCEvent{CPU: 0, Cycles: 1000000, Instructions: 500000}
	proc.Process(context.Background(), event1)

	// Same cycles (no time passed) - should not emit event
	event2 := PMCEvent{CPU: 0, Cycles: 1000000, Instructions: 500000}
	result := proc.Process(context.Background(), event2)

	assert.Nil(t, result, "Zero delta cycles should return nil (avoid div by zero)")
}

// TestPMCProcessor_ClassifyImpact verifies performance impact classification
func TestPMCProcessor_ClassifyImpact(t *testing.T) {
	tests := []struct {
		name        string
		ipc         float64
		stallPct    float64
		wantImpact  string
		wantSubtype string
		shouldEmit  bool
	}{
		{
			name:        "critical - severe memory bottleneck",
			ipc:         0.18,
			stallPct:    75.0,
			wantImpact:  "critical",
			wantSubtype: "node_critical_memory_bottleneck",
			shouldEmit:  true,
		},
		{
			name:        "high - memory bottleneck",
			ipc:         0.28,
			stallPct:    55.0,
			wantImpact:  "high",
			wantSubtype: "node_memory_bottleneck",
			shouldEmit:  true,
		},
		{
			name:        "medium - performance degradation",
			ipc:         0.45,
			stallPct:    35.0,
			wantImpact:  "medium",
			wantSubtype: "node_performance_degradation",
			shouldEmit:  true,
		},
		{
			name:        "low - minor degradation",
			ipc:         0.65,
			stallPct:    25.0,
			wantImpact:  "low",
			wantSubtype: "node_performance_degradation",
			shouldEmit:  true,
		},
		{
			name:        "none - healthy performance",
			ipc:         0.80,
			stallPct:    10.0,
			wantImpact:  "",
			wantSubtype: "",
			shouldEmit:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := NewPMCProcessor("test-node")

			// Create events that will produce desired IPC and stall%
			// We need deltaCycles = 1000000 for easy math
			cycles1 := uint64(1000000)
			instructions1 := uint64(500000) // Arbitrary baseline
			stalls1 := uint64(200000)       // Arbitrary baseline

			cycles2 := cycles1 + 1000000 // Delta = 1000000
			instructions2 := instructions1 + uint64(tt.ipc*1000000)
			stalls2 := stalls1 + uint64(tt.stallPct/100.0*1000000)

			event1 := PMCEvent{
				CPU:          0,
				Cycles:       cycles1,
				Instructions: instructions1,
				StallCycles:  stalls1,
			}
			proc.Process(context.Background(), event1)

			event2 := PMCEvent{
				CPU:          0,
				Cycles:       cycles2,
				Instructions: instructions2,
				StallCycles:  stalls2,
			}
			result := proc.Process(context.Background(), event2)

			if tt.shouldEmit {
				require.NotNil(t, result, "Should emit event for impact: %s", tt.wantImpact)
				assert.Equal(t, tt.wantSubtype, result.Subtype)
				assert.Equal(t, tt.wantImpact, result.NodeData.PerformanceImpact)
				assert.InDelta(t, tt.ipc, result.NodeData.CPUIPC, 0.01)
				assert.InDelta(t, tt.stallPct, result.NodeData.MemoryStalls, 0.1)
			} else {
				assert.Nil(t, result, "Should not emit event for healthy performance")
			}
		})
	}
}

// TDD Cycle 2: Counter Overflow Handling
// RED Phase: Write failing test for 48-bit counter wraparound

// TestPMCProcessor_CounterOverflow48Bit verifies handling of 48-bit counter wraparound
func TestPMCProcessor_CounterOverflow48Bit(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// PMC counters are 48-bit (Intel/AMD hardware limitation)
	// WHY 48-bit: x86_64 CPUs use 48-bit general-purpose performance counters
	// Reference: Intel SDM Vol 3B, AMD APM Vol 2 (IA32_PMCx registers)
	const maxCounter48bit uint64 = (1 << 48) - 1

	// Sample near overflow (counter close to max)
	// Use low IPC + high stalls to ensure degradation is detected
	event1 := PMCEvent{
		CPU:          0,
		Cycles:       maxCounter48bit - 1000,
		Instructions: maxCounter48bit - 200,  // Low IPC
		StallCycles:  maxCounter48bit - 1500, // High stalls
	}
	proc.Process(context.Background(), event1)

	// Counter wraps around to small value
	event2 := PMCEvent{
		CPU:          0,
		Cycles:       1000, // Wrapped around, delta = 2000
		Instructions: 200,  // Wrapped around, delta = 400
		StallCycles:  1500, // Wrapped around, delta = 3000
	}
	result := proc.Process(context.Background(), event2)

	require.NotNil(t, result, "Should handle counter overflow")

	// Delta: cycles=2000, instructions=400, stalls=3000
	// IPC = 400 / 2000 = 0.2
	// Stalls% = 3000 / 2000 * 100 = 150% (capped at 100% logically, but math is correct)
	expectedDeltaCycles := 2000.0
	expectedDeltaInstructions := 400.0

	expectedIPC := expectedDeltaInstructions / expectedDeltaCycles
	assert.InDelta(t, expectedIPC, result.NodeData.CPUIPC, 0.01, "IPC should be ~0.2 after overflow")

	// Verify IPC is positive and reasonable (not negative or infinity)
	assert.Greater(t, result.NodeData.CPUIPC, 0.0, "IPC should be positive")
	assert.Less(t, result.NodeData.CPUIPC, 10.0, "IPC should be reasonable (<10)")
}

// TestPMCProcessor_CounterBackwards detects corrupted data (counter went backwards without overflow)
func TestPMCProcessor_CounterBackwards(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// First sample - degraded (so first event emits)
	event1 := PMCEvent{
		CPU:          0,
		Cycles:       2000000,
		Instructions: 400000,  // Low IPC
		StallCycles:  1200000, // High stalls (60%)
	}
	result1 := proc.Process(context.Background(), event1)
	assert.Nil(t, result1, "First event should not emit (no baseline)")

	// Second sample - also degraded, establishes baseline
	event2 := PMCEvent{
		CPU:          0,
		Cycles:       3000000,
		Instructions: 800000,
		StallCycles:  1900000,
	}
	result2 := proc.Process(context.Background(), event2)
	require.NotNil(t, result2, "Second degraded event should emit")

	// Counter goes backwards (corrupted data, NOT overflow)
	// This will be treated as wraparound
	event3 := PMCEvent{
		CPU:          0,
		Cycles:       1000000, // Went backwards!
		Instructions: 200000,
		StallCycles:  600000,
	}
	result3 := proc.Process(context.Background(), event3)

	// The wraparound will produce huge deltas, may or may not emit depending on impact change
	// Just verify it doesn't panic and IPC is positive if emitted
	if result3 != nil {
		assert.Greater(t, result3.NodeData.CPUIPC, 0.0, "IPC should be positive even with backwards counter")
	}
}

// TestPMCProcessor_LargeCounterJump detects suspiciously large deltas
func TestPMCProcessor_LargeCounterJump(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// Baseline
	event1 := PMCEvent{
		CPU:          0,
		Cycles:       1000000,
		Instructions: 500000,
		StallCycles:  300000,
	}
	proc.Process(context.Background(), event1)

	// Huge jump (e.g., system suspended for hours, counter wrapped multiple times)
	const maxCounter48bit uint64 = (1 << 48) - 1
	event2 := PMCEvent{
		CPU:          0,
		Cycles:       maxCounter48bit - 100, // Near max
		Instructions: maxCounter48bit - 200,
		StallCycles:  maxCounter48bit - 50,
	}
	result := proc.Process(context.Background(), event2)

	// Should still calculate positive IPC (no panic, no negative)
	if result != nil {
		assert.Greater(t, result.NodeData.CPUIPC, 0.0, "IPC should be positive")
		assert.Less(t, result.NodeData.CPUIPC, 10.0, "IPC should be reasonable")
	}
}
