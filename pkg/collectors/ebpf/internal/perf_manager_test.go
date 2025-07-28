//go:build linux
// +build linux

package internal

import (
	"context"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/perf"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

func TestPerfEventManager_RegisterPerfMap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	// Create a test perf event array map
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	perfMap, err := ebpf.NewMap(spec)
	if err != nil {
		t.Skipf("Failed to create perf map (requires root): %v", err)
	}
	defer perfMap.Close()

	// Test registering a perf map
	err = pm.RegisterPerfMap("test_perf", perfMap)
	if err != nil {
		t.Errorf("RegisterPerfMap() error = %v", err)
	}

	// Test registering duplicate
	err = pm.RegisterPerfMap("test_perf", perfMap)
	if err == nil {
		t.Error("RegisterPerfMap() should fail for duplicate registration")
	}
}

func TestPerfEventManager_ParsePerfEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	tests := []struct {
		name     string
		mapName  string
		record   perf.Record
		wantType string
		wantNil  bool
	}{
		{
			name:    "lost samples",
			mapName: "test_map",
			record: perf.Record{
				LostSamples: 100,
			},
			wantType: "perf_lost",
			wantNil:  false,
		},
		{
			name:    "network event",
			mapName: "network_events",
			record: perf.Record{
				RawSample: make([]byte, 32), // Minimum size for parsing
			},
			wantType: "network",
			wantNil:  false,
		},
		{
			name:    "syscall event",
			mapName: "syscall_events",
			record: perf.Record{
				RawSample: make([]byte, 32),
			},
			wantType: "syscall",
			wantNil:  false,
		},
		{
			name:    "file event",
			mapName: "file_events",
			record: perf.Record{
				RawSample: make([]byte, 32),
			},
			wantType: "file",
			wantNil:  false,
		},
		{
			name:    "generic perf event",
			mapName: "unknown_events",
			record: perf.Record{
				RawSample: make([]byte, 32),
			},
			wantType: "generic_perf",
			wantNil:  false,
		},
		{
			name:    "too small sample",
			mapName: "test_map",
			record: perf.Record{
				RawSample: make([]byte, 8), // Too small
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := pm.parsePerfEvent(tt.mapName, tt.record)

			if tt.wantNil {
				if event != nil {
					t.Error("parsePerfEvent() should return nil")
				}
				return
			}

			if event == nil {
				t.Fatal("parsePerfEvent() returned nil, want event")
			}

			if event.Type != tt.wantType {
				t.Errorf("parsePerfEvent() Type = %v, want %v", event.Type, tt.wantType)
			}

			if tt.record.LostSamples > 0 && event.Comm != tt.mapName {
				t.Errorf("parsePerfEvent() Comm = %v, want %v", event.Comm, tt.mapName)
			}
		})
	}
}

func TestPerfEventManager_GetPerfStats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	// Initial stats
	stats := pm.GetPerfStats()
	if totalMaps, ok := stats["total_perf_maps"].(int); !ok || totalMaps != 0 {
		t.Errorf("Initial total_perf_maps = %v, want 0", stats["total_perf_maps"])
	}
	if activeReaders, ok := stats["active_readers"].(int); !ok || activeReaders != 0 {
		t.Errorf("Initial active_readers = %v, want 0", stats["active_readers"])
	}

	// Create and register a perf map
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	perfMap, err := ebpf.NewMap(spec)
	if err != nil {
		t.Skipf("Failed to create perf map (requires root): %v", err)
	}
	defer perfMap.Close()

	pm.RegisterPerfMap("test_perf", perfMap)

	// Stats after registration
	stats = pm.GetPerfStats()
	if totalMaps, ok := stats["total_perf_maps"].(int); !ok || totalMaps != 1 {
		t.Errorf("After registration total_perf_maps = %v, want 1", stats["total_perf_maps"])
	}

	// Verify readers stats structure
	if readers, ok := stats["readers"].(map[string]interface{}); !ok {
		t.Error("GetPerfStats() missing readers field")
	} else if len(readers) != 0 {
		t.Errorf("GetPerfStats() readers count = %d, want 0 (not started)", len(readers))
	}
}

