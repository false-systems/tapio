package k8s

import "github.com/yairfalse/tapio/pkg/collectors"

// DefaultK8sConfig returns default configuration for K8s collector
func DefaultK8sConfig() collectors.CollectorConfig {
	return collectors.CollectorConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels:         make(map[string]string),
	}
}
