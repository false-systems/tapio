# Kubernetes Data Landscape Research

## Overview

This document maps the complete data landscape that Kubernetes provides, identifying all sources of observable information that can be used to construct coherent narratives about system behavior.

## Data Source Taxonomy

### 1. API Server Data

#### 1.1 Core Resources
```yaml
Workload Resources:
  - Pods: Status, conditions, containers, volumes
  - ReplicaSets: Replicas, selector, status
  - Deployments: Strategy, replicas, conditions
  - StatefulSets: Replicas, update strategy, volume claims
  - DaemonSets: Node selector, update strategy
  - Jobs: Completions, parallelism, status
  - CronJobs: Schedule, job template, history

Service & Networking:
  - Services: Type, ports, endpoints, selector
  - Endpoints: Addresses, ports, conditions
  - Ingresses: Rules, TLS, backend services
  - NetworkPolicies: Selectors, ingress/egress rules
  - EndpointSlices: Addresses, conditions, topology

Configuration & Storage:
  - ConfigMaps: Data, binary data, immutable flag
  - Secrets: Type, data, immutable flag
  - PersistentVolumeClaims: Access modes, resources, status
  - PersistentVolumes: Capacity, access modes, reclaim policy
  - StorageClasses: Provisioner, parameters, reclaim policy

Cluster Resources:
  - Nodes: Status, capacity, allocatable, conditions
  - Namespaces: Status, finalizers, labels
  - ResourceQuotas: Hard limits, used resources
  - LimitRanges: Limits for different resource types
  - PriorityClasses: Value, global default
```

#### 1.2 Resource Metadata
```go
type ResourceMetadata struct {
    // Identity
    Name              string
    Namespace         string
    UID               string
    ResourceVersion   string
    Generation        int64
    
    // Timestamps
    CreationTimestamp time.Time
    DeletionTimestamp *time.Time
    
    // Relationships
    OwnerReferences   []OwnerReference
    Labels            map[string]string
    Annotations       map[string]string
    Finalizers        []string
    
    // Management
    ManagedFields     []ManagedFieldsEntry
}
```

#### 1.3 Status Conditions
```go
type StatusCondition struct {
    Type               string // Ready, Progressing, Available
    Status             string // True, False, Unknown
    LastUpdateTime     time.Time
    LastTransitionTime time.Time
    Reason             string
    Message            string
    ObservedGeneration int64
}
```

### 2. Event Stream Data

#### 2.1 Kubernetes Events
```yaml
Event Types:
  Normal Events:
    - Scheduled: Pod scheduled to node
    - Pulled: Container image pulled
    - Created: Container created
    - Started: Container started
    - Killing: Container being terminated
    
  Warning Events:
    - FailedScheduling: Insufficient resources
    - FailedMount: Volume mount failed
    - BackOff: Restart backoff
    - Unhealthy: Health check failed
    - FailedCreate: Resource creation failed
    
Event Sources:
    - kubelet: Node-level events
    - scheduler: Scheduling decisions
    - controller-manager: Controller actions
    - cloud-provider: Cloud-specific events
```

#### 2.2 Audit Events
```go
type AuditEvent struct {
    // Request info
    Verb         string // get, list, create, update, delete
    ObjectRef    ObjectReference
    RequestURI   string
    
    // User info
    User         UserInfo
    ImpersonatedUser *UserInfo
    
    // Response
    ResponseStatus *Status
    ResponseObject runtime.Object
    
    // Metadata
    RequestReceivedTimestamp time.Time
    StageTimestamp          time.Time
    Annotations             map[string]string
}
```

### 3. Controller-Specific Data

#### 3.1 Deployment Controller
```yaml
Observations:
  - Replica management decisions
  - Rolling update progress
  - Rollback triggers
  - Scaling events
  - Strategy execution

Derived Metrics:
  - Update velocity
  - Rollback frequency
  - Replica availability
  - Update duration
```

#### 3.2 Scheduler Data
```yaml
Scheduling Decisions:
  - Node scoring results
  - Predicate evaluations
  - Priority calculations
  - Preemption decisions
  - Binding results

Queue Metrics:
  - Pending pods
  - Scheduling attempts
  - Scheduling latency
  - Queue wait time
```

