package correlation

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// K8sNativeCorrelator extracts correlations from K8s structure
type K8sNativeCorrelator struct {
	logger *zap.Logger

	// Relationship loader that populates caches
	loader *K8sRelationshipLoader

	// Cache for K8s relationships
	ownerCache    *OwnershipCache
	selectorCache *SelectorCache
	eventCache    *EventRelationCache

	mu sync.RWMutex
}

// OwnershipCache tracks owner relationships
type OwnershipCache struct {
	// owner UID -> owned resources
	owners map[string][]*ResourceRef
	// resource UID -> owner
	owned map[string]*ResourceRef
	mu    sync.RWMutex
}

// SelectorCache tracks service/deployment selectors
type SelectorCache struct {
	// selector hash -> resources that match
	selectors map[string][]*ResourceRef
	// resource -> selectors it matches
	matches map[string][]string
	mu      sync.RWMutex
}

// EventRelationCache tracks K8s event relationships
type EventRelationCache struct {
	// involvedObject -> related events
	events map[string][]*K8sEventRef
	mu     sync.RWMutex
}

// ResourceRef is a lightweight reference to a K8s resource
type ResourceRef struct {
	Kind      string
	Namespace string
	Name      string
	UID       string
	Labels    map[string]string
}

// K8sEventRef is a K8s-specific event reference
type K8sEventRef struct {
	Reason    string
	Message   string
	Object    string
	Timestamp time.Time
}

// NewK8sNativeCorrelator creates a new K8s-based correlator
func NewK8sNativeCorrelator(logger *zap.Logger, loader *K8sRelationshipLoader) *K8sNativeCorrelator {
	// Get caches from loader
	ownerCache := loader.GetOwnershipCache()
	selectorCache := loader.GetSelectorCache()

	return &K8sNativeCorrelator{
		logger:        logger,
		loader:        loader,
		ownerCache:    ownerCache,
		selectorCache: selectorCache,
		eventCache: &EventRelationCache{
			events: make(map[string][]*K8sEventRef),
		},
	}
}

// FindCorrelations finds K8s-native correlations for an event
func (c *K8sNativeCorrelator) FindCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	// 1. Owner-based correlations
	if ownerCorr := c.findOwnerCorrelations(event); len(ownerCorr) > 0 {
		correlations = append(correlations, ownerCorr...)
	}

	// 2. Selector-based correlations
	if selectorCorr := c.findSelectorCorrelations(event); len(selectorCorr) > 0 {
		correlations = append(correlations, selectorCorr...)
	}

	// 3. Label-based correlations
	if labelCorr := c.findLabelCorrelations(event); len(labelCorr) > 0 {
		correlations = append(correlations, labelCorr...)
	}

	// 4. Event-based correlations
	if eventCorr := c.findEventCorrelations(event); len(eventCorr) > 0 {
		correlations = append(correlations, eventCorr...)
	}

	// 5. Network topology correlations
	if netCorr := c.findNetworkCorrelations(event); len(netCorr) > 0 {
		correlations = append(correlations, netCorr...)
	}

	return correlations
}

// K8sCorrelation represents a correlation found through K8s structure
type K8sCorrelation struct {
	Type       string      // owner, selector, label, event, network
	Source     ResourceRef // The source of correlation
	Target     ResourceRef // The target of correlation
	Confidence float64     // Always 1.0 for K8s native
	Reason     string      // Why they're correlated
	Direction  string      // source->target, target->source, bidirectional
}

// findOwnerCorrelations finds correlations through ownership
func (c *K8sNativeCorrelator) findOwnerCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	// Extract resource info from event
	resource := c.extractResourceRef(event)
	if resource == nil {
		return correlations
	}

	c.ownerCache.mu.RLock()
	defer c.ownerCache.mu.RUnlock()

	// Check if this resource owns others
	if owned, exists := c.ownerCache.owners[resource.UID]; exists {
		for _, ownedRes := range owned {
			correlations = append(correlations, K8sCorrelation{
				Type:       "ownership",
				Source:     *resource,
				Target:     *ownedRes,
				Confidence: 1.0,
				Reason:     fmt.Sprintf("%s/%s owns %s/%s", resource.Kind, resource.Name, ownedRes.Kind, ownedRes.Name),
				Direction:  "source->target",
			})
		}
	}

	// Check if this resource is owned
	if owner, exists := c.ownerCache.owned[resource.UID]; exists {
		correlations = append(correlations, K8sCorrelation{
			Type:       "ownership",
			Source:     *owner,
			Target:     *resource,
			Confidence: 1.0,
			Reason:     fmt.Sprintf("%s/%s is owned by %s/%s", resource.Kind, resource.Name, owner.Kind, owner.Name),
			Direction:  "target->source",
		})
	}

	return correlations
}

