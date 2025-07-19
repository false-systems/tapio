package systemd

import (
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/internal"
)

// Collector is the public interface for the systemd collector
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

// NewCollector creates a new systemd collector with the given configuration
func NewCollector(config Config) (Collector, error) {
	return internal.NewCollector(config)
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		Name:             "systemd-collector",
		Enabled:          true,
		EventBufferSize:  1000,
		WatchAllServices: false,
		ServiceFilter:    []string{}, // Empty means watch based on other criteria
		ServiceExclude: []string{
			// Exclude noisy/unimportant services by default
			"getty@",
			"user@",
			"session-",
			"dbus-",
		},
		UnitTypes: []string{
			"service", // Focus on services by default
		},
		WatchServiceStates:   true,
		WatchServiceFailures: true,
		WatchServiceReloads:  true,
		WatchJobQueue:        false, // Can be noisy
		PollInterval:         30 * time.Second,
		EventRateLimit:       1000,
		DBusTimeout:          30 * time.Second,
		MaxConcurrentWatch:   100,
	}
}

// CriticalServicesConfig returns a configuration for monitoring critical services only
func CriticalServicesConfig() Config {
	config := DefaultConfig()
	config.Name = "systemd-critical-collector"
	config.WatchAllServices = false
	config.ServiceFilter = []string{
		"sshd",
		"systemd-networkd",
		"systemd-resolved",
		"dbus",
		"systemd-journald",
		"kubelet",
		"docker",
		"containerd",
		"nginx",
		"apache",
		"mysql",
		"postgresql",
		"redis",
	}
	config.ServiceExclude = []string{} // Don't exclude anything for critical services
	return config
}

// AllServicesConfig returns a configuration for monitoring all services
func AllServicesConfig() Config {
	config := DefaultConfig()
	config.Name = "systemd-all-collector"
	config.WatchAllServices = true
	config.ServiceFilter = []string{} // Watch all services
	config.ServiceExclude = []string{
		// Still exclude very noisy ones
		"getty@",
		"user@",
		"session-",
	}
	config.UnitTypes = []string{
		"service",
		"socket",
		"timer",
	}
	config.EventRateLimit = 5000 // Higher limit for all services
	return config
}
