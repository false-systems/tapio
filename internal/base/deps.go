package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/pkg/intelligence"
)

// Deps holds shared dependencies for all observers.
// Injected at construction - no inheritance, explicit dependencies.
type Deps struct {
	// Metrics for recording events, errors, drops
	Metrics *PromObserverMetrics

	// Emitter for sending events to NATS/OTLP
	Emitter intelligence.Service
}

// NewDeps creates shared dependencies for observers.
// Call once at startup, pass to all observers.
func NewDeps(reg prometheus.Registerer, emitter intelligence.Service) *Deps {
	if reg == nil {
		reg = GlobalRegistry
	}
	return &Deps{
		Metrics: NewPromObserverMetrics(reg),
		Emitter: emitter,
	}
}
