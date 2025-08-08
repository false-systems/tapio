package correlation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// K8sCorrelator finds correlations based on Kubernetes relationships
type K8sCorrelator struct {
	logger    *zap.Logger
	k8sClient domain.K8sClient // Using domain interface instead of kubernetes.Interface

	// Caches for fast lookup
	ownerCache    *OwnershipCache
	selectorCache *SelectorCache

	// Event cache for correlation
	eventCache *EventCache

	// Watch channels
	podWatcher     <-chan domain.K8sWatchEvent
	serviceWatcher <-chan domain.K8sWatchEvent
	cancelFunc     context.CancelFunc

	started bool
	mu      sync.RWMutex
}

// OwnershipCache tracks K8s ownership relationships
type OwnershipCache struct {
	mu    sync.RWMutex
	items map[string]*OwnershipInfo // key: namespace/kind/name
}

type OwnershipInfo struct {
	Owners   []ResourceRef
	Children []ResourceRef
}

type ResourceRef struct {
	Kind      string
	Name      string
	Namespace string
	UID       string
}

// SelectorCache tracks label selectors
type SelectorCache struct {
	mu        sync.RWMutex
	selectors map[string]SelectorInfo // key: namespace/kind/name
}

type SelectorInfo struct {
	Labels map[string]string
}

// EventCache stores recent events for correlation
type EventCache struct {
	mu     sync.RWMutex
	events map[string]*CachedEvent
	ttl    time.Duration
}

type CachedEvent struct {
	Event     *domain.UnifiedEvent
	Timestamp time.Time
}

// NewK8sCorrelator creates a new K8s correlator using domain interfaces
func NewK8sCorrelator(logger *zap.Logger, k8sClient domain.K8sClient) *K8sCorrelator {
	return &K8sCorrelator{
		logger:    logger,
		k8sClient: k8sClient,
		ownerCache: &OwnershipCache{
			items: make(map[string]*OwnershipInfo),
		},
		selectorCache: &SelectorCache{
			selectors: make(map[string]SelectorInfo),
		},
		eventCache: &EventCache{
			events: make(map[string]*CachedEvent),
			ttl:    5 * time.Minute,
		},
	}
}

// Start initializes the K8s watchers
func (k *K8sCorrelator) Start(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.started {
		return nil
	}

	// Create cancellable context
	watchCtx, cancel := context.WithCancel(ctx)
	k.cancelFunc = cancel

	// Start pod watcher
	podWatcher, err := k.k8sClient.WatchPods(watchCtx, "")
	if err != nil {
		return fmt.Errorf("failed to start pod watcher: %w", err)
	}
	k.podWatcher = podWatcher

	// Start service watcher
	serviceWatcher, err := k.k8sClient.WatchServices(watchCtx, "")
	if err != nil {
		return fmt.Errorf("failed to start service watcher: %w", err)
	}
	k.serviceWatcher = serviceWatcher

	// Start processing watchers
	go k.processPodEvents(watchCtx)
	go k.processServiceEvents(watchCtx)

	// Periodically clean up caches
	go k.cleanupLoop(watchCtx)

	k.started = true
	k.logger.Info("K8s correlator started")

	return nil
}

// Stop shuts down the K8s correlator
func (k *K8sCorrelator) Stop() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.started {
		return nil
	}

	if k.cancelFunc != nil {
		k.cancelFunc()
	}

	k.started = false
	k.logger.Info("K8s correlator stopped")

	return nil
}

// Name returns the correlator name
func (k *K8sCorrelator) Name() string {
	return "k8s"
}

// Process analyzes an event and returns K8s-based correlations
func (k *K8sCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Skip if not K8s event
	if event.K8sContext == nil {
		return nil, nil
	}

	// Cache the event
	k.cacheEvent(event)

	var results []*CorrelationResult

	// Find owner chain correlations
	if owners := k.findOwnerChain(event); len(owners) > 0 {
		results = append(results, &CorrelationResult{
			Type:       "ownership",
			Confidence: 0.8,
			Related:    k.ownersToEvents(owners),
			Message:    fmt.Sprintf("Found %d owner relationships", len(owners)),
		})
	}

	// Find selector-based correlations
	if related := k.findSelectorMatches(event); len(related) > 0 {
		results = append(results, &CorrelationResult{
			Type:       "selector",
			Confidence: 0.7,
			Related:    related,
			Message:    fmt.Sprintf("Found %d selector matches", len(related)),
		})
	}

	// Find namespace correlations
	if nsEvents := k.findNamespaceEvents(event); len(nsEvents) > 0 {
		results = append(results, &CorrelationResult{
			Type:       "namespace",
			Confidence: 0.6,
			Related:    nsEvents,
			Message:    fmt.Sprintf("Found %d namespace events", len(nsEvents)),
		})
	}

	return results, nil
}

