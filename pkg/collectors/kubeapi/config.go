package kubeapi

import (
	"fmt"
	"time"
)

// Config holds configuration for kubeapi collector
type Config struct {
	// What to watch
	WatchNamespaces  []string // Empty = all namespaces
	IgnoreNamespaces []string // System namespaces to ignore

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

	// Performance
	ResyncPeriod time.Duration
	BufferSize   int

	// Features
	TrackRelationships   bool
	TrackCRDs            bool
	TrackRBAC            bool
	TrackNetworkPolicies bool
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	if c.ResyncPeriod < time.Minute {
		return fmt.Errorf("resync period must be at least 1 minute")
	}
	if c.ResyncPeriod > 24*time.Hour {
		return fmt.Errorf("resync period must not exceed 24 hours")
	}

	// At least one resource type must be watched
	hasWatchType := c.WatchPods || c.WatchServices || c.WatchDeployments ||
		c.WatchStatefulSets || c.WatchDaemonSets || c.WatchReplicaSets ||
		c.WatchConfigMaps || c.WatchSecrets || c.WatchIngresses

	if !hasWatchType {
		return fmt.Errorf("at least one resource type must be watched")
	}

	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		WatchNamespaces: []string{}, // Watch all
		IgnoreNamespaces: []string{
			"kube-system",
			"kube-public",
			"kube-node-lease",
		},
		WatchPods:            true,
		WatchServices:        true,
		WatchDeployments:     true,
		WatchStatefulSets:    true,
		WatchDaemonSets:      true,
		WatchReplicaSets:     true,
		WatchConfigMaps:      true,
		WatchSecrets:         true,
		WatchIngresses:       true,
		ResyncPeriod:         30 * time.Minute,
		BufferSize:           10000,
		TrackRelationships:   true,
		TrackCRDs:            false,
		TrackRBAC:            false,
		TrackNetworkPolicies: false,
	}
}
