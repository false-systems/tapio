package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	ebpf "github.com/yairfalse/tapio/pkg/collectors/ebpf_new"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/linux"
)

var (
	checkSupport = flag.Bool("check", false, "Check eBPF support on this system")
	listPrograms = flag.Bool("list", false, "List loaded eBPF programs")
	showStats    = flag.Bool("stats", false, "Show collector statistics")
	testLoad     = flag.Bool("test-load", false, "Test loading a minimal eBPF program")
)

func main() {
	flag.Parse()

	if runtime.GOOS != "linux" {
		fmt.Printf("eBPF is not supported on %s\n", runtime.GOOS)
		os.Exit(1)
	}

	if *checkSupport {
		checkSystemSupport()
		return
	}

	if *testLoad {
		testMinimalProgram()
		return
	}

	// For other operations, create a collector
	config := core.MinimalConfig()
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	ctx := context.Background()

	if *listPrograms {
		if err := collector.LoadPrograms(ctx); err != nil {
			log.Fatalf("Failed to load programs: %v", err)
		}
		listLoadedPrograms(collector)
		return
	}

	if *showStats {
		if err := collector.Start(ctx); err != nil {
			log.Fatalf("Failed to start collector: %v", err)
		}
		time.Sleep(5 * time.Second)
		showCollectorStats(collector)
		collector.Stop()
		return
	}

	flag.Usage()
}

func checkSystemSupport() {
	fmt.Println("Checking eBPF support...")
	fmt.Println()

	// Check OS
	fmt.Printf("Operating System: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// Check kernel version
	if kernelVersion, err := linux.GetKernelVersion(); err == nil {
		fmt.Printf("Kernel Version: %s", kernelVersion)
	} else {
		fmt.Printf("Kernel Version: Unable to determine (%v)\n", err)
	}

	// Check permissions
	if os.Geteuid() == 0 {
		fmt.Println("Permissions: Running as root ✓")
	} else {
		fmt.Println("Permissions: Not running as root (may need sudo)")
	}

	// Check BPF support
	if err := linux.CheckBPFSupport(); err != nil {
		fmt.Printf("BPF Support: Not available (%v)\n", err)
	} else {
		fmt.Println("BPF Support: Available ✓")
	}

	// Check BPF filesystem
	if _, err := os.Stat("/sys/fs/bpf"); err == nil {
		fmt.Println("BPF Filesystem: Mounted ✓")
	} else {
		fmt.Println("BPF Filesystem: Not mounted")
	}

	// Check debugfs
	if _, err := os.Stat("/sys/kernel/debug"); err == nil {
		fmt.Println("Debugfs: Mounted ✓")
	} else {
		fmt.Println("Debugfs: Not mounted (optional)")
	}

	fmt.Println()
	fmt.Println("Summary:")
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		fmt.Println("✓ System appears to support eBPF")
	} else {
		fmt.Println("✗ System may not fully support eBPF")
		if runtime.GOOS != "linux" {
			fmt.Println("  - eBPF requires Linux")
		}
		if os.Geteuid() != 0 {
			fmt.Println("  - Root privileges required (use sudo)")
		}
	}
}

func testMinimalProgram() {
	fmt.Println("Testing minimal eBPF program load...")
	
	config := core.MinimalConfig()
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Loading programs...")
	if err := collector.LoadPrograms(ctx); err != nil {
		log.Fatalf("Failed to load programs: %v", err)
	}

	programs, err := collector.GetLoadedPrograms()
	if err != nil {
		log.Fatalf("Failed to get loaded programs: %v", err)
	}

	fmt.Printf("Successfully loaded %d programs:\n", len(programs))
	for _, prog := range programs {
		fmt.Printf("  - %s (type: %s, target: %s)\n", prog.Name, prog.Type, prog.AttachTarget)
	}

	fmt.Println("\nUnloading programs...")
	if err := collector.UnloadPrograms(); err != nil {
		log.Fatalf("Failed to unload programs: %v", err)
	}

	fmt.Println("✓ Test successful!")
}