func TestPerfEventManager_EventFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	// Mock a perf event being parsed
	testRecord := perf.Record{
		RawSample: make([]byte, 32),
	}

	// Parse event directly
	event := pm.parsePerfEvent("test_events", testRecord)
	if event == nil {
		t.Fatal("parsePerfEvent() returned nil")
	}

	// Simulate event flow
	select {
	case eventChan <- *event:
		// Event sent successfully
	case <-time.After(100 * time.Millisecond):
		t.Error("Failed to send event to channel")
	}

	// Verify event was received
	select {
	case receivedEvent := <-eventChan:
		if receivedEvent.Type != "generic_perf" {
			t.Errorf("Received event Type = %v, want generic_perf", receivedEvent.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Failed to receive event from channel")
	}
}

func TestPerfEventManager_Stop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	// Create and register perf maps
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	perfMap1, err := ebpf.NewMap(spec)
	if err != nil {
		t.Skipf("Failed to create perf map (requires root): %v", err)
	}
	defer perfMap1.Close()

	perfMap2, err := ebpf.NewMap(spec)
	if err != nil {
		t.Skipf("Failed to create second perf map (requires root): %v", err)
	}
	defer perfMap2.Close()

	pm.RegisterPerfMap("perf1", perfMap1)
	pm.RegisterPerfMap("perf2", perfMap2)

	// Start the manager
	err = pm.Start()
	if err != nil {
		t.Skipf("Failed to start perf manager (requires root): %v", err)
	}

	// Let it run briefly
	time.Sleep(10 * time.Millisecond)

	// Stop the manager
	err = pm.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify readers are closed
	stats := pm.GetPerfStats()
	if activeReaders, ok := stats["active_readers"].(int); !ok || activeReaders != 0 {
		t.Errorf("After Stop() active_readers = %v, want 0", stats["active_readers"])
	}
}

func TestPerfEventManager_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 1000)
	pm := NewPerfEventManager(ctx, eventChan)

	// Create multiple perf maps
	numMaps := 5
	perfMaps := make([]*ebpf.Map, numMaps)

	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	for i := 0; i < numMaps; i++ {
		perfMap, err := ebpf.NewMap(spec)
		if err != nil {
			t.Skipf("Failed to create perf map (requires root): %v", err)
		}
		defer perfMap.Close()

		perfMaps[i] = perfMap
		pm.RegisterPerfMap(t.Name()+string(rune(i)), perfMap)
	}

	// Run concurrent operations
	done := make(chan bool)

	// Stats reader goroutines
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				pm.GetPerfStats()
				time.Sleep(time.Microsecond)
			}
			done <- true
		}()
	}

	// Event parser goroutines
	for i := 0; i < 3; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				record := perf.Record{
					RawSample: make([]byte, 32),
				}
				pm.parsePerfEvent("test_map", record)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		<-done
	}
}

// Benchmark tests
func BenchmarkPerfEventManager_ParsePerfEvent(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 1000)
	pm := NewPerfEventManager(ctx, eventChan)

	// Prepare test record
	record := perf.Record{
		RawSample: make([]byte, 64),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.parsePerfEvent("bench_events", record)
	}
}

func BenchmarkPerfEventManager_GetPerfStats(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventChan := make(chan core.RawEvent, 100)
	pm := NewPerfEventManager(ctx, eventChan)

	// Register some maps
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	for i := 0; i < 10; i++ {
		perfMap, err := ebpf.NewMap(spec)
		if err != nil {
			b.Skipf("Failed to create perf map: %v", err)
		}
		defer perfMap.Close()
		pm.RegisterPerfMap(string(rune(i)), perfMap)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pm.GetPerfStats()
		}
	})
}