#### 3.3 HPA (Horizontal Pod Autoscaler)
```go
type HPAMetrics struct {
    CurrentReplicas  int32
    DesiredReplicas  int32
    CurrentMetrics   []MetricStatus
    Conditions       []HPACondition
    
    // Scaling decisions
    ScaleUpEvents    []ScaleEvent
    ScaleDownEvents  []ScaleEvent
    
    // Metrics
    ObservedGeneration   int64
    LastScaleTime       *time.Time
}
```

### 4. Node-Level Data

#### 4.1 Kubelet Data
```yaml
Node Status:
  - Capacity: CPU, memory, pods, storage
  - Allocatable: Available resources
  - Conditions: Ready, MemoryPressure, DiskPressure
  - NodeInfo: Kernel, OS, container runtime
  - Images: Cached container images
  - VolumesInUse: Attached volumes

Pod Status:
  - Phase: Pending, Running, Succeeded, Failed
  - Conditions: PodScheduled, Ready, Initialized
  - Container Statuses: State, ready, restart count
  - Init Container Statuses
  - QoS Class: Guaranteed, Burstable, BestEffort
```

#### 4.2 CRI (Container Runtime Interface) Data
```yaml
Container Lifecycle:
  - CreateContainer: Config, sandbox ID
  - StartContainer: Success/failure
  - StopContainer: Timeout, reason
  - RemoveContainer: Success/failure
  
Container Status:
  - State: Created, Running, Exited, Unknown
  - CreatedAt, StartedAt, FinishedAt
  - ExitCode: Success/failure indicator
  - Reason: OOMKilled, Error, Completed
  - Message: Detailed error information
  
Image Operations:
  - PullImage: Image spec, auth, progress
  - ImageStatus: Size, repo tags, digest
  - ListImages: Available images
  - RemoveImage: Cleanup operations
```

#### 4.3 CNI (Container Network Interface) Data
```yaml
Network Operations:
  - ADD: Attach container to network
  - DEL: Detach container from network
  - CHECK: Verify network connectivity
  - VERSION: Plugin capabilities

Network Configuration:
  - Interfaces: Created network interfaces
  - IPs: Assigned IP addresses
  - Routes: Configured routes
  - DNS: Nameservers, search domains
```

### 5. etcd Data

#### 5.1 Key Structure
```yaml
Registry Structure:
  /registry/pods/namespace/name
  /registry/services/namespace/name
  /registry/deployments/namespace/name
  /registry/nodes/name
  /registry/events/namespace/name
  
Lease Information:
  /registry/masterleases/IP
  /registry/leases/namespace/name
  
Configuration:
  /registry/configmaps/namespace/name
  /registry/secrets/namespace/name
```

#### 5.2 Watch Events
```go
type WatchEvent struct {
    Type   string // ADDED, MODIFIED, DELETED
    Object runtime.Object
    
    // etcd specific
    ResourceVersion int64
    PrevKV          *KeyValue
}
```

### 6. Metrics Data

#### 6.1 Resource Metrics
```yaml
Container Metrics:
  - CPU: Usage, limits, requests, throttling
  - Memory: Usage, limits, requests, working set
  - Network: RX/TX bytes, packets, errors
  - Filesystem: Usage, available, capacity
  - Process: Count, threads

Node Metrics:
  - CPU: Usage, capacity, allocatable
  - Memory: Usage, capacity, allocatable
  - Network: Interface statistics
  - Disk: IOPS, throughput, latency
  - Runtime: Container operations latency
```

#### 6.2 Control Plane Metrics
```yaml
API Server:
  - Request rate, latency, errors
  - Admission webhook latency
  - etcd request latency
  - Authentication/authorization latency
  - Watch connections

Controller Manager:
  - Work queue depth, latency
  - Reconciliation duration
  - Controller-specific metrics
  
Scheduler:
  - Scheduling attempts, duration
  - Preemption attempts
  - Queue wait time
  - Binding latency
```

### 7. Service Mesh Data (when present)

#### 7.1 Envoy/Istio Metrics
```yaml
Request Metrics:
  - Request rate, duration, size
  - Response codes, size
  - Retry attempts, timeouts
  - Circuit breaker state

Connection Metrics:
  - Active connections
  - Connection duration
  - TLS handshake time
  - Connection errors
```

### 8. Custom Resource Data

