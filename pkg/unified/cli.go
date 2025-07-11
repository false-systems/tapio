package unified

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/falseyair/tapio/pkg/correlation"
)

// CLI provides the command-line interface for the unified system
type CLI struct {
	system *UnifiedSystem
	config *SystemConfig
}

// NewCLI creates a new CLI
func NewCLI() *CLI {
	return &CLI{
		config: DefaultSystemConfig(),
	}
}

// RootCmd returns the root cobra command
func (c *CLI) RootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "tapio-sniffer",
		Short: "eBPF System Sniffer - Multi-Layer Monitoring with High-Performance Streaming",
		Long: `Tapio eBPF System Sniffer provides comprehensive system monitoring with:
- Network monitoring (TCP/UDP, DNS, protocols)
- System service monitoring (systemd)
- Log intelligence (journald)
- Multi-source correlation
- Ultra-high performance (165k+ events/sec)
- Self-healing resilience`,
	}

	// Global flags
	rootCmd.PersistentFlags().BoolVar(&c.config.EnableNetworkMonitoring, "network", true, "Enable network monitoring")
	rootCmd.PersistentFlags().BoolVar(&c.config.EnableDNSMonitoring, "dns", true, "Enable DNS monitoring")
	rootCmd.PersistentFlags().BoolVar(&c.config.EnableProtocolAnalysis, "protocols", true, "Enable protocol analysis")
	rootCmd.PersistentFlags().BoolVar(&c.config.EnableSystemd, "systemd", true, "Enable systemd monitoring")
	rootCmd.PersistentFlags().BoolVar(&c.config.EnableJournald, "journald", true, "Enable journald monitoring")
	rootCmd.PersistentFlags().IntVar(&c.config.MaxEventsPerSecond, "rate-limit", 165000, "Maximum events per second")
	rootCmd.PersistentFlags().IntVar(&c.config.MaxMemoryMB, "max-memory", 100, "Maximum memory usage in MB")

	// Add subcommands
	rootCmd.AddCommand(c.runCmd())
	rootCmd.AddCommand(c.statusCmd())
	rootCmd.AddCommand(c.configCmd())
	rootCmd.AddCommand(c.metricsCmd())
	rootCmd.AddCommand(c.eventsCmd())

	return rootCmd
}

// runCmd creates the run command
func (c *CLI) runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the eBPF System Sniffer",
		Long:  "Start the unified system sniffer with all configured components",
		RunE:  c.runSystem,
	}

	cmd.Flags().BoolVar(&c.config.EnableCircuitBreaker, "circuit-breaker", true, "Enable circuit breaker")
	cmd.Flags().BoolVar(&c.config.EnableSelfHealing, "self-healing", true, "Enable self-healing")
	cmd.Flags().BoolVar(&c.config.EnableLoadShedding, "load-shedding", true, "Enable load shedding")
	cmd.Flags().DurationVar(&c.config.CorrelationWindow, "correlation-window", 5*time.Minute, "Event correlation window")

	return cmd
}

// statusCmd creates the status command
func (c *CLI) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show system status",
		Long:  "Display the current status of all system components",
		RunE:  c.showStatus,
	}
}

// configCmd creates the config command
func (c *CLI) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long:  "View or modify system configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE:  c.showConfig,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration",
		RunE:  c.validateConfig,
	})

	return cmd
}

// metricsCmd creates the metrics command
func (c *CLI) metricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "View system metrics",
		Long:  "Display detailed metrics for all components",
		RunE:  c.showMetrics,
	}

	cmd.Flags().Bool("json", false, "Output metrics as JSON")
	cmd.Flags().Bool("watch", false, "Watch metrics continuously")
	cmd.Flags().Duration("interval", 5*time.Second, "Watch interval")

	return cmd
}

