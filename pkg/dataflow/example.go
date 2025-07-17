package dataflow

import (
    "context"
    "log"
    "time"
)

// ExampleUsage shows how to use the complete dataflow
func ExampleUsage() {
    ctx := context.Background()
    
    // Create manager
    manager := NewManager()
    
    // Add collectors (when you have them)
    // ebpfCollector := // ... create eBPF collector
    // manager.AddCollector(ebpfCollector)
    
    // Create server bridge
    bridge := NewServerBridge(manager)
    
    // Start everything
    if err := manager.Start(ctx); err != nil {
        log.Fatal(err)
    }
    bridge.Start(ctx)
    
    log.Println("DataFlow active: Collectors → Correlation → Server")
    
    // Keep running
    time.Sleep(time.Hour)
    
    // Clean shutdown
    manager.Stop()
}
