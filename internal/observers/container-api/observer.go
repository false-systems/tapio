//go:build linux

package containerapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Config for K8s API-based container observer
type Config struct {
	Clientset kubernetes.Interface
	Namespace string // "" = all namespaces
}

// Validate checks that config is valid
func (c *Config) Validate() error {
	if c.Clientset == nil {
		return fmt.Errorf("clientset is required")
	}
	return nil
}

// APIObserver watches containers via Kubernetes API for cluster-level monitoring
// Complements eBPF observer by providing:
// - Pod context (namespace, labels, annotations)
// - Container types (init, main, ephemeral)
// - K8s-specific failure reasons (ImagePullBackOff, etc.)
// - Cross-node visibility without eBPF deployment
type APIObserver struct {
	name     string
	deps     *base.Deps
	logger   zerolog.Logger
	config   Config
	informer cache.SharedIndexInformer
}

// New creates a K8s API-based container observer with dependency injection.
func New(cfg Config, deps *base.Deps) (*APIObserver, error) {
	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create informer factory
	var informerFactory informers.SharedInformerFactory
	if cfg.Namespace == "" {
		// Watch all namespaces
		informerFactory = informers.NewSharedInformerFactory(cfg.Clientset, 30*time.Second)
	} else {
		// Watch specific namespace
		informerFactory = informers.NewSharedInformerFactoryWithOptions(
			cfg.Clientset,
			30*time.Second,
			informers.WithNamespace(cfg.Namespace),
		)
	}

	// Create pod informer
	informer := informerFactory.Core().V1().Pods().Informer()

	return &APIObserver{
		name:     "container-api",
		deps:     deps,
		logger:   base.NewLogger("container-api"),
		config:   cfg,
		informer: informer,
	}, nil
}

// Run starts the container-api observer and blocks until context is cancelled.
func (o *APIObserver) Run(ctx context.Context) error {
	o.logger.Info().
		Str("namespace", o.config.Namespace).
		Msg("starting container-api observer")

	// Register update handler
	_, err := o.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldPod, ok := oldObj.(*corev1.Pod)
			if !ok {
				o.logger.Warn().Msg("failed to cast oldObj to Pod")
				return
			}
			newPod, ok := newObj.(*corev1.Pod)
			if !ok {
				o.logger.Warn().Msg("failed to cast newObj to Pod")
				return
			}
			o.handleUpdate(context.Background(), oldPod, newPod)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	// Start informer (non-blocking)
	go o.informer.Run(ctx.Done())

	// Wait for cache sync
	o.logger.Info().Msg("waiting for informer cache sync")
	if !cache.WaitForCacheSync(ctx.Done(), o.informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}

	o.logger.Info().Msg("container-api observer started")

	// Block until context is cancelled
	<-ctx.Done()

	o.logger.Info().Msg("container-api observer stopped")
	return nil
}

// handleUpdate processes pod update events
func (o *APIObserver) handleUpdate(ctx context.Context, oldPod, newPod *corev1.Pod) {
	if oldPod == nil || newPod == nil {
		return
	}

	// Check all container types
	o.checkContainers(ctx, oldPod, newPod, oldPod.Status.InitContainerStatuses, newPod.Status.InitContainerStatuses)
	o.checkContainers(ctx, oldPod, newPod, oldPod.Status.ContainerStatuses, newPod.Status.ContainerStatuses)
	o.checkContainers(ctx, oldPod, newPod, oldPod.Status.EphemeralContainerStatuses, newPod.Status.EphemeralContainerStatuses)
}

// checkContainers compares container statuses and emits events for failures
func (o *APIObserver) checkContainers(ctx context.Context, oldPod, newPod *corev1.Pod, oldStatuses, newStatuses []corev1.ContainerStatus) {
	// Build map of old statuses for lookup
	oldStatusMap := make(map[string]*corev1.ContainerStatus)
	for i := range oldStatuses {
		oldStatusMap[oldStatuses[i].Name] = &oldStatuses[i]
	}

	// Check each new status
	for i := range newStatuses {
		newStatus := &newStatuses[i]
		oldStatus := oldStatusMap[newStatus.Name]

		// Detect failures
		hasFailure := detectOOMKill(newStatus) || detectCrash(newStatus) || detectImagePullFailure(newStatus)
		if !hasFailure {
			continue
		}

		// Check if state changed (don't re-emit for same failure)
		if oldStatus != nil && statesEqual(oldStatus, newStatus) {
			continue
		}

		// Create and emit event
		event := createDomainEvent(newPod, newStatus)
		if event == nil {
			continue
		}

		o.deps.Metrics.RecordEvent(o.name, event.Type)
		if o.deps.Emitter != nil {
			if err := o.deps.Emitter.Emit(ctx, event); err != nil {
				o.deps.Metrics.RecordError(o.name, event.Type, "emit_failed")
				o.logger.Error().Err(err).
					Str("pod", newPod.Name).
					Str("namespace", newPod.Namespace).
					Str("container", newStatus.Name).
					Str("subtype", event.Subtype).
					Msg("failed to emit container event")
			} else {
				o.logger.Debug().
					Str("pod", newPod.Name).
					Str("namespace", newPod.Namespace).
					Str("container", newStatus.Name).
					Str("subtype", event.Subtype).
					Msg("emitted container event")
			}
		}
	}
}