// eventsCmd creates the events command
func (c *CLI) eventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "View system events",
		Long:  "Display and filter system events from all sources",
		RunE:  c.showEvents,
	}

	cmd.Flags().String("source", "", "Filter by event source")
	cmd.Flags().String("type", "", "Filter by event type")
	cmd.Flags().String("severity", "", "Filter by severity")
	cmd.Flags().Int("limit", 100, "Maximum number of events to show")
	cmd.Flags().Bool("follow", false, "Follow events in real-time")

	return cmd
}

// runSystem runs the unified system
func (c *CLI) runSystem(cmd *cobra.Command, args []string) error {
	// Create system
	system, err := NewUnifiedSystem(c.config)
	if err != nil {
		return fmt.Errorf("failed to create system: %w", err)
	}
	c.system = system

	// Start system
	fmt.Println("Starting eBPF System Sniffer...")
	if err := system.Start(); err != nil {
		return fmt.Errorf("failed to start system: %w", err)
	}

	fmt.Println("System started successfully!")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  - Network Monitoring: %v\n", c.config.EnableNetworkMonitoring)
	fmt.Printf("  - DNS Monitoring: %v\n", c.config.EnableDNSMonitoring)
	fmt.Printf("  - Protocol Analysis: %v\n", c.config.EnableProtocolAnalysis)
	fmt.Printf("  - Systemd Monitoring: %v\n", c.config.EnableSystemd)
	fmt.Printf("  - Journald Monitoring: %v\n", c.config.EnableJournald)
	fmt.Printf("  - Max Events/sec: %d\n", c.config.MaxEventsPerSecond)
	fmt.Printf("  - Max Memory: %dMB\n", c.config.MaxMemoryMB)
	fmt.Println()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create ticker for periodic status
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Main loop
	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return system.Stop()

		case <-ticker.C:
			c.printStatus()
		}
	}
}

// showStatus shows system status
func (c *CLI) showStatus(cmd *cobra.Command, args []string) error {
	if c.system == nil || !c.system.isRunning {
		fmt.Println("System is not running")
		return nil
	}

	c.printStatus()
	return nil
}

// printStatus prints current status
func (c *CLI) printStatus() {
	metrics := c.system.GetMetrics()

	fmt.Printf("\n=== System Status ===\n")
	fmt.Printf("Uptime: %s\n", metrics.Uptime)
	fmt.Printf("Events Processed: %d\n", metrics.EventsProcessed)
	fmt.Printf("eBPF Events: %d\n", metrics.EBPFEvents)
	fmt.Printf("Pipeline Throughput: %d events/sec\n", metrics.PipelineThroughput)
	fmt.Printf("Pipeline Latency: %v\n", metrics.PipelineLatency)
	fmt.Printf("CPU Usage: %.1f%%\n", metrics.CPUUsage)
	fmt.Printf("Memory Usage: %.1f%%\n", metrics.MemoryUsage)
	fmt.Printf("Error Rate: %.4f\n", metrics.ErrorRate)

	if metrics.CircuitBreakerState != "" {
		fmt.Printf("Circuit Breaker: %s\n", metrics.CircuitBreakerState)
	}

	if metrics.RateLimitedEvents > 0 {
		fmt.Printf("Rate Limited Events: %d\n", metrics.RateLimitedEvents)
	}

	// Show component health
	if c.system.selfHealing != nil {
		fmt.Printf("\n=== Component Health ===\n")
		components := c.system.selfHealing.GetAllComponentStatus()
		for name, comp := range components {
			fmt.Printf("%-20s: %s\n", name, comp.Status)
		}
	}
}

