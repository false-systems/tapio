package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/events_correlation/integration"
)

// DemoCollector simulates a real collector with demo events
type DemoCollector struct {
	name      string
	events    chan collector.Event
	ctx       context.Context
	cancel    context.CancelFunc
	isClosed  bool
}

func NewDemoCollector(name string) *DemoCollector {
	return &DemoCollector{
		name:   name,
		events: make(chan collector.Event, 100),
	}
}

func (d *DemoCollector) Name() string {
	return d.name
}

func (d *DemoCollector) Start(ctx context.Context, config collector.Config) error {
	d.ctx, d.cancel = context.WithCancel(ctx)
	
	go d.generateDemoEvents()
	
	return nil
}

func (d *DemoCollector) generateDemoEvents() {
	defer func() {
		if !d.isClosed {
			close(d.events)
			d.isClosed = true
		}
	}()
	
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	eventCounter := 0
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			eventCounter++
			
			// Generate different types of events to trigger correlations
			switch eventCounter % 4 {
			case 0:
				d.sendMemoryPressureEvent()
			case 1:
				d.sendOOMEvent()
			case 2:
				d.sendRestartEvent()
			case 3:
				d.sendCPUThrottleEvent()
			}
		}
	}
}

func (d *DemoCollector) sendMemoryPressureEvent() {
	event := collector.Event{
		ID:        fmt.Sprintf("mem-pressure-%d", time.Now().Unix()),
		Timestamp: time.Now(),
		Type:      "memory_pressure",
		Source:    d.name,
		Severity:  collector.SeverityMedium,
		Context: &collector.EventContext{
			Namespace: "demo-namespace",
			Pod:       "demo-pod-abc123",
			Container: "app",
			Node:      "worker-1",
			PID:       1234,
		},
		Data: map[string]interface{}{
			"current_usage":   850 * 1024 * 1024, // 850MB
			"limit":          1024 * 1024 * 1024, // 1GB
			"usage_percent":  83.0,
			"allocation_rate": 25 * 1024 * 1024, // 25MB/s
		},
	}
	
	select {
	case d.events <- event:
		fmt.Printf("📊 Generated memory pressure event for %s\n", event.Context.Pod)
	default:
	}
}

func (d *DemoCollector) sendOOMEvent() {
	event := collector.Event{
		ID:        fmt.Sprintf("oom-%d", time.Now().Unix()),
		Timestamp: time.Now(),
		Type:      "oom_kill",
		Source:    d.name,
		Severity:  collector.SeverityHigh,
		Context: &collector.EventContext{
			Namespace: "demo-namespace",
			Pod:       "demo-pod-abc123",
			Container: "app",
			Node:      "worker-1",
			PID:       1234,
		},
		Data: map[string]interface{}{
			"killed_process": "java",
			"memory_limit":   1024 * 1024 * 1024,
			"memory_usage":   1024 * 1024 * 1024,
		},
	}
	
	select {
	case d.events <- event:
		fmt.Printf("💀 Generated OOM kill event for %s\n", event.Context.Pod)
	default:
	}
}

func (d *DemoCollector) sendRestartEvent() {
	event := collector.Event{
		ID:        fmt.Sprintf("restart-%d", time.Now().Unix()),
		Timestamp: time.Now(),
		Type:      "container_restart",
		Source:    d.name,
		Severity:  collector.SeverityMedium,
		Context: &collector.EventContext{
			Namespace: "demo-namespace",
			Pod:       "demo-pod-abc123",
			Container: "app",
			Node:      "worker-1",
		},
		Data: map[string]interface{}{
			"restart_count": 3,
			"exit_code":     137, // OOM killed
			"reason":        "OOMKilled",
		},
	}
	
	select {
	case d.events <- event:
		fmt.Printf("🔄 Generated container restart event for %s\n", event.Context.Pod)
	default:
	}
}

