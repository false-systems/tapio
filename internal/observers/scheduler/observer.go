package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
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

	// Intelligence service for event emission
	Emitter intelligence.Service
}

// SchedulerObserver monitors Kubernetes scheduler using Prometheus + Events API
type SchedulerObserver struct {
	*base.BaseObserver
	config        Config
	promScraper   *PrometheusScraper
	eventsWatcher *EventsWatcher       // K8s Events API watcher
	kv            nats.KeyValue        // NATS KV for metadata storage
	emitter       intelligence.Service // Intelligence service for events

	// Scheduler-specific Prometheus metrics
	schedulingAttemptsTotal *prometheus.Counter   // scheduling_attempts_total
	schedulingErrorsTotal   *prometheus.Counter   // scheduling_errors_total
	pendingPodsGauge        *prometheus.Gauge     // pending_pods_current
	preemptionEventsTotal   *prometheus.Counter   // preemption_events_total
	pluginDurationMs        *prometheus.Histogram // plugin_duration_ms
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

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		config:       config,
		promScraper:  promScraper,
		kv:           kv,
		emitter:      config.Emitter,
	}

	// Create scheduler-specific Prometheus metrics using fluent API
	err = base.NewPromMetricBuilder(base.GlobalRegistry, name).
		Counter(&obs.schedulingAttemptsTotal, "scheduling_attempts_total", "Pod scheduling attempts").
		Counter(&obs.schedulingErrorsTotal, "scheduling_errors_total", "Pod scheduling errors").
		Gauge(&obs.pendingPodsGauge, "pending_pods_current", "Current number of pending pods waiting to be scheduled").
		Counter(&obs.preemptionEventsTotal, "preemption_events_total", "Pod preemption events").
		Histogram(&obs.pluginDurationMs, "plugin_duration_ms", "Scheduler plugin execution duration in milliseconds", prometheus.DefBuckets).
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
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
