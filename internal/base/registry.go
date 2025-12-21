//go:build linux

package base

import "github.com/prometheus/client_golang/prometheus"

// GlobalRegistry is the central Prometheus registry for all TAPIO metrics.
// Following Cortex/Mimir pattern of registry injection.
var GlobalRegistry = prometheus.NewRegistry()

func init() {
	// Register default collectors for Go runtime and process metrics
	GlobalRegistry.MustRegister(prometheus.NewGoCollector())
	GlobalRegistry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
}