func (d *DemoCollector) sendCPUThrottleEvent() {
	event := collector.Event{
		ID:        fmt.Sprintf("cpu-throttle-%d", time.Now().Unix()),
		Timestamp: time.Now(),
		Type:      "cpu_throttle",
		Source:    d.name,
		Severity:  collector.SeverityMedium,
		Context: &collector.EventContext{
			Namespace: "demo-namespace",
			Pod:       "demo-pod-xyz789",
			Container: "worker",
			Node:      "worker-2",
			PID:       5678,
		},
		Data: map[string]interface{}{
			"throttle_count":    150,
			"throttle_duration": 2.5, // seconds
			"cpu_usage":         0.95,
			"cpu_limit":         1.0,
		},
	}
	
	select {
	case d.events <- event:
		fmt.Printf("🚫 Generated CPU throttle event for %s\n", event.Context.Pod)
	default:
	}
}

func (d *DemoCollector) Events() <-chan collector.Event {
	return d.events
}

func (d *DemoCollector) Health() collector.Health {
	return collector.Health{
		Status:  collector.HealthStatusHealthy,
		Message: "Demo collector running",
	}
}

func (d *DemoCollector) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

func main() {
	fmt.Println("🚀 Starting Tapio Correlation Engine Demo")
	fmt.Println("==========================================")
	
	// Create integration manager with demo configuration
	config := integration.DefaultIntegrationConfig()
	config.CorrelationWindow = 30 * time.Second // Shorter window for demo
	config.EventBufferSize = 1000
	config.ResultBufferSize = 100
	
	manager := integration.NewIntegratedManager(config)
	
	// Create demo collectors
	ebpfCollector := NewDemoCollector("ebpf-demo")
	k8sCollector := NewDemoCollector("k8s-demo")
	
	// Register collectors
	if err := manager.RegisterCollectors(ebpfCollector, k8sCollector); err != nil {
		log.Fatalf("Failed to register collectors: %v", err)
	}
	
	// Start the system
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()
	
	fmt.Println("✅ Correlation engine started successfully!")
	fmt.Println("\n📡 Processing events and correlations...")
	fmt.Println("   Look for correlation results below:")
	fmt.Println("   ===================================")
	
	// Process correlation results
	go func() {
		results := manager.Results()
		
		for result := range results {
			fmt.Printf("\n🔍 CORRELATION DETECTED!\n")
			fmt.Printf("   Rule: %s\n", result.RuleName)
			fmt.Printf("   Severity: %s | Confidence: %.1f%%\n", 
				result.Severity, result.Confidence*100)
			fmt.Printf("   Category: %s\n", result.Category)
			fmt.Printf("   Description: %s\n", result.Description)
			
			if len(result.Evidence.Entities) > 0 {
				fmt.Printf("   Affected Entities:\n")
				for _, entity := range result.Evidence.Entities {
					fmt.Printf("     - %s: %s\n", entity.Type, entity.String())
				}
			}
			
			if len(result.Recommendations) > 0 {
				fmt.Printf("   Recommendations:\n")
				for i, rec := range result.Recommendations {
					if i < 2 { // Show first 2 recommendations
						fmt.Printf("     • %s\n", rec)
					}
				}
			}
			
			fmt.Printf("   Timestamp: %s\n", result.Timestamp.Format("15:04:05"))
			fmt.Println("   " + strings.Repeat("─", 50))
		}
	}()
	
	// Monitor system health
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := manager.GetStats()
				
				fmt.Printf("\n📊 System Status at %s:\n", time.Now().Format("15:04:05"))
				
				if integrationStats, ok := stats["integration"].(map[string]interface{}); ok {
					if running, ok := integrationStats["is_running"].(bool); ok {
						fmt.Printf("   Integration: %s\n", map[bool]string{true: "✅ Running", false: "❌ Stopped"}[running])
					}
					if bufferSize, ok := integrationStats["results_buffer"].(int); ok {
						fmt.Printf("   Results Buffer: %d events\n", bufferSize)
					}
				}
				
				if correlationStats, ok := stats["correlation"].(map[string]interface{}); ok {
					fmt.Printf("   Correlation Engine: %v\n", correlationStats)
				}
				
				fmt.Println()
			}
		}
	}()
	
	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	fmt.Println("\n💡 Demo is running! Events will be generated every 2 seconds.")
	fmt.Println("   Watch for memory pressure correlations when multiple related events occur.")
	fmt.Println("   Press Ctrl+C to stop the demo.\n")
	
	<-sigChan
	
	fmt.Println("\n🛑 Shutting down...")
	cancel()
	
	// Give some time for graceful shutdown
	time.Sleep(2 * time.Second)
	
	fmt.Println("👋 Demo completed successfully!")
}