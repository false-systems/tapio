package cni

import (
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

// ConfigPreset returns predefined configurations for common scenarios
type ConfigPreset string

const (
	// PresetDevelopment for local development with Colima/Docker Desktop
	PresetDevelopment ConfigPreset = "development"

	// PresetProduction for production Kubernetes clusters
	PresetProduction ConfigPreset = "production"

	// PresetHighPerformance for high-throughput environments
	PresetHighPerformance ConfigPreset = "high-performance"

	// PresetMinimal for resource-constrained environments
	PresetMinimal ConfigPreset = "minimal"
)

// GetConfigPreset returns a predefined configuration
func GetConfigPreset(preset ConfigPreset) core.Config {
	switch preset {
	case PresetDevelopment:
		return getDevelopmentConfig()
	case PresetProduction:
		return getProductionConfig()
	case PresetHighPerformance:
		return getHighPerformanceConfig()
	case PresetMinimal:
		return getMinimalConfig()
	default:
		return getDefaultConfig()
	}
}

// getDevelopmentConfig returns config optimized for Colima/local development
func getDevelopmentConfig() core.Config {
	return core.Config{
		Name:            "cni-collector-dev",
		Enabled:         true,
		EventBufferSize: 5000,

		// CNI paths
		CNIBinPath:  "/opt/cni/bin",
		CNIConfPath: "/etc/cni/net.d",

		// Enable all monitoring for development
		EnableLogMonitoring:     true,
		EnableProcessMonitoring: true,
		EnableEventMonitoring:   true,
		EnableFileMonitoring:    true,

		// Use efficient monitors in Colima Linux VM
		UseEBPF:        true,
		UseInotify:     true,
		UseK8sInformer: true,

		// Reasonable limits for development
		PollInterval:       30 * time.Second,
		EventRateLimit:     500,
		MaxConcurrentWatch: 5,

		// Local K8s access
		InCluster: false,
		Namespace: "default", // Focus on default namespace

		// Enable correlation for debugging
		EnableTraceCorrelation: true,
		CorrelationTimeout:     5 * time.Minute,

		// Common plugins in dev environments
		MonitoredPlugins: []string{
			"bridge", "loopback", "host-local", "portmap",
		},
	}
}

// getProductionConfig returns config optimized for production clusters
func getProductionConfig() core.Config {
	return core.Config{
		Name:            "cni-collector-prod",
		Enabled:         true,
		EventBufferSize: 50000,

		// Standard CNI paths
		CNIBinPath:  "/opt/cni/bin",
		CNIConfPath: "/etc/cni/net.d",

		// Selective monitoring in production
		EnableLogMonitoring:     false, // Logs can be noisy
		EnableProcessMonitoring: true,
		EnableEventMonitoring:   true,
		EnableFileMonitoring:    true,

		// Use efficient monitors
		UseEBPF:        true,
		UseInotify:     true,
		UseK8sInformer: true,

		// Production limits
		PollInterval:       60 * time.Second,
		EventRateLimit:     10000,
		MaxConcurrentWatch: 20,

		// In-cluster access
		InCluster: true,
		Namespace: "", // Monitor all namespaces

		// Correlation settings
		EnableTraceCorrelation: true,
		CorrelationTimeout:     2 * time.Minute,

		// Monitor all major CNI plugins
		MonitoredPlugins: []string{
			"cilium", "calico", "flannel", "weave",
			"aws-vpc-cni", "azure-cni", "bridge",
		},
	}
}

// getHighPerformanceConfig returns config for high-throughput environments
func getHighPerformanceConfig() core.Config {
	return core.Config{
		Name:            "cni-collector-hp",
		Enabled:         true,
		EventBufferSize: 100000, // Large buffer

		// Standard paths
		CNIBinPath:  "/opt/cni/bin",
		CNIConfPath: "/etc/cni/net.d",

		// Only efficient monitors
		EnableLogMonitoring:     false,
		EnableProcessMonitoring: true,
		EnableEventMonitoring:   true,
		EnableFileMonitoring:    true,

		// Always use efficient monitors
		UseEBPF:        true,
		UseInotify:     true,
		UseK8sInformer: true,

		// High performance settings
		PollInterval:       120 * time.Second, // Less frequent polling
		EventRateLimit:     50000,             // High rate limit
		MaxConcurrentWatch: 50,                // More concurrent watches

		// Cluster-wide monitoring
		InCluster: true,
		Namespace: "",

		// Quick correlation
		EnableTraceCorrelation: true,
		CorrelationTimeout:     30 * time.Second,
	}
}

// getMinimalConfig returns config for resource-constrained environments
func getMinimalConfig() core.Config {
	return core.Config{
		Name:            "cni-collector-minimal",
		Enabled:         true,
		EventBufferSize: 1000,

		// Standard paths
		CNIBinPath:  "/opt/cni/bin",
		CNIConfPath: "/etc/cni/net.d",

		// Minimal monitoring
		EnableLogMonitoring:     false,
		EnableProcessMonitoring: false,
		EnableEventMonitoring:   true, // Only K8s events
		EnableFileMonitoring:    false,

		// Use efficient monitors when available
		UseEBPF:        false, // eBPF can use resources
		UseInotify:     true,  // Inotify is lightweight
		UseK8sInformer: true,  // More efficient than kubectl

		// Conservative settings
		PollInterval:       300 * time.Second, // 5 minutes
		EventRateLimit:     100,
		MaxConcurrentWatch: 2,

		// Limited scope
		InCluster:     true,
		Namespace:     "default",
		LabelSelector: "app.kubernetes.io/managed-by=tapio",

		// Minimal correlation
		EnableTraceCorrelation: false,
	}
}

// getDefaultConfig returns a balanced default configuration
func getDefaultConfig() core.Config {
	return core.Config{
		Name:            "cni-collector",
		Enabled:         true,
		EventBufferSize: 10000,

		CNIBinPath:  "/opt/cni/bin",
		CNIConfPath: "/etc/cni/net.d",

		EnableLogMonitoring:     true,
		EnableProcessMonitoring: true,
		EnableEventMonitoring:   true,
		EnableFileMonitoring:    true,

		// Try to use efficient monitors
		UseEBPF:        true,
		UseInotify:     true,
		UseK8sInformer: true,

		PollInterval:       60 * time.Second,
		EventRateLimit:     1000,
		MaxConcurrentWatch: 10,

		InCluster: false,

		EnableTraceCorrelation: true,
		CorrelationTimeout:     2 * time.Minute,
	}
}
