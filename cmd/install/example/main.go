package main

import (
	"context"
	"fmt"
	"log"
	"time"
	
	"tapio/cmd/install/installer"
	"tapio/cmd/install/platform"
	"tapio/cmd/install/progress"
)

func main() {
	// Example usage of the installation system
	ctx := context.Background()
	
	// Detect platform
	detector := platform.NewDetector()
	platformInfo := detector.Detect()
	
	fmt.Printf("Detected Platform:\n")
	fmt.Printf("  OS: %s\n", platformInfo.OS)
	fmt.Printf("  Arch: %s\n", platformInfo.Arch)
	fmt.Printf("  Distribution: %s\n", platformInfo.Distribution)
	fmt.Printf("  Is Container: %v\n", platformInfo.IsContainer)
	fmt.Printf("  Is WSL: %v\n", platformInfo.IsWSL)
	fmt.Println()
	
	// Create factory
	factory := platform.NewFactory(platformInfo)
	
	// List available strategies
	strategies := factory.GetAvailableStrategies()
	fmt.Printf("Available Installation Strategies:\n")
	for _, s := range strategies {
		fmt.Printf("  - %s\n", s)
	}
	fmt.Println()
	
	// Create binary installer
	inst, err := factory.Create(installer.StrategyBinary)
	if err != nil {
		log.Fatalf("Failed to create installer: %v", err)
	}
	
	// Get capabilities
	caps := inst.GetCapabilities()
	fmt.Printf("Installer Capabilities:\n")
	fmt.Printf("  Supports Upgrade: %v\n", caps.SupportsUpgrade)
	fmt.Printf("  Supports Rollback: %v\n", caps.SupportsRollback)
	fmt.Printf("  Supports Validation: %v\n", caps.SupportsValidation)
	fmt.Printf("  Requires Root: %v\n", caps.RequiresRoot)
	fmt.Println()
	
	// Create progress reporter
	progressReporter := progress.NewSimpleTerminalReporter()
	
	// Build installation options
	opts := installer.InstallOptions{
		Version:        "v1.0.0",
		InstallPath:    "/tmp/tapio-test",
		ConfigPath:     "/tmp/tapio-test/config",
		DataPath:       "/tmp/tapio-test/data",
		Force:          false,
		SkipValidation: false,
		DryRun:         true, // Dry run for example
		Progress:       progressReporter,
		DownloadOpts: installer.DownloadOptions{
			URL:            "https://example.com/tapio-v1.0.0.tar.gz",
			MaxRetries:     3,
			RetryDelay:     5 * time.Second,
			Timeout:        5 * time.Minute,
			CircuitBreaker: installer.NewCircuitBreaker(5, 1*time.Minute),
		},
	}
	
	// Perform installation (dry run)
	fmt.Println("Performing dry run installation...")
	if err := inst.Install(ctx, opts); err != nil {
		log.Printf("Installation failed: %v", err)
	}
	
	// Example of pipeline usage
	fmt.Println("\nExample Pipeline:")
	demostreatePipeline()
	
	// Example of metrics collection
	fmt.Println("\nExample Metrics:")
	demostrateMetrics()
}

func demostreatePipeline() {
	// Create a simple pipeline
	type Data struct {
		Value int
	}
	
	// Create steps
	step1 := &simpleStep{name: "double", fn: func(d Data) (Data, error) {
		d.Value *= 2
		return d, nil
	}}
	
	step2 := &simpleStep{name: "add10", fn: func(d Data) (Data, error) {
		d.Value += 10
		return d, nil
	}}
	
	// Build pipeline
	pipeline := installer.NewPipeline[Data]().
		AddStep(step1).
		AddStep(step2).
		WithRollback(true)
	
	// Execute
	initial := Data{Value: 5}
	result, err := pipeline.Execute(context.Background(), initial)
	if err != nil {
		log.Printf("Pipeline failed: %v", err)
	} else {
		fmt.Printf("Pipeline result: %d (5 * 2 + 10 = 20)\n", result.Value)
	}
}

func demostrateMetrics() {
	collector := progress.NewMetricsCollector()
	
	// Record some metrics
	collector.RecordDuration("download", 5*time.Second)
	collector.RecordDuration("extract", 2*time.Second)
	collector.RecordSuccess("download")
	collector.RecordSuccess("extract")
	collector.RecordDuration("install", 3*time.Second)
	collector.RecordError("install", fmt.Errorf("permission denied"))
	
	// Get report
	report := collector.GetReport()
	fmt.Printf("Total Duration: %v\n", report.TotalDuration)
	fmt.Printf("Successful Steps: %v\n", report.SuccessfulSteps)
	fmt.Printf("Failed Steps: %v\n", report.FailedSteps)
}

// simpleStep is a basic step implementation for demo
type simpleStep struct {
	name string
	fn   func(Data) (Data, error)
}

type Data struct {
	Value int
}

func (s *simpleStep) Name() string { return s.name }
func (s *simpleStep) Execute(ctx context.Context, data Data) (Data, error) {
	return s.fn(data)
}
func (s *simpleStep) Rollback(ctx context.Context, data Data) error { return nil }
func (s *simpleStep) Validate(ctx context.Context, data Data) error { return nil }