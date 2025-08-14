# Real-Time Event Enrichment Architecture

## Problem Statement

We have multiple collectors emitting raw events:
- **Kernel/eBPF**: PID, cgroup ID, syscalls, network connections
- **KubeAPI**: Pod/Service/ConfigMap lifecycle events  
- **DNS**: Service DNS queries
- **CRI**: Container runtime events
- **CNI**: Network interface events

These events are disconnected - we need to connect kernel PIDs to Kubernetes pods, network connections to services, and DNS queries to actual endpoints.

## Current State Analysis

### What We Have
1. **Kernel Collector** (`pkg/collectors/kernel/`):
   - Has `K8sIntegration` that watches K8s resources
   - Updates eBPF maps with pod/container info
   - **Problem**: Placeholder implementation, no real cgroup/PID extraction

2. **CRI Collector** (`pkg/collectors/cri/`):
   - Direct access to container runtime
   - Can get container PIDs and cgroup paths
   - **Missing**: Not sharing this info with other collectors

3. **Pipeline Enricher** (`pkg/collectors/pipeline/k8s_enricher.go`):
   - Just parses JSON - doesn't actually enrich
   - No shared state or caching

## Proposed Architecture

### Core Design: Shared Enrichment Cache

```
┌─────────────────────────────────────────────────────────┐
│                   Enrichment Service                     │
│  ┌─────────────────────────────────────────────────┐   │
│  │              In-Memory Cache                     │   │
│  │  ┌──────────────────────────────────────────┐   │   │
│  │  │  PID → Container → Pod → Namespace       │   │   │
│  │  │  CgroupID → Pod UID                      │   │   │
│  │  │  IP → Service → Endpoints                │   │   │
│  │  │  DNS Name → Service                      │   │   │
│  │  │  Container ID → Pod Metadata             │   │   │
│  │  └──────────────────────────────────────────┘   │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │            Population Workers                    │   │
│  │  ├─ CRI Worker: Container → PID mapping         │   │
│  │  ├─ K8s Worker: Pod/Service/Endpoint watch      │   │
│  │  └─ Procfs Worker: PID → Cgroup extraction      │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                            ▲
                            │ Query
                            │
          ┌─────────────────┴─────────────────┐
          │           Collectors               │
          │  ├─ Kernel: Query PID → Pod       │
          │  ├─ DNS: Query Name → Service     │
          │  └─ CNI: Query IP → Pod           │
          └────────────────────────────────────┘
```

### Key Components

#### 1. Enrichment Cache Service

**Location**: `pkg/collectors/enrichment/cache.go`

```go
type EnrichmentCache struct {
    // PID to container mapping
    pidToContainer map[uint32]*ContainerInfo
    
    // Cgroup to pod mapping  
    cgroupToPod map[uint64]*PodInfo
    
    // Container ID to pod mapping
    containerToPod map[string]*PodInfo
    
    // IP to service mapping
    ipToService map[string]*ServiceInfo
    
    // DNS to service mapping
    dnsToService map[string]*ServiceInfo
    
    // All protected by RWMutex for read-heavy workload
    mu sync.RWMutex
}

type ContainerInfo struct {
    ContainerID   string
    PodUID        string
    PodName       string
    PodNamespace  string
    CgroupPath    string
    CgroupID      uint64
    MainPID       uint32
    Image         string
    Labels        map[string]string
}

type PodInfo struct {
    UID           string
    Name          string
    Namespace     string
    NodeName      string
    ServiceAccount string
    Labels        map[string]string
    Annotations   map[string]string
    Containers    []ContainerInfo
    HostNetwork   bool
    PodIP         string
}

type ServiceInfo struct {
    Name          string
    Namespace     string
    ClusterIP     string
    Ports         []ServicePort
    Selector      map[string]string
    Type          string // ClusterIP, NodePort, LoadBalancer
    Endpoints     []EndpointInfo
}
```

#### 2. CRI Integration Worker

**Location**: `pkg/collectors/enrichment/cri_worker.go`

