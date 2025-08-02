package kubeapi

import "time"

// Config holds configuration for kubeapi collector
type Config struct {
	// Namespaces to watch (empty = all namespaces)
	Namespaces []string
	
	// Resource types to watch
	WatchPods         bool
	WatchServices     bool
	WatchDeployments  bool
	WatchStatefulSets bool
	WatchDaemonSets   bool
	WatchReplicaSets  bool
	WatchConfigMaps   bool
	WatchSecrets      bool
	WatchIngresses    bool
	
	// Resync period for informers
	ResyncPeriod time.Duration
	
	// Buffer size for events channel
	BufferSize int
	
	// Enable relationship tracking
	TrackRelationships bool
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Namespaces:         []string{}, // Watch all namespaces
		WatchPods:          true,
		WatchServices:      true,
		WatchDeployments:   true,
		WatchStatefulSets:  true,
		WatchDaemonSets:    true,
		WatchReplicaSets:   true,
		WatchConfigMaps:    true,
		WatchSecrets:       true,
		WatchIngresses:     true,
		ResyncPeriod:       10 * time.Minute,
		BufferSize:         10000,
		TrackRelationships: true,
	}
}