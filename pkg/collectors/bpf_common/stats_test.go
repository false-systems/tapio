package bpf_common

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestBPFStatsCollector_Basic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Start collector
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	// Register a program
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	// Update statistics
	collector.UpdateStats("test_program", func(stats *BPFStatistics) {
		stats.EventsReceived = 100
		stats.EventsProcessed = 95
		stats.EventsDropped = 5
		stats.RingBufferSize = 1024
		stats.RingBufferUsed = 512
	})
	
	// Get statistics
	stats, exists := collector.GetStats("test_program")
	if !exists {
		t.Fatal("Program statistics not found")
	}
	
	if stats.EventsReceived != 100 {
		t.Errorf("Expected EventsReceived=100, got %d", stats.EventsReceived)
	}
	if stats.EventsProcessed != 95 {
		t.Errorf("Expected EventsProcessed=95, got %d", stats.EventsProcessed)
	}
	if stats.EventsDropped != 5 {
		t.Errorf("Expected EventsDropped=5, got %d", stats.EventsDropped)
	}
	if stats.RingBufferUtilization != 0.5 {
		t.Errorf("Expected RingBufferUtilization=0.5, got %f", stats.RingBufferUtilization)
	}
}

func TestBPFStatsCollector_IncrementCounters(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	// Increment counters
	collector.IncrementEventCounter("test_program", CounterEventsReceived, 10)
	collector.IncrementEventCounter("test_program", CounterEventsProcessed, 8)
	collector.IncrementEventCounter("test_program", CounterEventsDropped, 2)
	
	// Wait a bit for async updates
	time.Sleep(200 * time.Millisecond)
	
	stats, exists := collector.GetStats("test_program")
	if !exists {
		t.Fatal("Program statistics not found")
	}
	
	if stats.EventsReceived != 10 {
		t.Errorf("Expected EventsReceived=10, got %d", stats.EventsReceived)
	}
	if stats.EventsProcessed != 8 {
		t.Errorf("Expected EventsProcessed=8, got %d", stats.EventsProcessed)
	}
	if stats.EventsDropped != 2 {
		t.Errorf("Expected EventsDropped=2, got %d", stats.EventsDropped)
	}
}

func TestBPFStatsCollector_RecordProcessingTime(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	// Record processing times
	collector.RecordProcessingTime("test_program", 100*time.Microsecond)
	collector.RecordProcessingTime("test_program", 200*time.Microsecond)
	
	// Increment processed events so we can calculate averages
	collector.IncrementEventCounter("test_program", CounterEventsProcessed, 2)
	
	// Wait a bit for async updates
	time.Sleep(200 * time.Millisecond)
	
	stats, exists := collector.GetStats("test_program")
	if !exists {
		t.Fatal("Program statistics not found")
	}
	
	if stats.ProcessingTimeNs == 0 {
		t.Error("Expected non-zero ProcessingTimeNs")
	}
	
	if stats.AverageLatencyNs == 0 {
		t.Error("Expected non-zero AverageLatencyNs")
	}
}

func TestBPFStatsCollector_UpdateRingBufferStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	// Update ring buffer stats
	collector.UpdateRingBufferStats("test_program", 2048, 1024)
	
	// Wait a bit for async updates
	time.Sleep(200 * time.Millisecond)
	
	stats, exists := collector.GetStats("test_program")
	if !exists {
		t.Fatal("Program statistics not found")
	}
	
	if stats.RingBufferSize != 2048 {
		t.Errorf("Expected RingBufferSize=2048, got %d", stats.RingBufferSize)
	}
	if stats.RingBufferUsed != 1024 {
		t.Errorf("Expected RingBufferUsed=1024, got %d", stats.RingBufferUsed)
	}
	if stats.RingBufferUtilization != 0.5 {
		t.Errorf("Expected RingBufferUtilization=0.5, got %f", stats.RingBufferUtilization)
	}
}

func TestBPFStatsCollector_GetAllStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	// Register multiple programs
	collector.RegisterProgram("program1", "tcp_monitor", 12345)
	collector.RegisterProgram("program2", "dns_monitor", 23456)
	
	// Update stats for both programs
	collector.IncrementEventCounter("program1", CounterEventsReceived, 100)
	collector.IncrementEventCounter("program2", CounterEventsReceived, 200)
	
	// Wait a bit for async updates
	time.Sleep(200 * time.Millisecond)
	
	allStats := collector.GetAllStats()
	
	if len(allStats) != 2 {
		t.Errorf("Expected 2 programs, got %d", len(allStats))
	}
	
	if stats1, exists := allStats["program1"]; !exists {
		t.Error("program1 stats not found")
	} else if stats1.EventsReceived != 100 {
		t.Errorf("Expected program1 EventsReceived=100, got %d", stats1.EventsReceived)
	}
	
	if stats2, exists := allStats["program2"]; !exists {
		t.Error("program2 stats not found")
	} else if stats2.EventsReceived != 200 {
		t.Errorf("Expected program2 EventsReceived=200, got %d", stats2.EventsReceived)
	}
}

func TestBPFStatsCollector_UnregisterProgram(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	collector, err := NewBPFStatsCollector(logger, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	// Verify program exists
	_, exists := collector.GetStats("test_program")
	if !exists {
		t.Fatal("Program should exist after registration")
	}
	
	// Unregister program
	collector.UnregisterProgram("test_program")
	
	// Verify program no longer exists
	_, exists = collector.GetStats("test_program")
	if exists {
		t.Fatal("Program should not exist after unregistration")
	}
}

// Benchmark tests
func BenchmarkBPFStatsCollector_IncrementCounter(b *testing.B) {
	logger := zaptest.NewLogger(b)
	
	collector, err := NewBPFStatsCollector(logger, 1*time.Second)
	if err != nil {
		b.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.IncrementEventCounter("test_program", CounterEventsReceived, 1)
		}
	})
}

func BenchmarkBPFStatsCollector_RecordProcessingTime(b *testing.B) {
	logger := zaptest.NewLogger(b)
	
	collector, err := NewBPFStatsCollector(logger, 1*time.Second)
	if err != nil {
		b.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.RecordProcessingTime("test_program", 100*time.Microsecond)
		}
	})
}

func BenchmarkBPFStatsCollector_GetStats(b *testing.B) {
	logger := zaptest.NewLogger(b)
	
	collector, err := NewBPFStatsCollector(logger, 1*time.Second)
	if err != nil {
		b.Fatalf("Failed to create stats collector: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := collector.Start(ctx); err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()
	
	collector.RegisterProgram("test_program", "tcp_monitor", 12345)
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.GetStats("test_program")
		}
	})
}