```go
type CRIWorker struct {
    cache    *EnrichmentCache
    client   cri.RuntimeServiceClient
    logger   *zap.Logger
    interval time.Duration
}

func (w *CRIWorker) Start(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            w.updateContainerMappings(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (w *CRIWorker) updateContainerMappings(ctx context.Context) {
    // List all containers
    resp, err := w.client.ListContainers(ctx, &cri.ListContainersRequest{})
    if err != nil {
        return
    }
    
    for _, container := range resp.Containers {
        // Get container status for PID
        status, err := w.client.ContainerStatus(ctx, &cri.ContainerStatusRequest{
            ContainerId: container.Id,
            Verbose: true, // Get PID info
        })
        if err != nil {
            continue
        }
        
        // Extract PID from info (JSON in Verbose response)
        pid := w.extractPID(status.Info)
        
        // Extract cgroup path
        cgroupPath := w.extractCgroupPath(status.Info)
        cgroupID := w.getCgroupID(cgroupPath)
        
        // Extract pod info from labels
        podUID := container.Labels["io.kubernetes.pod.uid"]
        podName := container.Labels["io.kubernetes.pod.name"]
        podNamespace := container.Labels["io.kubernetes.pod.namespace"]
        
        // Update cache
        w.cache.UpdateContainer(&ContainerInfo{
            ContainerID:  container.Id,
            PodUID:       podUID,
            PodName:      podName,
            PodNamespace: podNamespace,
            CgroupPath:   cgroupPath,
            CgroupID:     cgroupID,
            MainPID:      pid,
            Image:        container.Image.Image,
            Labels:       container.Labels,
        })
    }
}

func (w *CRIWorker) getCgroupID(cgroupPath string) uint64 {
    // Read cgroup inode from /sys/fs/cgroup path
    info, err := os.Stat(cgroupPath)
    if err != nil {
        return 0
    }
    
    stat, ok := info.Sys().(*syscall.Stat_t)
    if !ok {
        return 0
    }
    
    return stat.Ino
}
```

#### 3. Kubernetes Watcher

**Location**: `pkg/collectors/enrichment/k8s_watcher.go`

```go
type K8sWatcher struct {
    cache     *EnrichmentCache
    client    kubernetes.Interface
    logger    *zap.Logger
    informers map[string]cache.SharedInformer
}

func (w *K8sWatcher) Start(ctx context.Context) error {
    // Pod informer
    podInformer := cache.NewSharedInformer(
        &cache.ListWatch{
            ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
                return w.client.CoreV1().Pods("").List(ctx, options)
            },
            WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
                return w.client.CoreV1().Pods("").Watch(ctx, options)
            },
        },
        &v1.Pod{},
        time.Minute,
    )
    
    podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    w.handlePodAdd,
        UpdateFunc: w.handlePodUpdate,
        DeleteFunc: w.handlePodDelete,
    })
    
    // Service informer
    svcInformer := cache.NewSharedInformer(
        &cache.ListWatch{
            ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
                return w.client.CoreV1().Services("").List(ctx, options)
            },
            WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
                return w.client.CoreV1().Services("").Watch(ctx, options)
            },
        },
        &v1.Service{},
        time.Minute,
    )
    
    svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    w.handleServiceAdd,
        UpdateFunc: w.handleServiceUpdate,
        DeleteFunc: w.handleServiceDelete,
    })
    
    // Endpoints informer for service discovery
    epInformer := cache.NewSharedInformer(
        &cache.ListWatch{
            ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
                return w.client.CoreV1().Endpoints("").List(ctx, options)
            },
            WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
                return w.client.CoreV1().Endpoints("").Watch(ctx, options)
            },
        },
        &v1.Endpoints{},
        time.Minute,
    )
    
    epInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    w.handleEndpointsAdd,
        UpdateFunc: w.handleEndpointsUpdate,
        DeleteFunc: w.handleEndpointsDelete,
    })
    
    // Start all informers
    go podInformer.Run(ctx.Done())
    go svcInformer.Run(ctx.Done())
    go epInformer.Run(ctx.Done())
    
    return nil
}

func (w *K8sWatcher) handleServiceAdd(obj interface{}) {
    svc := obj.(*v1.Service)
    
    // Update DNS mappings
    dnsNames := []string{
        svc.Name,
        fmt.Sprintf("%s.%s", svc.Name, svc.Namespace),
        fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace),
        fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace),
    }
    
    svcInfo := &ServiceInfo{
        Name:      svc.Name,
        Namespace: svc.Namespace,
        ClusterIP: svc.Spec.ClusterIP,
        Selector:  svc.Spec.Selector,
        Type:      string(svc.Spec.Type),
    }
    
    for _, port := range svc.Spec.Ports {
        svcInfo.Ports = append(svcInfo.Ports, ServicePort{
            Name:       port.Name,
            Port:       port.Port,
            TargetPort: port.TargetPort.IntVal,
            Protocol:   string(port.Protocol),
        })
    }
    
    // Update cache
    w.cache.UpdateService(svcInfo, dnsNames)
}
```

