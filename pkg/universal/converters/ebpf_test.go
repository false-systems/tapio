package converters

import (
	"testing"
	"time"

	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/universal"
)

func TestEBPFConverter_ConvertProcessMemoryStats(t *testing.T) {
	converter := NewEBPFConverter("test-source", "1.0")

	tests := []struct {
		name  string
		stats *ebpf.ProcessMemoryStats
		check func(t *testing.T, metric *universal.UniversalMetric, err error)
	}{
		{
			name: "Basic conversion",
			stats: &ebpf.ProcessMemoryStats{
				PID:          1234,
				CurrentUsage: 100 * 1024 * 1024, // 100MB
				LastUpdate:   time.Now(),
			},
			check: func(t *testing.T, metric *universal.UniversalMetric, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if metric.Name != "memory_usage_bytes" {
					t.Errorf("Expected name 'memory_usage_bytes', got %s", metric.Name)
				}

				if metric.Value != float64(100*1024*1024) {
					t.Errorf("Expected value %f, got %f", float64(100*1024*1024), metric.Value)
				}

				if metric.Unit != "bytes" {
					t.Errorf("Expected unit 'bytes', got %s", metric.Unit)
				}

				if metric.Target.PID != 1234 {
					t.Errorf("Expected PID 1234, got %d", metric.Target.PID)
				}
			},
		},
		{
			name: "Container process",
			stats: &ebpf.ProcessMemoryStats{
				PID:          5678,
				CurrentUsage: 200 * 1024 * 1024,
				InContainer:  true,
				ContainerPID: 12345,
			},
			check: func(t *testing.T, metric *universal.UniversalMetric, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if metric.Labels["in_container"] != "true" {
					t.Error("Expected in_container label")
				}

				if metric.Labels["container_pid"] != "12345" {
					t.Errorf("Expected container_pid '12345', got %s", metric.Labels["container_pid"])
				}
			},
		},
		{
			name:  "Nil stats",
			stats: nil,
			check: func(t *testing.T, metric *universal.UniversalMetric, err error) {
				if err == nil {
					t.Error("Expected error for nil stats")
				}
				if metric != nil {
					t.Error("Expected nil metric for nil stats")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric, err := converter.ConvertProcessMemoryStats(tt.stats)
			tt.check(t, metric, err)

			// Clean up
			if metric != nil {
				universal.PutMetric(metric)
			}
		})
	}
}

func TestEBPFConverter_ConvertMemoryGrowthToMetrics(t *testing.T) {
	converter := NewEBPFConverter("test-source", "1.0")

	now := time.Now()
	stats := &ebpf.ProcessMemoryStats{
		PID: 1234,
		GrowthPattern: []ebpf.MemoryDataPoint{
			{Timestamp: now.Add(-2 * time.Minute), Usage: 50 * 1024 * 1024},
			{Timestamp: now.Add(-1 * time.Minute), Usage: 75 * 1024 * 1024},
			{Timestamp: now, Usage: 100 * 1024 * 1024},
		},
	}

	metrics, err := converter.ConvertMemoryGrowthToMetrics(stats)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(metrics) != 3 {
		t.Fatalf("Expected 3 metrics, got %d", len(metrics))
	}

	// Check growth pattern
	for i, metric := range metrics {
		expectedUsage := float64((i+1)*25+25) * 1024 * 1024
		if metric.Value != expectedUsage {
			t.Errorf("Metric %d: expected value %f, got %f", i, expectedUsage, metric.Value)
		}

		if metric.Labels["pattern"] != "growth" {
			t.Errorf("Expected pattern label 'growth', got %s", metric.Labels["pattern"])
		}

		// Clean up
		universal.PutMetric(metric)
	}
}

func TestEBPFConverter_ConvertOOMEvent(t *testing.T) {
	converter := NewEBPFConverter("test-source", "1.0")

	pid := int32(9999)
	timestamp := time.Now()
	details := map[string]interface{}{
		"memory_limit": 512 * 1024 * 1024,
		"last_usage":   510 * 1024 * 1024,
	}

	event, err := converter.ConvertOOMEvent(pid, timestamp, details)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if event.Type != universal.EventTypeOOMKill {
		t.Errorf("Expected event type %s, got %s", universal.EventTypeOOMKill, event.Type)
	}

	if event.Level != universal.EventLevelCritical {
		t.Errorf("Expected event level %s, got %s", universal.EventLevelCritical, event.Level)
	}

	if event.Target.PID != pid {
		t.Errorf("Expected PID %d, got %d", pid, event.Target.PID)
	}

	if event.Quality.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0 for OOM event, got %f", event.Quality.Confidence)
	}

	// Clean up
	universal.PutEvent(event)
}

