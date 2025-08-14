package collectors

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestRegistryRegister(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry, err := NewRegistry(logger)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	ctx := context.Background()

	// Test successful registration
	collector1 := newMockCollector("collector1", 100)
	if err := registry.Register(ctx, "collector1", collector1); err != nil {
		t.Fatalf("failed to register collector1: %v", err)
	}

	// Test duplicate registration
	collector2 := newMockCollector("collector2", 100)
	if err := registry.Register(ctx, "collector1", collector2); err == nil {
		t.Error("expected error for duplicate registration")
	}

	// Test registration after start
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}
	defer registry.Stop()

	collector3 := newMockCollector("collector3", 100)
	if err := registry.Register(ctx, "collector3", collector3); err == nil {
		t.Error("expected error for registration after start")
	}
}

func TestRegistryUnregister(t *testing.T) {
	registry := NewRegistry()

	// Register a collector
	collector := newMockCollector("test", 100)
	if err := registry.Register("test", collector); err != nil {
		t.Fatalf("failed to register collector: %v", err)
	}

	// Test successful unregistration
	if err := registry.Unregister("test"); err != nil {
		t.Fatalf("failed to unregister collector: %v", err)
	}

	// Test unregistering non-existent collector
	if err := registry.Unregister("test"); err == nil {
		t.Error("expected error for unregistering non-existent collector")
	}

	// Test unregistration after start
	collector2 := newMockCollector("test2", 100)
	registry.Register("test2", collector2)

	ctx := context.Background()
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}
	defer registry.Stop()

	if err := registry.Unregister("test2"); err == nil {
		t.Error("expected error for unregistration after start")
	}
}

func TestRegistryStartStop(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	// Test starting with no collectors
	if err := registry.Start(ctx); err == nil {
		t.Error("expected error when starting with no collectors")
	}

	// Register collectors
	for i := 0; i < 3; i++ {
		collector := newMockCollector(string(rune('a'+i)), 100)
		if err := registry.Register(string(rune('a'+i)), collector); err != nil {
			t.Fatalf("failed to register collector %d: %v", i, err)
		}
	}

	// Test successful start
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}

	// Test duplicate start
	if err := registry.Start(ctx); err == nil {
		t.Error("expected error for duplicate start")
	}

	// Test stop
	if err := registry.Stop(); err != nil {
		t.Fatalf("failed to stop registry: %v", err)
	}

	// Test idempotent stop
	if err := registry.Stop(); err != nil {
		t.Fatalf("stop should be idempotent: %v", err)
	}
}

func TestRegistryEventAggregation(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	// Register multiple collectors
	collectors := make([]*mockCollector, 3)
	for i := 0; i < 3; i++ {
		collectors[i] = newMockCollector(string(rune('a'+i)), 100)
		if err := registry.Register(string(rune('a'+i)), collectors[i]); err != nil {
			t.Fatalf("failed to register collector %d: %v", i, err)
		}
	}

	// Start registry
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}

	// Collect events
	eventsByCollector := make(map[string]int)
	timeout := time.After(200 * time.Millisecond)
	done := false

	for !done {
		select {
		case event := <-registry.Events():
			collectorName := event.Metadata["collector"]
			eventsByCollector[collectorName]++

			// Check we have events from all collectors
			if len(eventsByCollector) == 3 {
				allHaveEvents := true
				for _, count := range eventsByCollector {
					if count < 2 {
						allHaveEvents = false
						break
					}
				}
				if allHaveEvents {
					done = true
				}
			}
		case <-timeout:
			done = true
		}
	}

	// Verify all collectors contributed events
	if len(eventsByCollector) != 3 {
		t.Errorf("expected events from 3 collectors, got %d", len(eventsByCollector))
	}

	for name, count := range eventsByCollector {
		if count < 2 {
			t.Errorf("collector %s only generated %d events", name, count)
		}
	}

	// Stop registry
	if err := registry.Stop(); err != nil {
		t.Fatalf("failed to stop registry: %v", err)
	}

	// Verify events channel is closed
	select {
	case _, ok := <-registry.Events():
		if ok {
			t.Error("expected events channel to be closed")
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("events channel not closed after stop")
	}
}