// findSelectorCorrelations finds service->pod correlations
func (c *K8sNativeCorrelator) findSelectorCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	resource := c.extractResourceRef(event)
	if resource == nil || resource.Labels == nil {
		return correlations
	}

	c.selectorCache.mu.RLock()
	defer c.selectorCache.mu.RUnlock()

	// Check if this resource matches any selectors
	for selectorHash, matchingResources := range c.selectorCache.selectors {
		if c.matchesSelector(resource.Labels, selectorHash) {
			for _, service := range matchingResources {
				if service.Kind == "Service" {
					correlations = append(correlations, K8sCorrelation{
						Type:       "selector",
						Source:     *service,
						Target:     *resource,
						Confidence: 1.0,
						Reason:     fmt.Sprintf("Service %s selects pod %s", service.Name, resource.Name),
						Direction:  "bidirectional",
					})
				}
			}
		}
	}

	return correlations
}

// findLabelCorrelations finds resources with matching labels
func (c *K8sNativeCorrelator) findLabelCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	resource := c.extractResourceRef(event)
	if resource == nil || resource.Labels == nil {
		return correlations
	}

	// Key labels that indicate strong correlation
	importantLabels := []string{"app", "component", "tier", "version"}

	for _, label := range importantLabels {
		if value, exists := resource.Labels[label]; exists {
			// In a real implementation, we'd look up other resources with this label
			// For now, we'll create a placeholder
			correlations = append(correlations, K8sCorrelation{
				Type:       "label",
				Source:     *resource,
				Target:     ResourceRef{}, // Would be filled by lookup
				Confidence: 0.8,           // Less than ownership but still high
				Reason:     fmt.Sprintf("Share label %s=%s", label, value),
				Direction:  "bidirectional",
			})
		}
	}

	return correlations
}

// findEventCorrelations finds correlations through K8s events
func (c *K8sNativeCorrelator) findEventCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	if event.Kubernetes == nil {
		return correlations
	}

	c.eventCache.mu.RLock()
	defer c.eventCache.mu.RUnlock()

	// Check for related events on the same object
	if relatedEvents, exists := c.eventCache.events[event.Kubernetes.Object]; exists {
		for _, related := range relatedEvents {
			// Events within 5 minutes are likely correlated
			if math.Abs(event.Timestamp.Sub(related.Timestamp).Minutes()) < 5 {
				correlations = append(correlations, K8sCorrelation{
					Type: "event-sequence",
					Source: ResourceRef{
						Name: related.Object,
					},
					Target: ResourceRef{
						Name: event.Kubernetes.Object,
					},
					Confidence: 0.9,
					Reason:     fmt.Sprintf("Event sequence: %s -> %s", related.Reason, event.Kubernetes.Reason),
					Direction:  "source->target",
				})
			}
		}
	}

	return correlations
}

// findNetworkCorrelations finds network-based correlations
func (c *K8sNativeCorrelator) findNetworkCorrelations(event *domain.UnifiedEvent) []K8sCorrelation {
	correlations := []K8sCorrelation{}

	if event.Network == nil {
		return correlations
	}

	// Service ClusterIP correlation
	if c.isServiceIP(event.Network.DestIP) {
		service := c.getServiceByIP(event.Network.DestIP)
		if service != nil {
			correlations = append(correlations, K8sCorrelation{
				Type:       "network",
				Source:     *c.extractResourceRef(event),
				Target:     *service,
				Confidence: 1.0,
				Reason:     fmt.Sprintf("Network connection to service %s", service.Name),
				Direction:  "source->target",
			})
		}
	}

	return correlations
}

// UpdateCache updates the correlation caches with new K8s data
func (c *K8sNativeCorrelator) UpdateCache(update CacheUpdate) {
	switch update.Type {
	case "owner":
		if data, ok := update.Data.(OwnershipUpdate); ok {
			c.updateOwnerCache(data)
		}
	case "selector":
		if data, ok := update.Data.(SelectorUpdate); ok {
			c.updateSelectorCache(data)
		}
	case "event":
		if data, ok := update.Data.(EventUpdate); ok {
			c.updateEventCache(data)
		}
	}
}

