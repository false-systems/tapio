package scheduler

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Config holds scheduler observer configuration
type Config struct {
	// Prometheus scraper config
	SchedulerMetricsURL string        // e.g., "http://kube-scheduler:10251/metrics"
	ScrapeInterval      time.Duration // How often to scrape (default: 30s)
}

// SchedulerObserver monitors Kubernetes scheduler using Prometheus scraping
type SchedulerObserver struct {
	name        string
	deps        *base.Deps
	logger      zerolog.Logger
	config      Config
	promScraper *PrometheusScraper

	// Scheduler-specific Prometheus metrics
	schedulingAttemptsTotal *prometheus.Counter   // scheduling_attempts_total
	schedulingErrorsTotal   *prometheus.Counter   // scheduling_errors_total
	pendingPodsGauge        *prometheus.Gauge     // pending_pods_current
	preemptionEventsTotal   *prometheus.Counter   // preemption_events_total
	pluginDurationMs        *prometheus.Histogram // plugin_duration_ms
}

// New creates a scheduler observer with dependency injection.
func New(config Config, deps *base.Deps) (*SchedulerObserver, error) {
	// Create Prometheus scraper
	promConfig := PrometheusConfig{
		SchedulerMetricsURL: config.SchedulerMetricsURL,
		ScrapeInterval:      config.ScrapeInterval,
	}
	promScraper := NewPrometheusScraper(promConfig)

	obs := &SchedulerObserver{
		name:        "scheduler",
		deps:        deps,
		logger:      base.NewLogger("scheduler"),
		config:      config,
		promScraper: promScraper,
	}

	// Create scheduler-specific Prometheus metrics using fluent API
	builder := base.NewPromMetricBuilder(base.GlobalRegistry, "scheduler")
	builder.Counter(&obs.schedulingAttemptsTotal, "scheduling_attempts_total", "Pod scheduling attempts")
	builder.Counter(&obs.schedulingErrorsTotal, "scheduling_errors_total", "Pod scheduling errors")
	builder.Gauge(&obs.pendingPodsGauge, "pending_pods_current", "Current number of pending pods waiting to be scheduled")
	builder.Counter(&obs.preemptionEventsTotal, "preemption_events_total", "Pod preemption events")
	builder.Histogram(&obs.pluginDurationMs, "plugin_duration_ms", "Scheduler plugin execution duration in milliseconds", prometheus.DefBuckets)
	if err := builder.Build(); err != nil {
		obs.logger.Warn().Err(err).Msg("failed to register scheduler metrics")
	}

	return obs, nil
}

// Run executes the observer until context is cancelled.
func (o *SchedulerObserver) Run(ctx context.Context) error {
	o.logger.Info().Msg("starting scheduler observer")

	// Block until context cancelled
	<-ctx.Done()
	o.logger.Info().Msg("scheduler observer stopped")
	return nil
}

// emitDomainEvent emits a domain event (exposed for EventsWatcher)
func (o *SchedulerObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) error {
	if evt.Source == "" {
		evt.Source = "scheduler"
	}

	o.deps.Metrics.RecordEvent(o.name, evt.Type)

	if o.deps.Emitter != nil {
		if err := o.deps.Emitter.Emit(ctx, evt); err != nil {
			o.logger.Error().Err(err).Msg("failed to emit event")
			o.deps.Metrics.RecordError(o.name, evt.Type, "emit_failed")
			return err
		}
	}
	return nil
}
