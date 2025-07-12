package main

import (
	"fmt"
	"github.com/yairfalse/tapio/pkg/ebpf"
)

func main() {
	fmt.Println("🔍 Testing eBPF availability...")
	fmt.Println("Status:", ebpf.GetAvailabilityStatus())

	details := ebpf.GetDetailedStatus()
	fmt.Println("\nDetailed Status:")
	for k, v := range details {
		fmt.Printf("  %s: %v\n", k, v)
	}
}
