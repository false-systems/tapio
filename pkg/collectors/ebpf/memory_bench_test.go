//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"testing"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// BenchmarkMemoryEventProcessing benchmarks the core event processing pipeline
func BenchmarkMemoryEventProcessing(b *testing.B) {
	config := collectors.CollectorConfig{
		Name:            "test-memory-collector",
		Enabled:         true,
		EventBufferSize: 10000,
		Debug:           false,
	}

	collector, err := NewMemoryCollector(config)
	if err != nil {
		b.Skipf("Skipping benchmark: %v", err)
	}

	memCollector := collector.(*MemoryCollector)

	// Create sample memory events
	sampleEvents := make([]*MemoryEvent, 1000)
	for i := 0; i < 1000; i++ {
		sampleEvents[i] = &MemoryEvent{
			Timestamp:    uint64(time.Now().UnixNano()),
			PID:          uint32(1000 + i%100),
			TID:          uint32(1000 + i%100),
			Size:         uint64(4096 * (i%10 + 1)),
			TotalMemory:  uint64(1024*1024 + i*4096),
			EventType:    uint32(1 + i%3), // Mix of alloc, free, oom
			Command:      "test-process",
			InContainer:  i%3 == 0,
			ContainerPID: uint32(100 + i%10),
		}
	}

	b.ResetTimer()

	b.Run("ProcessBatchedEvents", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			batchSize := 100
			startIdx := (i * batchSize) % len(sampleEvents)
			endIdx := startIdx + batchSize
			if endIdx > len(sampleEvents) {
				endIdx = len(sampleEvents)
			}
			
			batch := sampleEvents[startIdx:endIdx]
			_ = memCollector.processBatchedEvents(batch)
		}
	})

	b.Run("UpdateProcessTracker", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			event := sampleEvents[i%len(sampleEvents)]
			memCollector.updateProcessTracker(event)
		}
	})

	b.Run("CreateCollectorEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			event := sampleEvents[i%len(sampleEvents)]
			_ = memCollector.createCollectorEvent(event)
		}
	})
}

// BenchmarkOOMPrediction benchmarks the OOM prediction algorithms
func BenchmarkOOMPrediction(b *testing.B) {
	predictor := NewOOMPredictor()

	// Create a process tracker with growth history
	tracker := &ProcessMemoryTracker{
		PID:           1001,
		Command:       "test-process",
		GrowthHistory: make([]MemoryDataPoint, 50),
		GrowthTrend:   TrendLinear,
		RiskScore:     0.5,
	}

	// Fill with sample data showing linear growth
	baseTime := time.Now()
	for i := 0; i < 50; i++ {
		tracker.GrowthHistory[i] = MemoryDataPoint{
			Timestamp: baseTime.Add(time.Duration(i) * 10 * time.Second),
			Usage:     uint64(1024*1024 + i*1024*100), // 100KB growth per point
			Rate:      10240, // 10KB/s
		}
	}

	b.ResetTimer()

	b.Run("PredictLinearGrowth", func(b *testing.B) {
		tracker.GrowthTrend = TrendLinear
		for i := 0; i < b.N; i++ {
			_ = predictor.predictLinearGrowth(tracker)
		}
	})

	b.Run("PredictExponentialGrowth", func(b *testing.B) {
		tracker.GrowthTrend = TrendExponential
		// Adjust data for exponential growth
		for i := 1; i < len(tracker.GrowthHistory); i++ {
			tracker.GrowthHistory[i].Usage = tracker.GrowthHistory[i-1].Usage * 110 / 100
		}
		
		for i := 0; i < b.N; i++ {
			_ = predictor.predictExponentialGrowth(tracker)
		}
	})

	b.Run("AnalyzeGrowthTrend", func(b *testing.B) {
		collector := &MemoryCollector{}
		for i := 0; i < b.N; i++ {
			_ = collector.analyzeGrowthTrend(tracker.GrowthHistory)
		}
	})
}