#### 8.1 Operator-Managed Resources
```go
type CustomResourceData struct {
    // Standard metadata
    ObjectMeta metav1.ObjectMeta
    
    // Custom spec
    Spec interface{}
    
    // Custom status
    Status interface{}
    
    // Often includes
    Conditions []Condition
    Phase      string
}
```

## Data Correlation Opportunities

### 1. Vertical Correlation (Same Resource)
```yaml
Pod Example:
  - API: Pod spec, status, conditions
  - Events: Scheduling, pulling, starting events
  - Metrics: CPU, memory, network usage
  - CRI: Container state, exit codes
  - Logs: Application output
```

### 2. Horizontal Correlation (Related Resources)
```yaml
Deployment Story:
  - Deployment: Spec changes, conditions
  - ReplicaSet: Created, scaled
  - Pods: Created, terminated
  - Events: All related events
  - Metrics: Aggregate metrics
```

### 3. Temporal Correlation
```yaml
Time-based Patterns:
  - Event sequences within time windows
  - Metric anomalies before failures
  - Configuration changes impact
  - Cascading failures
```

### 4. Causal Correlation
```yaml
Cause-Effect Chains:
  - Resource pressure → Eviction
  - Config change → Pod restart
  - Node failure → Pod rescheduling
  - Network policy → Connection failures
```

## Multi-Dimensional Analysis Framework

### 1. Ownership Dimension
```go
type OwnershipDimension struct {
    // Direct ownership
    OwnerChain []OwnerReference
    
    // Indirect relationships
    SharedNamespace string
    SharedNode      string
    SharedService   string
    
    // Dependency graph
    DependsOn  []ResourceRef
    UsedBy     []ResourceRef
}
```

### 2. Lifecycle Dimension
```go
type LifecycleDimension struct {
    // Creation context
    CreatedBy    string
    CreatedWhen  time.Time
    CreatedWhy   string // From annotations
    
    // State transitions
    StateHistory []StateTransition
    
    // Termination context
    DeletedBy    string
    DeletedWhen  time.Time
    DeletedWhy   string
}
```

### 3. Performance Dimension
```go
type PerformanceDimension struct {
    // Resource efficiency
    RequestedVsUsed map[string]float64
    
    // Latency metrics
    StartupTime     time.Duration
    SchedulingDelay time.Duration
    PullImageTime   time.Duration
    
    // Reliability
    RestartCount    int
    UptimePercent   float64
    FailureRate     float64
}
```

### 4. Business Context Dimension
```go
type BusinessDimension struct {
    // Service ownership
    Team        string
    Application string
    Environment string
    
    // Criticality
    Tier        string // critical, standard, development
    SLA         string
    CostCenter  string
    
    // Dependencies
    UpstreamServices   []string
    DownstreamServices []string
}
```

## Research Questions

### 1. Data Completeness
- What percentage of failures can be explained with available data?
- Which data sources provide highest signal-to-noise ratio?
- What critical data is missing for common scenarios?

### 2. Correlation Effectiveness
- Which dimensional correlations yield most accurate stories?
- How do we handle conflicting signals?
- What confidence thresholds work best?

### 3. Performance Considerations
- How much data can we process in real-time?
- Which correlations can be pre-computed?
- How do we scale with cluster size?

### 4. User Experience
- Which stories do SREs find most valuable?
- How much detail should stories include?
- How do we present uncertainty effectively?

## Implementation Roadmap

### Phase 1: Data Collection Infrastructure
1. API server data collector
2. Event stream processor
3. Metrics aggregator
4. CRI integration

### Phase 2: Correlation Engine
1. Ownership graph builder
2. Temporal correlation
3. Causal chain detection
4. Multi-dimensional scoring

### Phase 3: Story Construction
1. Template engine
2. Narrative generator
3. Confidence scoring
4. Alternative explanations

### Phase 4: Validation & Refinement
1. A/B testing story accuracy
2. SRE feedback integration
3. Pattern learning
4. Continuous improvement

## Conclusion

Kubernetes provides an incredibly rich data landscape that, when properly correlated through multiple dimensions, can yield powerful narratives about system behavior. The key is not collecting more data, but understanding the relationships and constructing coherent stories that help SREs make sense of complex distributed systems.

This research forms the foundation for building a truly revolutionary observability platform that moves beyond naive realism to provide genuine understanding through constructed narratives.