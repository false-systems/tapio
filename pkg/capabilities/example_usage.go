package capabilities

import (
	"context"
	"fmt"
	"log"
)

// Example demonstrates how to use the new capability-aware interface
// This replaces the old stub-based approach with clear error handling

func ExampleMemoryMonitoring() {
	// OLD WAY (with stubs):
	// monitor := ebpf.NewMonitor(config) // Would return stub on non-Linux
	// stats, _ := monitor.GetMemoryStats() // Would return fake data

	// NEW WAY (capability-aware):
	memCap, err := RequestMemoryMonitoring()
	if err != nil {
		// Handle capability not available - no fake data!
		if IsCapabilityError(err) {
			capErr := err.(*CapabilityError)
			log.Printf("Memory monitoring not available: %s (platform: %s)",
				capErr.Reason, capErr.Platform)

			// Show user what's available instead
			report := GetCapabilityReport()
			fmt.Printf("Available capabilities: %v\n",
				getAvailableCapabilityNames(report))
			return
		}
		log.Fatalf("Unexpected error: %v", err)
	}

	// Start monitoring
	ctx := context.Background()
	if err := memCap.Start(ctx); err != nil {
		log.Fatalf("Failed to start memory monitoring: %v", err)
	}
	defer memCap.Stop()

	// Get real data (no stubs!)
	stats, err := memCap.GetMemoryStats()
	if err != nil {
		log.Fatalf("Failed to get memory stats: %v", err)
	}

	fmt.Printf("Memory monitoring active, tracking %d processes\n", len(stats))
}

func ExampleSystemStartup() {
	// Initialize all capabilities with graceful degradation
	ctx := context.Background()
	report := StartWithGracefulDegradation(ctx)

	fmt.Printf("Platform: %s\n", report.Platform)
	fmt.Printf("Started: %v\n", report.Started)

	// Show what failed with reasons (no silent failures!)
	for name, reason := range report.Failed {
		fmt.Printf("Failed to start %s: %s\n", name, reason)
	}

	// Show what was skipped with reasons (no fake implementations!)
	for name, reason := range report.Skipped {
		fmt.Printf("Skipped %s: %s\n", name, reason)
	}
}

func ExampleCapabilityDiscovery() {
	// Discover what's available on this platform
	report := GetCapabilityReport()

	fmt.Printf("Platform Capability Report\n")
	fmt.Printf("==========================\n")
	fmt.Printf("Platform: %s\n", report.Platform)
	fmt.Printf("Total capabilities: %d\n", report.Summary.Total)
	fmt.Printf("Available: %d\n", report.Summary.Available)
	fmt.Printf("Enabled: %d\n", report.Summary.Enabled)
	fmt.Printf("Not available: %d\n", report.Summary.NotAvailable)

	// Show detailed status
	for name, status := range report.Capabilities {
		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  Status: %s\n", status.Info.Status)
		fmt.Printf("  Health: %s\n", status.Health.Status)

		if status.Info.Error != "" {
			fmt.Printf("  Error: %s\n", status.Info.Error)
		}

		if len(status.Info.Requirements) > 0 {
			fmt.Printf("  Requirements: %v\n", status.Info.Requirements)
		}
	}
}

func ExamplePlatformDetection() {
	// Runtime platform capability detection
	info := GetDetailedPlatformInfo()

	fmt.Printf("Platform Detection Results\n")
	fmt.Printf("==========================\n")
	fmt.Printf("OS: %s\n", info.OS)
	fmt.Printf("Architecture: %s\n", info.Architecture)

	if info.KernelVersion != "" {
		fmt.Printf("Kernel: %s\n", info.KernelVersion)
	}

	fmt.Printf("\nCapability Detection:\n")
	for name, detection := range info.Capabilities {
		status := "❌"
		if detection.Supported {
			status = "✅"
		}
		fmt.Printf("%s %s: %s\n", status, detection.Name, detection.Reason)
	}
}

func ExampleErrorHandling() {
	// Demonstrate clear error handling vs stub behavior

	// Try to get network monitoring
	netCap, err := RequestNetworkMonitoring()
	if err != nil {
		if IsCapabilityError(err) {
			capErr := err.(*CapabilityError)
			fmt.Printf("Network monitoring not available: %s\n", capErr.Reason)

			// OLD WAY: Would have returned stub with fake data
			// NEW WAY: Clear error message, no fake data

			return
		}
		log.Fatalf("Unexpected error: %v", err)
	}

	// If we get here, we have real network monitoring
	fmt.Printf("Network monitoring available: %s\n", netCap.Name())
}

// Helper function to extract available capability names
func getAvailableCapabilityNames(report *CapabilityReport) []string {
	var available []string
	for name, status := range report.Capabilities {
		if status.Info.Status == CapabilityAvailable || status.Info.Status == CapabilityEnabled {
			available = append(available, name)
		}
	}
	return available
}

// Migration guide for existing code:
//
// BEFORE (stub-based):
// ```go
// monitor := ebpf.NewMonitor(config)
// stats, _ := monitor.GetMemoryStats() // Always "worked" (with fake data)
// ```
//
// AFTER (capability-aware):
// ```go
// memCap, err := capabilities.RequestMemoryMonitoring()
// if err != nil {
//     // Handle unavailability explicitly
//     log.Printf("Memory monitoring not available: %v", err)
//     return
// }
// stats, err := memCap.GetMemoryStats() // Real data or real error
// ```
//
// Key differences:
// 1. No fake data - real implementations only
// 2. Clear error messages about availability
// 3. Capability discovery built-in
// 4. Platform detection integrated
// 5. Graceful degradation with reporting