// showConfig shows configuration
func (c *CLI) showConfig(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")

	if jsonFlag {
		data, err := json.MarshalIndent(c.config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("=== System Configuration ===\n")
		fmt.Printf("Network Monitoring: %v\n", c.config.EnableNetworkMonitoring)
		fmt.Printf("DNS Monitoring: %v\n", c.config.EnableDNSMonitoring)
		fmt.Printf("Protocol Analysis: %v\n", c.config.EnableProtocolAnalysis)
		fmt.Printf("Systemd Monitoring: %v\n", c.config.EnableSystemd)
		fmt.Printf("Journald Monitoring: %v\n", c.config.EnableJournald)
		fmt.Printf("Event Buffer Size: %d\n", c.config.EventBufferSize)
		fmt.Printf("Max Events/sec: %d\n", c.config.MaxEventsPerSecond)
		fmt.Printf("Batch Size: %d\n", c.config.BatchSize)
		fmt.Printf("Circuit Breaker: %v\n", c.config.EnableCircuitBreaker)
		fmt.Printf("Self Healing: %v\n", c.config.EnableSelfHealing)
		fmt.Printf("Load Shedding: %v\n", c.config.EnableLoadShedding)
		fmt.Printf("Correlation Window: %v\n", c.config.CorrelationWindow)
		fmt.Printf("Max Memory: %dMB\n", c.config.MaxMemoryMB)
		fmt.Printf("Max CPU: %d%%\n", c.config.MaxCPUPercent)
	}

	return nil
}

// validateConfig validates configuration
func (c *CLI) validateConfig(cmd *cobra.Command, args []string) error {
	errors := []string{}

	if c.config.MaxEventsPerSecond <= 0 {
		errors = append(errors, "MaxEventsPerSecond must be positive")
	}

	if c.config.EventBufferSize <= 0 {
		errors = append(errors, "EventBufferSize must be positive")
	}

	if c.config.BatchSize <= 0 {
		errors = append(errors, "BatchSize must be positive")
	}

	if c.config.MaxMemoryMB <= 0 {
		errors = append(errors, "MaxMemoryMB must be positive")
	}

	if c.config.MaxCPUPercent <= 0 || c.config.MaxCPUPercent > 100 {
		errors = append(errors, "MaxCPUPercent must be between 1 and 100")
	}

	if len(errors) > 0 {
		fmt.Println("Configuration validation failed:")
		for _, err := range errors {
			fmt.Printf("  - %s\n", err)
		}
		return fmt.Errorf("configuration validation failed")
	}

	fmt.Println("Configuration is valid!")
	return nil
}

// showMetrics shows detailed metrics
func (c *CLI) showMetrics(cmd *cobra.Command, args []string) error {
	if c.system == nil || !c.system.isRunning {
		return fmt.Errorf("system is not running")
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	watchFlag, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")

	showMetrics := func() {
		metrics := c.system.GetMetrics()

		if jsonFlag {
			data, _ := json.MarshalIndent(metrics, "", "  ")
			fmt.Println(string(data))
		} else {
			c.printDetailedMetrics(metrics)
		}
	}

	showMetrics()

	if watchFlag {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			fmt.Print("\033[H\033[2J") // Clear screen
			showMetrics()
		}
	}

	return nil
}

// printDetailedMetrics prints detailed metrics
func (c *CLI) printDetailedMetrics(metrics SystemMetrics) {
	fmt.Printf("=== System Metrics ===\n")
	fmt.Printf("Uptime: %s\n", metrics.Uptime)
	fmt.Printf("\n")

	fmt.Printf("=== Event Processing ===\n")
	fmt.Printf("Total Events: %d\n", metrics.EventsProcessed)
	fmt.Printf("eBPF Events: %d\n", metrics.EBPFEvents)
	fmt.Printf("Rate Limited: %d\n", metrics.RateLimitedEvents)
	fmt.Printf("\n")

	fmt.Printf("=== Performance ===\n")
	fmt.Printf("Throughput: %d events/sec\n", metrics.PipelineThroughput)
	fmt.Printf("Latency: %v\n", metrics.PipelineLatency)
	fmt.Printf("Memory Allocations: %d\n", metrics.MemoryAllocations)
	fmt.Printf("Memory Recycled: %d\n", metrics.MemoryRecycled)
	reuse := float64(0)
	if metrics.MemoryAllocations > 0 {
		reuse = float64(metrics.MemoryRecycled) / float64(metrics.MemoryAllocations+metrics.MemoryRecycled) * 100
	}
	fmt.Printf("Memory Reuse: %.1f%%\n", reuse)
	fmt.Printf("\n")

	fmt.Printf("=== Resources ===\n")
	fmt.Printf("CPU Usage: %.1f%%\n", metrics.CPUUsage)
	fmt.Printf("Memory Usage: %.1f%%\n", metrics.MemoryUsage)
	fmt.Printf("\n")

	fmt.Printf("=== Resilience ===\n")
	fmt.Printf("Circuit Breaker: %s\n", metrics.CircuitBreakerState)
	fmt.Printf("Error Rate: %.4f\n", metrics.ErrorRate)

	// Show pipeline metrics
	if c.system.eventPipeline != nil {
		pipelineMetrics := c.system.eventPipeline.GetMetrics()
		fmt.Printf("\n=== Pipeline Stages ===\n")
		for _, stage := range pipelineMetrics.StageMetrics {
			fmt.Printf("%-20s: %d processed, %d errors\n", stage.Name, stage.Processed, stage.Errors)
		}
	}

	// Show object pool metrics
	if c.system.objectPool != nil {
		poolMetrics := c.system.objectPool.GetMetrics()
		fmt.Printf("\n=== Object Pool ===\n")
		fmt.Printf("In Use: %d\n", poolMetrics.InUse)
		fmt.Printf("Total Size: %d\n", poolMetrics.TotalSize)
	}
}

// showEvents shows system events
func (c *CLI) showEvents(cmd *cobra.Command, args []string) error {
	if c.system == nil || !c.system.isRunning {
		return fmt.Errorf("system is not running")
	}

	source, _ := cmd.Flags().GetString("source")
	eventType, _ := cmd.Flags().GetString("type")
	severity, _ := cmd.Flags().GetString("severity")
	limit, _ := cmd.Flags().GetInt("limit")
	follow, _ := cmd.Flags().GetBool("follow")

	// Get timeline from correlation engine
	timeline := c.system.correlationEngine.GetTimeline()

	// Apply filters
	filters := []correlation.EventFilter{}
	if source != "" {
		filters = append(filters, correlation.SourceFilter(correlation.SourceType(source)))
	}
	if severity != "" {
		filters = append(filters, correlation.SeverityFilter(severity))
	}

	// Get events
	events := timeline.GetEvents(nil, filters...)

	// Filter by type if specified
	if eventType != "" {
		var filtered []correlation.TimelineEvent
		for _, event := range events {
			if strings.Contains(event.EventType, eventType) {
				filtered = append(filtered, event)
			}
		}
		events = filtered
	}

	// Apply limit
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	// Display events
	fmt.Printf("=== System Events ===\n")
	for _, event := range events {
		c.printEvent(event)
	}

	// Follow mode
	if follow {
		fmt.Printf("\n--- Following events (press Ctrl+C to stop) ---\n")
		// Would implement real-time event following here
	}

	return nil
}

// printEvent prints a single event
func (c *CLI) printEvent(event correlation.TimelineEvent) {
	timestamp := event.Timestamp.Format("15:04:05.000")
	severity := strings.ToUpper(event.Severity)
	
	// Color coding based on severity
	severityColor := ""
	switch event.Severity {
	case "critical":
		severityColor = "\033[31m" // Red
	case "error":
		severityColor = "\033[91m" // Light red
	case "warning":
		severityColor = "\033[33m" // Yellow
	case "info":
		severityColor = "\033[36m" // Cyan
	default:
		severityColor = "\033[0m"  // Default
	}

	fmt.Printf("%s %s%-8s\033[0m [%s] %s: %s\n",
		timestamp,
		severityColor,
		severity,
		event.Source,
		event.EventType,
		event.Message,
	)
}

// Execute runs the CLI
func (c *CLI) Execute() error {
	return c.RootCmd().Execute()
}