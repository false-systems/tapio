package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// GlobalRegistry is the central Prometheus registry for all TAPIO metrics.
// Following Cortex/Mimir pattern of registry injection.
var GlobalRegistry = prometheus.NewRegistry()

func init() {
	// Register default collectors for Go runtime and process metrics
	GlobalRegistry.MustRegister(collectors.NewGoCollector())
	GlobalRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}
