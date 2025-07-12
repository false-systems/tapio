# Multi-Source Integration Design

## Overview

This document details the integration strategy for combining eBPF, systemd, journald, and Kubernetes data sources into a unified event stream with normalized schemas and cross-source correlation capabilities.

## Event Normalization Architecture

### Unified Event Model

```go
// UnifiedEvent represents a normalized event from any source
type UnifiedEvent struct {
    // Core Fields
    ID          uuid.UUID              `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    Source      SourceType             `json:"source"`
    Type        EventType              `json:"type"`
    Severity    Severity               `json:"severity"`
    
    // Entity Information
    Entity      EntityReference        `json:"entity"`
    
    // Event Details
    Title       string                 `json:"title"`
    Description string                 `json:"description"`
    
    // Structured Data
    Attributes  map[string]interface{} `json:"attributes"`
    
    // Correlation Data
    TraceID     string                 `json:"trace_id,omitempty"`
    SpanID      string                 `json:"span_id,omitempty"`
    ParentID    string                 `json:"parent_id,omitempty"`
    
    // Metadata
    Labels      map[string]string      `json:"labels"`
    Annotations map[string]string      `json:"annotations"`
}

// EntityReference identifies the entity associated with an event
type EntityReference struct {
    Type       EntityType        `json:"type"`
    Name       string            `json:"name"`
    Namespace  string            `json:"namespace,omitempty"`
    UID        string            `json:"uid"`
    Parent     *EntityReference  `json:"parent,omitempty"`
    Attributes map[string]string `json:"attributes,omitempty"`
}

// EntityType defines the type of entity
type EntityType string

const (
    EntityPod         EntityType = "pod"
    EntityContainer   EntityType = "container"
    EntityNode        EntityType = "node"
    EntityService     EntityType = "service"
    EntityProcess     EntityType = "process"
    EntitySystemdUnit EntityType = "systemd_unit"
    EntityNamespace   EntityType = "namespace"
)
```

### Source-Specific Normalizers

#### eBPF Event Normalizer

```go
type EBPFNormalizer struct {
    pidMapper   *PIDMapper
    cgroupCache *CgroupCache
}

func (n *EBPFNormalizer) Normalize(raw *ebpf.Event) UnifiedEvent {
    event := UnifiedEvent{
        ID:        uuid.New(),
        Timestamp: time.Unix(0, raw.Timestamp),
        Source:    SourceEBPF,
        Type:      n.mapEventType(raw.Type),
        Severity:  n.calculateSeverity(raw),
    }
    
    // Map process to container/pod
    if entity := n.mapEntity(raw.PID); entity != nil {
        event.Entity = *entity
    }
    
    // Extract attributes based on event type
    switch raw.Type {
    case ebpf.EventTypeOOM:
        event.Attributes = map[string]interface{}{
            "pid":           raw.PID,
            "comm":          raw.Comm,
            "memory_limit":  raw.Data["limit"],
            "memory_usage":  raw.Data["usage"],
            "cgroup":        raw.Data["cgroup"],
        }
        event.Title = fmt.Sprintf("OOM Kill: %s (PID: %d)", raw.Comm, raw.PID)
        
    case ebpf.EventTypeCPUThrottle:
        event.Attributes = map[string]interface{}{
            "pid":            raw.PID,
            "throttled_ns":   raw.Data["throttled_ns"],
            "runtime_ns":     raw.Data["runtime_ns"],
            "throttle_ratio": raw.Data["throttle_ratio"],
        }
        event.Title = "CPU Throttling Detected"
        
    case ebpf.EventTypeNetworkDrop:
        event.Attributes = map[string]interface{}{
            "protocol": raw.Data["protocol"],
            "src_ip":   raw.Data["src_ip"],
            "dst_ip":   raw.Data["dst_ip"],
            "reason":   raw.Data["reason"],
        }
        event.Title = "Network Packet Drop"
    }
    
    return event
}

