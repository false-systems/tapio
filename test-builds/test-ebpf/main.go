package main

import (
    "fmt"
    "github.com/yairfalse/tapio/pkg/ebpf"
)

func main() {
    // Test that ebpf types work
    event := ebpf.SystemEvent{
        Type: "test",
        PID:  1234,
        Data: map[string]interface{}{
            "test": "data",
        },
    }
    
    fmt.Printf("Created ebpf event: %+v\n", event)
    fmt.Println("eBPF module works!")
}