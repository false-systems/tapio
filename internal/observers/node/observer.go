//go:build linux

package node

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Config for node observer
type Config struct {
	Clientset kubernetes.Interface
	Emitter   base.Emitter
}

// Validate checks config is valid
func (c *Config) Validate() error {
	if c.Clientset == nil {
		return fmt.Errorf("clientset is required")
	}
	if c.Emitter == nil {
		return fmt.Errorf("emitter is required")
	}
	return nil
}

// Observer watches Kubernetes nodes for health and resource changes
type Observer struct {
	*base.BaseObserver
	config   Config
	informer cache.SharedIndexInformer
	emitter  base.Emitter
}

// NewObserver creates a new node observer
func NewObserver(name string, cfg Config) (*Observer, error) {
	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create base observer
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	// Create informer factory (cluster-wide)
	informerFactory := informers.NewSharedInformerFactory(cfg.Clientset, 30*time.Second)
	informer := informerFactory.Core().V1().Nodes().Informer()

	observer := &Observer{
		BaseObserver: baseObs,
		config:       cfg,
		informer:     informer,
		emitter:      cfg.Emitter,
	}

	// Register event handlers
	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node, ok := obj.(*corev1.Node)
			if !ok {
				return
			}
			observer.handleNode(context.Background(), nil, node)
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
			observer.handleNode(context.Background(), oldNode, newNode)
		},
		DeleteFunc: func(obj interface{}) {
			// Node deletions are tracked but not emitted as events (design decision)
			// We only care about health/pressure changes while node exists
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to register event handlers: %w", err)
	}

	return observer, nil
}

// Start starts the node observer
func (o *Observer) Start(ctx context.Context) error {
	logger := o.Logger(ctx)

	// Start informer in background
	go o.informer.Run(ctx.Done())

	// Wait for cache sync
	logger.Info().Msg("Waiting for node informer cache sync")
	if !cache.WaitForCacheSync(ctx.Done(), o.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}

	logger.Info().Msg("Node informer cache synced")

	// Start base observer (for lifecycle management)
	return o.BaseObserver.Start(ctx)
}

// Stop stops the node observer
func (o *Observer) Stop() error {
	return o.BaseObserver.Stop()
}

// handleNode processes node changes and emits events
func (o *Observer) handleNode(ctx context.Context, oldNode, newNode *corev1.Node) {
	if newNode == nil {
		return
	}

	logger := o.Logger(ctx)

	// Check for condition changes
	for _, condition := range newNode.Status.Conditions {
		oldCondition := findCondition(oldNode, condition.Type)

		// Detect state changes
		if conditionChanged(oldCondition, &condition) {
			event := o.createNodeEvent(newNode, &condition)
			if event != nil {
				if err := o.emitter.Emit(ctx, event); err != nil {
					o.RecordError(ctx, event)
					logger.Error().Err(err).
						Str("node", newNode.Name).
						Str("condition", string(condition.Type)).
						Msg("Failed to emit node event")
				} else {
					o.RecordEvent(ctx)
					logger.Debug().
						Str("node", newNode.Name).
						Str("condition", string(condition.Type)).
						Str("status", string(condition.Status)).
						Msg("Emitted node event")
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
		Source:    o.Name(),
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
