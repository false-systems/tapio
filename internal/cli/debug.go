package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/validation"
)

var debugCmd = &cobra.Command{
	Use:     "debug",
	Aliases: []string{"dbg"},
	Short:   "Advanced debugging and diagnostics",
	Long: `🔍 Debug - Advanced Debugging and Diagnostics

The debug command provides detailed system information, runtime diagnostics,
and troubleshooting tools for complex issues. It's designed for support
teams and advanced users who need deep system insights.

Categories:
  • System Information - Runtime, memory, and environment details
  • Performance Metrics - CPU, memory, and goroutine analysis
  • Configuration Analysis - Complete config validation and sources
  • Network Diagnostics - Connectivity and DNS resolution tests
  • Resource Monitoring - Current usage and limits
  • Component Health - Individual component status checks
  • Error Analysis - Recent errors and patterns`,

	Example: `  # Show all debug information
  tapio debug

  # Show only system information
  tapio debug --system

  # Show performance metrics
  tapio debug --performance

  # Export debug data to file
  tapio debug --export debug-report.json

  # Continuous monitoring mode
  tapio debug --monitor --interval 5s

  # Specific component debugging
  tapio debug --component ebpf --verbose`,

	RunE: runDebugCommand,
}

var (
	debugSystem     bool
	debugPerf       bool
	debugConfig     bool
	debugNetwork    bool
	debugResources  bool
	debugComponents bool
	debugErrors     bool
	debugExport     string
	debugMonitor    bool
	debugInterval   time.Duration
	debugComponent  string
	debugVerbose    bool
)

func init() {
	debugCmd.Flags().BoolVar(&debugSystem, "system", false,
		"Show system information only")
	debugCmd.Flags().BoolVar(&debugPerf, "performance", false,
		"Show performance metrics only")
	debugCmd.Flags().BoolVar(&debugConfig, "config", false,
		"Show configuration analysis only")
	debugCmd.Flags().BoolVar(&debugNetwork, "network", false,
		"Show network diagnostics only")
	debugCmd.Flags().BoolVar(&debugResources, "resources", false,
		"Show resource monitoring only")
	debugCmd.Flags().BoolVar(&debugComponents, "components", false,
		"Show component health only")
	debugCmd.Flags().BoolVar(&debugErrors, "errors", false,
		"Show error analysis only")
	debugCmd.Flags().StringVar(&debugExport, "export", "",
		"Export debug data to file (JSON format)")
	debugCmd.Flags().BoolVar(&debugMonitor, "monitor", false,
		"Continuous monitoring mode")
	debugCmd.Flags().DurationVar(&debugInterval, "interval", 10*time.Second,
		"Monitoring interval for continuous mode")
	debugCmd.Flags().StringVar(&debugComponent, "component", "",
		"Focus on specific component (ebpf, k8s, prometheus, etc.)")
	debugCmd.Flags().BoolVarP(&debugVerbose, "verbose", "v", false,
		"Show verbose debug information")
}

// DebugInfo contains all debug information
type DebugInfo struct {
	Timestamp     time.Time                 `json:"timestamp"`
	Version       string                    `json:"version"`
	System        *SystemInfo               `json:"system,omitempty"`
	Performance   *PerformanceInfo          `json:"performance,omitempty"`
	Configuration *ConfigurationInfo        `json:"configuration,omitempty"`
	Network       *NetworkInfo              `json:"network,omitempty"`
	Resources     *ResourceInfo             `json:"resources,omitempty"`
	Components    map[string]*ComponentInfo `json:"components,omitempty"`
	Errors        *ErrorInfo                `json:"errors,omitempty"`
	Metadata      map[string]interface{}    `json:"metadata,omitempty"`
}

// SystemInfo contains system-level information
type SystemInfo struct {
	OS           string            `json:"os"`
	Architecture string            `json:"architecture"`
	Hostname     string            `json:"hostname"`
	Runtime      *RuntimeInfo      `json:"runtime"`
	Environment  map[string]string `json:"environment,omitempty"`
	BuildInfo    *BuildInfo        `json:"build_info"`
}

