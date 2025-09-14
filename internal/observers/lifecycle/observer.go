package lifecycle

import (
	"context"
	"fmt"
	"time"

	"os"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Observer tracks lifecycle transitions that affect system stability
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channel with drop counting
	*base.LifecycleManager    // Manages goroutines and graceful shutdown

	logger   *zap.Logger
	client   kubernetes.Interface
	detector *TransitionDetector
	tracker  *StateTracker

	// Informers for K8s resources
	informers []cache.SharedIndexInformer
}

// Config for the lifecycle observer
type Config struct {
	BufferSize   int           `json:"buffer_size"`
	ResyncPeriod time.Duration `json:"resync_period"`

	// What to track
	TrackPods        bool `json:"track_pods"`
	TrackDeployments bool `json:"track_deployments"`
	TrackNodes       bool `json:"track_nodes"`
	TrackServices    bool `json:"track_services"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		BufferSize:       10000,
		ResyncPeriod:     30 * time.Minute,
		TrackPods:        true,
		TrackDeployments: true,
		TrackNodes:       true,
		TrackServices:    true,
	}
}

// NewObserver creates a lean lifecycle observer
func NewObserver(logger *zap.Logger, config Config) (*Observer, error) {
	// Get K8s client
	k8sConfig, err := getK8sConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s config: %w", err)
	}

	client, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &Observer{
		BaseObserver:        base.NewBaseObserver("lifecycle", 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, "lifecycle", logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		logger:              logger,
		client:              client,
		detector:            NewTransitionDetector(),
		tracker:             NewStateTracker(),
		informers:           make([]cache.SharedIndexInformer, 0),
	}, nil
}

// Start begins observing lifecycle transitions
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting lifecycle observer")

	// Setup watchers for resources we care about
	if err := o.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	// Start informers
	for _, informer := range o.informers {
		o.LifecycleManager.Start("informer", func() {
			informer.Run(o.LifecycleManager.Context().Done())
		})
	}

	// Wait for cache sync
	o.logger.Info("Waiting for cache sync...")
	for _, informer := range o.informers {
		if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
			return fmt.Errorf("failed to sync cache")
		}
	}

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Lifecycle observer started")
	return nil
}

// Stop gracefully shuts down
func (o *Observer) Stop() error {
	o.logger.Info("Stopping lifecycle observer")

	o.LifecycleManager.Stop(5 * time.Second)
	o.EventChannelManager.Close()
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("Lifecycle observer stopped")
	return nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return "lifecycle"
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// handleTransition processes a lifecycle transition
func (o *Observer) handleTransition(transition *LifecycleTransition) {
	// Skip non-breaking changes
	if !o.detector.IsBreaking(transition) {
		return
	}

	// Track state for pattern detection
	o.tracker.Track(transition)

	// Check for dangerous patterns
	if pattern := o.tracker.DetectPattern(transition.State.Resource); pattern != nil {
		// Inject pattern warning into transition
		transition.Cascade = append(transition.Cascade, CascadeEffect{
			Effect: pattern.Prediction,
			Count:  pattern.Occurrences,
		})
	}

	// Convert to domain event
	event := o.convertToDomainEvent(transition)

	// Send event
	if !o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	} else {
		o.BaseObserver.RecordEvent()
	}
}

// convertToDomainEvent converts lifecycle transition to domain event
func (o *Observer) convertToDomainEvent(transition *LifecycleTransition) *domain.CollectorEvent {
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("lifecycle-%s-%d", transition.State.Resource.UID, time.Now().UnixNano()),
		Timestamp: transition.Timestamp,
		Type:      domain.CollectorEventType(transition.Type), // Use the transition type as event type
		Source:    "lifecycle",
		Severity:  o.mapSeverity(transition.Type),
		EventData: domain.EventDataContainer{
			KubernetesResource: &domain.K8sResourceData{
				Kind:       transition.State.Resource.Kind,
				Name:       transition.State.Resource.Name,
				Namespace:  transition.State.Resource.Namespace,
				UID:        string(transition.State.Resource.UID),
				APIVersion: transition.State.Resource.APIVersion,
				Operation:  string(transition.Type), // Use Operation field instead of Action
				Labels: map[string]string{
					"from_state": transition.State.FromState,
					"to_state":   transition.State.ToState,
				},
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"transition_type": string(transition.Type),
				"pods_affected":   fmt.Sprintf("%d", transition.Resources.DirectCount),
			},
		},
	}
}

// mapSeverity maps transition types to severity
func (o *Observer) mapSeverity(t TransitionType) domain.EventSeverity {
	switch t {
	case TransitionScaleToZero, TransitionDeletion, TransitionOOMKill, TransitionCrashLoop:
		return domain.EventSeverityCritical
	case TransitionScaleDown, TransitionResourceCut, TransitionEviction:
		return domain.EventSeverityError
	case TransitionRollout, TransitionConfigChange:
		return domain.EventSeverityWarning
	default:
		return domain.EventSeverityInfo
	}
}

// getK8sConfig gets kubernetes config
func getK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
