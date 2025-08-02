package correlation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// K8sCorrelator finds correlations based on Kubernetes relationships
type K8sCorrelator struct {
	logger          *zap.Logger
	clientset       kubernetes.Interface
	informerFactory informers.SharedInformerFactory

	// Caches for fast lookup
	ownerCache    *OwnershipCache
	selectorCache *SelectorCache

	// Event cache for correlation
	eventCache *EventCache

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
	selectors map[string]labels.Selector // key: namespace/kind/name
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

// NewK8sCorrelator creates a new K8s correlator
func NewK8sCorrelator(logger *zap.Logger, clientset kubernetes.Interface) *K8sCorrelator {
	informerFactory := informers.NewSharedInformerFactory(clientset, 30*time.Second)

	return &K8sCorrelator{
		logger:          logger,
		clientset:       clientset,
		informerFactory: informerFactory,
		ownerCache: &OwnershipCache{
			items: make(map[string]*OwnershipInfo),
		},
		selectorCache: &SelectorCache{
			selectors: make(map[string]labels.Selector),
		},
		eventCache: &EventCache{
			events: make(map[string]*CachedEvent),
			ttl:    5 * time.Minute,
		},
	}
}

// Start initializes the K8s informers
func (k *K8sCorrelator) Start(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.started {
		return nil
	}

	// Set up informers for key resources
	deployInformer := k.informerFactory.Apps().V1().Deployments().Informer()
	rsInformer := k.informerFactory.Apps().V1().ReplicaSets().Informer()
	podInformer := k.informerFactory.Core().V1().Pods().Informer()
	svcInformer := k.informerFactory.Core().V1().Services().Informer()

	// Add event handlers
	deployInformer.AddEventHandler(k.createResourceEventHandler("Deployment"))
	rsInformer.AddEventHandler(k.createResourceEventHandler("ReplicaSet"))
	podInformer.AddEventHandler(k.createResourceEventHandler("Pod"))
	svcInformer.AddEventHandler(k.createResourceEventHandler("Service"))

	// Start informers
	k.informerFactory.Start(ctx.Done())

	// Wait for caches to sync
	if !cache.WaitForCacheSync(ctx.Done(),
		deployInformer.HasSynced,
		rsInformer.HasSynced,
		podInformer.HasSynced,
		svcInformer.HasSynced,
	) {
		return fmt.Errorf("failed to sync caches")
	}

	k.started = true

	// Start cleanup routine
	go k.cleanupRoutine(ctx)

	return nil
}

// Process implements the Correlator interface
func (k *K8sCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	if event.K8sContext == nil {
		return nil, nil
	}

	// Cache the event
	k.cacheEvent(event)

	var results []*CorrelationResult

	// Check ownership correlations
	if ownershipResults := k.findOwnershipCorrelations(event); len(ownershipResults) > 0 {
		results = append(results, ownershipResults...)
	}

	// Check selector-based correlations
	if selectorResults := k.findSelectorCorrelations(event); len(selectorResults) > 0 {
		results = append(results, selectorResults...)
	}

	// Check cross-resource correlations
	if crossResults := k.findCrossResourceCorrelations(event); len(crossResults) > 0 {
		results = append(results, crossResults...)
	}

	return results, nil
}

// Name returns the correlator name
func (k *K8sCorrelator) Name() string {
	return "k8s_native"
}

// findOwnershipCorrelations finds parent-child relationships
func (k *K8sCorrelator) findOwnershipCorrelations(event *domain.UnifiedEvent) []*CorrelationResult {
	k8sCtx := event.K8sContext
	key := makeResourceKey(k8sCtx.Namespace, k8sCtx.Kind, k8sCtx.Name)

	k.ownerCache.mu.RLock()
	ownership, exists := k.ownerCache.items[key]
	k.ownerCache.mu.RUnlock()

	if !exists || ownership == nil {
		return nil
	}

	var results []*CorrelationResult

	// Check for parent events
	for _, owner := range ownership.Owners {
		if relatedEvents := k.findEventsForResource(owner); len(relatedEvents) > 0 {
			result := &CorrelationResult{
				ID:         fmt.Sprintf("k8s-owner-%s-%d", event.ID, time.Now().UnixNano()),
				Type:       "k8s_ownership",
				Confidence: 1.0, // K8s relationships are definitive
				Events:     append([]string{event.ID}, getEventIDs(relatedEvents)...),
				Summary:    fmt.Sprintf("%s %s is owned by %s %s", k8sCtx.Kind, k8sCtx.Name, owner.Kind, owner.Name),
				Details:    fmt.Sprintf("Event in %s/%s is related to its owner %s/%s through Kubernetes ownership", k8sCtx.Kind, k8sCtx.Name, owner.Kind, owner.Name),
				Evidence:   []string{fmt.Sprintf("OwnerReference: %s/%s", owner.Kind, owner.Name)},
				StartTime:  event.Timestamp,
				EndTime:    event.Timestamp,
			}

			// Determine root cause
			result.RootCause = k.determineRootCause(event, relatedEvents)
			result.Impact = k.assessImpact(event, relatedEvents)

			results = append(results, result)
		}
	}

	// Check for child events
	for _, child := range ownership.Children {
		if relatedEvents := k.findEventsForResource(child); len(relatedEvents) > 0 {
			result := &CorrelationResult{
				ID:         fmt.Sprintf("k8s-child-%s-%d", event.ID, time.Now().UnixNano()),
				Type:       "k8s_ownership",
				Confidence: 1.0,
				Events:     append([]string{event.ID}, getEventIDs(relatedEvents)...),
				Summary:    fmt.Sprintf("%s %s owns %s %s", k8sCtx.Kind, k8sCtx.Name, child.Kind, child.Name),
				Details:    fmt.Sprintf("Event in %s/%s affects its child %s/%s through Kubernetes ownership", k8sCtx.Kind, k8sCtx.Name, child.Kind, child.Name),
				Evidence:   []string{fmt.Sprintf("Child resource: %s/%s", child.Kind, child.Name)},
				StartTime:  event.Timestamp,
				EndTime:    event.Timestamp,
			}

			result.RootCause = k.determineRootCause(event, relatedEvents)
			result.Impact = k.assessImpact(event, relatedEvents)

			results = append(results, result)
		}
	}

	return results
}

// findSelectorCorrelations finds service-pod relationships
func (k *K8sCorrelator) findSelectorCorrelations(event *domain.UnifiedEvent) []*CorrelationResult {
	k8sCtx := event.K8sContext

	// Only relevant for Services and Pods
	if k8sCtx.Kind != "Service" && k8sCtx.Kind != "Pod" {
		return nil
	}

	var results []*CorrelationResult

	if k8sCtx.Kind == "Service" {
		// Find pods matching this service
		key := makeResourceKey(k8sCtx.Namespace, k8sCtx.Kind, k8sCtx.Name)

		k.selectorCache.mu.RLock()
		selector, exists := k.selectorCache.selectors[key]
		k.selectorCache.mu.RUnlock()

		if exists && selector != nil {
			// Find pods with matching labels
			if matchingPods := k.findPodsMatchingSelector(k8sCtx.Namespace, selector); len(matchingPods) > 0 {
				result := &CorrelationResult{
					ID:         fmt.Sprintf("k8s-selector-%s-%d", event.ID, time.Now().UnixNano()),
					Type:       "k8s_selector",
					Confidence: 1.0,
					Events:     []string{event.ID},
					Summary:    fmt.Sprintf("Service %s selects %d pods", k8sCtx.Name, len(matchingPods)),
					Details:    fmt.Sprintf("Service %s/%s is experiencing issues which affects %d selected pods", k8sCtx.Namespace, k8sCtx.Name, len(matchingPods)),
					Evidence:   []string{fmt.Sprintf("Selector: %s", selector.String())},
					StartTime:  event.Timestamp,
					EndTime:    event.Timestamp,
				}

				result.Impact = &Impact{
					Severity:  event.Severity,
					Resources: matchingPods,
					Services:  []string{k8sCtx.Name},
				}

				results = append(results, result)
			}
		}
	}

	return results
}

// findCrossResourceCorrelations finds correlations across resource types
func (k *K8sCorrelator) findCrossResourceCorrelations(event *domain.UnifiedEvent) []*CorrelationResult {
	k8sCtx := event.K8sContext

	// Look for related events in the same namespace within time window
	k.eventCache.mu.RLock()
	defer k.eventCache.mu.RUnlock()

	var relatedEvents []*domain.UnifiedEvent
	cutoff := event.Timestamp.Add(-30 * time.Second)

	for _, cached := range k.eventCache.events {
		if cached.Event.ID == event.ID {
			continue
		}

		if cached.Event.K8sContext != nil &&
			cached.Event.K8sContext.Namespace == k8sCtx.Namespace &&
			cached.Event.Timestamp.After(cutoff) {

			// Check if resources are related
			if k.areResourcesRelated(event, cached.Event) {
				relatedEvents = append(relatedEvents, cached.Event)
			}
		}
	}

	if len(relatedEvents) == 0 {
		return nil
	}

	// Group by correlation pattern
	result := &CorrelationResult{
		ID:         fmt.Sprintf("k8s-cross-%s-%d", event.ID, time.Now().UnixNano()),
		Type:       "k8s_cascade",
		Confidence: 0.85,
		Events:     append([]string{event.ID}, getEventIDs(relatedEvents)...),
		Summary:    fmt.Sprintf("Cascade failure detected across %d resources", len(relatedEvents)+1),
		Details:    k.buildCascadeDescription(event, relatedEvents),
		Evidence:   k.buildCascadeEvidence(event, relatedEvents),
		StartTime:  event.Timestamp,
		EndTime:    event.Timestamp,
	}

	result.RootCause = k.determineRootCause(event, relatedEvents)
	result.Impact = k.assessImpact(event, relatedEvents)

	return []*CorrelationResult{result}
}

// Helper methods

func (k *K8sCorrelator) createResourceEventHandler(kind string) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			k.updateCachesFromObject(obj, kind)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			k.updateCachesFromObject(newObj, kind)
		},
		DeleteFunc: func(obj interface{}) {
			k.removeCachesFromObject(obj, kind)
		},
	}
}