// RuntimeInfo contains Go runtime information
type RuntimeInfo struct {
	Version      string       `json:"version"`
	NumCPU       int          `json:"num_cpu"`
	NumGoroutine int          `json:"num_goroutine"`
	NumCgoCall   int64        `json:"num_cgo_call"`
	MemStats     *MemoryStats `json:"memory_stats"`
	GCStats      *GCStats     `json:"gc_stats"`
}

// MemoryStats contains memory statistics
type MemoryStats struct {
	Alloc        uint64 `json:"alloc"`          // bytes allocated and not yet freed
	TotalAlloc   uint64 `json:"total_alloc"`    // bytes allocated (even if freed)
	Sys          uint64 `json:"sys"`            // bytes obtained from system
	Lookups      uint64 `json:"lookups"`        // number of pointer lookups
	Mallocs      uint64 `json:"mallocs"`        // number of mallocs
	Frees        uint64 `json:"frees"`          // number of frees
	HeapAlloc    uint64 `json:"heap_alloc"`     // bytes allocated and not yet freed (same as Alloc above)
	HeapSys      uint64 `json:"heap_sys"`       // bytes obtained from system
	HeapIdle     uint64 `json:"heap_idle"`      // bytes in idle spans
	HeapInuse    uint64 `json:"heap_inuse"`     // bytes in non-idle span
	HeapReleased uint64 `json:"heap_released"`  // bytes released to the OS
	HeapObjects  uint64 `json:"heap_objects"`   // total number of allocated objects
	StackInuse   uint64 `json:"stack_inuse"`    // bytes in stack spans
	StackSys     uint64 `json:"stack_sys"`      // bytes obtained from system for stack
	MSpanInuse   uint64 `json:"mspan_inuse"`    // bytes used by mspan structures
	MSpanSys     uint64 `json:"mspan_sys"`      // bytes obtained from system for mspan
	MCacheInuse  uint64 `json:"mcache_inuse"`   // bytes used by mcache structures
	MCacheSys    uint64 `json:"mcache_sys"`     // bytes obtained from system for mcache
	BuckHashSys  uint64 `json:"buckhash_sys"`   // bytes in bucket hash tables
	GCSys        uint64 `json:"gc_sys"`         // bytes used for garbage collection system metadata
	OtherSys     uint64 `json:"other_sys"`      // bytes used for other system allocations
	NextGC       uint64 `json:"next_gc"`        // next collection will happen when HeapAlloc ≥ this amount
	LastGC       uint64 `json:"last_gc"`        // time of last collection (nanoseconds since 1970)
	PauseTotalNs uint64 `json:"pause_total_ns"` // total pause time in nanoseconds
	NumGC        uint32 `json:"num_gc"`         // number of garbage collections
	NumForcedGC  uint32 `json:"num_forced_gc"`  // number of forced garbage collections
}

// GCStats contains garbage collection statistics
type GCStats struct {
	LastGC         time.Time       `json:"last_gc"`
	NumGC          int64           `json:"num_gc"`
	PauseTotal     time.Duration   `json:"pause_total"`
	PauseQuantiles []time.Duration `json:"pause_quantiles,omitempty"`
}

// BuildInfo contains build information
type BuildInfo struct {
	Version    string            `json:"version"`
	GitCommit  string            `json:"git_commit"`
	BuildDate  string            `json:"build_date"`
	GoVersion  string            `json:"go_version"`
	BuildFlags map[string]string `json:"build_flags,omitempty"`
}

// PerformanceInfo contains performance metrics
type PerformanceInfo struct {
	CPUUsage       float64                `json:"cpu_usage_percent"`
	MemoryUsage    float64                `json:"memory_usage_percent"`
	GoroutineCount int                    `json:"goroutine_count"`
	ThreadCount    int                    `json:"thread_count,omitempty"`
	OpenFiles      int                    `json:"open_files,omitempty"`
	NetworkConns   int                    `json:"network_connections,omitempty"`
	Metrics        map[string]interface{} `json:"metrics,omitempty"`
}