#### 4. Enrichment API

**Location**: `pkg/collectors/enrichment/enricher.go`

```go
type Enricher interface {
    // Enrich by PID
    EnrichByPID(pid uint32) *EnrichmentData
    
    // Enrich by container ID
    EnrichByContainerID(containerID string) *EnrichmentData
    
    // Enrich by cgroup ID
    EnrichByCgroupID(cgroupID uint64) *EnrichmentData
    
    // Enrich by IP address
    EnrichByIP(ip string) *EnrichmentData
    
    // Enrich by DNS name
    EnrichByDNS(dnsName string) *EnrichmentData
    
    // Enrich raw event
    EnrichEvent(event *collectors.RawEvent) *EnrichedEvent
}

type EnrichmentData struct {
    // Pod context
    Pod *PodInfo
    
    // Container context
    Container *ContainerInfo
    
    // Service context
    Service *ServiceInfo
    
    // Network context
    NetworkPolicy []NetworkPolicyInfo
    
    // Security context
    ServiceAccount string
    RBAC          []RBACInfo
}

type EnrichedEvent struct {
    Raw        *collectors.RawEvent
    Enrichment *EnrichmentData
    
    // Correlation IDs for graph building
    PodUID       string
    ServiceName  string
    Namespace    string
    ContainerID  string
    NodeName     string
}
```

### Integration Points

#### 1. Kernel Collector Integration

```go
// In kernel collector
func (c *KernelCollector) processEvent(event *KernelEvent) {
    // Get enrichment for PID
    enrichment := c.enricher.EnrichByPID(event.PID)
    
    if enrichment != nil && enrichment.Pod != nil {
        // Add K8s context to event metadata
        metadata := map[string]string{
            "pod_name":      enrichment.Pod.Name,
            "pod_namespace": enrichment.Pod.Namespace,
            "pod_uid":       enrichment.Pod.UID,
            "container_id":  enrichment.Container.ContainerID,
            "container_image": enrichment.Container.Image,
        }
        
        // Add service context if network event
        if event.EventType == EventTypeNetwork {
            netInfo := (*NetworkInfo)(unsafe.Pointer(&event.Data[0]))
            dstIP := ipToString(netInfo.DAddr)
            
            if svcEnrichment := c.enricher.EnrichByIP(dstIP); svcEnrichment != nil {
                metadata["service_name"] = svcEnrichment.Service.Name
                metadata["service_namespace"] = svcEnrichment.Service.Namespace
            }
        }
        
        // Emit enriched event
        c.emitEvent(&collectors.RawEvent{
            Type:      "kernel",
            Timestamp: time.Now(),
            Data:      eventToJSON(event),
            Metadata:  metadata,
        })
    }
}
```

#### 2. DNS Collector Integration

```go
// In DNS collector
func (c *DNSCollector) processDNSQuery(query *DNSEvent) {
    // Enrich DNS query
    enrichment := c.enricher.EnrichByDNS(query.QueryName)
    
    metadata := map[string]string{
        "query_name": query.QueryName,
        "query_type": query.QueryType,
    }
    
    if enrichment != nil && enrichment.Service != nil {
        metadata["service_name"] = enrichment.Service.Name
        metadata["service_namespace"] = enrichment.Service.Namespace
        metadata["service_clusterip"] = enrichment.Service.ClusterIP
        
        // Add endpoint information
        for i, ep := range enrichment.Service.Endpoints {
            metadata[fmt.Sprintf("endpoint_%d", i)] = ep.IP
        }
    }
    
    // Enrich source PID if available
    if srcEnrichment := c.enricher.EnrichByPID(query.PID); srcEnrichment != nil {
        metadata["source_pod"] = srcEnrichment.Pod.Name
        metadata["source_namespace"] = srcEnrichment.Pod.Namespace
    }
    
    c.emitEvent(&collectors.RawEvent{
        Type:      "dns",
        Timestamp: time.Now(),
        Data:      queryToJSON(query),
        Metadata:  metadata,
    })
}
```

### Performance Optimizations

