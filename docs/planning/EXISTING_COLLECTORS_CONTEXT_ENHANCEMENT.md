# Existing Collectors: Context Enhancement Plan

## 📊 Current Collector Inventory

### 1. eBPF Collector (`pkg/collectors/ebpf/`)
**Current Capabilities**:
- Kernel syscalls, network events, file operations
- Process lifecycle, memory/CPU events
- Container context enrichment
- DualPathProcessor (raw + enriched)

**Enhancement Opportunities**:
- Already has container context - needs K8s linking
- Rich kernel-level causality data
- Can correlate syscalls → pod failures

### 2. K8s API Collector (`pkg/collectors/k8s/`)
**Current Capabilities**:
- Pod, Service, Deployment watchers
- Event stream processing
- Resource lifecycle tracking

**Enhancement Opportunities**:
- Primary source for ownership chains
- Service topology mapping
- Resource dependency extraction

### 3. CNI Collector (`pkg/collectors/cni/`)
**Current Capabilities**:
- Network plugin events
- Interface creation/deletion
- IP allocation tracking

**Enhancement Opportunities**:
- Network topology correlation
- Pod → IP → Service mapping
- Network policy impact detection

### 4. systemd Collector (`pkg/collectors/systemd/`)
**Current Capabilities**:
- Service start/stop/restart events
- Journal log collection
- Unit status monitoring

**Enhancement Opportunities**:
- Node-level service failures → Pod impacts
- System service dependencies
- Infrastructure health correlation

### 5. Network Collector (part of eBPF)
**Current Capabilities**:
- L4/L7 traffic monitoring
- Connection tracking
- Protocol detection

**Enhancement Opportunities**:
- Service mesh traffic correlation
- API call chains
- Latency impact analysis

## 🔄 Updated Implementation Strategy

### Phase 1: Enhance Existing Collectors (Week 1)

Instead of building new context from scratch, enhance what we have:

```go
// Example: eBPF collector enhancement
func (c *eBPFCollector) EnrichWithK8sContext(event *EnrichedEvent) *domain.UnifiedEvent {
    unified := event.ToUnifiedEvent()
    
    // NEW: Add rich K8s context
    if event.Container != nil {
        k8sContext := c.k8sCache.GetPodByContainerID(event.Container.ID)
        unified.K8sContext = &domain.K8sContext{
            Name:         k8sContext.Name,
            Namespace:    k8sContext.Namespace,
            WorkloadName: k8sContext.WorkloadName,
            OwnerChain:   k8sContext.GetOwnerChain(),
            // ... 50+ fields
        }
    }
    
    return unified
}
```

### Phase 2: Cross-Collector Correlation Opportunities

#### 2.1 eBPF + K8s API Correlation
```yaml
Examples:
- OOM Kill: eBPF (kernel OOM) + K8s (pod Failed event)
- File Access: eBPF (open syscall) + K8s (ConfigMap mount)
- Network: eBPF (connect) + K8s (Service endpoints)
```

#### 2.2 CNI + K8s API Correlation
```yaml
Examples:
- Pod Network Setup: CNI (ADD) + K8s (Pod Running)
- Network Failure: CNI (DEL failed) + K8s (Pod NetworkNotReady)
- IP Allocation: CNI (IP assigned) + K8s (Endpoint created)
```

#### 2.3 systemd + K8s API Correlation
```yaml
Examples:
- kubelet restart: systemd (kubelet.service) + K8s (Node NotReady)
- Container runtime: systemd (containerd) + K8s (pods evicted)
- Node services: systemd (failures) + K8s (node conditions)
```

## 📈 Correlation Power Matrix

| Collector 1 | Collector 2 | Correlation Type | Value |
|------------|-------------|------------------|--------|
| eBPF | K8s API | Kernel → Pod lifecycle | Critical |
| eBPF | CNI | Network setup → Traffic | High |
| K8s API | CNI | Pod → Network readiness | High |
| systemd | K8s API | Node health → Pod impact | Critical |
| eBPF | systemd | System calls → Service health | Medium |

## 🎯 Quick Win Correlations

### 1. Container Lifecycle (eBPF + K8s)
```go
Pattern: "ContainerOOM"
Events: [
    {Source: "ebpf", Type: "memory_pressure"},
    {Source: "ebpf", Type: "oom_kill"},
    {Source: "k8s", Type: "pod.Failed", Reason: "OOMKilled"}
]
Confidence: 95%
```

### 2. Network Setup (CNI + K8s)
```go
Pattern: "PodNetworkFailure"
Events: [
    {Source: "k8s", Type: "pod.Created"},
    {Source: "cni", Type: "ADD", Status: "failed"},
    {Source: "k8s", Type: "pod.NetworkNotReady"}
]
Confidence: 90%
```

### 3. Node Degradation (systemd + K8s + eBPF)
```go
Pattern: "NodeServiceFailure"
Events: [
    {Source: "systemd", Type: "service.failed", Unit: "kubelet"},
    {Source: "k8s", Type: "node.NotReady"},
    {Source: "ebpf", Type: "connection_refused", Port: 10250}
]
Confidence: 85%
```

## 💡 Why This Is Powerful

With 5 collectors already working:

1. **Immediate Correlation Value** - Can correlate across layers TODAY
2. **Rich Context Available** - eBPF has container IDs, K8s has ownership
3. **Multiple Perspectives** - Same incident seen from kernel/network/k8s/system
4. **No New Collectors Needed** - Just enhance correlation logic

## 📊 Context Enhancement Priority

1. **K8s API Collector** - Primary source of truth for ownership/topology
2. **eBPF Collector** - Already has container context, needs K8s linking
3. **CNI Collector** - Critical for network correlation
4. **systemd Collector** - Node-level context
5. **CRI Collector** - Add later for container runtime details
6. **Control Plane Collector** - Add later for cluster-wide view

## 🚀 Revised Week 1 Goals

Instead of building from scratch:

1. **Enhance K8s API collector** to extract full ownership chains
2. **Link eBPF container IDs** to K8s pod information
3. **Map CNI network events** to pod/service topology
4. **Connect systemd events** to node/pod impacts
5. **Create first multi-collector correlation** (OOM kill pattern)

This leverages our **existing rich data sources** immediately!