// processPodEvents processes pod watch events
func (k *K8sCorrelator) processPodEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-k.podWatcher:
			k.handlePodEvent(event)
		}
	}
}

// processServiceEvents processes service watch events
func (k *K8sCorrelator) processServiceEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-k.serviceWatcher:
			k.handleServiceEvent(event)
		}
	}
}

// handlePodEvent processes a pod watch event
func (k *K8sCorrelator) handlePodEvent(event domain.K8sWatchEvent) {
	pod, ok := event.Object.(*domain.K8sPod)
	if !ok {
		return
	}

	// Update ownership cache
	k.updateOwnershipForPod(pod)

	// Create unified event for the pod change
	unifiedEvent := k.podToUnifiedEvent(pod, event.Type)
	if unifiedEvent != nil {
		k.cacheEvent(unifiedEvent)
	}
}

// handleServiceEvent processes a service watch event
func (k *K8sCorrelator) handleServiceEvent(event domain.K8sWatchEvent) {
	service, ok := event.Object.(*domain.K8sService)
	if !ok {
		return
	}

	// Update selector cache
	k.updateSelectorForService(service)

	// Create unified event for the service change
	unifiedEvent := k.serviceToUnifiedEvent(service, event.Type)
	if unifiedEvent != nil {
		k.cacheEvent(unifiedEvent)
	}
}

// updateOwnershipForPod updates the ownership cache for a pod
func (k *K8sCorrelator) updateOwnershipForPod(pod *domain.K8sPod) {
	k.ownerCache.mu.Lock()
	defer k.ownerCache.mu.Unlock()

	key := fmt.Sprintf("%s/Pod/%s", pod.Namespace, pod.Name)
	info := &OwnershipInfo{
		Owners: make([]ResourceRef, 0),
	}

	// Add owner references
	for _, owner := range pod.OwnerReferences {
		info.Owners = append(info.Owners, ResourceRef{
			Kind:      owner.Kind,
			Name:      owner.Name,
			Namespace: pod.Namespace,
			UID:       owner.UID,
		})

		// Update parent's children
		parentKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, owner.Kind, owner.Name)
		if parentInfo, exists := k.ownerCache.items[parentKey]; exists {
			// Check if child already exists
			childExists := false
			for _, child := range parentInfo.Children {
				if child.Name == pod.Name && child.Kind == "Pod" {
					childExists = true
					break
				}
			}
			if !childExists {
				parentInfo.Children = append(parentInfo.Children, ResourceRef{
					Kind:      "Pod",
					Name:      pod.Name,
					Namespace: pod.Namespace,
					UID:       pod.UID,
				})
			}
		} else {
			k.ownerCache.items[parentKey] = &OwnershipInfo{
				Children: []ResourceRef{{
					Kind:      "Pod",
					Name:      pod.Name,
					Namespace: pod.Namespace,
					UID:       pod.UID,
				}},
			}
		}
	}

	k.ownerCache.items[key] = info
}

// updateSelectorForService updates the selector cache for a service
func (k *K8sCorrelator) updateSelectorForService(service *domain.K8sService) {
	k.selectorCache.mu.Lock()
	defer k.selectorCache.mu.Unlock()

	key := fmt.Sprintf("%s/Service/%s", service.Namespace, service.Name)
	k.selectorCache.selectors[key] = SelectorInfo{
		Labels: service.Selector,
	}
}

// cacheEvent adds an event to the cache
func (k *K8sCorrelator) cacheEvent(event *domain.UnifiedEvent) {
	k.eventCache.mu.Lock()
	defer k.eventCache.mu.Unlock()

	key := k.eventKey(event)
	k.eventCache.events[key] = &CachedEvent{
		Event:     event,
		Timestamp: time.Now(),
	}
}

// eventKey generates a cache key for an event
func (k *K8sCorrelator) eventKey(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil {
		return fmt.Sprintf("%s/%s/%s/%s",
			event.K8sContext.Namespace,
			event.K8sContext.Kind,
			event.K8sContext.Name,
			event.ID)
	}
	return event.ID
}

