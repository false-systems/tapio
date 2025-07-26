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
	events map[string][]*EventRef
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

// EventRef is a lightweight reference to an event
type EventRef struct {
	Reason    string
	Message   string
	Object    string
	Timestamp time.Time
}

// NewK8sNativeCorrelator creates a new K8s-based correlator
func NewK8sNativeCorrelator(logger *zap.Logger) *K8sNativeCorrelator {
	return &K8sNativeCorrelator{
		logger: logger,
		ownerCache: &OwnershipCache{
			owners: make(map[string][]*ResourceRef),
			owned:  make(map[string]*ResourceRef),
		},
		selectorCache: &SelectorCache{
			selectors: make(map[string][]*ResourceRef),
			matches:   make(map[string][]string),
		},
		eventCache: &EventRelationCache{
			events: make(map[string][]*EventRef),
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
		// TODO: implement updateOwnerCache
	case "selector":
		// TODO: implement updateSelectorCache
	case "event":
		// TODO: implement updateEventCache
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
	// Simplified - real implementation would parse selector
	return true
}

func (c *K8sNativeCorrelator) isServiceIP(ip string) bool {
	// Check if IP is in service CIDR range
	// Simplified - real implementation would check actual range
	return strings.HasPrefix(ip, "10.96.")
}

func (c *K8sNativeCorrelator) getServiceByIP(ip string) *ResourceRef {
	// Lookup service by ClusterIP
	// Simplified - real implementation would query cache
	return &ResourceRef{
		Kind: "Service",
		Name: "example-service",
	}
}

// CacheUpdate represents an update to the correlation cache
type CacheUpdate struct {
	Type   string      // owner, selector, event
	Action string      // add, update, delete
	Data   interface{} // The actual update data
}