func (n *EBPFNormalizer) mapEntity(pid uint32) *EntityReference {
    // Map PID to container
    if container := n.pidMapper.GetContainer(pid); container != nil {
        return &EntityReference{
            Type:      EntityContainer,
            Name:      container.Name,
            Namespace: container.Namespace,
            UID:       container.ID,
            Parent: &EntityReference{
                Type:      EntityPod,
                Name:      container.PodName,
                Namespace: container.Namespace,
                UID:       container.PodUID,
            },
        }
    }
    
    // Fallback to process
    return &EntityReference{
        Type: EntityProcess,
        Name: fmt.Sprintf("pid-%d", pid),
        UID:  fmt.Sprintf("process-%d", pid),
    }
}
```

#### systemd Event Normalizer

```go
type SystemdNormalizer struct {
    unitCache *UnitCache
}

func (n *SystemdNormalizer) Normalize(change *dbus.UnitStateChange) UnifiedEvent {
    event := UnifiedEvent{
        ID:        uuid.New(),
        Timestamp: time.Now(),
        Source:    SourceSystemd,
        Type:      EventTypeServiceState,
        Entity: EntityReference{
            Type: EntitySystemdUnit,
            Name: change.Unit,
            UID:  change.Unit,
            Attributes: map[string]string{
                "active_state": change.ActiveState,
                "sub_state":    change.SubState,
            },
        },
    }
    
    // Determine severity based on state transition
    event.Severity = n.calculateSeverity(change)
    
    // Build attributes
    event.Attributes = map[string]interface{}{
        "previous_state": change.PreviousState,
        "current_state":  change.ActiveState,
        "sub_state":      change.SubState,
        "job_id":         change.JobID,
        "job_type":       change.JobType,
    }
    
    // Generate title
    event.Title = fmt.Sprintf("Service %s: %s → %s", 
        change.Unit, change.PreviousState, change.ActiveState)
    
    // Add Kubernetes correlation if applicable
    if pod := n.unitCache.GetPodForUnit(change.Unit); pod != nil {
        event.Entity.Parent = &EntityReference{
            Type:      EntityPod,
            Name:      pod.Name,
            Namespace: pod.Namespace,
            UID:       string(pod.UID),
        }
    }
    
    return event
}

func (n *SystemdNormalizer) calculateSeverity(change *dbus.UnitStateChange) Severity {
    // Failed states
    if change.ActiveState == "failed" {
        return SeverityCritical
    }
    
    // Degraded states
    if change.SubState == "auto-restart" {
        return SeverityError
    }
    
    // Starting/stopping
    if change.ActiveState == "activating" || change.ActiveState == "deactivating" {
        return SeverityInfo
    }
    
    return SeverityInfo
}
```

#### journald Event Normalizer

```go
type JournaldNormalizer struct {
    patterns    *PatternMatcher
    classifiers []LogClassifier
}

func (n *JournaldNormalizer) Normalize(entry *sdjournal.JournalEntry) UnifiedEvent {
    event := UnifiedEvent{
        ID:        uuid.New(),
        Timestamp: entry.RealtimeTimestamp,
        Source:    SourceJournald,
        Type:      EventTypeLog,
    }
    
    // Extract entity from journal fields
    event.Entity = n.extractEntity(entry.Fields)
    
    // Parse message and extract structured data
    parsed := n.parseMessage(entry.Fields["MESSAGE"])
    event.Title = parsed.Summary
    event.Description = entry.Fields["MESSAGE"]
    event.Attributes = parsed.Attributes
    
    // Classify log entry
    classification := n.classify(entry)
    event.Severity = classification.Severity
    event.Type = classification.EventType
    
    // Add journal metadata
    event.Labels = map[string]string{
        "unit":          entry.Fields["_SYSTEMD_UNIT"],
        "hostname":      entry.Fields["_HOSTNAME"],
        "exe":           entry.Fields["_EXE"],
        "cmdline":       entry.Fields["_CMDLINE"],
    }
    
    return event
}

