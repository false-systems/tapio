package ebpf_test

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/internal"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/linux"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Helper functions
func int32Ptr(v int32) *int32 {
	return &v
}

// Mock implementations for benchmarking

type benchmarkProgramLoader struct{}

func (b *benchmarkProgramLoader) Load(ctx context.Context, spec core.ProgramSpec) (core.Program, error) {
	return core.Program{
		ID:           1,
		Name:         spec.Name,
		Type:         spec.Type,
		AttachTarget: spec.AttachTarget,
		LoadTime:     time.Now(),
	}, nil
}

func (b *benchmarkProgramLoader) Unload(program core.Program) error {
	return nil
}

func (b *benchmarkProgramLoader) List() ([]core.Program, error) {
	return []core.Program{}, nil
}

type benchmarkEventParser struct{}

func (b *benchmarkEventParser) Parse(data []byte, eventType core.EventType) (domain.Event, error) {
	return domain.Event{
		ID:        "bench-event",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Severity:  domain.SeverityInfo,
		Payload: domain.SystemEventPayload{
			Component: "bench",
			Operation: "test",
			Status:    "success",
			Message:   "Benchmark event",
		},
	}, nil
}

func (b *benchmarkEventParser) CanParse(eventType core.EventType) bool {
	return true
}

type benchmarkMapManager struct{}

func (b *benchmarkMapManager) CreateMap(spec core.MapSpec) (core.Map, error) {
	return &benchmarkMap{}, nil
}

func (b *benchmarkMapManager) GetMap(name string) (core.Map, error) {
	return &benchmarkMap{}, nil
}

func (b *benchmarkMapManager) DeleteMap(name string) error {
	return nil
}

func (b *benchmarkMapManager) ListMaps() ([]core.MapInfo, error) {
	return []core.MapInfo{}, nil
}

type benchmarkMap struct{}

func (b *benchmarkMap) Lookup(key []byte) ([]byte, error) {
	return make([]byte, 64), nil
}

func (b *benchmarkMap) Update(key, value []byte) error {
	return nil
}

func (b *benchmarkMap) Delete(key []byte) error {
	return nil
}

func (b *benchmarkMap) Iterate(fn func(key, value []byte) error) error {
	return nil
}

func (b *benchmarkMap) Close() error {
	return nil
}

// Benchmarks

func BenchmarkConfigValidation(b *testing.B) {
	configs := []struct {
		name   string
		config core.Config
	}{
		{"Default", core.DefaultConfig()},
		{"Syscall", core.SyscallMonitorConfig()},
		{"Network", core.NetworkMonitorConfig()},
		{"Process", core.ProcessMonitorConfig()},
		{"Memory", core.MemoryMonitorConfig()},
		{"FileIO", core.FileIOMonitorConfig()},
	}

	for _, tc := range configs {
		b.Run(tc.name, func(b *testing.B) {
			config := tc.config
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_ = config.Validate()
			}
		})
	}
}

func BenchmarkEventParsing(b *testing.B) {
	parser := linux.NewEventParser()
	
	// Create sample event data for different event types
	events := []struct {
		name      string
		eventType core.EventType
		data      []byte
	}{
		{
			name:      "Syscall",
			eventType: core.EventTypeSyscall,
			data:      make([]byte, 128), // Typical syscall event size
		},
		{
			name:      "Network",
			eventType: core.EventTypeNetworkIn,
			data:      make([]byte, 256), // Typical network event size
		},
		{
			name:      "Process",
			eventType: core.EventTypeProcessExec,
			data:      make([]byte, 512), // Typical process event size
		},
		{
			name:      "Memory",
			eventType: core.EventTypeMemoryAlloc,
			data:      make([]byte, 64), // Typical memory event size
		},
	}

	for _, event := range events {
		b.Run(event.name, func(b *testing.B) {
			b.SetBytes(int64(len(event.data)))
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_, _ = parser.Parse(event.data, event.eventType)
			}
		})
	}
}

func BenchmarkCollectorSubscribe(b *testing.B) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "bench",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test",
			Code:         []byte{1},
		},
	}

	collector, err := internal.NewCollector(
		config,
		&benchmarkProgramLoader{},
		&benchmarkEventParser{},
		&benchmarkMapManager{},
	)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now().Add(-time.Hour),
			End:   time.Now().Add(time.Hour),
		},
	}
	options := domain.SubscriptionOptions{
		BufferSize: 1000,
	}

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		ch, err := collector.Subscribe(ctx, criteria, options)
		if err != nil {
			b.Fatalf("Subscribe failed: %v", err)
		}
		// Close channel to clean up subscription
		go func() {
			for range ch {
			}
		}()
	}
}