// BenchmarkRingBufferProcessing benchmarks the optimized ring buffer processing
func BenchmarkRingBufferProcessing(b *testing.B) {
	// Mock ring buffer data
	sampleData := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		// Create realistic eBPF event data (56 bytes)
		data := make([]byte, 56)
		// Fill with sample memory event data
		timestamp := uint64(time.Now().UnixNano())
		copy(data[0:8], (*(*[8]byte)(unsafe.Pointer(&timestamp)))[:])
		pid := uint32(1000 + i%100)
		copy(data[8:12], (*(*[4]byte)(unsafe.Pointer(&pid)))[:])
		// ... fill rest with sample data
		sampleData[i] = data
	}

	var eventsProcessed uint64
	var eventsDropped uint64

	processor := NewRingBufferProcessor(
		nil, // Mock reader
		func(events []*MemoryEvent) error {
			eventsProcessed += uint64(len(events))
			return nil
		},
		&eventsDropped,
		&eventsProcessed,
	)

	b.ResetTimer()

	b.Run("ParseMemoryEvent", func(b *testing.B) {
		event := &MemoryEvent{}
		for i := 0; i < b.N; i++ {
			data := sampleData[i%len(sampleData)]
			processor.parseMemoryEvent(data, event)
		}
	})

	b.Run("BatchProcessing", func(b *testing.B) {
		events := make([]*MemoryEvent, 100)
		for i := 0; i < 100; i++ {
			events[i] = &MemoryEvent{
				PID:       uint32(1000 + i),
				EventType: 1,
			}
		}

		for i := 0; i < b.N; i++ {
			for _, event := range events {
				processor.addToBatch(event)
			}
			processor.flushBatch()
		}
	})
}

// BenchmarkKernelCompatibility benchmarks kernel compatibility checking
func BenchmarkKernelCompatibility(b *testing.B) {
	collector := &MemoryCollector{}

	b.Run("CheckKernelCompatibility", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = collector.checkKernelCompatibility()
		}
	})

	b.Run("ParseKernelVersion", func(b *testing.B) {
		compat := &KernelCompatibility{}
		version := "5.4.0-74-generic"
		
		for i := 0; i < b.N; i++ {
			_ = collector.parseKernelVersion(version, compat)
		}
	})
}

// BenchmarkUnifiedEventConversion benchmarks conversion to unified protobuf format
func BenchmarkUnifiedEventConversion(b *testing.B) {
	config := collectors.CollectorConfig{
		Name:    "test-collector",
		Enabled: true,
	}

	collector := &MemoryCollector{
		config: config,
	}

	event := &MemoryEvent{
		Timestamp:    uint64(time.Now().UnixNano()),
		PID:          1001,
		TID:          1001,
		Size:         4096,
		TotalMemory:  1024 * 1024,
		EventType:    1,
		Command:      "test-process",
		InContainer:  false,
		ContainerPID: 0,
	}

	tracker := &ProcessMemoryTracker{
		PID:         1001,
		Command:     "test-process",
		RiskScore:   0.3,
		GrowthTrend: TrendStable,
	}

	b.ResetTimer()

	b.Run("ConvertToUnifiedEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = collector.convertToUnifiedEvent(event, tracker)
		}
	})

	b.Run("GetEventSeverity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = collector.getEventSeverity(event, tracker)
		}
	})

	b.Run("GetEventConfidence", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = collector.getEventConfidence(event, tracker)
		}
	})
}