func (n *JournaldNormalizer) classify(entry *sdjournal.JournalEntry) Classification {
    msg := entry.Fields["MESSAGE"]
    
    // Check patterns
    if match := n.patterns.Match(msg); match != nil {
        return Classification{
            EventType: match.EventType,
            Severity:  match.Severity,
            Category:  match.Category,
        }
    }
    
    // Run classifiers
    for _, classifier := range n.classifiers {
        if class := classifier.Classify(entry); class != nil {
            return *class
        }
    }
    
    // Default classification based on priority
    return n.defaultClassification(entry.Fields["PRIORITY"])
}
```

### Pattern Library

#### Common Patterns

```yaml
patterns:
  # OOM Patterns
  - name: kernel_oom_kill
    pattern: 'Out of memory: Kill process (\d+) \((.+)\)'
    event_type: oom_kill
    severity: critical
    extract:
      - pid
      - process_name
      
  - name: container_oom
    pattern: 'Memory cgroup out of memory: Kill process'
    event_type: container_oom
    severity: critical
    
  # Network Patterns  
  - name: connection_refused
    pattern: 'connect\(\) to (.+):(\d+) failed: Connection refused'
    event_type: connection_failed
    severity: error
    extract:
      - host
      - port
      
  - name: dns_failure
    pattern: 'Name or service not known|NXDOMAIN'
    event_type: dns_resolution_failed
    severity: error
    
  # Service Patterns
  - name: service_crash
    pattern: 'Main process exited, code=exited, status=(\d+)'
    event_type: service_crashed
    severity: error
    extract:
      - exit_code
      
  - name: restart_limit
    pattern: 'Start request repeated too quickly'
    event_type: restart_limit_hit
    severity: critical
```

## Cross-Source Correlation

### Entity Mapping

```go
type EntityMapper struct {
    pidToContainer map[uint32]*ContainerInfo
    unitToPod      map[string]*PodInfo
    containerToPod map[string]*PodInfo
    mu             sync.RWMutex
}

func (em *EntityMapper) MapProcessToKubernetes(pid uint32) *KubernetesEntity {
    em.mu.RLock()
    defer em.mu.RUnlock()
    
    // Check if process belongs to a container
    if container := em.pidToContainer[pid]; container != nil {
        if pod := em.containerToPod[container.ID]; pod != nil {
            return &KubernetesEntity{
                Pod:       pod,
                Container: container,
            }
        }
    }
    
    return nil
}

func (em *EntityMapper) UpdateMappings() {
    // Update PID to container mappings from /proc
    em.updatePIDMappings()
    
    // Update systemd unit to pod mappings
    em.updateUnitMappings()
    
    // Update container to pod mappings from CRI
    em.updateContainerMappings()
}
```

### Timeline Reconstruction

```go
type TimelineReconstructor struct {
    window    time.Duration
    resolver  *EntityResolver
    correlator *EventCorrelator
}

func (tr *TimelineReconstructor) ReconstructTimeline(
    events []UnifiedEvent, 
    focus EntityReference,
) *Timeline {
    
    timeline := &Timeline{
        Entity: focus,
        Window: tr.window,
        Events: make([]TimelineEntry, 0),
    }
    
    // Filter events related to the entity
    related := tr.filterRelatedEvents(events, focus)
    
    // Sort by timestamp
    sort.Slice(related, func(i, j int) bool {
        return related[i].Timestamp.Before(related[j].Timestamp)
    })
    
    // Build timeline entries with context
    for i, event := range related {
        entry := TimelineEntry{
            Event:    event,
            Index:    i,
            Relative: event.Timestamp.Sub(related[0].Timestamp),
        }
        
        // Find correlated events
        entry.Correlated = tr.findCorrelatedEvents(event, related)
        
        // Calculate impact
        entry.Impact = tr.calculateImpact(event, related[i:])
        
        timeline.Events = append(timeline.Events, entry)
    }
    
    // Identify patterns
    timeline.Patterns = tr.identifyPatterns(timeline.Events)
    
    return timeline
}

type Timeline struct {
    Entity   EntityReference
    Window   time.Duration
    Events   []TimelineEntry
    Patterns []Pattern
}

type TimelineEntry struct {
    Event      UnifiedEvent
    Index      int
    Relative   time.Duration
    Correlated []string // Event IDs
    Impact     ImpactScore
}
```

### Correlation Rules

```go
type CorrelationRule struct {
    Name        string
    Sources     []SourceType
    Window      time.Duration
    MinEvents   int
    Conditions  []Condition
    Correlation CorrelationFunc
}