func BenchmarkEventProcessing(b *testing.B) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "bench",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test",
			Code:         []byte{1},
		},
	}

	loader := &benchmarkProgramLoader{}
	parser := &benchmarkEventParser{}
	manager := &benchmarkMapManager{}

	// Create a mock collector that can process events
	type benchCollector struct {
		core.Collector
		eventChan chan domain.Event
	}

	b.Run("Sequential", func(b *testing.B) {
		events := make([]domain.Event, b.N)
		for i := 0; i < b.N; i++ {
			events[i] = domain.Event{
				ID:        "bench",
				Type:      domain.EventTypeSystem,
				Timestamp: time.Now(),
			}
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Simulate event processing
			event := events[i]
			_ = event.ID
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		eventChan := make(chan domain.Event, 1000)
		done := make(chan bool)

		// Consumer
		go func() {
			count := 0
			for range eventChan {
				count++
				if count >= b.N {
					done <- true
					return
				}
			}
		}()

		b.ResetTimer()

		// Producer
		for i := 0; i < b.N; i++ {
			eventChan <- domain.Event{
				ID:        "bench",
				Type:      domain.EventTypeSystem,
				Timestamp: time.Now(),
			}
		}

		<-done
		close(eventChan)
	})

	_ = loader
	_ = parser
	_ = manager
}

func BenchmarkFilterEvaluation(b *testing.B) {
	filters := []struct {
		name   string
		filter core.Filter
		event  domain.Event
	}{
		{
			name: "NoFilter",
			filter: core.Filter{},
			event: domain.Event{
				Type:     domain.EventTypeSystem,
				Severity: domain.SeverityInfo,
			},
		},
		{
			name: "EventTypeFilter",
			filter: core.Filter{
				EventTypes: []core.EventType{
					core.EventTypeSyscall,
					core.EventTypeNetworkIn,
					core.EventTypeProcessExec,
				},
			},
			event: domain.Event{
				Type:     domain.EventTypeSystem,
				Severity: domain.SeverityInfo,
			},
		},
		{
			name: "ProcessIDFilter",
			filter: core.Filter{
				ProcessIDs: []uint32{1234, 5678, 9012, 3456, 7890},
			},
			event: domain.Event{
				Type: domain.EventTypeSystem,
				Context: domain.EventContext{
					PID: int32Ptr(1234),
				},
			},
		},
		{
			name: "ComplexFilter",
			filter: core.Filter{
				EventTypes:   []core.EventType{core.EventTypeSyscall, core.EventTypeNetworkIn},
				ProcessIDs:   []uint32{1234, 5678},
				ContainerIDs: []string{"container1", "container2"},
				Namespaces:   []string{"default", "kube-system"},
				MinSeverity:  domain.SeverityWarn,
			},
			event: domain.Event{
				Type:     domain.EventTypeSystem,
				Severity: domain.SeverityError,
				Context: domain.EventContext{
					PID:       int32Ptr(1234),
					Container: "container1",
					Namespace: "default",
				},
			},
		},
	}

	for _, tc := range filters {
		b.Run(tc.name, func(b *testing.B) {
			// Simulate filter evaluation
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				// Check event type
				if len(tc.filter.EventTypes) > 0 {
					found := false
					for _, t := range tc.filter.EventTypes {
						if t == core.EventTypeSyscall {
							found = true
							break
						}
					}
					_ = found
				}

				// Check process ID
				if len(tc.filter.ProcessIDs) > 0 {
					found := false
					for _, pid := range tc.filter.ProcessIDs {
						if tc.event.Context.PID != nil && pid == uint32(*tc.event.Context.PID) {
							found = true
							break
						}
					}
					_ = found
				}

				// Check severity
				_ = tc.event.Severity >= tc.filter.MinSeverity
			}
		})
	}
}

func BenchmarkStatsCollection(b *testing.B) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "bench",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test",
			Code:         []byte{1},
		},
	}

	collector, err := internal.NewCollector(
		config,
		&benchmarkProgramLoader{},
		&benchmarkEventParser{},
		&benchmarkMapManager{},
	)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, _ = collector.GetStats()
	}
}

func BenchmarkHealthCheck(b *testing.B) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "bench",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test",
			Code:         []byte{1},
		},
	}

	collector, err := internal.NewCollector(
		config,
		&benchmarkProgramLoader{},
		&benchmarkEventParser{},
		&benchmarkMapManager{},
	)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = collector.Health()
	}
}