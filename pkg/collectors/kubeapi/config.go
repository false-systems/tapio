package kubeapi

import "time"

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