// BenchmarkMemoryOptimization benchmarks memory optimization routines
func BenchmarkMemoryOptimization(b *testing.B) {
	collector := &MemoryCollector{
		processes: make(map[uint32]*ProcessMemoryTracker),
	}

	// Create many process trackers with history
	for i := 0; i < 1000; i++ {
		pid := uint32(1000 + i)
		tracker := &ProcessMemoryTracker{
			PID:           pid,
			Command:       "test-process",
			GrowthHistory: make([]MemoryDataPoint, 100),
			LastUpdate:    time.Now(),
		}
		
		// Fill with sample data
		for j := 0; j < 100; j++ {
			tracker.GrowthHistory[j] = MemoryDataPoint{
				Timestamp: time.Now().Add(-time.Duration(100-j) * time.Second),
				Usage:     uint64(1024*1024 + j*1024),
			}
		}
		
		collector.processes[pid] = tracker
	}

	b.ResetTimer()

	b.Run("OptimizeMemoryTracking", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collector.optimizeMemoryTracking()
		}
	})

	b.Run("CleanupTerminatedProcesses", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collector.cleanupTerminatedProcesses()
		}
	})

	b.Run("GetProcessLifecycleStats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = collector.getProcessLifecycleStats()
		}
	})
}

// Performance target tests
func TestPerformanceTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance target tests in short mode")
	}

	config := collectors.CollectorConfig{
		Name:            "perf-test-collector",
		Enabled:         true,
		EventBufferSize: 10000,
	}

	collector, err := NewMemoryCollector(config)
	if err != nil {
		t.Skipf("Skipping performance test: %v", err)
	}

	memCollector := collector.(*MemoryCollector)

	// Test: Process 50,000 events per second per CPU
	t.Run("EventProcessingThroughput", func(t *testing.T) {
		events := make([]*MemoryEvent, 50000)
		for i := 0; i < 50000; i++ {
			events[i] = &MemoryEvent{
				PID:       uint32(1000 + i%1000),
				EventType: uint32(1 + i%3),
				Size:      4096,
			}
		}

		start := time.Now()
		err := memCollector.processBatchedEvents(events)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Event processing failed: %v", err)
		}

		eventsPerSecond := float64(len(events)) / duration.Seconds()
		t.Logf("Processed %d events in %v (%.0f events/sec)", len(events), duration, eventsPerSecond)

		if eventsPerSecond < 50000 {
			t.Errorf("Performance target not met: %.0f events/sec < 50,000 events/sec", eventsPerSecond)
		}
	})

	// Test: OOM prediction latency < 1ms
	t.Run("OOMPredictionLatency", func(t *testing.T) {
		predictor := NewOOMPredictor()
		tracker := &ProcessMemoryTracker{
			PID:           1001,
			GrowthHistory: make([]MemoryDataPoint, 10),
			GrowthTrend:   TrendLinear,
		}

		// Fill with sample data
		for i := 0; i < 10; i++ {
			tracker.GrowthHistory[i] = MemoryDataPoint{
				Timestamp: time.Now().Add(-time.Duration(10-i) * time.Second),
				Usage:     uint64(1024*1024 + i*1024*100),
			}
		}

		start := time.Now()
		prediction := predictor.PredictOOM(tracker)
		duration := time.Since(start)

		t.Logf("OOM prediction took %v", duration)

		if duration > time.Millisecond {
			t.Errorf("OOM prediction latency target not met: %v > 1ms", duration)
		}

		if prediction == nil {
			t.Log("No OOM prediction generated (expected for stable growth)")
		}
	})

	// Test: Memory usage < 100MB for tracking 10,000 processes
	t.Run("MemoryUsage", func(t *testing.T) {
		// This test would need runtime memory profiling
		// For now, we'll test that we can track 10,000 processes
		for i := 0; i < 10000; i++ {
			event := &MemoryEvent{
				PID:       uint32(1000 + i),
				EventType: 1,
				Size:      4096,
			}
			memCollector.updateProcessTracker(event)
		}

		stats := memCollector.getProcessLifecycleStats()
		totalProcesses := stats["total_processes"].(int)

		if totalProcesses < 10000 {
			t.Errorf("Failed to track enough processes: %d < 10,000", totalProcesses)
		}

		t.Logf("Successfully tracking %d processes", totalProcesses)
	})
}