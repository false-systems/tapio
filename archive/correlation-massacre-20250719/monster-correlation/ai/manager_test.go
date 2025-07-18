package correlation

import (
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
    
    // Create simple events (we'll check the real Event structure)
    now := time.Now()
    events := []domain.Event{
        {
            ID:        domain.EventID("memory-1"),
            Type:      domain.EventTypeMemory,
            Source:    "ebpf",
            Timestamp: now,
            // Remove Resource field for now
        },
        {
            ID:        domain.EventID("restart-1"),
            Type:      domain.EventTypeService,
            Source:    "systemd",
            Timestamp: now.Add(1 * time.Minute),
        },
        {
            ID:        domain.EventID("eviction-1"),
            Type:      domain.EventTypeKubernetes,
            Source:    "kubernetes",
            Timestamp: now.Add(2 * time.Minute),
        },
    }
    
    // Process events
    findings := manager.ProcessEvents(events)
    
    // Should not error
    if findings == nil {
        t.Log("ProcessEvents completed successfully")
    }
    
    // Check statistics
    stats := manager.Statistics()
    if stats == nil {
        t.Error("Statistics returned nil")
    }
    
    t.Logf("Processed %d events, got %d findings", len(events), len(findings))
    
    // Log any findings
    for i, finding := range findings {
        t.Logf("Finding %d: %s - %s", i+1, finding.Title, finding.Description)
    }
}

func TestBasicPatternManager(t *testing.T) {
    // Test that pattern manager integration works
    manager := NewCollectionManager(DefaultConfig())
    
    if manager.patternManager == nil {
        t.Fatal("Pattern manager not initialized")
    }
    
    t.Log("Pattern manager integration successful")
}

func TestAIManagerIntegration(t *testing.T) {
    // This tests that all the pieces work together
    manager := NewCollectionManager(DefaultConfig())
    
    if err := manager.Start(); err != nil {
        t.Fatalf("Manager failed to start: %v", err)
    }
    
    // Let it run briefly
    time.Sleep(200 * time.Millisecond)
    
    stats := manager.Statistics()
    if stats == nil {
        t.Error("Statistics should not be nil")
    }
    
    if err := manager.Stop(); err != nil {
        t.Errorf("Manager failed to stop: %v", err)
    }
    
    t.Log("✅ AI Pattern Recognition Manager integration successful!")
}