// Example: OOM Cascade Detection
var OOMCascadeRule = CorrelationRule{
    Name:      "oom_cascade",
    Sources:   []SourceType{SourceEBPF, SourceSystemd, SourceJournald},
    Window:    30 * time.Second,
    MinEvents: 3,
    Conditions: []Condition{
        HasEventType(EventTypeOOM),
        HasEventType(EventTypeServiceRestart),
    },
    Correlation: func(events []UnifiedEvent) *Correlation {
        // Find initial OOM event
        var oomEvent *UnifiedEvent
        for _, e := range events {
            if e.Type == EventTypeOOM {
                oomEvent = &e
                break
            }
        }
        
        if oomEvent == nil {
            return nil
        }
        
        // Find subsequent impacts
        impacts := []UnifiedEvent{}
        for _, e := range events {
            if e.Timestamp.After(oomEvent.Timestamp) {
                if e.Type == EventTypeServiceRestart &&
                   e.Entity.Parent != nil &&
                   e.Entity.Parent.UID == oomEvent.Entity.Parent.UID {
                    impacts = append(impacts, e)
                }
            }
        }
        
        if len(impacts) > 0 {
            return &Correlation{
                Type:        "oom_cascade",
                Confidence:  0.9,
                RootCause:   oomEvent,
                Impacts:     impacts,
                Description: "OOM event caused service cascade failure",
            }
        }
        
        return nil
    },
}

// Example: Network Partition Detection
var NetworkPartitionRule = CorrelationRule{
    Name:      "network_partition",
    Sources:   []SourceType{SourceEBPF, SourceJournald},
    Window:    1 * time.Minute,
    MinEvents: 5,
    Conditions: []Condition{
        HasPattern("connection refused|timeout"),
        MultipleEntities(3),
    },
    Correlation: func(events []UnifiedEvent) *Correlation {
        // Group by source/destination
        connections := make(map[string][]UnifiedEvent)
        
        for _, e := range events {
            if src, ok := e.Attributes["src_ip"].(string); ok {
                if dst, ok := e.Attributes["dst_ip"].(string); ok {
                    key := fmt.Sprintf("%s->%s", src, dst)
                    connections[key] = append(connections[key], e)
                }
            }
        }
        
        // Detect partition pattern
        if len(connections) >= 3 {
            return &Correlation{
                Type:       "network_partition",
                Confidence: 0.85,
                Pattern:    "Multiple connection failures between nodes",
            }
        }
        
        return nil
    },
}
```

## Data Enrichment Pipeline

### Metadata Enrichment

```go
type MetadataEnricher struct {
    k8sClient   kubernetes.Interface
    nodeInfo    *NodeInfoCache
    serviceMap  *ServiceMap
}

func (me *MetadataEnricher) Enrich(event *UnifiedEvent) {
    switch event.Entity.Type {
    case EntityPod:
        me.enrichPodEvent(event)
    case EntityContainer:
        me.enrichContainerEvent(event)
    case EntityNode:
        me.enrichNodeEvent(event)
    case EntitySystemdUnit:
        me.enrichSystemdEvent(event)
    }
    
    // Add trace context if available
    me.addTraceContext(event)
    
    // Add service mesh metadata
    me.addServiceMeshInfo(event)
}

func (me *MetadataEnricher) enrichPodEvent(event *UnifiedEvent) {
    pod, err := me.k8sClient.CoreV1().Pods(event.Entity.Namespace).
        Get(context.TODO(), event.Entity.Name, metav1.GetOptions{})
    
    if err == nil {
        event.Labels = pod.Labels
        event.Annotations = pod.Annotations
        
        // Add owner references
        if len(pod.OwnerReferences) > 0 {
            owner := pod.OwnerReferences[0]
            event.Attributes["owner_kind"] = owner.Kind
            event.Attributes["owner_name"] = owner.Name
        }
        
        // Add QoS class
        event.Attributes["qos_class"] = string(pod.Status.QOSClass)
    }
}
```

### Context Propagation

```go
type ContextPropagator struct {
    tracer trace.Tracer
}

func (cp *ContextPropagator) PropagateContext(event *UnifiedEvent) {
    // Extract trace context from various sources
    var traceID, spanID string
    
    // Check for OpenTelemetry context
    if tid, ok := event.Attributes["trace_id"].(string); ok {
        traceID = tid
    }
    
    // Check for Jaeger headers
    if traceID == "" {
        if uber, ok := event.Attributes["uber-trace-id"].(string); ok {
            parts := strings.Split(uber, ":")
            if len(parts) >= 2 {
                traceID = parts[0]
                spanID = parts[1]
            }
        }
    }
    
    // Check for W3C trace context
    if traceID == "" {
        if tc, ok := event.Attributes["traceparent"].(string); ok {
            parts := strings.Split(tc, "-")
            if len(parts) >= 3 {
                traceID = parts[1]
                spanID = parts[2]
            }
        }
    }
    
    event.TraceID = traceID
    event.SpanID = spanID
}
```

## Performance Optimizations

### Event Deduplication

```go
type EventDeduplicator struct {
    cache      *lru.Cache
    window     time.Duration
    hashFunc   func(UnifiedEvent) string
}