// statesEqual checks if two container statuses have the same state
func statesEqual(old, new *corev1.ContainerStatus) bool {
	// Compare state types
	oldState := getStateString(old)
	newState := getStateString(new)
	if oldState != newState {
		return false
	}

	// Compare reasons (for Waiting and Terminated states)
	if old.State.Waiting != nil && new.State.Waiting != nil {
		return old.State.Waiting.Reason == new.State.Waiting.Reason
	}
	if old.State.Terminated != nil && new.State.Terminated != nil {
		return old.State.Terminated.Reason == new.State.Terminated.Reason &&
			old.State.Terminated.ExitCode == new.State.Terminated.ExitCode
	}

	return true
}

// getStateString returns the state as a string
func getStateString(status *corev1.ContainerStatus) string {
	if status.State.Waiting != nil {
		return "Waiting"
	}
	if status.State.Running != nil {
		return "Running"
	}
	if status.State.Terminated != nil {
		return "Terminated"
	}
	return ""
}

// detectOOMKill returns true if the container was killed due to out-of-memory.
// Checks for:
// - Reason "OOMKilled"
// - Exit code 137 (SIGKILL for memory limits)
func detectOOMKill(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Check if container is terminated
	if status.State.Terminated == nil {
		return false
	}

	terminated := status.State.Terminated

	// Check explicit OOMKilled reason
	if terminated.Reason == "OOMKilled" {
		return true
	}

	// Check exit code 137 (SIGKILL for memory)
	if terminated.ExitCode == 137 {
		return true
	}

	return false
}

// detectCrash returns true if the container crashed (non-zero exit code).
// Excludes OOMKills (exit code 137) as they are handled separately.
func detectCrash(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Check if container is terminated
	if status.State.Terminated == nil {
		return false
	}

	terminated := status.State.Terminated

	// Exit code 0 = success, not a crash
	if terminated.ExitCode == 0 {
		return false
	}

	// Exit code 137 = OOMKill, handled separately
	if terminated.ExitCode == 137 {
		return false
	}

	// Any other non-zero exit code = crash
	return true
}

// detectImagePullFailure returns true if the container failed to pull its image.
// Checks for:
// - Reason "ErrImagePull"
// - Reason "ImagePullBackOff"
func detectImagePullFailure(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Image pull failures occur in Waiting state
	if status.State.Waiting == nil {
		return false
	}

	waiting := status.State.Waiting

	// Check for image pull error reasons
	if waiting.Reason == "ErrImagePull" || waiting.Reason == "ImagePullBackOff" {
		return true
	}

	return false
}

// getContainerType returns the type of container ("init", "main", or "")
// by searching through pod's container status arrays.
func getContainerType(pod *corev1.Pod, containerName string) string {
	if pod == nil {
		return ""
	}

	// Check init containers
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == containerName {
			return "init"
		}
	}

	// Check main containers
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return "main"
		}
	}

	// Check ephemeral containers (future support)
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name == containerName {
			return "ephemeral"
		}
	}

	// Not found
	return ""
}

// createDomainEvent creates an ObserverEvent from pod and container status.
// Determines event subtype based on failure type (OOMKill, crash, image pull).
func createDomainEvent(pod *corev1.Pod, status *corev1.ContainerStatus) *domain.ObserverEvent {
	if pod == nil || status == nil {
		return nil
	}

	// Determine subtype based on failure type
	subtype := "container_unknown"
	if detectOOMKill(status) {
		subtype = "container_oom_killed"
	} else if detectCrash(status) {
		subtype = "container_crashed"
	} else if detectImagePullFailure(status) {
		subtype = "container_image_pull_failed"
	}

	// Extract container type (init, main, ephemeral)
	containerType := getContainerType(pod, status.Name)

	// Find container spec to get image
	var image string
	for _, c := range pod.Spec.InitContainers {
		if c.Name == status.Name {
			image = c.Image
			break
		}
	}
	if image == "" {
		for _, c := range pod.Spec.Containers {
			if c.Name == status.Name {
				image = c.Image
				break
			}
		}
	}

	// Extract state and details
	state := ""
	reason := ""
	message := ""
	exitCode := int32(0)
	signal := int32(0)

	if status.State.Waiting != nil {
		state = "Waiting"
		reason = status.State.Waiting.Reason
		message = status.State.Waiting.Message
	} else if status.State.Running != nil {
		state = "Running"
	} else if status.State.Terminated != nil {
		state = "Terminated"
		reason = status.State.Terminated.Reason
		message = status.State.Terminated.Message
		exitCode = status.State.Terminated.ExitCode
		signal = status.State.Terminated.Signal
	}

	// Create event
	event := &domain.ObserverEvent{
		ID:        uuid.New().String(),
		Type:      string(domain.EventTypeContainer),
		Subtype:   subtype,
		Source:    "container-observer-k8s",
		Timestamp: time.Now(),
		ContainerData: &domain.ContainerEventData{
			ContainerName: status.Name,
			ContainerType: containerType,
			PodName:       pod.Name,
			PodNamespace:  pod.Namespace,
			NodeName:      pod.Spec.NodeName,
			Image:         image,
			State:         state,
			Reason:        reason,
			Message:       message,
			RestartCount:  status.RestartCount,
			ExitCode:      exitCode,
			Signal:        signal,
		},
	}

	return event
}