func (k *K8sCorrelator) cacheEvent(event *domain.UnifiedEvent) {
	k.eventCache.mu.Lock()
	defer k.eventCache.mu.Unlock()

	k.eventCache.events[event.ID] = &CachedEvent{
		Event:     event,
		Timestamp: time.Now(),
	}
}

func (k *K8sCorrelator) findEventsForResource(ref ResourceRef) []*domain.UnifiedEvent {
	k.eventCache.mu.RLock()
	defer k.eventCache.mu.RUnlock()

	var events []*domain.UnifiedEvent
	for _, cached := range k.eventCache.events {
		if cached.Event.K8sContext != nil &&
			cached.Event.K8sContext.Kind == ref.Kind &&
			cached.Event.K8sContext.Name == ref.Name &&
			cached.Event.K8sContext.Namespace == ref.Namespace {
			events = append(events, cached.Event)
		}
	}

	return events
}

func (k *K8sCorrelator) determineRootCause(current *domain.UnifiedEvent, related []*domain.UnifiedEvent) *RootCause {
	// Find the earliest event with highest severity
	rootEvent := current
	for _, event := range related {
		if event.Timestamp.Before(rootEvent.Timestamp) ||
			(event.Timestamp.Equal(rootEvent.Timestamp) && event.Severity > rootEvent.Severity) {
			rootEvent = event
		}
	}

	return &RootCause{
		EventID:     rootEvent.ID,
		Confidence:  0.9,
		Description: fmt.Sprintf("%s in %s/%s", rootEvent.Type, rootEvent.K8sContext.Kind, rootEvent.K8sContext.Name),
		Evidence:    []string{fmt.Sprintf("First occurrence at %s", rootEvent.Timestamp.Format(time.RFC3339))},
	}
}

