package main

import (
    "fmt"
    "time"
    
    "github.com/falseyair/tapio/pkg/domain"
)

func main() {
    // Test using domain types
    event := domain.Event{
        ID:        "test-1",
        Timestamp: time.Now(),
        Type:      "test",
        Source:    "test-program",
        Data:      map[string]interface{}{"message": "hello"},
    }
    
    fmt.Printf("Domain module works! Created event: %s\n", event.ID)
}
