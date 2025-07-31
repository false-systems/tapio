package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// Example demonstrates basic usage of the minimal CNI collector
func ExampleCollector() {
	// Create minimal CNI collector
	collector, err := NewCollector("cni-monitor")
	if err != nil {
		log.Fatal(err)
	}

	// Start collector
	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer collector.Stop()

	// Process events (in practice, run this in a goroutine)
	eventCount := 0
	for event := range collector.Events() {
		// Parse the raw event data
		var data map[string]interface{}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			continue
		}

		fmt.Printf("CNI Event: type=%s, event=%s\n",
			event.Type, event.Metadata["event"])

		eventCount++
		if eventCount >= 5 {
			break
		}
	}

	// Output:
	// CNI Event: type=cni, event=network_namespace
}

// Example_ebpfEvents shows how network namespace events are captured
func Example_ebpfEvents() {
	collector, _ := NewCollector("cni-ebpf")
	ctx := context.Background()
	collector.Start(ctx)
	defer collector.Stop()

	// Example of processing eBPF network namespace events
	for event := range collector.Events() {
		if event.Metadata["event"] == "network_namespace" {
			var data map[string]interface{}
			json.Unmarshal(event.Data, &data)

			// Network namespace events include:
			// - pid: Process ID
			// - netns: Network namespace ID
			// - type: netns_create, netns_enter, netns_exit
			// - comm: Process command
			fmt.Printf("Process %v (%v) %v network namespace %v\n",
				data["pid"], data["comm"], data["type"], data["netns"])
			break
		}
	}

	// Output:
	// Process 1234 (cni-plugin) netns_create network namespace 4026532456
}
