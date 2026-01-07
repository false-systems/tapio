//go:build linux

package storage

import (
	"github.com/cilium/ebpf"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/internal/base"
)

// Config holds storage observer configuration.
type Config struct {
	EventChannelSize int // Ring buffer → processor channel size (default: 1000)

	// Latency thresholds (milliseconds)
	LatencyWarningMs  float64 // Emit warning event (default: 50ms)
	LatencyCriticalMs float64 // Emit critical event (default: 200ms)
}

// applyDefaults sets default values for optional config fields.
func (c *Config) applyDefaults() {
	if c.EventChannelSize == 0 {
		c.EventChannelSize = 1000
	}
	if c.LatencyWarningMs == 0 {
		c.LatencyWarningMs = 50.0
	}
	if c.LatencyCriticalMs == 0 {
		c.LatencyCriticalMs = 200.0
	}
}

// StorageObserver tracks block I/O events using eBPF.
type StorageObserver struct {
	name    string
	deps    *base.Deps
	config  Config
	ebpfMgr *base.EBPFManager

	// eBPF map references (nil when eBPF not loaded)
	inflightIOMap *ebpf.Map // Track inflight I/O for latency calculation

	// Storage-specific Prometheus metrics
	ioOpsTotal          *prometheus.Counter // io_operations_total
	ioLatencySpikeTotal *prometheus.Counter // io_latency_spikes_total
	ioErrorsTotal       *prometheus.Counter // io_errors_total
	ioLatencyMs         *prometheus.Gauge   // io_latency_ms (last observed)
	ioThroughputBytes   *prometheus.Gauge   // io_throughput_bytes (per flush interval)
}

// New creates a storage observer with dependency injection.
func New(config Config, deps *base.Deps) *StorageObserver {
	config.applyDefaults()

	obs := &StorageObserver{
		name:   "storage",
		deps:   deps,
		config: config,
	}

	// Create observer-specific Prometheus metrics
	builder := base.NewPromMetricBuilder(base.GlobalRegistry, "storage")
	builder.Counter(&obs.ioOpsTotal, "io_operations_total", "Total block I/O operations observed")
	builder.Counter(&obs.ioLatencySpikeTotal, "io_latency_spikes_total", "I/O latency spike events (above threshold)")
	builder.Counter(&obs.ioErrorsTotal, "io_errors_total", "Total I/O errors detected")
	builder.Gauge(&obs.ioLatencyMs, "io_latency_ms", "Last observed I/O latency in milliseconds")
	builder.Gauge(&obs.ioThroughputBytes, "io_throughput_bytes", "I/O throughput in bytes per second")
	//nolint:errcheck // metrics registration errors are non-fatal for observer operation
	builder.Build()

	return obs
}
