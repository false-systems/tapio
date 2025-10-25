package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/client-go/kubernetes"
)

// Config holds scheduler observer configuration
type Config struct {
	// Prometheus scraper config
	SchedulerMetricsURL string        // e.g., "http://kube-scheduler:10251/metrics"
	ScrapeInterval      time.Duration // How often to scrape (default: 30s)

	// K8s Events API watcher config
	K8sClientset kubernetes.Interface // K8s client for Events API

	// NATS KV storage for scheduler metadata
	NATSConn *nats.Conn // NATS connection for storing metadata
	KVBucket string     // KV bucket name for metadata storage

	// Event emitter (OTEL, Tapio, or both)
	Emitter base.Emitter

	// Output configuration
	Output base.OutputConfig
}

// SchedulerObserver monitors Kubernetes scheduler using Prometheus + Events API
type SchedulerObserver struct {
	*base.BaseObserver
	config        Config
	promScraper   *PrometheusScraper
	eventsWatcher *EventsWatcher // K8s Events API watcher
	kv            nats.KeyValue  // NATS KV for metadata storage
	emitter       base.Emitter   // Event emitter

	// Scheduler-specific OTEL metrics
	schedulingAttemptsTotal metric.Int64Counter     // scheduling_attempts_total
	schedulingErrorsTotal   metric.Int64Counter     // scheduling_errors_total
	pendingPodsGauge        metric.Int64Gauge       // pending_pods_current
	preemptionEventsTotal   metric.Int64Counter     // preemption_events_total
	pluginDurationMs        metric.Float64Histogram // plugin_duration_ms
}

// NewSchedulerObserver creates a new scheduler observer with BaseObserver
func NewSchedulerObserver(name string, config Config) (*SchedulerObserver, error) {
	// Create base observer with event publisher
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	// Create Prometheus scraper
	promConfig := PrometheusConfig{
		SchedulerMetricsURL: config.SchedulerMetricsURL,
		ScrapeInterval:      config.ScrapeInterval,
	}
	promScraper := NewPrometheusScraper(promConfig)

	// Get or create NATS KV bucket for metadata storage
	var kv nats.KeyValue
	if config.NATSConn != nil && config.KVBucket != "" {
		js, err := config.NATSConn.JetStream()
		if err != nil {
			return nil, fmt.Errorf("failed to get JetStream context: %w", err)
		}

		kv, err = js.KeyValue(config.KVBucket)
		if err != nil {
			// Bucket doesn't exist, create it
			kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
				Bucket: config.KVBucket,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get/create KV bucket %s: %w", config.KVBucket, err)
			}
		}
	}

	// Create scheduler-specific OTEL metrics
	meter := otel.Meter("tapio.observer.scheduler")

	schedulingAttemptsTotal, err := meter.Int64Counter(
		"scheduling_attempts_total",
		metric.WithDescription("Total number of pod scheduling attempts"),
		metric.WithUnit("{attempts}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduling_attempts_total counter: %w", err)
	}

	schedulingErrorsTotal, err := meter.Int64Counter(
		"scheduling_errors_total",
		metric.WithDescription("Total number of pod scheduling errors"),
		metric.WithUnit("{errors}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduling_errors_total counter: %w", err)
	}

	pendingPodsGauge, err := meter.Int64Gauge(
		"pending_pods_current",
		metric.WithDescription("Current number of pending pods waiting to be scheduled"),
		metric.WithUnit("{pods}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pending_pods_current gauge: %w", err)
	}

	preemptionEventsTotal, err := meter.Int64Counter(
		"preemption_events_total",
		metric.WithDescription("Total number of pod preemption events"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preemption_events_total counter: %w", err)
	}

	pluginDurationMs, err := meter.Float64Histogram(
		"plugin_duration_ms",
		metric.WithDescription("Scheduler plugin execution duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin_duration_ms histogram: %w", err)
	}

	obs := &SchedulerObserver{
		BaseObserver:            baseObs,
		config:                  config,
		promScraper:             promScraper,
		kv:                      kv,
		emitter:                 config.Emitter,
		schedulingAttemptsTotal: schedulingAttemptsTotal,
		schedulingErrorsTotal:   schedulingErrorsTotal,
		pendingPodsGauge:        pendingPodsGauge,
		preemptionEventsTotal:   preemptionEventsTotal,
		pluginDurationMs:        pluginDurationMs,
	}

	// Create Events API watcher if K8s client provided
	if config.K8sClientset != nil {
		obs.eventsWatcher = NewEventsWatcher(config.K8sClientset, obs)
	}

	return obs, nil
}

// Start initiates the scheduler observer
func (o *SchedulerObserver) Start(ctx context.Context) error {
	logger := o.Logger(ctx)

	// Add Events API watcher to pipeline if configured
	if o.eventsWatcher != nil {
		o.AddStage(func(ctx context.Context) error {
			logger.Info().Msg("Starting Events API watcher")
			return o.eventsWatcher.Run(ctx)
		})
	}

	// Start base observer (runs pipeline via errgroup)
	if err := o.BaseObserver.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base observer: %w", err)
	}

	logger.Info().Msg("Scheduler observer started")

	return nil
}

// Stop gracefully stops the scheduler observer
func (o *SchedulerObserver) Stop() error {
	ctx := context.Background()
	logger := o.Logger(ctx)
	logger.Info().Msg("Stopping scheduler observer")

	// Stop base observer (cancels context, all pipeline stages stop)
	return o.BaseObserver.Stop()
}

// emitDomainEvent emits a domain event (exposed for EventsWatcher)
func (o *SchedulerObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) error {
	if o.emitter == nil {
		return nil
	}

	if evt.Source == "" {
		evt.Source = "scheduler"
	}

	o.RecordEvent(ctx)

	return o.emitter.Emit(ctx, evt)
}
