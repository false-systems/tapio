package scheduler

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// EventsWatcher watches K8s Events API for scheduling failures
type EventsWatcher struct {
	clientset kubernetes.Interface
	informer  cache.SharedIndexInformer
	observer  *SchedulerObserver
}

// NewEventsWatcher creates Events API watcher
func NewEventsWatcher(clientset kubernetes.Interface, observer *SchedulerObserver) *EventsWatcher {
	return &EventsWatcher{
		clientset: clientset,
		observer:  observer,
	}
}

// Run starts watching K8s Events API and blocks until context is cancelled
func (w *EventsWatcher) Run(ctx context.Context) error {
	factory := informers.NewSharedInformerFactory(w.clientset, 0)
	w.informer = factory.Core().V1().Events().Informer()

	if _, err := w.informer.AddEventHandler(&eventsEventHandler{watcher: w}); err != nil {
		return fmt.Errorf("failed to add events event handler: %w", err)
	}

	// Create stopper channel for informer
	stopper := make(chan struct{})
	defer close(stopper)

	// Start informer factory
	factory.Start(stopper)

	// Wait for cache sync with context timeout
	if !cache.WaitForCacheSync(ctx.Done(), w.informer.HasSynced) {
		return fmt.Errorf("failed to sync Events cache")
	}

	// Block until context is cancelled
	<-ctx.Done()

	return nil
}

// eventsEventHandler implements cache.ResourceEventHandler for K8s Events
type eventsEventHandler struct {
	watcher *EventsWatcher
}

// OnAdd is called when an event is added - Skip to avoid processing historical events
func (h *eventsEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	// Skip Add events to avoid processing historical events on startup
	// Only process real-time updates via OnUpdate
}

// OnUpdate is called when an event is updated
func (h *eventsEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldEvent, ok := oldObj.(*corev1.Event)
	if !ok {
		ctx := context.Background()
		logger := h.watcher.observer.Logger(ctx)
		logger.Error().
			Str("handler", "OnUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", oldObj)).
			Str("object", "old").
			Msg("type assertion failed")
		return
	}

	newEvent, ok := newObj.(*corev1.Event)
	if !ok {
		ctx := context.Background()
		logger := h.watcher.observer.Logger(ctx)
		logger.Error().
			Str("handler", "OnUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", newObj)).
			Str("object", "new").
			Msg("type assertion failed")
		return
	}

	// Only process if event count increased (new failure attempt)
	if newEvent.Count <= oldEvent.Count {
		return
	}

	eventCopy := newEvent.DeepCopy()
	h.watcher.processEvent(eventCopy)
}

// OnDelete is called when an event is deleted
func (h *eventsEventHandler) OnDelete(obj interface{}) {
	// Nothing to do for deleted events
}

// processEvent handles FailedScheduling events
func (w *EventsWatcher) processEvent(event *corev1.Event) {
	if event.Reason != "FailedScheduling" {
		return
	}

	if event.InvolvedObject.Kind != "Pod" {
		return
	}

	ctx := context.Background()
	logger := w.observer.Logger(ctx)

	failure := parseSchedulingFailure(event.Message)

	observerEvent := &domain.ObserverEvent{
		ID:        generateEventID(),
		Type:      "scheduler",
		Subtype:   "failed_scheduling",
		Source:    "scheduler",
		Timestamp: getEventTime(event),
		K8sData: &domain.K8sEventData{
			ResourceKind: "Pod",
			ResourceName: event.InvolvedObject.Name,
			Action:       "scheduling_failed",
			Reason:       event.Reason,
			Message:      event.Message,
		},
		SchedulingData: &domain.SchedulingEventData{
			PodUID:         string(event.InvolvedObject.UID),
			Attempts:       event.Count,
			NodesFailed:    failure.NodesFailed,
			NodesTotal:     failure.NodesTotal,
			FailureReasons: failure.Reasons,
		},
	}

	if err := w.observer.emitDomainEvent(ctx, observerEvent); err != nil {
		logger.Error().
			Err(err).
			Str("pod", event.InvolvedObject.Name).
			Str("namespace", event.InvolvedObject.Namespace).
			Msg("failed to emit scheduling failure event")
	}

	// Record metric if available (may be nil in tests)
	if w.observer.schedulingErrorsTotal != nil {
		w.observer.schedulingErrorsTotal.Add(ctx, 1)
	}
}

// SchedulingFailure parsed from event message
type SchedulingFailure struct {
	NodesFailed int
	NodesTotal  int
	Reasons     map[string]int
}

// parseSchedulingFailure parses K8s scheduler event messages
// Example: "0/5 nodes are available: 2 Insufficient cpu, 3 node(s) had taints"
func parseSchedulingFailure(message string) SchedulingFailure {
	failure := SchedulingFailure{
		Reasons: make(map[string]int),
	}

	// Parse "X/Y nodes" pattern
	nodePattern := regexp.MustCompile(`(\d+)/(\d+) nodes`)
	if matches := nodePattern.FindStringSubmatch(message); len(matches) == 3 {
		if nodesFailed, err := strconv.Atoi(matches[1]); err == nil {
			failure.NodesFailed = nodesFailed
		}
		if nodesTotal, err := strconv.Atoi(matches[2]); err == nil {
			failure.NodesTotal = nodesTotal
		}
	}

	// Find the reasons section (after ":")
	colonIdx := strings.Index(message, ":")
	if colonIdx == -1 {
		return failure
	}
	reasonsSection := message[colonIdx+1:]

	// Split by comma to get individual failure reasons
	parts := strings.Split(reasonsSection, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse "N reason" pattern (e.g., "2 Insufficient cpu")
		reasonPattern := regexp.MustCompile(`^(\d+)\s+(.+)$`)
		if matches := reasonPattern.FindStringSubmatch(part); len(matches) == 3 {
			if count, err := strconv.Atoi(matches[1]); err == nil {
				reason := strings.TrimSpace(matches[2])
				if reason != "" && count > 0 {
					failure.Reasons[reason] = count
				}
			}
		}
	}

	return failure
}

// getEventTime returns event timestamp
func getEventTime(event *corev1.Event) time.Time {
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return time.Now()
}

// generateEventID generates unique event ID
func generateEventID() string {
	return uuid.New().String()
}