1. **Read-Heavy Optimization**:
   - Use RWMutex for cache
   - Multiple readers, single writer per data type
   - Copy-on-write for large updates

2. **Memory Efficiency**:
   - Pool enrichment data objects
   - Reuse metadata maps
   - Intern common strings (namespace names, labels)

3. **Lookup Optimization**:
   - Multiple indexes (PID, container ID, cgroup ID)
   - Bloom filters for negative lookups
   - LRU cache for frequent queries

4. **Batch Updates**:
   - Batch CRI queries every 5 seconds
   - Use K8s informers with shared caches
   - Debounce rapid changes

### Direct Neo4j Integration

Instead of NATS, we can go directly to Neo4j:

```go
type Neo4jPublisher struct {
    driver   neo4j.Driver
    enricher Enricher
    batch    []*EnrichedEvent
    batchMu  sync.Mutex
}

func (p *Neo4jPublisher) PublishEvent(event *collectors.RawEvent) error {
    // Enrich event
    enriched := p.enricher.EnrichEvent(event)
    
    // Add to batch
    p.batchMu.Lock()
    p.batch = append(p.batch, enriched)
    if len(p.batch) >= 100 {
        go p.flushBatch()
    }
    p.batchMu.Unlock()
    
    return nil
}

func (p *Neo4jPublisher) flushBatch() {
    session := p.driver.NewSession(neo4j.SessionConfig{})
    defer session.Close()
    
    tx, _ := session.BeginTransaction()
    
    for _, event := range p.batch {
        // Create nodes and relationships
        query := `
            MERGE (p:Pod {uid: $pod_uid})
            SET p.name = $pod_name, p.namespace = $namespace
            
            MERGE (s:Service {name: $service_name, namespace: $namespace})
            
            CREATE (e:Event {
                id: $event_id,
                type: $event_type,
                timestamp: $timestamp
            })
            
            CREATE (p)-[:GENERATED]->(e)
            CREATE (e)-[:TARGETED]->(s)
        `
        
        tx.Run(query, map[string]interface{}{
            "pod_uid":      event.Enrichment.Pod.UID,
            "pod_name":     event.Enrichment.Pod.Name,
            "namespace":    event.Enrichment.Pod.Namespace,
            "service_name": event.Enrichment.Service.Name,
            "event_id":     event.Raw.ID,
            "event_type":   event.Raw.Type,
            "timestamp":    event.Raw.Timestamp.Unix(),
        })
    }
    
    tx.Commit()
}
```

## Implementation Plan

### Phase 1: Core Cache Infrastructure
1. Implement `EnrichmentCache` with thread-safe operations
2. Add basic PID → Container → Pod mappings
3. Create pool for enrichment data objects

### Phase 2: CRI Integration
1. Implement `CRIWorker` to populate PID mappings
2. Extract real cgroup IDs from container runtime
3. Test with containerd and CRI-O

### Phase 3: K8s Watcher
1. Implement informers for Pods, Services, Endpoints
2. Build DNS → Service mappings
3. Maintain IP → Service mappings

### Phase 4: Collector Integration
1. Update kernel collector to use enrichment
2. Update DNS collector to resolve services
3. Update CNI collector for network enrichment

### Phase 5: Direct Neo4j Publishing
1. Create Neo4j publisher with batching
2. Build graph model with enriched relationships
3. Remove NATS dependency

## Success Metrics

1. **Enrichment Coverage**:
   - 95% of kernel events have pod context
   - 100% of DNS queries resolved to services
   - 90% of network connections mapped to services

2. **Performance**:
   - < 100μs enrichment lookup latency
   - < 1% CPU overhead for enrichment
   - < 100MB memory for 1000 pods

3. **Accuracy**:
   - Zero false positive PID → Pod mappings
   - Correct service endpoint resolution
   - Accurate cgroup → container mapping

## Migration Path

1. **Keep existing pipeline working** during migration
2. **Run enrichment in parallel** initially
3. **Compare enriched vs non-enriched** events
4. **Gradual rollout** with feature flags
5. **Remove old enricher** after validation

## Key Differentiators

This design provides:
- **Real container runtime integration** (not stubs)
- **Actual PID to Pod mapping** (not placeholders)
- **Live service discovery** (not static)
- **Direct kernel to K8s correlation** (not guessing)
- **Zero-copy enrichment** with object pools
- **Lock-free lookups** for performance