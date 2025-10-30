//go:build linux
// +build linux

package node

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	corev1 "k8s.io/api/core/v1"
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
	name     string
	informer cache.SharedIndexInformer
	emitter  domain.Emitter
	stopCh   chan struct{}

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
			node, ok := obj.(*corev1.Node)
			if !ok {
				return
			}
			// Use background context since handlers run async after Start() completes
			o.handleNode(context.Background(), nil, node)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNode, ok := oldObj.(*corev1.Node)
			if !ok {
				return
			}
			newNode, ok := newObj.(*corev1.Node)
			if !ok {
				return
			}
			// Use background context since handlers run async after Start() completes
			o.handleNode(context.Background(), oldNode, newNode)
		},
		DeleteFunc: func(obj interface{}) {
			// Node deletions are tracked but not emitted as events (design decision)
			// We only care about health/pressure changes while node exists
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

// handleNode processes node changes and emits events
func (o *Observer) handleNode(ctx context.Context, oldNode, newNode *corev1.Node) {
	if newNode == nil {
		return
	}

	// Track processing time
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		o.processingTimeMs.Record(ctx, float64(duration.Milliseconds()))
	}()

	// Check for condition changes
	for _, condition := range newNode.Status.Conditions {
		oldCondition := findCondition(oldNode, condition.Type)

		// Detect state changes
		if conditionChanged(oldCondition, &condition) {
			event := o.createNodeEvent(newNode, &condition)
			if event != nil {
				if err := o.emitter.Emit(ctx, event); err != nil {
					o.errorsTotal.Add(ctx, 1)
				} else {
					o.eventsProcessed.Add(ctx, 1)
				}
			}
		}
	}
}

// findCondition finds a condition in old node by type
func findCondition(node *corev1.Node, condType corev1.NodeConditionType) *corev1.NodeCondition {
	if node == nil {
		return nil
	}
	for i := range node.Status.Conditions {
		if node.Status.Conditions[i].Type == condType {
			return &node.Status.Conditions[i]
		}
	}
	return nil
}

// conditionChanged checks if condition status or reason changed
func conditionChanged(old, new *corev1.NodeCondition) bool {
	if old == nil {
		return true // New condition
	}
	return old.Status != new.Status || old.Reason != new.Reason
}

// createNodeEvent creates domain event from node condition
func (o *Observer) createNodeEvent(node *corev1.Node, condition *corev1.NodeCondition) *domain.ObserverEvent {
	// Determine subtype based on condition
	subtype := "node_condition_change"

	switch condition.Type {
	case corev1.NodeReady:
		if condition.Status == corev1.ConditionTrue {
			subtype = "node_ready"
		} else {
			subtype = "node_not_ready"
		}
	case corev1.NodeMemoryPressure:
		subtype = "node_memory_pressure"
	case corev1.NodeDiskPressure:
		subtype = "node_disk_pressure"
	case corev1.NodePIDPressure:
		subtype = "node_pid_pressure"
	case corev1.NodeNetworkUnavailable:
		subtype = "node_network_unavailable"
	}

	// Extract resource capacity (with nil safety)
	var cpuCapacity, memoryCapacity, podCapacity int64
	if cpu := node.Status.Capacity.Cpu(); cpu != nil {
		cpuCapacity = cpu.MilliValue()
	}
	if memory := node.Status.Capacity.Memory(); memory != nil {
		memoryCapacity = memory.Value()
	}
	if pods := node.Status.Capacity.Pods(); pods != nil {
		podCapacity = pods.Value()
	}

	return &domain.ObserverEvent{
		ID:        uuid.NewString(),
		Type:      "node",
		Subtype:   subtype,
		Source:    o.name,
		Timestamp: time.Now(),
		NodeData: &domain.NodeEventData{
			NodeName:  node.Name,
			Condition: string(condition.Type),
			Status:    string(condition.Status),
			Reason:    condition.Reason,
			Message:   condition.Message,
			// Resource capacity
			CPUCapacity:    cpuCapacity,
			MemoryCapacity: memoryCapacity,
			PodCapacity:    podCapacity,
		},
	}
}