// findOwnerChain finds the ownership chain for an event
func (k *K8sCorrelator) findOwnerChain(event *domain.UnifiedEvent) []ResourceRef {
	if event.K8sContext == nil {
		return nil
	}

	k.ownerCache.mu.RLock()
	defer k.ownerCache.mu.RUnlock()

	var chain []ResourceRef
	visited := make(map[string]bool)

	// Start from the current resource
	key := fmt.Sprintf("%s/%s/%s",
		event.K8sContext.Namespace,
		event.K8sContext.Kind,
		event.K8sContext.Name)

	// Traverse up the ownership chain
	for {
		if visited[key] {
			break // Cycle detected
		}
		visited[key] = true

		info, exists := k.ownerCache.items[key]
		if !exists || len(info.Owners) == 0 {
			break
		}

		// Add first owner to chain (controllers typically have single owner)
		owner := info.Owners[0]
		chain = append(chain, owner)

		// Move to parent
		key = fmt.Sprintf("%s/%s/%s", owner.Namespace, owner.Kind, owner.Name)
	}

	return chain
}

// findSelectorMatches finds resources matching the same selector
func (k *K8sCorrelator) findSelectorMatches(event *domain.UnifiedEvent) []*domain.UnifiedEvent {
	if event.K8sContext == nil {
		return nil
	}

	k.selectorCache.mu.RLock()
	defer k.selectorCache.mu.RUnlock()

	k.eventCache.mu.RLock()
	defer k.eventCache.mu.RUnlock()

	var matches []*domain.UnifiedEvent

	// Get labels from the event
	var eventLabels map[string]string
	if event.K8sContext.Labels != nil {
		eventLabels = event.K8sContext.Labels
	}

	// Find services that select this resource
	for key, selector := range k.selectorCache.selectors {
		if matchesSelector(eventLabels, selector.Labels) {
			// Find events for this service
			parts := strings.Split(key, "/")
			if len(parts) == 3 {
				for eventKey, cached := range k.eventCache.events {
					if strings.Contains(eventKey, parts[2]) { // Service name
						matches = append(matches, cached.Event)
					}
				}
			}
		}
	}

	return matches
}

// findNamespaceEvents finds recent events in the same namespace
func (k *K8sCorrelator) findNamespaceEvents(event *domain.UnifiedEvent) []*domain.UnifiedEvent {
	if event.K8sContext == nil || event.K8sContext.Namespace == "" {
		return nil
	}

	k.eventCache.mu.RLock()
	defer k.eventCache.mu.RUnlock()

	var nsEvents []*domain.UnifiedEvent
	cutoff := time.Now().Add(-k.eventCache.ttl)

	for _, cached := range k.eventCache.events {
		// Skip if too old
		if cached.Timestamp.Before(cutoff) {
			continue
		}

		// Check namespace match
		if cached.Event.K8sContext != nil &&
			cached.Event.K8sContext.Namespace == event.K8sContext.Namespace &&
			cached.Event.ID != event.ID {
			nsEvents = append(nsEvents, cached.Event)
		}
	}

	return nsEvents
}

// ownersToEvents converts owner references to unified events
func (k *K8sCorrelator) ownersToEvents(owners []ResourceRef) []*domain.UnifiedEvent {
	k.eventCache.mu.RLock()
	defer k.eventCache.mu.RUnlock()

	var events []*domain.UnifiedEvent
	for _, owner := range owners {
		key := fmt.Sprintf("%s/%s/%s", owner.Namespace, owner.Kind, owner.Name)
		for eventKey, cached := range k.eventCache.events {
			if strings.Contains(eventKey, key) {
				events = append(events, cached.Event)
			}
		}
	}

	return events
}

