package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/common"
)

type Observer struct {
	*base.BaseObserver
	*base.LifecycleManager

	config *Config
	logger *zap.Logger

	objs       *statusObjects
	links      []link.Link
	perfReader *perf.Reader

	hashDecoder *HashDecoder
	aggregator  *StatusAggregator

	metrics struct {
		httpErrors metric.Int64Counter
		grpcErrors metric.Int64Counter
		timeouts   metric.Int64Counter
		latency    metric.Float64Histogram
		errorRate  metric.Float64ObservableGauge
	}

	mu         sync.RWMutex
	errorRates map[uint32]float64
	lastFlush  time.Time
	EventChan  chan common.ObserverEvent
}

type Config struct {
	Enabled         bool          `yaml:"enabled"`
	SampleRate      float64       `yaml:"sample_rate"`
	MaxEventsPerSec int           `yaml:"max_events_per_sec"`
	MaxMemoryMB     int           `yaml:"max_memory_mb"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	RedactHeaders   []string      `yaml:"redact_headers"`
}

func NewObserver(cfg *Config, logger *zap.Logger) (*Observer, error) {
	// Only try to remove memlock on Linux
	if err := rlimit.RemoveMemlock(); err != nil {
		// Ignore error on non-Linux platforms
		logger.Debug("Could not remove memlock", zap.Error(err))
	}

	ctx := context.Background()
	eventChan := make(chan common.ObserverEvent, 1000)

	o := &Observer{
		BaseObserver:     base.NewBaseObserver("status", 30*time.Second),
		LifecycleManager: base.NewLifecycleManager(ctx, logger),
		EventChan:        eventChan,
		config:           cfg,
		logger:           logger.Named("status"),
		hashDecoder:      NewHashDecoder(),
		aggregator:       NewStatusAggregator(cfg.FlushInterval),
		errorRates:       make(map[uint32]float64),
		lastFlush:        time.Now(),
	}

	if err := o.setupMetrics(); err != nil {
		return nil, fmt.Errorf("setting up metrics: %w", err)
	}

	return o, nil
}

func (o *Observer) setupMetrics() error {
	meter := otel.GetMeterProvider().Meter("tapio.observers.status")

	var err error

	o.metrics.httpErrors, err = meter.Int64Counter(
		"status.http.errors",
		metric.WithDescription("Count of HTTP errors by status code"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.metrics.grpcErrors, err = meter.Int64Counter(
		"status.grpc.errors",
		metric.WithDescription("Count of gRPC errors by status code"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.metrics.timeouts, err = meter.Int64Counter(
		"status.timeouts",
		metric.WithDescription("Count of connection timeouts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.metrics.latency, err = meter.Float64Histogram(
		"status.latency",
		metric.WithDescription("L7 request latency"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	o.metrics.errorRate, err = meter.Float64ObservableGauge(
		"status.error_rate",
		metric.WithDescription("L7 error rate by service"),
		metric.WithUnit("ratio"),
	)

	return err
}

func (o *Observer) observeErrorRate(ctx context.Context, observer metric.Observer) error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for serviceHash, rate := range o.errorRates {
		serviceName := o.hashDecoder.GetService(serviceHash)
		if serviceName != "" {
			observer.ObserveFloat64(o.metrics.errorRate, rate,
				metric.WithAttributes(
					attribute.String("service", serviceName),
				),
			)
		}
	}

	return nil
}

func (o *Observer) Name() string {
	return "status"
}

func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting status observer")

	if err := o.loadBPF(); err != nil {
		return fmt.Errorf("loading BPF: %w", err)
	}

	if err := o.attachProbes(); err != nil {
		return fmt.Errorf("attaching probes: %w", err)
	}

	o.LifecycleManager.Start("processEvents", func() {
		o.processEvents(ctx)
	})

	o.LifecycleManager.Start("flushAggregates", func() {
		o.flushAggregates(ctx)
	})

	o.SetHealthy(true)
	o.logger.Info("Status observer started successfully")

	return nil
}

func (o *Observer) Stop() error {
	o.logger.Info("Stopping status observer")

	if o.perfReader != nil {
		o.perfReader.Close()
	}

	for _, l := range o.links {
		l.Close()
	}

	if o.objs != nil {
		o.objs.Close()
	}

	o.LifecycleManager.Stop(30 * time.Second)
	close(o.EventChan)

	o.logger.Info("Status observer stopped")
	return nil
}

func (o *Observer) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		default:
			record, err := o.perfReader.Read()
			if err != nil {
				if err == perf.ErrClosed {
					return
				}
				o.RecordError(err)
				continue
			}

			o.RecordEvent()
			o.handleEvent(record.RawSample)
		}
	}
}

func (o *Observer) handleEvent(data []byte) {
	event, err := parseStatusEvent(data)
	if err != nil {
		o.logger.Warn("Failed to parse event", zap.Error(err))
		return
	}

	o.aggregator.Add(event)

	if event.StatusCode >= 500 {
		o.metrics.httpErrors.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.Int("status_code", int(event.StatusCode)),
				attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
			),
		)
	}

	if event.ErrorType == ErrorTimeout {
		o.metrics.timeouts.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
			),
		)
	}

	if event.Latency > 0 {
		o.metrics.latency.Record(context.Background(), float64(event.Latency)/1e6,
			metric.WithAttributes(
				attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
				attribute.String("endpoint", o.hashDecoder.GetEndpoint(event.EndpointHash)),
			),
		)
	}
}

func (o *Observer) flushAggregates(ctx context.Context) {
	ticker := time.NewTicker(o.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			aggregates := o.aggregator.Flush()
			o.updateErrorRates(aggregates)
			o.publishAggregates(aggregates)
		}
	}
}

func (o *Observer) updateErrorRates(aggregates map[uint32]*AggregatedStatus) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for serviceHash, agg := range aggregates {
		if agg.TotalCount > 0 {
			o.errorRates[serviceHash] = float64(agg.ErrorCount) / float64(agg.TotalCount)
		}
	}
}

func (o *Observer) publishAggregates(aggregates map[uint32]*AggregatedStatus) {
	for serviceHash, agg := range aggregates {
		errorTypes := make(map[uint16]uint64)
		for k, v := range agg.ErrorTypes {
			errorTypes[uint16(k)] = v
		}

		event := common.ObserverEvent{
			Type:      common.EventTypeStatus,
			Timestamp: time.Now(),
			Service:   o.hashDecoder.GetService(serviceHash),
			Data: common.EventData{
				ErrorCount: agg.ErrorCount,
				TotalCount: agg.TotalCount,
				ErrorRate:  float64(agg.ErrorCount) / float64(agg.TotalCount),
				AvgLatency: agg.AvgLatency(),
				ErrorTypes: errorTypes,
			},
		}

		select {
		case o.EventChan <- event:
		default:
			o.RecordDrop()
		}
	}
}

func (o *Observer) GetEvents() <-chan common.ObserverEvent {
	return o.EventChan
}