func (ed *EventDeduplicator) Deduplicate(event UnifiedEvent) (bool, string) {
    hash := ed.hashFunc(event)
    
    if existing, ok := ed.cache.Get(hash); ok {
        lastSeen := existing.(time.Time)
        if time.Since(lastSeen) < ed.window {
            return true, hash // Duplicate
        }
    }
    
    ed.cache.Add(hash, time.Now())
    return false, hash
}

func DefaultHashFunc(event UnifiedEvent) string {
    h := fnv.New64a()
    h.Write([]byte(event.Source))
    h.Write([]byte(event.Type))
    h.Write([]byte(event.Entity.UID))
    h.Write([]byte(event.Title))
    return fmt.Sprintf("%x", h.Sum64())
}
```

### Parallel Processing

```go
type ParallelProcessor struct {
    workers    int
    normalizer Normalizer
    enricher   Enricher
    dedup      Deduplicator
}

func (pp *ParallelProcessor) Process(
    inputs map[SourceType]<-chan RawEvent,
) <-chan UnifiedEvent {
    
    output := make(chan UnifiedEvent, 10000)
    
    // Start workers for each source
    var wg sync.WaitGroup
    
    for source, input := range inputs {
        for i := 0; i < pp.workers; i++ {
            wg.Add(1)
            go func(src SourceType, in <-chan RawEvent) {
                defer wg.Done()
                pp.processSource(src, in, output)
            }(source, input)
        }
    }
    
    go func() {
        wg.Wait()
        close(output)
    }()
    
    return output
}

func (pp *ParallelProcessor) processSource(
    source SourceType,
    input <-chan RawEvent,
    output chan<- UnifiedEvent,
) {
    for raw := range input {
        // Normalize
        event := pp.normalizer.Normalize(source, raw)
        
        // Deduplicate
        if isDup, _ := pp.dedup.Deduplicate(event); isDup {
            continue
        }
        
        // Enrich
        pp.enricher.Enrich(&event)
        
        // Send to output
        output <- event
    }
}
```

## Integration Examples

### Complete Pipeline

```go
func BuildIntegrationPipeline() *Pipeline {
    // Initialize components
    normalizers := map[SourceType]Normalizer{
        SourceEBPF:     NewEBPFNormalizer(),
        SourceSystemd:  NewSystemdNormalizer(),
        SourceJournald: NewJournaldNormalizer(),
    }
    
    enricher := &ChainedEnricher{
        enrichers: []Enricher{
            NewMetadataEnricher(),
            NewContextPropagator(),
            NewGeoIPEnricher(),
        },
    }
    
    dedup := NewEventDeduplicator(
        10000,              // cache size
        5 * time.Minute,    // window
        DefaultHashFunc,
    )
    
    // Build pipeline
    return &Pipeline{
        Sources:     NewSourceManager(),
        Normalizers: normalizers,
        Enricher:    enricher,
        Dedup:       dedup,
        Timeline:    NewTimeline(1000000),
        Correlator:  NewCorrelationEngine(),
    }
}
```

## Monitoring and Observability

### Integration Metrics

```go
type IntegrationMetrics struct {
    EventsReceived   *prometheus.CounterVec
    EventsNormalized *prometheus.CounterVec
    EventsDropped    *prometheus.CounterVec
    EventsCorrelated *prometheus.CounterVec
    
    NormalizationLatency *prometheus.HistogramVec
    EnrichmentLatency    *prometheus.HistogramVec
    CorrelationLatency   *prometheus.HistogramVec
    
    TimelineSize *prometheus.GaugeVec
    CacheHitRate *prometheus.GaugeVec
}

func NewIntegrationMetrics() *IntegrationMetrics {
    return &IntegrationMetrics{
        EventsReceived: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "tapio_events_received_total",
                Help: "Total events received by source",
            },
            []string{"source"},
        ),
        // ... other metrics
    }
}
```