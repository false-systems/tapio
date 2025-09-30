package base

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OutputTargets defines which output destinations are enabled
type OutputTargets struct {
	OTEL    bool // Export domain metrics to OTEL
	NATS    bool // Publish events to NATS
	Stdout  bool // Print events to stdout (debugging)
	Channel bool // Send to local Go channel (always enabled for backward compat)
}

// OTELOutputConfig configures OTEL domain metrics output
type OTELOutputConfig struct {
	Endpoint string            // OTEL collector endpoint
	Headers  map[string]string // Optional headers
	Insecure bool              // Allow insecure connections
}

// NATSOutputConfig configures NATS event publishing
type NATSOutputConfig struct {
	URL         string // NATS server URL
	Credentials string // Path to credentials file (optional)
	StreamName  string // JetStream stream name
	MaxPending  int    // Max pending messages
}

// DefaultOutputTargets returns default output configuration
// By default, only local channel is enabled (backward compatible)
func DefaultOutputTargets() OutputTargets {
	return OutputTargets{
		OTEL:    false,
		NATS:    false,
		Stdout:  false,
		Channel: true, // Always enabled for backward compatibility
	}
}

// Validate checks if the output targets configuration is valid
func (ot OutputTargets) Validate() error {
	// At least channel should be enabled
	if !ot.Channel && !ot.OTEL && !ot.NATS && !ot.Stdout {
		return fmt.Errorf("at least one output target must be enabled")
	}
	return nil
}

// HasAnyOutput returns true if any output is enabled
func (ot OutputTargets) HasAnyOutput() bool {
	return ot.OTEL || ot.NATS || ot.Stdout || ot.Channel
}

// Validate checks if OTEL config is valid
func (cfg *OTELOutputConfig) Validate() error {
	if cfg == nil {
		return fmt.Errorf("OTEL config is nil")
	}
	if cfg.Endpoint == "" {
		return fmt.Errorf("OTEL endpoint is required")
	}
	return nil
}

// Validate checks if NATS config is valid
func (cfg *NATSOutputConfig) Validate() error {
	if cfg == nil {
		return fmt.Errorf("NATS config is nil")
	}
	if cfg.URL == "" {
		return fmt.Errorf("NATS URL is required")
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "TAPIO_EVENTS" // Default stream name
	}
	if cfg.MaxPending == 0 {
		cfg.MaxPending = 1000 // Default max pending
	}
	return nil
}

// DomainMetric represents a domain-specific metric to be emitted
type DomainMetric struct {
	Name       string
	Value      int64
	Attributes []attribute.KeyValue
}

// DomainGauge represents a domain-specific gauge metric
type DomainGauge struct {
	Name       string
	Value      int64
	Attributes []attribute.KeyValue
}

// OutputEmitter defines the interface for emitting events to different outputs
type OutputEmitter interface {
	// EmitEvent emits an event to the output destination
	EmitEvent(ctx context.Context, event *domain.CollectorEvent) error

	// EmitDomainMetric emits a domain-specific counter metric (OTEL only)
	EmitDomainMetric(ctx context.Context, metric DomainMetric) error

	// EmitDomainGauge emits a domain-specific gauge metric (OTEL only)
	EmitDomainGauge(ctx context.Context, gauge DomainGauge) error

	// Close closes the emitter and releases resources
	Close() error
}

// domainMetricsCache caches created domain metrics to avoid recreation
type domainMetricsCache struct {
	counters map[string]metric.Int64Counter
	gauges   map[string]metric.Int64Gauge
}

// newDomainMetricsCache creates a new domain metrics cache
func newDomainMetricsCache() *domainMetricsCache {
	return &domainMetricsCache{
		counters: make(map[string]metric.Int64Counter),
		gauges:   make(map[string]metric.Int64Gauge),
	}
}
