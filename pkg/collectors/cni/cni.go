// Package cni provides CNI (Container Network Interface) event collection for Tapio
//
// This collector monitors CNI plugin operations and produces UnifiedEvent directly,
// enabling rich semantic correlation for container networking observability.
//
// The CNI collector uses multiple monitoring approaches:
// - Log monitoring: Parses CNI plugin logs for operation events
// - Process monitoring: Watches CNI binary executions
// - Event monitoring: Monitors Kubernetes networking events
// - File monitoring: Watches CNI configuration file changes
//
// Architecture:
// This collector follows Tapio's 5-level hierarchy and produces UnifiedEvent
// directly from sources, eliminating conversion overhead and enabling sophisticated
// correlation analysis in the analytics engine.
//
// Key Features:
// - Multi-plugin support (Cilium, Calico, Flannel, AWS VPC CNI, etc.)
// - Rich semantic correlation context (Kubernetes, Network, Performance, Security)
// - Real-time monitoring with configurable event rate limiting
// - Comprehensive health monitoring and statistics
// - Kubernetes integration with RBAC support
//
// Usage Example:
//
//	config := core.Config{
//		Name:                    "production-cni-collector",
//		Enabled:                 true,
//		EventBufferSize:         1000,
//		EnableLogMonitoring:     true,
//		EnableProcessMonitoring: true,
//		EnableEventMonitoring:   true,
//		CNIBinPath:             "/opt/cni/bin",
//		CNIConfPath:            "/etc/cni/net.d",
//		InCluster:              true,
//	}
//
//	collector, err := NewCNICollector(config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	ctx := context.Background()
//	if err := collector.Start(ctx); err != nil {
//		log.Fatal(err)
//	}
//	defer collector.Stop()
//
//	// Process UnifiedEvents
//	for event := range collector.Events() {
//		// Events are ready for analytics engine
//		processUnifiedEvent(event)
//	}
package cni

import (
	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/collectors/cni/internal"
)

// NewCNICollector creates a new CNI collector instance
//
// The collector monitors CNI plugin operations and produces UnifiedEvent
// directly for optimal performance and rich semantic correlation.
//
// Parameters:
//   - config: Configuration specifying monitoring approaches and settings
//
// Returns:
//   - core.Collector: The CNI collector interface
//   - error: Any initialization error
//
// The collector supports multiple CNI plugins and monitoring modes:
//   - Log monitoring: Real-time parsing of CNI plugin logs
//   - Process monitoring: Detection of CNI binary executions
//   - Event monitoring: Kubernetes networking event watching
//   - File monitoring: CNI configuration change detection
func NewCNICollector(config core.Config) (core.Collector, error) {
	return internal.NewCNICollector(config)
}

// DefaultConfig returns a default CNI collector configuration
//
// This configuration enables log and event monitoring with sensible defaults
// for production environments. It can be customized based on specific requirements.
//
// Default settings:
//   - Event buffer size: 1000 events
//   - Poll interval: 5 seconds
//   - Log and event monitoring enabled
//   - Standard CNI paths: /opt/cni/bin, /etc/cni/net.d
//   - Kubernetes in-cluster configuration
func DefaultConfig() core.Config {
	return core.Config{
		Name:                    "cni-collector",
		Enabled:                 true,
		EventBufferSize:         1000,
		CNIBinPath:              "/opt/cni/bin",
		CNIConfPath:             "/etc/cni/net.d",
		EnableLogMonitoring:     true,
		EnableProcessMonitoring: false, // Can be CPU intensive
		EnableEventMonitoring:   true,
		EnableFileMonitoring:    false, // Enable for config change tracking
		InCluster:               true,
		PollInterval:            5000, // 5 seconds in milliseconds
		EventRateLimit:          100,  // Events per second
		MaxConcurrentWatch:      10,
		EnableTraceCorrelation:  true,
		CorrelationTimeout:      30000, // 30 seconds in milliseconds
	}
}

// ProductionConfig returns a production-ready CNI collector configuration
//
// This configuration is optimized for production environments with:
//   - Higher event buffer sizes for burst handling
//   - All monitoring approaches enabled
//   - Trace correlation for distributed debugging
//   - Conservative resource limits
func ProductionConfig() core.Config {
	config := DefaultConfig()
	config.Name = "production-cni-collector"
	config.EventBufferSize = 5000
	config.EnableProcessMonitoring = true
	config.EnableFileMonitoring = true
	config.EventRateLimit = 500
	config.MaxConcurrentWatch = 20
	return config
}

// DevelopmentConfig returns a development-friendly CNI collector configuration
//
// This configuration is optimized for development and testing with:
//   - Smaller buffer sizes for faster feedback
//   - All monitoring enabled for comprehensive debugging
//   - Shorter polling intervals for responsiveness
func DevelopmentConfig() core.Config {
	config := DefaultConfig()
	config.Name = "development-cni-collector"
	config.EventBufferSize = 100
	config.EnableProcessMonitoring = true
	config.EnableFileMonitoring = true
	config.PollInterval = 2000 // 2 seconds
	config.EventRateLimit = 50
	config.MaxConcurrentWatch = 5
	return config
}
