package correlation

import (
    "context"
    "testing"
    "time"

    "github.com/yairfalse/tapio/pkg/domain"
)

func TestCollectionManager(t *testing.T) {
    config := DefaultConfig()
    config.PatternDetectionInterval = 100 * time.Millisecond
    
    manager := NewCollectionManager(config)
    
    // Start the manager
    err := manager.Start()
    if err != nil {
        t.Fatalf("Failed to start manager: %v", err)
    }
    defer manager.Stop()
    
    // Create test events
    events := []domain.Event{
        {
            ID:        "test-1",
            Type:      "memory_usage",
            Source:    "test",
            Timestamp: time.Now(),
            Data: map[string]interface{}{
                "memory_mb": 800,
            },
        },
        {
            ID:        "test-2", 
            Type:      "service_restart",
            Source:    "test",
            Timestamp: time.Now(),
            Data: map[string]interface{}{
                "service": "test-service",
            },
        },
    }
    
    // Process events
    insights := manager.ProcessEvents(events)
    
    // Should not error
    if insights == nil {
        t.Error("ProcessEvents returned nil")
    }
    
    // Check statistics
    stats := manager.Statistics()
    if stats == nil {
        t.Error("Statistics returned nil")
    }
}
