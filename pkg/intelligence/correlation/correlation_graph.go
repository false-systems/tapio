package correlation

import (
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationGraph stores events and provides efficient multi-dimensional lookups
type CorrelationGraph struct {
	mu sync.RWMutex

	// Event storage
	events map[string]*domain.UnifiedEvent

	// Time-based index
	timeIndex *TimeIndex

	// K8s indexes
	workloadIndex   map[string][]*domain.UnifiedEvent // workload_kind/name/namespace -> events
	nodeIndex       map[string][]*domain.UnifiedEvent // node_name -> events
	namespaceIndex  map[string][]*domain.UnifiedEvent // namespace -> events
	zoneIndex       map[string][]*domain.UnifiedEvent // zone -> events
	labelIndex      map[string][]*domain.UnifiedEvent // label_key:value:namespace -> events
	ownerIndex      map[string][]*domain.UnifiedEvent // owner_kind/name/namespace -> events
	serviceIndex    map[string][]*domain.UnifiedEvent // service_name/namespace -> events
	dependencyIndex map[string][]*domain.UnifiedEvent // dep_kind/name/namespace -> events

	// Semantic indexes
	semanticIndex map[string][]*domain.UnifiedEvent // intent -> events
	categoryIndex map[string][]*domain.UnifiedEvent // category -> events
	domainIndex   map[string][]*domain.UnifiedEvent // domain -> events

	// Configuration
	maxEvents     int
	retentionTime time.Duration

	// Stats
	totalEvents   int64
	evictionCount int64
}

// TimeIndex provides efficient time-based queries
type TimeIndex struct {
	mu      sync.RWMutex
	buckets map[int64][]*domain.UnifiedEvent // Unix timestamp (minute precision) -> events
	minTime time.Time
	maxTime time.Time
}

// NewCorrelationGraph creates a new correlation graph
func NewCorrelationGraph() *CorrelationGraph {
	return &CorrelationGraph{
		events:          make(map[string]*domain.UnifiedEvent),
		timeIndex:       NewTimeIndex(),
		workloadIndex:   make(map[string][]*domain.UnifiedEvent),
		nodeIndex:       make(map[string][]*domain.UnifiedEvent),
		namespaceIndex:  make(map[string][]*domain.UnifiedEvent),
		zoneIndex:       make(map[string][]*domain.UnifiedEvent),
		labelIndex:      make(map[string][]*domain.UnifiedEvent),
		ownerIndex:      make(map[string][]*domain.UnifiedEvent),
		serviceIndex:    make(map[string][]*domain.UnifiedEvent),
		dependencyIndex: make(map[string][]*domain.UnifiedEvent),
		semanticIndex:   make(map[string][]*domain.UnifiedEvent),
		categoryIndex:   make(map[string][]*domain.UnifiedEvent),
		domainIndex:     make(map[string][]*domain.UnifiedEvent),
		maxEvents:       10000,
		retentionTime:   1 * time.Hour,
	}
}

// AddEvent adds an event to the graph
func (g *CorrelationGraph) AddEvent(event *domain.UnifiedEvent) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check if we need to evict old events
	if len(g.events) >= g.maxEvents {
		g.evictOldEvents()
	}

	// Store event
	g.events[event.ID] = event
	g.totalEvents++

	// Index by time
	g.timeIndex.Add(event)

	// Index by K8s context
	if event.K8sContext != nil {
		k8s := event.K8sContext

		// Workload index
		if k8s.WorkloadName != "" {
			key := makeKey(k8s.WorkloadKind, k8s.WorkloadName, k8s.Namespace)
			g.workloadIndex[key] = append(g.workloadIndex[key], event)
		}

		// Node index
		if k8s.NodeName != "" {
			g.nodeIndex[k8s.NodeName] = append(g.nodeIndex[k8s.NodeName], event)
		}

		// Namespace index
		g.namespaceIndex[k8s.Namespace] = append(g.namespaceIndex[k8s.Namespace], event)

		// Zone index
		if k8s.Zone != "" {
			g.zoneIndex[k8s.Zone] = append(g.zoneIndex[k8s.Zone], event)
		}

		// Label index
		for k, v := range k8s.Labels {
			labelKey := makeLabelKey(k, v, k8s.Namespace)
			g.labelIndex[labelKey] = append(g.labelIndex[labelKey], event)
		}

		// Owner index
		for _, owner := range k8s.OwnerReferences {
			ownerKey := makeKey(owner.Kind, owner.Name, k8s.Namespace)
			g.ownerIndex[ownerKey] = append(g.ownerIndex[ownerKey], event)
		}

		// Service index (from consumers)
		for _, consumer := range k8s.Consumers {
			if consumer.Kind == "Service" {
				svcKey := makeServiceKey(consumer.Name, consumer.Namespace)
				g.serviceIndex[svcKey] = append(g.serviceIndex[svcKey], event)
			}
		}

		// Dependency index
		for _, dep := range k8s.Dependencies {
			depKey := makeKey(dep.Kind, dep.Name, dep.Namespace)
			g.dependencyIndex[depKey] = append(g.dependencyIndex[depKey], event)
		}
	}

	// Index by semantic context
	if event.Semantic != nil {
		if event.Semantic.Intent != "" {
			g.semanticIndex[event.Semantic.Intent] = append(g.semanticIndex[event.Semantic.Intent], event)
		}
		if event.Semantic.Category != "" {
			g.categoryIndex[event.Semantic.Category] = append(g.categoryIndex[event.Semantic.Category], event)
		}
		if event.Semantic.Domain != "" {
			g.domainIndex[event.Semantic.Domain] = append(g.domainIndex[event.Semantic.Domain], event)
		}
	}
}