func listLoadedPrograms(collector core.Collector) {
	programs, err := collector.GetLoadedPrograms()
	if err != nil {
		log.Fatalf("Failed to get loaded programs: %v", err)
	}

	if len(programs) == 0 {
		fmt.Println("No programs loaded")
		return
	}

	fmt.Printf("Loaded eBPF Programs (%d):\n", len(programs))
	fmt.Println()

	for _, prog := range programs {
		fmt.Printf("Program: %s\n", prog.Name)
		fmt.Printf("  ID: %d\n", prog.ID)
		fmt.Printf("  Type: %s\n", prog.Type)
		fmt.Printf("  Target: %s\n", prog.AttachTarget)
		fmt.Printf("  Loaded: %s\n", prog.LoadTime.Format(time.RFC3339))
		fmt.Printf("  Stats:\n")
		fmt.Printf("    Run Count: %d\n", prog.Stats.RunCount)
		fmt.Printf("    Run Time: %s\n", prog.Stats.RunTime)
		if !prog.Stats.LastRun.IsZero() {
			fmt.Printf("    Last Run: %s\n", prog.Stats.LastRun.Format(time.RFC3339))
		}
		
		if len(prog.Maps) > 0 {
			fmt.Printf("  Maps:\n")
			for _, m := range prog.Maps {
				fmt.Printf("    - %s (type: %s, entries: %d/%d)\n",
					m.Name, m.Type, m.CurrentEntries, m.MaxEntries)
			}
		}
		fmt.Println()
	}
}

func showCollectorStats(collector core.Collector) {
	stats, err := collector.GetStats()
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	health := collector.GetHealth()

	fmt.Println("Collector Statistics:")
	fmt.Println()

	fmt.Printf("Health Status: %s\n", health.Status)
	fmt.Printf("Health Message: %s\n", health.Message)
	fmt.Printf("Programs: %d loaded, %d healthy\n", health.ProgramsLoaded, health.ProgramsHealthy)
	
	if len(health.Issues) > 0 {
		fmt.Println("Issues:")
		for _, issue := range health.Issues {
			fmt.Printf("  - [%s] %s: %s (since %s)\n",
				issue.Severity, issue.Component, issue.Issue,
				issue.Since.Format(time.RFC3339))
		}
	}

	fmt.Println()
	fmt.Printf("Events:\n")
	fmt.Printf("  Collected: %d\n", stats.EventsCollected)
	fmt.Printf("  Dropped: %d\n", stats.EventsDropped)
	fmt.Printf("  Filtered: %d\n", stats.EventsFiltered)
	fmt.Printf("  Errors: %d\n", stats.CollectionErrors)
	fmt.Println()

	fmt.Printf("Performance:\n")
	fmt.Printf("  Bytes Processed: %d\n", stats.BytesProcessed)
	fmt.Printf("  Start Time: %s\n", stats.StartTime.Format(time.RFC3339))
	if !stats.LastCollectionTime.IsZero() {
		fmt.Printf("  Last Collection: %s\n", stats.LastCollectionTime.Format(time.RFC3339))
	}
	fmt.Printf("  Uptime: %s\n", time.Since(stats.StartTime).Round(time.Second))
	
	if stats.EventsCollected > 0 {
		rate := float64(stats.EventsCollected) / time.Since(stats.StartTime).Seconds()
		fmt.Printf("  Event Rate: %.2f events/sec\n", rate)
	}

	fmt.Println()
	fmt.Printf("Ring Buffer:\n")
	fmt.Printf("  Size: %d bytes\n", stats.RingBufferStats.Size)
	fmt.Printf("  Used: %d bytes\n", stats.RingBufferStats.Used)
	fmt.Printf("  Lost: %d events\n", stats.RingBufferStats.Lost)
	fmt.Printf("  Read Errors: %d\n", stats.RingBufferStats.ReadErrors)
}