func (k *K8sCorrelator) assessImpact(current *domain.UnifiedEvent, related []*domain.UnifiedEvent) *Impact {
	impact := &Impact{
		Severity:  current.Severity,
		Resources: make([]string, 0),
		Services:  make([]string, 0),
	}

	// Collect affected resources
	resourceMap := make(map[string]bool)
	serviceMap := make(map[string]bool)

	allEvents := append(related, current)
	for _, event := range allEvents {
		if event.K8sContext != nil {
			resource := fmt.Sprintf("%s/%s/%s", event.K8sContext.Kind, event.K8sContext.Namespace, event.K8sContext.Name)
			resourceMap[resource] = true

			if event.K8sContext.Kind == "Service" || event.K8sContext.Kind == "Deployment" {
				serviceMap[event.K8sContext.Name] = true
			}
		}

		// Upgrade severity if needed
		if event.Severity > impact.Severity {
			impact.Severity = event.Severity
		}
	}

	for resource := range resourceMap {
		impact.Resources = append(impact.Resources, resource)
	}

	for service := range serviceMap {
		impact.Services = append(impact.Services, service)
	}

	return impact
}

func (k *K8sCorrelator) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			k.cleanupOldEvents()
		}
	}
}

func (k *K8sCorrelator) cleanupOldEvents() {
	k.eventCache.mu.Lock()
	defer k.eventCache.mu.Unlock()

	cutoff := time.Now().Add(-k.eventCache.ttl)
	for id, cached := range k.eventCache.events {
		if cached.Timestamp.Before(cutoff) {
			delete(k.eventCache.events, id)
		}
	}
}