func TestRegistryList(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	if names := registry.List(); len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}

	// Register collectors
	expected := []string{"alpha", "beta", "gamma"}
	for _, name := range expected {
		collector := newMockCollector(name, 100)
		if err := registry.Register(name, collector); err != nil {
			t.Fatalf("failed to register %s: %v", name, err)
		}
	}

	// Check list
	names := registry.List()
	if len(names) != len(expected) {
		t.Errorf("expected %d collectors, got %d", len(expected), len(names))
	}

	// Verify all expected names are present
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	for _, exp := range expected {
		if !nameSet[exp] {
			t.Errorf("expected collector %s not found in list", exp)
		}
	}
}

func TestRegistryGet(t *testing.T) {
	registry := NewRegistry()

	// Test getting non-existent collector
	if _, exists := registry.Get("test"); exists {
		t.Error("expected collector not to exist")
	}

	// Register collector
	collector := newMockCollector("test", 100)
	if err := registry.Register("test", collector); err != nil {
		t.Fatalf("failed to register collector: %v", err)
	}

	// Test getting existing collector
	retrieved, exists := registry.Get("test")
	if !exists {
		t.Error("expected collector to exist")
	}

	if retrieved.Name() != collector.Name() {
		t.Errorf("expected collector name %s, got %s", collector.Name(), retrieved.Name())
	}
}

func TestRegistryHealth(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	// Register healthy collectors
	for i := 0; i < 3; i++ {
		collector := newMockCollector(string(rune('a'+i)), 100)
		if err := registry.Register(string(rune('a'+i)), collector); err != nil {
			t.Fatalf("failed to register collector %d: %v", i, err)
		}
	}

	// Check health before start
	health := registry.Health()
	if len(health) != 3 {
		t.Errorf("expected 3 health entries, got %d", len(health))
	}

	for name, healthy := range health {
		if !healthy {
			t.Errorf("expected collector %s to be healthy", name)
		}
	}

	if !registry.IsHealthy() {
		t.Error("expected registry to be healthy")
	}

	// Start registry
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}

	// Make one collector unhealthy
	if collector, exists := registry.Get("a"); exists {
		mock := collector.(*mockCollector)
		mock.mu.Lock()
		mock.healthy = false
		mock.mu.Unlock()
	}

	// Check health after making one unhealthy
	health = registry.Health()
	if health["a"] {
		t.Error("expected collector 'a' to be unhealthy")
	}

	if registry.IsHealthy() {
		t.Error("expected registry to be unhealthy when one collector is unhealthy")
	}

	// Stop registry
	registry.Stop()
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	// Register collectors
	for i := 0; i < 5; i++ {
		collector := newMockCollector(string(rune('a'+i)), 100)
		if err := registry.Register(string(rune('a'+i)), collector); err != nil {
			t.Fatalf("failed to register collector %d: %v", i, err)
		}
	}

	// Start registry
	if err := registry.Start(ctx); err != nil {
		t.Fatalf("failed to start registry: %v", err)
	}
	defer registry.Stop()

	// Concurrent operations
	var wg sync.WaitGroup

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				registry.List()
				registry.Health()
				registry.IsHealthy()
				registry.Get(string(rune('a' + j%5)))
			}
		}()
	}

	// Event reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(100 * time.Millisecond)
		for {
			select {
			case <-registry.Events():
			case <-timeout:
				return
			}
		}
	}()

	wg.Wait()
}

func TestRegistryCollectorFailure(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	// Register a normal collector
	goodCollector := newMockCollector("good", 100)
	if err := registry.Register("good", goodCollector); err != nil {
		t.Fatalf("failed to register good collector: %v", err)
	}

	// Register a collector that fails to start
	badCollector := &registryFailingCollector{name: "bad"}
	if err := registry.Register("bad", badCollector); err != nil {
		t.Fatalf("failed to register bad collector: %v", err)
	}

	// Start should fail
	if err := registry.Start(ctx); err == nil {
		t.Error("expected start to fail with bad collector")
	}

	// The registry should not be started
	if registry.started {
		t.Error("registry should not be marked as started after failure")
	}

	// Verify no collectors are running
	health := registry.Health()
	if len(health) != 2 {
		t.Errorf("expected 2 collectors in health check, got %d", len(health))
	}
}

// registryFailingCollector always fails to start (renamed to avoid conflict)
type registryFailingCollector struct {
	name string
}

func (f *registryFailingCollector) Name() string                    { return f.name }
func (f *registryFailingCollector) Start(ctx context.Context) error { return fmt.Errorf("start failed") }
func (f *registryFailingCollector) Stop() error                     { return nil }
func (f *registryFailingCollector) Events() <-chan RawEvent         { return nil }
func (f *registryFailingCollector) IsHealthy() bool                 { return false }
