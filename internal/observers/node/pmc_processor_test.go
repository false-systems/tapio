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
}

// TestPMCProcessor_MultiplePerCPU verifies per-CPU tracking
func TestPMCProcessor_MultiplePerCPU(t *testing.T) {
	proc := NewPMCProcessor("test-node")

	// CPU 0 samples
	cpu0_sample1 := PMCEvent{CPU: 0, Cycles: 1000000, Instructions: 800000}
	cpu0_sample2 := PMCEvent{CPU: 0, Cycles: 2000000, Instructions: 1600000}

	// CPU 1 samples
	cpu1_sample1 := PMCEvent{CPU: 1, Cycles: 1000000, Instructions: 400000}
	cpu1_sample2 := PMCEvent{CPU: 1, Cycles: 2000000, Instructions: 800000}

	// Process CPU 0
	proc.Process(context.Background(), cpu0_sample1)
	result0 := proc.Process(context.Background(), cpu0_sample2)

	// Process CPU 1
	proc.Process(context.Background(), cpu1_sample1)
	result1 := proc.Process(context.Background(), cpu1_sample2)

	// CPU 0: IPC = (1600000 - 800000) / (2000000 - 1000000) = 0.8
	require.NotNil(t, result0)
	assert.Equal(t, 0.8, result0.NodeData.CPUIPC)

	// CPU 1: IPC = (800000 - 400000) / (2000000 - 1000000) = 0.4
	require.NotNil(t, result1)
	assert.Equal(t, 0.4, result1.NodeData.CPUIPC)
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
