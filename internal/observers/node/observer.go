//go:build linux
// +build linux

package node

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Config for node observer
type Config struct {
	Clientset kubernetes.Interface
	Emitter   domain.Emitter
}

// Observer watches Kubernetes nodes for health and resource changes
type Observer struct {
	name      string
	clientset kubernetes.Interface
	informer  cache.SharedIndexInformer
	emitter   domain.Emitter
	stopCh    chan struct{}

	// OTEL metrics
	eventsProcessed  metric.Int64Counter
	errorsTotal      metric.Int64Counter
	processingTimeMs metric.Float64Histogram
}

// NewObserver creates a new node observer
func NewObserver(name string, cfg Config) (*Observer, error) {
	// Validate config
	if cfg.Clientset == nil {
		return nil, fmt.Errorf("clientset is required")
	}
	if cfg.Emitter == nil {
		return nil, fmt.Errorf("emitter is required")
	}

	// Create OTEL metrics
	meter := otel.Meter("tapio.observer.node")

	eventsProcessed, err := meter.Int64Counter(
		"events_processed_total",
		metric.WithDescription("Total number of node events processed"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_processed_total counter: %w", err)
	}

	errorsTotal, err := meter.Int64Counter(
		"errors_total",
		metric.WithDescription("Total number of errors while processing node events"),
		metric.WithUnit("{errors}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create errors_total counter: %w", err)
	}

	processingTimeMs, err := meter.Float64Histogram(
		"processing_time_ms",
		metric.WithDescription("Node event processing time in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing_time_ms histogram: %w", err)
	}

	// Create informer factory (cluster-wide)
	informerFactory := informers.NewSharedInformerFactory(cfg.Clientset, 30*time.Second)
	informer := informerFactory.Core().V1().Nodes().Informer()

	observer := &Observer{
		name:             name,
		clientset:        cfg.Clientset,
		informer:         informer,
		emitter:          cfg.Emitter,
		stopCh:           make(chan struct{}),
		eventsProcessed:  eventsProcessed,
		errorsTotal:      errorsTotal,
		processingTimeMs: processingTimeMs,
	}

	return observer, nil
}

// Start starts the node observer
func (o *Observer) Start(ctx context.Context) error {
	// Register event handlers
	_, err := o.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// Handler will be implemented in Cycle 3
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// Handler will be implemented in Cycle 3
		},
		DeleteFunc: func(obj interface{}) {
			// Handler will be implemented in Cycle 3
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handlers: %w", err)
	}

	// Start informer (non-blocking)
	go o.informer.Run(o.stopCh)

	// Wait for cache sync
	if !cache.WaitForCacheSync(ctx.Done(), o.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}

	return nil
}

// Stop stops the node observer
func (o *Observer) Stop() error {
	if o.stopCh != nil {
		close(o.stopCh)
		o.stopCh = nil
	}
	return nil
}

// IsHealthy returns true if the observer is ready to run or running
func (o *Observer) IsHealthy() bool {
	return o.stopCh != nil
}