// GetEvent retrieves a single event by ID
func (g *CorrelationGraph) GetEvent(id string) *domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.events[id]
}

// GetEvents retrieves multiple events by IDs
func (g *CorrelationGraph) GetEvents(ids []string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	events := make([]*domain.UnifiedEvent, 0, len(ids))
	for _, id := range ids {
		if event, ok := g.events[id]; ok {
			events = append(events, event)
		}
	}

	return events
}

// FindByWorkload finds events from a specific workload
func (g *CorrelationGraph) FindByWorkload(kind, name, namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	key := makeKey(kind, name, namespace)
	return copyEvents(g.workloadIndex[key])
}

// FindByNode finds events on a specific node
func (g *CorrelationGraph) FindByNode(nodeName string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.nodeIndex[nodeName])
}

// FindByNamespace finds events in a namespace
func (g *CorrelationGraph) FindByNamespace(namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.namespaceIndex[namespace])
}

// FindByZone finds events in a zone
func (g *CorrelationGraph) FindByZone(zone string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.zoneIndex[zone])
}

// FindByLabel finds events with a specific label
func (g *CorrelationGraph) FindByLabel(key, value, namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	labelKey := makeLabelKey(key, value, namespace)
	return copyEvents(g.labelIndex[labelKey])
}

// FindByOwner finds events owned by a specific resource
func (g *CorrelationGraph) FindByOwner(kind, name, namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	key := makeKey(kind, name, namespace)
	return copyEvents(g.ownerIndex[key])
}

// FindByService finds events related to a service
func (g *CorrelationGraph) FindByService(name, namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	key := makeServiceKey(name, namespace)
	return copyEvents(g.serviceIndex[key])
}

// FindByDependency finds events with a specific dependency
func (g *CorrelationGraph) FindByDependency(kind, name, namespace string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	key := makeKey(kind, name, namespace)
	return copyEvents(g.dependencyIndex[key])
}

// FindBySemantic finds events with specific semantic intent
func (g *CorrelationGraph) FindBySemantic(intent string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.semanticIndex[intent])
}

// FindByCategory finds events in a semantic category
func (g *CorrelationGraph) FindByCategory(category string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.categoryIndex[category])
}

// FindByDomain finds events in a business domain
func (g *CorrelationGraph) FindByDomain(domain string) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return copyEvents(g.domainIndex[domain])
}

// FindInTimeRange finds events within a time range
func (g *CorrelationGraph) FindInTimeRange(start, end time.Time) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.timeIndex.FindInRange(start, end)
}

// FindAfter finds events after a specific time
func (g *CorrelationGraph) FindAfter(after time.Time, window time.Duration) []*domain.UnifiedEvent {
	g.mu.RLock()
	defer g.mu.RUnlock()

	end := after.Add(window)
	return g.timeIndex.FindInRange(after, end)
}

// evictOldEvents removes events older than retention time
func (g *CorrelationGraph) evictOldEvents() {
	cutoff := time.Now().Add(-g.retentionTime)

	for id, event := range g.events {
		if event.Timestamp.Before(cutoff) {
			g.removeEvent(id, event)
			g.evictionCount++
		}
	}
}