// updateOwnerCache updates ownership relationships
func (c *K8sNativeCorrelator) updateOwnerCache(update OwnershipUpdate) {
	c.ownerCache.mu.Lock()
	defer c.ownerCache.mu.Unlock()

	switch update.Action {
	case "add":
		c.ownerCache.owners[update.Owner.UID] = append(c.ownerCache.owners[update.Owner.UID], &update.Owned)
		c.ownerCache.owned[update.Owned.UID] = &update.Owner
	case "delete":
		// Remove owned from owner's list
		if owned, ok := c.ownerCache.owners[update.Owner.UID]; ok {
			var filtered []*ResourceRef
			for _, o := range owned {
				if o.UID != update.Owned.UID {
					filtered = append(filtered, o)
				}
			}
			c.ownerCache.owners[update.Owner.UID] = filtered
		}
		delete(c.ownerCache.owned, update.Owned.UID)
	}
}

// updateSelectorCache updates selector relationships
func (c *K8sNativeCorrelator) updateSelectorCache(update SelectorUpdate) {
	c.selectorCache.mu.Lock()
	defer c.selectorCache.mu.Unlock()

	selectorKey := makeSelectorKey(update.Selector)

	switch update.Action {
	case "add":
		c.selectorCache.selectors[selectorKey] = append(c.selectorCache.selectors[selectorKey], &update.Resource)
		c.selectorCache.matches[update.Resource.UID] = append(c.selectorCache.matches[update.Resource.UID], selectorKey)
	case "delete":
		// Remove from selectors
		if resources, ok := c.selectorCache.selectors[selectorKey]; ok {
			var filtered []*ResourceRef
			for _, r := range resources {
				if r.UID != update.Resource.UID {
					filtered = append(filtered, r)
				}
			}
			c.selectorCache.selectors[selectorKey] = filtered
		}
		delete(c.selectorCache.matches, update.Resource.UID)
	}
}

// updateEventCache updates event relationships
func (c *K8sNativeCorrelator) updateEventCache(update EventUpdate) {
	c.eventCache.mu.Lock()
	defer c.eventCache.mu.Unlock()

	switch update.Action {
	case "add":
		c.eventCache.events[update.Object] = append(c.eventCache.events[update.Object], &update.Event)
	case "delete":
		// Clean up old events
		if events, ok := c.eventCache.events[update.Object]; ok {
			var filtered []*K8sEventRef
			for _, e := range events {
				if e.Timestamp.After(time.Now().Add(-30 * time.Minute)) {
					filtered = append(filtered, e)
				}
			}
			c.eventCache.events[update.Object] = filtered
		}
	}
}

// Helper methods

func (c *K8sNativeCorrelator) extractResourceRef(event *domain.UnifiedEvent) *ResourceRef {
	if event.Entity != nil {
		return &ResourceRef{
			Kind:      event.Entity.Type,
			Namespace: event.Entity.Namespace,
			Name:      event.Entity.Name,
			UID:       event.Entity.UID,
			Labels:    event.Entity.Labels,
		}
	}

	if event.Kubernetes != nil && event.Kubernetes.Object != "" {
		parts := strings.Split(event.Kubernetes.Object, "/")
		if len(parts) >= 2 {
			return &ResourceRef{
				Kind: parts[0],
				Name: parts[1],
			}
		}
	}

	return nil
}

func (c *K8sNativeCorrelator) matchesSelector(labels map[string]string, selectorHash string) bool {
	if labels == nil {
		return false
	}

	// Parse the selector hash back to a map
	selector := parseSelectorKey(selectorHash)
	if len(selector) == 0 {
		return false
	}

	// Check if all selector labels match
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}

	return true
}

func (c *K8sNativeCorrelator) isServiceIP(ip string) bool {
	// Check if IP is in service CIDR range
	// Simplified - real implementation would check actual range
	return strings.HasPrefix(ip, "10.96.")
}

func (c *K8sNativeCorrelator) getServiceByIP(ip string) *ResourceRef {
	// In production, this would look up services by their ClusterIP
	// For now, return nil to indicate no service found
	return nil
}

// CacheUpdate represents an update to the correlation cache
type CacheUpdate struct {
	Type   string      // owner, selector, event
	Action string      // add, update, delete
	Data   interface{} // The actual update data
}

// OwnershipUpdate represents an ownership relationship update
type OwnershipUpdate struct {
	Action string
	Owner  ResourceRef
	Owned  ResourceRef
}

// SelectorUpdate represents a selector relationship update
type SelectorUpdate struct {
	Action   string
	Resource ResourceRef
	Selector map[string]string
}

// EventUpdate represents a K8s event update
type EventUpdate struct {
	Action string
	Object string
	Event  K8sEventRef
}
