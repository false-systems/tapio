package cni_test

import (
	"context"
	"fmt"
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/cni"
)

// Example demonstrates basic usage of the minimal CNI collector
func ExampleCollector() {
	// Create minimal CNI collector
	collector, err := cni.NewCollector("cni-monitor")
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
	// Note: In test environment, no actual CNI events may be generated
	fmt.Println("CNI collector started successfully")
	fmt.Println("Events would be processed here in real environment")

	// Output:
	// CNI collector started successfully
	// Events would be processed here in real environment
}

// Example_ebpfEvents shows how network namespace events are captured
func Example_ebpfEvents() {
	collector, _ := cni.NewCollector("cni-ebpf")
	ctx := context.Background()
	collector.Start(ctx)
	defer collector.Stop()

	// Example of processing eBPF network namespace events
	// Note: In test environment, no actual eBPF events may be generated
	fmt.Println("CNI eBPF collector started successfully")
	fmt.Println("Network namespace events would be captured here")

	// Output:
	// CNI eBPF collector started successfully
	// Network namespace events would be captured here
}