// Utility functions

func makeResourceKey(namespace, kind, name string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, kind, name)
}

func getEventIDs(events []*domain.UnifiedEvent) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}

func (k *K8sCorrelator) areResourcesRelated(e1, e2 *domain.UnifiedEvent) bool {
	// Check if they share common labels
	if e1.K8sContext.Labels != nil && e2.K8sContext.Labels != nil {
		for key, val1 := range e1.K8sContext.Labels {
			if val2, exists := e2.K8sContext.Labels[key]; exists && val1 == val2 {
				// Skip common labels that don't indicate relationship
				if key != "kubernetes.io/metadata.name" && !strings.HasPrefix(key, "pod-template-hash") {
					return true
				}
			}
		}
	}

	// Check namespace events affect all resources in namespace
	if e1.K8sContext.Kind == "Namespace" || e2.K8sContext.Kind == "Namespace" {
		return true
	}

	// Check node events affect pods on that node
	if e1.K8sContext.Kind == "Node" && e2.K8sContext.Kind == "Pod" {
		return true // Would need node info to be more precise
	}

	return false
}

func (k *K8sCorrelator) buildCascadeDescription(root *domain.UnifiedEvent, related []*domain.UnifiedEvent) string {
	return fmt.Sprintf("A cascade of failures started with %s in %s/%s, affecting %d other resources in the namespace",
		root.Type, root.K8sContext.Kind, root.K8sContext.Name, len(related))
}

func (k *K8sCorrelator) buildCascadeEvidence(root *domain.UnifiedEvent, related []*domain.UnifiedEvent) []string {
	evidence := []string{
		fmt.Sprintf("Root event: %s at %s", root.Type, root.Timestamp.Format(time.RFC3339)),
		fmt.Sprintf("Affected resources: %d", len(related)),
	}

	// Add sample of affected resources
	for i, event := range related {
		if i >= 3 {
			evidence = append(evidence, fmt.Sprintf("... and %d more", len(related)-3))
			break
		}
		evidence = append(evidence, fmt.Sprintf("- %s/%s: %s", event.K8sContext.Kind, event.K8sContext.Name, event.Type))
	}

	return evidence
}

func (k *K8sCorrelator) findPodsMatchingSelector(namespace string, selector labels.Selector) []string {
	// This would use the pod informer to find matching pods
	// For now, return empty
	return []string{}
}

func (k *K8sCorrelator) updateCachesFromObject(obj interface{}, kind string) {
	// This would extract metadata and update caches
	// Implementation depends on specific K8s types
}

func (k *K8sCorrelator) removeCachesFromObject(obj interface{}, kind string) {
	// This would remove entries from caches
	// Implementation depends on specific K8s types
}
