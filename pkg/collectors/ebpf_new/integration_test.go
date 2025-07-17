//go:build integration && linux
// +build integration,linux

package ebpf_new_test

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	ebpf "github.com/yairfalse/tapio/pkg/collectors/ebpf_new"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestIntegrationBasicCollection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF integration tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	// Use minimal configuration for testing
	config := core.MinimalConfig()
	
	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Subscribe to events
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(10 * time.Second),
		},
	}

	options := domain.SubscriptionOptions{
		BufferSize: 100,
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Wait for some events
	eventCount := 0
	timeout := time.After(5 * time.Second)

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				t.Log("Event channel closed")
				goto done
			}
			eventCount++
			t.Logf("Received event: ID=%s, Type=%s, Severity=%s", 
				event.ID, event.Type, event.Severity)
			
			if eventCount >= 5 {
				goto done
			}

		case <-timeout:
			t.Log("Timeout reached")
			goto done
		case <-ctx.Done():
			t.Log("Context cancelled")
			goto done
		}
	}

done:
	if eventCount == 0 {
		t.Error("No events collected during test")
	}

	// Check health
	health := collector.Health()
	if health.Status != core.HealthStatusHealthy {
		t.Errorf("Collector health status = %s, want %s", health.Status, core.HealthStatusHealthy)
		for _, issue := range health.Issues {
			t.Logf("Health issue: %s - %s", issue.Component, issue.Issue)
		}
	}

	// Check stats
	stats, err := collector.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	t.Logf("Collection stats: Collected=%d, Dropped=%d, Filtered=%d, Errors=%d",
		stats.EventsCollected, stats.EventsDropped, stats.EventsFiltered, stats.CollectionErrors)

	if stats.EventsCollected == 0 && eventCount > 0 {
		t.Error("Stats show no events collected but we received events")
	}
}

func TestIntegrationMultiplePrograms(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF integration tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	// Create config with multiple programs
	config := core.ProcessMonitorConfig()
	
	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Load programs
	ctx := context.Background()
	err = collector.LoadPrograms(ctx)
	if err != nil {
		t.Fatalf("Failed to load programs: %v", err)
	}
	defer collector.UnloadPrograms()

	// Check loaded programs
	programs, err := collector.GetLoadedPrograms()
	if err != nil {
		t.Fatalf("Failed to get loaded programs: %v", err)
	}

	if len(programs) != len(config.Programs) {
		t.Errorf("Loaded %d programs, expected %d", len(programs), len(config.Programs))
	}

	for _, prog := range programs {
		t.Logf("Loaded program: %s (type: %s, target: %s)", 
			prog.Name, prog.Type, prog.AttachTarget)
	}
}

func TestIntegrationFilter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF integration tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	config := core.SyscallMonitorConfig()
	
	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Set filter to only capture events from current process
	filter := core.Filter{
		ProcessIDs: []uint32{uint32(os.Getpid())},
		EventTypes: []core.EventType{core.EventTypeSyscall},
	}

	err = collector.SetFilter(filter)
	if err != nil {
		t.Fatalf("Failed to set filter: %v", err)
	}

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Subscribe to events
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(5 * time.Second),
		},
	}

	options := domain.SubscriptionOptions{
		BufferSize: 10,
		Filters: map[string]interface{}{
			"process_name": os.Args[0],
		},
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Generate some syscalls
	for i := 0; i < 5; i++ {
		_ = os.Getpid()
		time.Sleep(10 * time.Millisecond)
	}

	// Collect events
	var collectedEvents []domain.Event
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				goto checkEvents
			}
			collectedEvents = append(collectedEvents, event)
			if len(collectedEvents) >= 3 {
				goto checkEvents
			}

		case <-timeout:
			goto checkEvents
		}
	}

checkEvents:
	// Verify events are from our process
	for _, event := range collectedEvents {
		if event.Context.PID == nil || *event.Context.PID != int32(os.Getpid()) {
			pid := int32(-1)
			if event.Context.PID != nil {
				pid = *event.Context.PID
			}
			t.Errorf("Received event from wrong PID: %d, expected %d", 
				pid, os.Getpid())
		}
	}

	t.Logf("Collected %d filtered events", len(collectedEvents))
}

func TestIntegrationStressTest(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF integration tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// Use memory monitor config for high-frequency events
	config := core.MemoryMonitorConfig()
	config.MaxEventsPerSecond = 50000 // High rate limit
	
	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Subscribe to events
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(20 * time.Second),
		},
	}

	options := domain.SubscriptionOptions{
		BufferSize: 10000,
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Generate load
	done := make(chan bool)
	go func() {
		buf := make([]byte, 1024)
		for i := 0; i < 10000; i++ {
			select {
			case <-ctx.Done():
				done <- true
				return
			default:
				// Allocate and free memory to generate events
				_ = append(buf, make([]byte, 1024)...)
				if i%100 == 0 {
					buf = buf[:1024] // Reset size
				}
			}
		}
		done <- true
	}()

	// Collect events
	eventCount := 0
	startTime := time.Now()
	
	for {
		select {
		case _, ok := <-eventChan:
			if !ok {
				goto report
			}
			eventCount++

		case <-done:
			time.Sleep(100 * time.Millisecond) // Let remaining events arrive
			goto report

		case <-ctx.Done():
			goto report
		}
	}

report:
	duration := time.Since(startTime)
	rate := float64(eventCount) / duration.Seconds()

	t.Logf("Stress test results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Events collected: %d", eventCount)
	t.Logf("  Event rate: %.2f events/sec", rate)

	// Check stats
	stats, err := collector.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	t.Logf("  Events dropped: %d", stats.EventsDropped)
	t.Logf("  Events filtered: %d", stats.EventsFiltered)
	t.Logf("  Collection errors: %d", stats.CollectionErrors)
	t.Logf("  Bytes processed: %d", stats.BytesProcessed)

	// Check health
	health := collector.Health()
	t.Logf("  Health status: %s", health.Status)

	if stats.EventsDropped > uint64(float64(eventCount)*0.1) {
		t.Errorf("Too many events dropped: %d (>10%% of %d collected)", 
			stats.EventsDropped, eventCount)
	}
}

func TestIntegrationGracefulShutdown(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF integration tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	config := core.NetworkMonitorConfig()
	
	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Create multiple subscriptions
	var subscriptions []<-chan domain.Event
	for i := 0; i < 5; i++ {
		criteria := domain.QueryCriteria{
			TimeWindow: domain.TimeWindow{
				Start: time.Now(),
				End:   time.Now().Add(time.Hour),
			},
		}

		eventChan, err := collector.Subscribe(ctx, criteria, domain.SubscriptionOptions{
			BufferSize: 100,
		})
		if err != nil {
			t.Fatalf("Failed to create subscription %d: %v", i, err)
		}
		subscriptions = append(subscriptions, eventChan)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}

	// Verify all subscriptions are closed
	for i, sub := range subscriptions {
		select {
		case _, ok := <-sub:
			if ok {
				t.Errorf("Subscription %d still open after Stop()", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Subscription %d not closed after Stop()", i)
		}
	}

	// Verify we can't start new subscriptions
	_, err = collector.Subscribe(ctx, domain.QueryCriteria{}, domain.SubscriptionOptions{})
	if err == nil {
		t.Error("Subscribe() should fail after Stop()")
	}
}