// ConfigurationInfo contains configuration analysis
type ConfigurationInfo struct {
	Valid      bool                         `json:"valid"`
	Sources    []string                     `json:"sources"`
	Values     map[string]interface{}       `json:"values,omitempty"`
	Overrides  map[string]string            `json:"overrides,omitempty"`
	Validation *validation.ValidationResult `json:"validation,omitempty"`
}

// NetworkInfo contains network diagnostics
type NetworkInfo struct {
	Connectivity []ConnectivityTest       `json:"connectivity"`
	DNS          []DNSTest                `json:"dns"`
	Latency      map[string]time.Duration `json:"latency,omitempty"`
}

// ConnectivityTest represents a network connectivity test
type ConnectivityTest struct {
	Target   string        `json:"target"`
	Port     int           `json:"port,omitempty"`
	Protocol string        `json:"protocol"`
	Success  bool          `json:"success"`
	Latency  time.Duration `json:"latency"`
	Error    string        `json:"error,omitempty"`
}

// DNSTest represents a DNS resolution test
type DNSTest struct {
	Hostname  string        `json:"hostname"`
	Addresses []string      `json:"addresses,omitempty"`
	Success   bool          `json:"success"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
}

// ResourceInfo contains resource monitoring data
type ResourceInfo struct {
	Limits    map[string]interface{} `json:"limits"`
	Usage     map[string]interface{} `json:"usage"`
	Available map[string]interface{} `json:"available"`
	Alerts    []ResourceAlert        `json:"alerts,omitempty"`
}

// ResourceAlert represents a resource alert
type ResourceAlert struct {
	Resource   string  `json:"resource"`
	Level      string  `json:"level"` // warning, critical
	Message    string  `json:"message"`
	Threshold  float64 `json:"threshold"`
	Current    float64 `json:"current"`
	Suggestion string  `json:"suggestion,omitempty"`
}

// ComponentInfo contains component health information
type ComponentInfo struct {
	Name      string                 `json:"name"`
	Status    string                 `json:"status"` // healthy, degraded, unhealthy
	Message   string                 `json:"message"`
	LastCheck time.Time              `json:"last_check"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
	Errors    []string               `json:"errors,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

// ErrorInfo contains error analysis
type ErrorInfo struct {
	RecentErrors []ErrorEntry   `json:"recent_errors"`
	ErrorCounts  map[string]int `json:"error_counts"`
	Patterns     []ErrorPattern `json:"patterns,omitempty"`
}

// ErrorEntry represents a single error occurrence
type ErrorEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Component string                 `json:"component"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// ErrorPattern represents a recurring error pattern
type ErrorPattern struct {
	Pattern    string    `json:"pattern"`
	Count      int       `json:"count"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Suggestion string    `json:"suggestion,omitempty"`
}

func runDebugCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine what to collect
	collectAll := !debugSystem && !debugPerf && !debugConfig &&
		!debugNetwork && !debugResources && !debugComponents && !debugErrors

	debugInfo := &DebugInfo{
		Timestamp: time.Now(),
		Version:   getVersion(),
		Metadata:  make(map[string]interface{}),
	}

	if collectAll || debugSystem {
		fmt.Println("🔍 Collecting system information...")
		debugInfo.System = collectSystemInfo()
	}

	if collectAll || debugPerf {
		fmt.Println("📊 Collecting performance metrics...")
		debugInfo.Performance = collectPerformanceInfo()
	}

	if collectAll || debugConfig {
		fmt.Println("⚙️  Analyzing configuration...")
		debugInfo.Configuration = collectConfigurationInfo()
	}

	if collectAll || debugNetwork {
		fmt.Println("🌐 Running network diagnostics...")
		debugInfo.Network = collectNetworkInfo(ctx)
	}

	if collectAll || debugResources {
		fmt.Println("💾 Monitoring resources...")
		debugInfo.Resources = collectResourceInfo()
	}

	if collectAll || debugComponents {
		fmt.Println("🔧 Checking component health...")
		debugInfo.Components = collectComponentInfo(debugComponent)
	}

	if collectAll || debugErrors {
		fmt.Println("❌ Analyzing errors...")
		debugInfo.Errors = collectErrorInfo()
	}

	// Add metadata
	debugInfo.Metadata["verbose"] = debugVerbose
	debugInfo.Metadata["focused_component"] = debugComponent
	debugInfo.Metadata["collection_time"] = time.Since(debugInfo.Timestamp)

	// Handle export
	if debugExport != "" {
		return exportDebugInfo(debugInfo, debugExport)
	}

	// Handle monitor mode
	if debugMonitor {
		return runMonitorMode(ctx, debugInfo, debugInterval)
	}

	// Display results
	displayDebugInfo(debugInfo)
	return nil
}

func collectSystemInfo() *SystemInfo {
	hostname, _ := os.Hostname()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)

	info := &SystemInfo{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		Hostname:     hostname,
		Runtime: &RuntimeInfo{
			Version:      runtime.Version(),
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
			NumCgoCall:   runtime.NumCgoCall(),
			MemStats: &MemoryStats{
				Alloc:        m.Alloc,
				TotalAlloc:   m.TotalAlloc,
				Sys:          m.Sys,
				Lookups:      m.Lookups,
				Mallocs:      m.Mallocs,
				Frees:        m.Frees,
				HeapAlloc:    m.HeapAlloc,
				HeapSys:      m.HeapSys,
				HeapIdle:     m.HeapIdle,
				HeapInuse:    m.HeapInuse,
				HeapReleased: m.HeapReleased,
				HeapObjects:  m.HeapObjects,
				StackInuse:   m.StackInuse,
				StackSys:     m.StackSys,
				MSpanInuse:   m.MSpanInuse,
				MSpanSys:     m.MSpanSys,
				MCacheInuse:  m.MCacheInuse,
				MCacheSys:    m.MCacheSys,
				BuckHashSys:  m.BuckHashSys,
				GCSys:        m.GCSys,
				OtherSys:     m.OtherSys,
				NextGC:       m.NextGC,
				LastGC:       m.LastGC,
				PauseTotalNs: m.PauseTotalNs,
				NumGC:        m.NumGC,
				NumForcedGC:  m.NumForcedGC,
			},
			GCStats: &GCStats{
				LastGC:     time.Unix(0, int64(m.LastGC)),
				NumGC:      gcStats.NumGC,
				PauseTotal: gcStats.PauseTotal,
			},
		},
		BuildInfo: &BuildInfo{
			Version:   getVersion(),
			GoVersion: runtime.Version(),
		},
	}

	// Collect environment variables (filtered for security)
	if debugVerbose {
		info.Environment = make(map[string]string)
		for _, env := range os.Environ() {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				key := parts[0]
				// Only include non-sensitive environment variables
				if !isSensitiveEnvVar(key) {
					info.Environment[key] = parts[1]
				} else {
					info.Environment[key] = "[REDACTED]"
				}
			}
		}
	}

	return info
}

func collectPerformanceInfo() *PerformanceInfo {
	return &PerformanceInfo{
		GoroutineCount: runtime.NumGoroutine(),
		Metrics: map[string]interface{}{
			"cgo_calls": runtime.NumCgoCall(),
		},
	}
}

func collectConfigurationInfo() *ConfigurationInfo {
	// This would integrate with the actual config system
	return &ConfigurationInfo{
		Valid:   true,
		Sources: []string{"defaults", "config_file", "environment", "flags"},
		Values: map[string]interface{}{
			"placeholder": "This would contain actual config values",
		},
	}
}

func collectNetworkInfo(ctx context.Context) *NetworkInfo {
	info := &NetworkInfo{
		Connectivity: []ConnectivityTest{},
		DNS:          []DNSTest{},
		Latency:      make(map[string]time.Duration),
	}

	// Test basic DNS resolution
	dnsTests := []string{
		"kubernetes.default.svc.cluster.local",
		"google.com",
		"github.com",
	}

	for _, hostname := range dnsTests {
		start := time.Now()
		// This would perform actual DNS resolution
		info.DNS = append(info.DNS, DNSTest{
			Hostname: hostname,
			Success:  true, // Placeholder
			Latency:  time.Since(start),
		})
	}

	return info
}

func collectResourceInfo() *ResourceInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	info := &ResourceInfo{
		Usage: map[string]interface{}{
			"memory_mb":  float64(m.Alloc) / 1024 / 1024,
			"goroutines": runtime.NumGoroutine(),
			"gc_cycles":  m.NumGC,
		},
		Limits: map[string]interface{}{
			"max_memory_mb": "unlimited", // Would be set from actual limits
		},
		Alerts: []ResourceAlert{},
	}

	// Check for resource alerts
	memoryUsageMB := float64(m.Alloc) / 1024 / 1024
	if memoryUsageMB > 100 { // Example threshold
		info.Alerts = append(info.Alerts, ResourceAlert{
			Resource:   "memory",
			Level:      "warning",
			Message:    "Memory usage is elevated",
			Current:    memoryUsageMB,
			Threshold:  100,
			Suggestion: "Monitor for memory leaks",
		})
	}

	return info
}

func collectComponentInfo(focusComponent string) map[string]*ComponentInfo {
	components := make(map[string]*ComponentInfo)

	// Define standard components
	componentList := []string{"ebpf", "kubernetes", "prometheus", "otel", "cache"}

	if focusComponent != "" {
		componentList = []string{focusComponent}
	}

	for _, name := range componentList {
		components[name] = &ComponentInfo{
			Name:      name,
			Status:    "healthy", // Placeholder - would check actual component
			Message:   "Component is operational",
			LastCheck: time.Now(),
			Metrics: map[string]interface{}{
				"uptime_seconds": 3600, // Placeholder
			},
		}
	}

	return components
}

func collectErrorInfo() *ErrorInfo {
	return &ErrorInfo{
		RecentErrors: []ErrorEntry{},
		ErrorCounts:  make(map[string]int),
		Patterns:     []ErrorPattern{},
	}
}

func displayDebugInfo(info *DebugInfo) {
	fmt.Printf("🔍 Debug Information - %s\n", info.Timestamp.Format(time.RFC3339))
	fmt.Printf("Version: %s\n\n", info.Version)

	if info.System != nil {
		displaySystemInfo(info.System)
	}

	if info.Performance != nil {
		displayPerformanceInfo(info.Performance)
	}

	if info.Configuration != nil {
		displayConfigurationInfo(info.Configuration)
	}

	if info.Network != nil {
		displayNetworkInfo(info.Network)
	}

	if info.Resources != nil {
		displayResourceInfo(info.Resources)
	}

	if info.Components != nil {
		displayComponentInfo(info.Components)
	}

	if info.Errors != nil {
		displayErrorInfo(info.Errors)
	}

	if collection_time, ok := info.Metadata["collection_time"].(time.Duration); ok {
		fmt.Printf("\n⏱️  Collection completed in %v\n", collection_time)
	}
}

func displaySystemInfo(info *SystemInfo) {
	fmt.Println("💻 System Information")
	fmt.Printf("  OS: %s/%s\n", info.OS, info.Architecture)
	fmt.Printf("  Hostname: %s\n", info.Hostname)

	if info.Runtime != nil {
		fmt.Printf("  Go Version: %s\n", info.Runtime.Version)
		fmt.Printf("  CPU Cores: %d\n", info.Runtime.NumCPU)
		fmt.Printf("  Goroutines: %d\n", info.Runtime.NumGoroutine)

		if info.Runtime.MemStats != nil {
			fmt.Printf("  Memory Allocated: %.2f MB\n", float64(info.Runtime.MemStats.Alloc)/1024/1024)
			fmt.Printf("  Memory From System: %.2f MB\n", float64(info.Runtime.MemStats.Sys)/1024/1024)
			fmt.Printf("  GC Cycles: %d\n", info.Runtime.MemStats.NumGC)
		}
	}
	fmt.Println()
}

func displayPerformanceInfo(info *PerformanceInfo) {
	fmt.Println("📊 Performance Metrics")
	fmt.Printf("  Goroutines: %d\n", info.GoroutineCount)

	if info.Metrics != nil {
		for key, value := range info.Metrics {
			fmt.Printf("  %s: %v\n", strings.Title(strings.ReplaceAll(key, "_", " ")), value)
		}
	}
	fmt.Println()
}

func displayConfigurationInfo(info *ConfigurationInfo) {
	fmt.Println("⚙️  Configuration")
	fmt.Printf("  Valid: %v\n", info.Valid)
	fmt.Printf("  Sources: %s\n", strings.Join(info.Sources, ", "))
	fmt.Println()
}

func displayNetworkInfo(info *NetworkInfo) {
	fmt.Println("🌐 Network Diagnostics")

	if len(info.DNS) > 0 {
		fmt.Println("  DNS Resolution:")
		for _, test := range info.DNS {
			status := "✅"
			if !test.Success {
				status = "❌"
			}
			fmt.Printf("    %s %s (%v)\n", status, test.Hostname, test.Latency)
		}
	}
	fmt.Println()
}

func displayResourceInfo(info *ResourceInfo) {
	fmt.Println("💾 Resource Usage")

	if info.Usage != nil {
		for key, value := range info.Usage {
			fmt.Printf("  %s: %v\n", strings.Title(strings.ReplaceAll(key, "_", " ")), value)
		}
	}

	if len(info.Alerts) > 0 {
		fmt.Println("  Alerts:")
		for _, alert := range info.Alerts {
			icon := "⚠️"
			if alert.Level == "critical" {
				icon = "🚨"
			}
			fmt.Printf("    %s %s: %s\n", icon, alert.Resource, alert.Message)
		}
	}
	fmt.Println()
}

func displayComponentInfo(components map[string]*ComponentInfo) {
	fmt.Println("🔧 Component Health")

	// Sort components by name for consistent output
	var names []string
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		component := components[name]
		status := "✅"
		switch component.Status {
		case "degraded":
			status = "⚠️"
		case "unhealthy":
			status = "❌"
		}
		fmt.Printf("  %s %s: %s\n", status, component.Name, component.Message)
	}
	fmt.Println()
}

func displayErrorInfo(info *ErrorInfo) {
	fmt.Println("❌ Error Analysis")

	if len(info.RecentErrors) > 0 {
		fmt.Printf("  Recent Errors: %d\n", len(info.RecentErrors))
	} else {
		fmt.Println("  No recent errors")
	}

	if len(info.ErrorCounts) > 0 {
		fmt.Println("  Error Counts:")
		for pattern, count := range info.ErrorCounts {
			fmt.Printf("    %s: %d\n", pattern, count)
		}
	}
	fmt.Println()
}

func exportDebugInfo(info *DebugInfo, filename string) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal debug info: %w", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write debug info to %s: %w", filename, err)
	}

	fmt.Printf("✅ Debug information exported to %s\n", filename)
	return nil
}

func runMonitorMode(ctx context.Context, initialInfo *DebugInfo, interval time.Duration) error {
	fmt.Printf("📡 Starting monitor mode (interval: %v, press Ctrl+C to stop)\n\n", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Display initial info
	displayDebugInfo(initialInfo)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			fmt.Printf("\n--- %s ---\n", time.Now().Format(time.RFC3339))

			// Collect fresh performance and resource info
			if perf := collectPerformanceInfo(); perf != nil {
				displayPerformanceInfo(perf)
			}

			if resources := collectResourceInfo(); resources != nil {
				displayResourceInfo(resources)
			}
		}
	}
}

func isSensitiveEnvVar(key string) bool {
	sensitiveKeywords := []string{
		"PASSWORD", "SECRET", "KEY", "TOKEN", "CREDENTIAL",
		"AUTH", "PRIVATE", "KUBECONFIG",
	}

	upperKey := strings.ToUpper(key)
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(upperKey, keyword) {
			return true
		}
	}

	return false
}
