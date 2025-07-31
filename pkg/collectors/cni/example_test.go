package cni

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Example usage of CNI collector with network policy monitoring
func ExampleCollector_networkPolicy() {
	// Create CNI collector
	config := collectors.CollectorConfig{
		BufferSize: 1000,
		Labels: map[string]string{
			"cluster": "production",
			"region":  "us-east-1",
		},
	}

	collector, err := NewCollector(config)
	if err != nil {
		log.Fatal(err)
	}

	// Start collector
	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Monitor events
	go func() {
		for event := range collector.Events() {
			// Parse event based on metadata
			if event.Metadata["source"] == "ebpf" {
				handlePolicyEvent(event)
			} else {
				handleGenericEvent(event)
			}
		}
	}()

	// Example of how to use enhanced features on Linux:
	// if runtime.GOOS == "linux" {
	//     collector.EnhanceWithNetworkPolicy()
	//     // The enhanced collector will now track:
	//     // - Network policy allow/drop decisions
	//     // - Pod-to-pod traffic with namespace context
	//     // - Policy rule matches
	//     // - CNI-specific enforcement points
	// }

	// Run for some time
	time.Sleep(5 * time.Minute)

	// Stop collector
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}
}

func handlePolicyEvent(event collectors.RawEvent) {
	// Parse policy event from metadata
	fmt.Printf("Network Policy Event: %s %s (%s)\n",
		event.Metadata["action"],
		event.Metadata["direction"],
		event.Metadata["policy_name"])
}

func handleGenericEvent(event collectors.RawEvent) {
	fmt.Printf("CNI Event: %s at %s\n",
		event.Metadata["source"],
		event.Timestamp.Format(time.RFC3339))
}
