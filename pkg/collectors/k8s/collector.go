package k8s

import (
	"os"
	"path/filepath"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/collectors/k8s/internal"
)

// Collector is the public interface for the Kubernetes collector
type Collector = core.Collector

// Config is the public configuration type
type Config = core.Config

// Health is the public health status type
type Health = core.Health

// Statistics is the public statistics type
type Statistics = core.Statistics

// HealthStatus constants
const (
	HealthStatusHealthy   = core.HealthStatusHealthy
	HealthStatusDegraded  = core.HealthStatusDegraded
	HealthStatusUnhealthy = core.HealthStatusUnhealthy
	HealthStatusUnknown   = core.HealthStatusUnknown
)

// NewCollector creates a new Kubernetes collector with the given configuration
func NewCollector(config Config) (Collector, error) {
	return internal.NewCollector(config)
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	// Try to detect kubeconfig location
	kubeconfig := ""
	if home := os.Getenv("HOME"); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfig); err != nil {
			kubeconfig = ""
		}
	}
	
	// Check if running in cluster
	inCluster := false
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		inCluster = true
		kubeconfig = ""
	}
	
	return Config{
		Name:             "k8s-collector",
		Enabled:          true,
		EventBufferSize:  1000,
		KubeConfig:       kubeconfig,
		InCluster:        inCluster,
		Namespace:        "", // Watch all namespaces by default
		WatchPods:        true,
		WatchNodes:       true,
		WatchServices:    true,
		WatchDeployments: true,
		WatchEvents:      true,
		WatchConfigMaps:  false, // Disabled by default for security
		WatchSecrets:     false, // Disabled by default for security
		ResyncPeriod:     30 * time.Minute,
		EventRateLimit:   1000,
	}
}