func TestEBPFConverter_ConvertMemoryPressureEvent(t *testing.T) {
	converter := NewEBPFConverter("test-source", "1.0")

	tests := []struct {
		name          string
		currentUsage  uint64
		totalAlloc    uint64
		threshold     float64
		expectedLevel universal.EventLevel
	}{
		{
			name:          "Critical pressure",
			currentUsage:  95 * 1024 * 1024,
			totalAlloc:    100 * 1024 * 1024,
			threshold:     0.95,
			expectedLevel: universal.EventLevelCritical,
		},
		{
			name:          "High pressure",
			currentUsage:  85 * 1024 * 1024,
			totalAlloc:    100 * 1024 * 1024,
			threshold:     0.85,
			expectedLevel: universal.EventLevelError,
		},
		{
			name:          "Medium pressure",
			currentUsage:  75 * 1024 * 1024,
			totalAlloc:    100 * 1024 * 1024,
			threshold:     0.75,
			expectedLevel: universal.EventLevelWarning,
		},
		{
			name:          "Low pressure",
			currentUsage:  50 * 1024 * 1024,
			totalAlloc:    100 * 1024 * 1024,
			threshold:     0.5,
			expectedLevel: universal.EventLevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &ebpf.ProcessMemoryStats{
				PID:            1234,
				CurrentUsage:   tt.currentUsage,
				TotalAllocated: tt.totalAlloc,
			}

			event, err := converter.ConvertMemoryPressureEvent(stats, tt.threshold)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if event.Level != tt.expectedLevel {
				t.Errorf("Expected level %s, got %s", tt.expectedLevel, event.Level)
			}

			// Clean up
			universal.PutEvent(event)
		})
	}
}

func TestPIDMapper(t *testing.T) {
	mapper := NewPIDMapper()

	// Test initial mapping
	target1, err := mapper.MapPIDToTarget(1234)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if target1.Type != universal.TargetTypeProcess {
		t.Errorf("Expected target type %s, got %s", universal.TargetTypeProcess, target1.Type)
	}

	// Test cache hit
	target2, err := mapper.MapPIDToTarget(1234)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if target1 != target2 {
		t.Error("Expected cached target to be returned")
	}

	// Test update mapping
	newTarget := &universal.Target{
		Type:      universal.TargetTypeContainer,
		Name:      "nginx",
		PID:       1234,
		Container: "nginx-abc123",
	}

	mapper.UpdateMapping(1234, newTarget)

	target3, err := mapper.MapPIDToTarget(1234)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if target3.Type != universal.TargetTypeContainer {
		t.Error("Expected updated target type")
	}

	// Test clear cache
	mapper.ClearCache()

	target4, err := mapper.MapPIDToTarget(1234)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if target4.Type == universal.TargetTypeContainer {
		t.Error("Expected cache to be cleared")
	}
}

func TestCalculateConfidence(t *testing.T) {
	converter := NewEBPFConverter("test-source", "1.0")

	tests := []struct {
		name    string
		stats   *ebpf.ProcessMemoryStats
		maxConf float64
	}{
		{
			name: "Good data",
			stats: &ebpf.ProcessMemoryStats{
				CurrentUsage:   100 * 1024 * 1024,
				TotalAllocated: 200 * 1024 * 1024,
				TotalFreed:     100 * 1024 * 1024,
				GrowthPattern: []ebpf.MemoryDataPoint{
					{Usage: 50}, {Usage: 75}, {Usage: 100},
				},
			},
			maxConf: 0.9,
		},
		{
			name: "Zero usage",
			stats: &ebpf.ProcessMemoryStats{
				CurrentUsage: 0,
			},
			maxConf: 0.5,
		},
		{
			name: "Few data points",
			stats: &ebpf.ProcessMemoryStats{
				CurrentUsage:  100,
				GrowthPattern: []ebpf.MemoryDataPoint{{Usage: 100}},
			},
			maxConf: 0.8,
		},
		{
			name: "Inconsistent data",
			stats: &ebpf.ProcessMemoryStats{
				CurrentUsage:   100,
				TotalAllocated: 100,
				TotalFreed:     200, // More freed than allocated
			},
			maxConf: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := converter.calculateConfidence(tt.stats)
			if confidence > tt.maxConf {
				t.Errorf("Expected confidence <= %f, got %f", tt.maxConf, confidence)
			}
		})
	}
}
