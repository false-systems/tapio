package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Deps holds shared dependencies for all observers.
// Injected at construction - no inheritance, explicit dependencies.
type Deps struct {
	// Metrics for recording events, errors, drops
	Metrics *PromObserverMetrics

	// Emitter for sending events to NATS/OTLP
	Emitter domain.EventEmitter
}

// NewDeps creates shared dependencies for observers.
// Call once at startup, pass to all observers.
func NewDeps(reg prometheus.Registerer, emitter domain.EventEmitter) *Deps {
	if reg == nil {
		reg = GlobalRegistry
	}
	return &Deps{
		Metrics: NewPromObserverMetrics(reg),
		Emitter: emitter,
	}
}