// podToUnifiedEvent converts a pod to a unified event
func (k *K8sCorrelator) podToUnifiedEvent(pod *domain.K8sPod, eventType domain.K8sWatchEventType) *domain.UnifiedEvent {
	severity := "info"
	message := fmt.Sprintf("Pod %s/%s %s", pod.Namespace, pod.Name, strings.ToLower(string(eventType)))

	// Determine severity based on pod phase and event type
	if eventType == domain.K8sWatchDeleted {
		severity = "warning"
	} else if pod.Phase == domain.PodFailed {
		severity = "error"
	} else if pod.Phase == domain.PodPending && pod.StartTime != nil {
		// Check if pod has been pending for too long
		if time.Since(*pod.StartTime) > 5*time.Minute {
			severity = "warning"
			message = fmt.Sprintf("Pod %s/%s pending for %v", pod.Namespace, pod.Name, time.Since(*pod.StartTime))
		}
	}

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("k8s-pod-%s-%s-%d", pod.Namespace, pod.Name, time.Now().Unix()),
		Type:      domain.EventType(fmt.Sprintf("k8s.pod.%s", strings.ToLower(string(eventType)))),
		Timestamp: time.Now(),
		Source:    "k8s-correlator",
		Severity:  domain.EventSeverity(severity),
		Message:   message,
		K8sContext: &domain.K8sContext{
			ClusterName: "", // Would need cluster info from config
			Namespace:   pod.Namespace,
			Kind:        "Pod",
			Name:        pod.Name,
			UID:         pod.UID,
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		},
		Attributes: map[string]interface{}{
			"phase":          string(pod.Phase),
			"ready":          pod.Ready,
			"restartCount":   pod.RestartCount,
			"nodeName":       pod.NodeName,
			"podIP":          pod.PodIP,
			"containerCount": len(pod.Containers),
		},
	}
}

// serviceToUnifiedEvent converts a service to a unified event
func (k *K8sCorrelator) serviceToUnifiedEvent(service *domain.K8sService, eventType domain.K8sWatchEventType) *domain.UnifiedEvent {
	severity := "info"
	message := fmt.Sprintf("Service %s/%s %s", service.Namespace, service.Name, strings.ToLower(string(eventType)))

	if eventType == domain.K8sWatchDeleted {
		severity = "warning"
	}

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("k8s-service-%s-%s-%d", service.Namespace, service.Name, time.Now().Unix()),
		Type:      domain.EventType(fmt.Sprintf("k8s.service.%s", strings.ToLower(string(eventType)))),
		Timestamp: time.Now(),
		Source:    "k8s-correlator",
		Severity:  domain.EventSeverity(severity),
		Message:   message,
		K8sContext: &domain.K8sContext{
			ClusterName: "", // Would need cluster info from config
			Namespace:   service.Namespace,
			Kind:        "Service",
			Name:        service.Name,
			UID:         service.UID,
			Labels:      service.Labels,
			Annotations: service.Annotations,
		},
		Attributes: map[string]interface{}{
			"type":      string(service.Type),
			"clusterIP": service.ClusterIP,
			"portCount": len(service.Ports),
			"selector":  service.Selector,
		},
	}
}

// cleanupLoop periodically cleans up old cache entries
func (k *K8sCorrelator) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			k.cleanupCache()
		}
	}
}

// cleanupCache removes old entries from the event cache
func (k *K8sCorrelator) cleanupCache() {
	k.eventCache.mu.Lock()
	defer k.eventCache.mu.Unlock()

	cutoff := time.Now().Add(-k.eventCache.ttl)
	for key, cached := range k.eventCache.events {
		if cached.Timestamp.Before(cutoff) {
			delete(k.eventCache.events, key)
		}
	}
}

// matchesSelector checks if labels match a selector
func matchesSelector(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for key, value := range selector {
		if labelValue, exists := labels[key]; !exists || labelValue != value {
			return false
		}
	}

	return true
}

// GetOwnerChain returns the ownership chain for a resource
func (k *K8sCorrelator) GetOwnerChain(namespace, kind, name string) []ResourceRef {
	k.ownerCache.mu.RLock()
	defer k.ownerCache.mu.RUnlock()

	var chain []ResourceRef
	visited := make(map[string]bool)
	key := fmt.Sprintf("%s/%s/%s", namespace, kind, name)

	for {
		if visited[key] {
			break
		}
		visited[key] = true

		info, exists := k.ownerCache.items[key]
		if !exists || len(info.Owners) == 0 {
			break
		}

		owner := info.Owners[0]
		chain = append(chain, owner)
		key = fmt.Sprintf("%s/%s/%s", owner.Namespace, owner.Kind, owner.Name)
	}

	return chain
}

// GetChildResources returns child resources for a parent
func (k *K8sCorrelator) GetChildResources(namespace, kind, name string) []ResourceRef {
	k.ownerCache.mu.RLock()
	defer k.ownerCache.mu.RUnlock()

	key := fmt.Sprintf("%s/%s/%s", namespace, kind, name)
	if info, exists := k.ownerCache.items[key]; exists {
		return info.Children
	}

	return []ResourceRef{}
}