// removeEvent removes an event from all indexes
func (g *CorrelationGraph) removeEvent(id string, event *domain.UnifiedEvent) {
	delete(g.events, id)

	// Remove from time index
	g.timeIndex.Remove(event)

	// Remove from K8s indexes
	if event.K8sContext != nil {
		k8s := event.K8sContext

		if k8s.WorkloadName != "" {
			key := makeKey(k8s.WorkloadKind, k8s.WorkloadName, k8s.Namespace)
			g.workloadIndex[key] = removeEventFromSlice(g.workloadIndex[key], event)
		}

		if k8s.NodeName != "" {
			g.nodeIndex[k8s.NodeName] = removeEventFromSlice(g.nodeIndex[k8s.NodeName], event)
		}

		g.namespaceIndex[k8s.Namespace] = removeEventFromSlice(g.namespaceIndex[k8s.Namespace], event)

		if k8s.Zone != "" {
			g.zoneIndex[k8s.Zone] = removeEventFromSlice(g.zoneIndex[k8s.Zone], event)
		}
	}

	// Remove from semantic indexes
	if event.Semantic != nil {
		if event.Semantic.Intent != "" {
			g.semanticIndex[event.Semantic.Intent] = removeEventFromSlice(g.semanticIndex[event.Semantic.Intent], event)
		}
		if event.Semantic.Category != "" {
			g.categoryIndex[event.Semantic.Category] = removeEventFromSlice(g.categoryIndex[event.Semantic.Category], event)
		}
		if event.Semantic.Domain != "" {
			g.domainIndex[event.Semantic.Domain] = removeEventFromSlice(g.domainIndex[event.Semantic.Domain], event)
		}
	}
}

// Stats returns graph statistics
func (g *CorrelationGraph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return GraphStats{
		TotalEvents:   g.totalEvents,
		CurrentEvents: int64(len(g.events)),
		Evictions:     g.evictionCount,
		Indexes: IndexStats{
			Workloads:  len(g.workloadIndex),
			Nodes:      len(g.nodeIndex),
			Namespaces: len(g.namespaceIndex),
			Zones:      len(g.zoneIndex),
			Labels:     len(g.labelIndex),
			Semantics:  len(g.semanticIndex),
			Categories: len(g.categoryIndex),
			Domains:    len(g.domainIndex),
		},
	}
}

// GraphStats contains correlation graph statistics
type GraphStats struct {
	TotalEvents   int64
	CurrentEvents int64
	Evictions     int64
	Indexes       IndexStats
}

// IndexStats contains index cardinality
type IndexStats struct {
	Workloads  int
	Nodes      int
	Namespaces int
	Zones      int
	Labels     int
	Semantics  int
	Categories int
	Domains    int
}

// TimeIndex implementation

// NewTimeIndex creates a new time index
func NewTimeIndex() *TimeIndex {
	return &TimeIndex{
		buckets: make(map[int64][]*domain.UnifiedEvent),
	}
}

// Add adds an event to the time index
func (ti *TimeIndex) Add(event *domain.UnifiedEvent) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	// Bucket by minute
	bucket := event.Timestamp.Unix() / 60
	ti.buckets[bucket] = append(ti.buckets[bucket], event)

	// Update min/max
	if ti.minTime.IsZero() || event.Timestamp.Before(ti.minTime) {
		ti.minTime = event.Timestamp
	}
	if ti.maxTime.IsZero() || event.Timestamp.After(ti.maxTime) {
		ti.maxTime = event.Timestamp
	}
}

// Remove removes an event from the time index
func (ti *TimeIndex) Remove(event *domain.UnifiedEvent) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	bucket := event.Timestamp.Unix() / 60
	ti.buckets[bucket] = removeEventFromSlice(ti.buckets[bucket], event)

	if len(ti.buckets[bucket]) == 0 {
		delete(ti.buckets, bucket)
	}
}

// FindInRange finds events within a time range
func (ti *TimeIndex) FindInRange(start, end time.Time) []*domain.UnifiedEvent {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	var events []*domain.UnifiedEvent

	startBucket := start.Unix() / 60
	endBucket := end.Unix() / 60

	for bucket := startBucket; bucket <= endBucket; bucket++ {
		for _, event := range ti.buckets[bucket] {
			if !event.Timestamp.Before(start) && !event.Timestamp.After(end) {
				events = append(events, event)
			}
		}
	}

	return events
}

// Helper functions

func makeKey(parts ...string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "/"
		}
		result += part
	}
	return result
}

func makeLabelKey(key, value, namespace string) string {
	return makeKey(key, value, namespace)
}

func makeServiceKey(name, namespace string) string {
	return makeKey(name, namespace)
}

func copyEvents(events []*domain.UnifiedEvent) []*domain.UnifiedEvent {
	if events == nil {
		return nil
	}

	result := make([]*domain.UnifiedEvent, len(events))
	copy(result, events)
	return result
}

func removeEventFromSlice(events []*domain.UnifiedEvent, toRemove *domain.UnifiedEvent) []*domain.UnifiedEvent {
	for i, event := range events {
		if event.ID == toRemove.ID {
			// Remove by swapping with last element
			events[i] = events[len(events)-1]
			return events[:len(events)-1]
		}
	}
	return events
}
