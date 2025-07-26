# Tapio Enhancement Roadmap & New Collectors

## 🚀 Enhancement Vision

**Transform Tapio from "event collector" to "story teller" - providing crystal-clear narratives that help overwhelmed SREs understand what happened, why it matters, and what to do about it.**

## 📈 K8s Story-Building Enhancements

### 1. Pod Lifecycle Narratives

**Concept**: Convert complex pod failures into clear stories with timeline, root cause, and impact.

```go
type PodStory struct {
    // Story metadata
    ID          string    `json:"id"`
    Title       string    `json:"title"`       // "Pod myapp-xyz failed during deployment"
    Timestamp   time.Time `json:"timestamp"`
    Duration    time.Duration `json:"duration"`
    
    // Narrative components
    Intent      string `json:"intent"`      // "Deploy new version of myapp"
    Timeline    []StoryEvent `json:"timeline"` // Chronological event sequence
    RootCause   string `json:"root_cause"`  // "Image pull failed due to registry timeout"
    Impact      string `json:"impact"`      // "3 minutes downtime, 50 users affected"
    Resolution  string `json:"resolution"`  // "Retry succeeded after registry recovered"
    
    // Technical details
    WorkloadType    string   `json:"workload_type"`    // "Deployment", "StatefulSet"
    AffectedPods    []string `json:"affected_pods"`
    RelatedServices []string `json:"related_services"`
    BusinessService string   `json:"business_service"`
    
    // Actionable insights
    Recommendations []string `json:"recommendations"`
    RunbookLinks    []string `json:"runbook_links"`
    SimilarIncidents []string `json:"similar_incidents"`
}

type StoryEvent struct {
    Timestamp   time.Time `json:"timestamp"`
    Source      string    `json:"source"`      // "k8s-api", "ebpf", "cri"
    Type        string    `json:"type"`        // "pod.created", "container.failed"
    Description string    `json:"description"` // Human readable
    Severity    string    `json:"severity"`
    Data        map[string]interface{} `json:"data,omitempty"`
}
```

### 2. Multi-Layer Event Correlation Enhancement

**Current**: Basic temporal/spatial correlation  
**Enhanced**: K8s-aware hierarchical correlation

```go
type K8sCorrelationRules struct {
    // Ownership chain correlation
    DeploymentFlow []string // Deployment → ReplicaSet → Pod → Container
    ServiceFlow    []string // Service → Endpoint → Pod → Network
    StorageFlow    []string // PVC → PV → Node → Disk I/O
    
    // Time windows for correlation
    DeploymentWindow time.Duration // 10 minutes
    ServiceWindow    time.Duration // 5 minutes
    NetworkWindow    time.Duration // 30 seconds
}

type K8sHierarchicalCorrelator struct {
    ownershipCache map[string][]string // Resource → Owners chain
    dependencyMap  map[string][]string // Resource → Dependencies
    businessMap    map[string]string   // Resource → Business service
}
```

### 3. Enhanced Kubernetes Context

**Extend UnifiedEvent.KubernetesData with story-building fields:**

```go
type KubernetesData struct {
    // Existing fields...
    EventType, Reason, Object, ObjectKind, Message, Action
    APIVersion, ResourceVersion, Labels, Annotations, ClusterName
    
    // NEW: Story context fields
    WorkloadType     string   `json:"workload_type"`     // "Deployment", "StatefulSet", "Job"
    OwnerChain       []string `json:"owner_chain"`       // ["Deployment/myapp", "ReplicaSet/myapp-abc"]
    DependsOn        []string `json:"depends_on"`        // Services, ConfigMaps, Secrets needed
    BusinessService  string   `json:"business_service"`   // Which business service this supports
    CriticalPath     bool     `json:"critical_path"`      // Is this on the critical path?
    HealthStatus     string   `json:"health_status"`      // "healthy", "degraded", "failing"
    SLOImpact        *SLOImpact `json:"slo_impact,omitempty"`
    
    // Workload-specific metadata
    DeploymentStrategy string `json:"deployment_strategy,omitempty"` // "RollingUpdate", "Recreate"
    ReplicaStatus      *ReplicaStatus `json:"replica_status,omitempty"`
    ResourceRequests   *ResourceSpec `json:"resource_requests,omitempty"`
    ResourceLimits     *ResourceSpec `json:"resource_limits,omitempty"`
}

type SLOImpact struct {
    SLOName           string  `json:"slo_name"`
    TargetAvailability float64 `json:"target_availability"`
    CurrentAvailability float64 `json:"current_availability"`
    ErrorBudgetBurn   float64 `json:"error_budget_burn"`
    TimeToExhaustion  string  `json:"time_to_exhaustion"`
}

type ReplicaStatus struct {
    Desired   int32 `json:"desired"`
    Current   int32 `json:"current"`
    Ready     int32 `json:"ready"`
    Updated   int32 `json:"updated"`
    Available int32 `json:"available"`
}
```

### 4. Story Template Engine

**Common K8s failure patterns with narrative templates:**

```go
type StoryTemplate struct {
    ID          string         `json:"id"`
    Name        string         `json:"name"`
    Category    string         `json:"category"`    // "availability", "performance", "security"
    Pattern     []EventPattern `json:"pattern"`     // Events that must match
    TimeWindow  time.Duration  `json:"time_window"` // Max time between first/last event
    Confidence  float64        `json:"confidence"`  // Pattern match confidence
    
    // Narrative generation
    TitleTemplate       string   `json:"title_template"`
    DescriptionTemplate string   `json:"description_template"`
    ImpactTemplate      string   `json:"impact_template"`
    Recommendations     []string `json:"recommendations"`
    RunbookURL          string   `json:"runbook_url,omitempty"`
    
    // Business impact
    DefaultSeverity     string  `json:"default_severity"`
    BusinessImpactScale float64 `json:"business_impact_scale"` // Multiplier
}

var K8sStoryTemplates = map[string]StoryTemplate{
    "pod-crashloop": {
        ID:         "k8s-pod-crashloop",
        Name:       "Pod CrashLoopBackOff",
        Category:   "availability",
        TimeWindow: 10 * time.Minute,
        Pattern: []EventPattern{
            {Type: "k8s.pod.failed", MaxAge: 5 * time.Minute},
            {Type: "container.exit", Condition: "exit_code != 0", MinCount: 2},
            {Type: "k8s.event", Reason: "BackOff", MinCount: 3},
        },
        TitleTemplate: "Pod {{.PodName}} in CrashLoopBackOff",
        DescriptionTemplate: `Pod {{.PodName}} in namespace {{.Namespace}} is crashing repeatedly. 
        The container has failed {{.FailureCount}} times in the last {{.Duration}}.
        Exit code: {{.ExitCode}}. Last error: {{.LastError}}`,
        Recommendations: []string{
            "Check application logs for startup errors",
            "Verify resource limits and requests",
            "Check ConfigMap and Secret availability",
            "Validate image and dependencies",
        },
    },
    
    "resource-starvation": {
        ID:       "k8s-resource-starvation",
        Name:     "Resource Starvation Cascade",
        Category: "performance",
        Pattern: []EventPattern{
            {Type: "k8s.pod.evicted", Reason: "OutOfMemory|OutOfDisk"},
            {Type: "ebpf.memory.pressure", Severity: "high"},
            {Type: "k8s.node.pressure", MinCount: 1},
        },
        TitleTemplate: "Resource starvation affecting {{.AffectedPods}} pods",
        DescriptionTemplate: `Node {{.NodeName}} is under resource pressure, causing pod evictions.
        {{.EvictedCount}} pods evicted due to {{.ResourceType}} shortage.
        Available {{.ResourceType}}: {{.AvailableAmount}}`,
    },
    
    "service-disruption": {
        ID:       "k8s-service-disruption", 
        Name:     "Service Disruption",
        Category: "availability",
        Pattern: []EventPattern{
            {Type: "k8s.endpoints.removed", MinCount: 1},
            {Type: "network.connection.failed", MinCount: 5},
            {Type: "k8s.pod.notready", MinCount: 1},
        },
        TitleTemplate: "Service {{.ServiceName}} disrupted",
        DescriptionTemplate: `Service {{.ServiceName}} lost {{.LostEndpoints}} endpoints.
        {{.FailedConnections}} connection failures detected.
        Affected business service: {{.BusinessService}}`,
    },
}
```

### 5. Business Impact Calculator

**Map K8s resources to business impact:**

```go
type K8sImpactAnalyzer struct {
    serviceMap     map[string]BusinessService `json:"service_map"`
    sloMap         map[string]SLO            `json:"slo_map"`
    dependencyGraph map[string][]string       `json:"dependency_graph"`
    userImpactRules []UserImpactRule          `json:"user_impact_rules"`
}

type BusinessService struct {
    Name             string            `json:"name"`
    Priority         float64           `json:"priority"`         // 0.0-1.0
    UserCount        int               `json:"user_count"`
    IsCustomerFacing bool              `json:"is_customer_facing"`
    IsRevenueImpacting bool            `json:"is_revenue_impacting"`
    SLOTargets       map[string]float64 `json:"slo_targets"`     // "availability": 99.9
    K8sResources     []string          `json:"k8s_resources"`   // Namespaces/services
    Dependencies     []string          `json:"dependencies"`
    OnCallTeam       string            `json:"on_call_team"`
    EscalationPolicy string            `json:"escalation_policy"`
}

type SLO struct {
    Name           string        `json:"name"`
    Type           string        `json:"type"`           // "availability", "latency", "error_rate"
    Target         float64       `json:"target"`         // 99.9% availability
    Window         time.Duration `json:"window"`         // 30 days
    ErrorBudget    float64       `json:"error_budget"`   // Remaining budget
    BurnRate       float64       `json:"burn_rate"`      // Current burn rate
    AlertThreshold float64       `json:"alert_threshold"` // When to alert
}

func (k *K8sImpactAnalyzer) CalculateImpact(story *PodStory) *ImpactContext {
    // Determine affected business services
    services := k.getAffectedServices(story.AffectedPods)
    
    // Calculate user impact
    totalUsers := 0
    maxPriority := 0.0
    for _, service := range services {
        totalUsers += service.UserCount
        if service.Priority > maxPriority {
            maxPriority = service.Priority
        }
    }
    
    // Check SLO impact
    sloImpact := k.calculateSLOImpact(services, story.Duration)
    
    return &ImpactContext{
        Severity:         k.mapPriorityToSeverity(maxPriority),
        BusinessImpact:   maxPriority,
        AffectedServices: k.getServiceNames(services),
        AffectedUsers:    totalUsers,
        SLOImpact:        sloImpact.HasViolation,
        CustomerFacing:   k.hasCustomerFacingService(services),
        RevenueImpacting: k.hasRevenueImpactingService(services),
    }
}
```

### 6. Story Assembly Engine

**Real-time story building as events arrive:**

```go
type K8sStoryBuilder struct {
    templates      map[string]StoryTemplate  `json:"templates"`
    correlationEngine *K8sHierarchicalCorrelator `json:"-"`
    impactAnalyzer *K8sImpactAnalyzer        `json:"-"`
    
    // Active story tracking
    activeStories  map[string]*PartialStory  `json:"-"`
    storyTimeout   time.Duration             `json:"story_timeout"`
    maxStories     int                       `json:"max_stories"`
    
    // Story completion
    completedStories chan *PodStory          `json:"-"`
    storyMetrics     *StoryMetrics           `json:"-"`
}

type PartialStory struct {
    ID            string                 `json:"id"`
    TemplateID    string                 `json:"template_id"`
    Events        []*UnifiedEvent        `json:"events"`
    StartTime     time.Time              `json:"start_time"`
    LastUpdate    time.Time              `json:"last_update"`
    Confidence    float64                `json:"confidence"`
    PatternMatch  map[string]int         `json:"pattern_match"` // Pattern → count
    Variables     map[string]interface{} `json:"variables"`     // Template variables
}

func (sb *K8sStoryBuilder) ProcessEvent(event *UnifiedEvent) (*PodStory, error) {
    // Try to match event against active stories
    for storyID, partial := range sb.activeStories {
        if sb.eventMatchesStory(event, partial) {
            partial.Events = append(partial.Events, event)
            partial.LastUpdate = time.Now()
            
            // Update pattern matching
            sb.updatePatternMatching(partial, event)
            
            // Check if story is complete
            if sb.isStoryComplete(partial) {
                story := sb.buildCompleteStory(partial)
                delete(sb.activeStories, storyID)
                return story, nil
            }
            return nil, nil
        }
    }
    
    // Try to start new story
    for templateID, template := range sb.templates {
        if sb.eventMatchesTemplate(event, template) {
            partial := &PartialStory{
                ID:           generateStoryID(),
                TemplateID:   templateID,
                Events:       []*UnifiedEvent{event},
                StartTime:    event.Timestamp,
                LastUpdate:   time.Now(),
                PatternMatch: make(map[string]int),
                Variables:    make(map[string]interface{}),
            }
            sb.activeStories[partial.ID] = partial
            break
        }
    }
    
    return nil, nil
}
```

## 🔧 New Collector Specifications

### 1. CRI (Container Runtime Interface) Collector

**Purpose**: Bridge the gap between K8s API events and actual container runtime behavior.

#### Technical Specification

```go
package cri

import (
    "context"
    "time"
    "google.golang.org/grpc"
    pb "k8s.io/cri-api/pkg/apis/runtime/v1"
    "github.com/yairfalse/tapio/pkg/domain"
)

// CRICollector monitors container runtime via CRI gRPC API
type CRICollector struct {
    // Connection
    runtimeClient pb.RuntimeServiceClient
    imageClient   pb.ImageServiceClient
    endpoint      string // Unix socket: /run/containerd/containerd.sock
    
    // Configuration  
    config        *CRIConfig
    pollInterval  time.Duration
    eventBuffer   chan *CRIEvent
    
    // State tracking
    containerCache map[string]*ContainerInfo
    podCache       map[string]*PodInfo
    lastSync       time.Time
    
    // Metrics
    metrics *CRIMetrics
}

type CRIConfig struct {
    RuntimeEndpoint string        `json:"runtime_endpoint"` // /run/containerd/containerd.sock
    PollInterval    time.Duration `json:"poll_interval"`    // 5s
    BufferSize      int           `json:"buffer_size"`      // 1000
    EnableEvents    bool          `json:"enable_events"`    // true
    EnableMetrics   bool          `json:"enable_metrics"`   // true
    MaxContainers   int           `json:"max_containers"`   // 10000
    
    // Filtering
    IncludeNamespaces []string `json:"include_namespaces,omitempty"`
    ExcludeNamespaces []string `json:"exclude_namespaces,omitempty"`
    IncludeLabels     map[string]string `json:"include_labels,omitempty"`
}

// CRI Event Types
type CRIEventType string

const (
    CRIEventContainerCreated  CRIEventType = "container_created"
    CRIEventContainerStarted  CRIEventType = "container_started"
    CRIEventContainerStopped  CRIEventType = "container_stopped"
    CRIEventContainerRemoved  CRIEventType = "container_removed"
    CRIEventContainerFailed   CRIEventType = "container_failed"
    CRIEventImagePulled       CRIEventType = "image_pulled"
    CRIEventImagePullFailed   CRIEventType = "image_pull_failed"
    CRIEventPodSandboxCreated CRIEventType = "pod_sandbox_created"
    CRIEventPodSandboxRemoved CRIEventType = "pod_sandbox_removed"
    CRIEventResourceLimitHit  CRIEventType = "resource_limit_hit"
)

type CRIEvent struct {
    // Core fields
    Type        CRIEventType `json:"type"`
    Timestamp   time.Time    `json:"timestamp"`
    ContainerID string       `json:"container_id,omitempty"`
    PodID       string       `json:"pod_id,omitempty"`
    
    // Container details
    Image       string            `json:"image,omitempty"`
    Command     []string          `json:"command,omitempty"`
    Args        []string          `json:"args,omitempty"`
    Labels      map[string]string `json:"labels,omitempty"`
    Annotations map[string]string `json:"annotations,omitempty"`
    
    // Runtime details
    Runtime     string `json:"runtime"`      // "containerd", "cri-o"
    ExitCode    int32  `json:"exit_code,omitempty"`
    ExitReason  string `json:"exit_reason,omitempty"`
    Signal      string `json:"signal,omitempty"`
    
    // Resource information
    Resources *ContainerResources `json:"resources,omitempty"`
    
    // Error details
    ErrorMessage string `json:"error_message,omitempty"`
    ErrorCode    string `json:"error_code,omitempty"`
}

type ContainerResources struct {
    CPULimit      int64  `json:"cpu_limit,omitempty"`      // CPU limit in millicores
    MemoryLimit   int64  `json:"memory_limit,omitempty"`   // Memory limit in bytes
    CPURequest    int64  `json:"cpu_request,omitempty"`    // CPU request in millicores
    MemoryRequest int64  `json:"memory_request,omitempty"` // Memory request in bytes
    CPUUsage      int64  `json:"cpu_usage,omitempty"`      // Current CPU usage
    MemoryUsage   int64  `json:"memory_usage,omitempty"`   // Current memory usage
}
```

#### CRI → UnifiedEvent Conversion

```go
func (c *CRICollector) convertToUnifiedEvent(criEvent *CRIEvent) *domain.UnifiedEvent {
    builder := domain.NewUnifiedEvent().
        WithSource("cri").
        WithType(domain.EventTypeKubernetes)
    
    // Add Kubernetes context
    k8sData := &domain.KubernetesData{
        EventType:    mapCRIEventType(criEvent.Type),
        Reason:       getCRIReason(criEvent),
        Message:      formatCRIMessage(criEvent),
        Labels:       criEvent.Labels,
        Annotations:  criEvent.Annotations,
        // NEW: Story context
        WorkloadType: getWorkloadTypeFromLabels(criEvent.Labels),
        OwnerChain:   buildOwnerChain(criEvent.Labels),
    }
    
    // Add semantic context
    builder = builder.WithSemantic(
        getCRISemanticIntent(criEvent.Type),
        "container-runtime",
        buildCRITags(criEvent)...,
    )
    
    // Add entity context
    entityType := "container"
    entityName := criEvent.ContainerID
    namespace := criEvent.Labels["io.kubernetes.pod.namespace"]
    
    if criEvent.Type == CRIEventPodSandboxCreated || criEvent.Type == CRIEventPodSandboxRemoved {
        entityType = "pod"
        entityName = criEvent.Labels["io.kubernetes.pod.name"]
    }
    
    builder = builder.WithEntity(entityType, entityName, namespace)
    
    // Add impact based on event type
    severity := "low"
    businessImpact := 0.1
    
    switch criEvent.Type {
    case CRIEventContainerFailed, CRIEventImagePullFailed:
        severity = "high"
        businessImpact = 0.8
    case CRIEventResourceLimitHit:
        severity = "medium" 
        businessImpact = 0.5
    }
    
    builder = builder.WithImpact(severity, businessImpact)
    
    event := builder.Build()
    
    // Add CRI-specific data to Kubernetes section
    if criEvent.Resources != nil {
        event.Kubernetes.ResourceRequests = &domain.ResourceSpec{
            CPU:    criEvent.Resources.CPURequest,
            Memory: criEvent.Resources.MemoryRequest,
        }
        event.Kubernetes.ResourceLimits = &domain.ResourceSpec{
            CPU:    criEvent.Resources.CPULimit,
            Memory: criEvent.Resources.MemoryLimit,
        }
    }
    
    return event
}
```

#### Story Integration

**CRI events enhance pod lifecycle stories with runtime details:**

1. **Container startup failures** → Root cause analysis (image pull, resource limits)
2. **Resource limit hits** → Performance degradation stories
3. **Exit code patterns** → Application health stories
4. **Image pull timing** → Deployment velocity stories

### 2. Control Plane Collector (etcd + API Server)

**Purpose**: Monitor Kubernetes control plane health and performance for cluster-wide story context.

#### Technical Specification

```go
package controlplane

import (
    "context"
    "time"
    clientv3 "go.etcd.io/etcd/client/v3"
    "k8s.io/client-go/kubernetes"
    "github.com/yairfalse/tapio/pkg/domain"
)

// ControlPlaneCollector monitors K8s control plane components
type ControlPlaneCollector struct {
    // Clients
    etcdClient clientv3.Client
    k8sClient  kubernetes.Interface
    
    // Configuration
    config *ControlPlaneConfig
    
    // Monitoring targets
    components map[string]*ComponentMonitor
    
    // Event streaming
    eventStream chan *ControlPlaneEvent
    metrics     *ControlPlaneMetrics
}

type ControlPlaneConfig struct {
    // etcd configuration
    EtcdEndpoints []string      `json:"etcd_endpoints"`
    EtcdTimeout   time.Duration `json:"etcd_timeout"`
    
    // API server configuration
    APIServerEndpoint string `json:"api_server_endpoint"`
    
    // Monitoring configuration
    PollInterval     time.Duration `json:"poll_interval"`     // 30s
    HealthInterval   time.Duration `json:"health_interval"`   // 10s
    MetricsInterval  time.Duration `json:"metrics_interval"`  // 60s
    
    // Component monitoring
    MonitorEtcd         bool `json:"monitor_etcd"`
    MonitorAPIServer    bool `json:"monitor_api_server"`
    MonitorScheduler    bool `json:"monitor_scheduler"`
    MonitorController   bool `json:"monitor_controller"`
    
    // Alert thresholds
    EtcdLatencyThreshold    time.Duration `json:"etcd_latency_threshold"`    // 100ms
    APIServerLatencyThreshold time.Duration `json:"api_server_latency_threshold"` // 1s
    LeaderElectionTimeout   time.Duration `json:"leader_election_timeout"`   // 30s
}

type ControlPlaneEventType string

const (
    // etcd events
    CPEventEtcdHealthy        ControlPlaneEventType = "etcd_healthy"
    CPEventEtcdUnhealthy      ControlPlaneEventType = "etcd_unhealthy"
    CPEventEtcdLeaderChanged  ControlPlaneEventType = "etcd_leader_changed"
    CPEventEtcdCompaction     ControlPlaneEventType = "etcd_compaction"
    CPEventEtcdHighLatency    ControlPlaneEventType = "etcd_high_latency"
    CPEventEtcdStorageFull    ControlPlaneEventType = "etcd_storage_full"
    
    // API Server events
    CPEventAPIServerHealthy   ControlPlaneEventType = "api_server_healthy"
    CPEventAPIServerUnhealthy ControlPlaneEventType = "api_server_unhealthy"
    CPEventAPIServerOverload  ControlPlaneEventType = "api_server_overload" 
    CPEventAPIServerThrottling ControlPlaneEventType = "api_server_throttling"
    
    // Scheduler events
    CPEventSchedulerHealthy   ControlPlaneEventType = "scheduler_healthy"
    CPEventSchedulerUnhealthy ControlPlaneEventType = "scheduler_unhealthy"
    CPEventSchedulingLatency  ControlPlaneEventType = "scheduling_latency"
    
    // Controller events
    CPEventControllerHealthy   ControlPlaneEventType = "controller_healthy"
    CPEventControllerUnhealthy ControlPlaneEventType = "controller_unhealthy"
    CPEventControllerLag       ControlPlaneEventType = "controller_lag"
)

type ControlPlaneEvent struct {
    Type        ControlPlaneEventType `json:"type"`
    Timestamp   time.Time            `json:"timestamp"`
    Component   string               `json:"component"`   // "etcd", "api-server", "scheduler"
    Node        string               `json:"node,omitempty"`
    
    // Health details
    Status      string               `json:"status"`      // "healthy", "degraded", "unhealthy"
    Message     string               `json:"message"`
    
    // Performance metrics
    Latency     time.Duration        `json:"latency,omitempty"`
    RequestRate float64              `json:"request_rate,omitempty"`
    ErrorRate   float64              `json:"error_rate,omitempty"`
    
    // etcd specific
    EtcdMember     string `json:"etcd_member,omitempty"`
    EtcdLeader     string `json:"etcd_leader,omitempty"`
    EtcdDBSize     int64  `json:"etcd_db_size,omitempty"`
    EtcdRevision   int64  `json:"etcd_revision,omitempty"`
    
    // API Server specific
    AdmissionLatency    time.Duration `json:"admission_latency,omitempty"`
    AuthenticationRate  float64       `json:"authentication_rate,omitempty"`
    WebhookLatency      time.Duration `json:"webhook_latency,omitempty"`
    
    // Additional context
    Severity    string                 `json:"severity"`
    Impact      string                 `json:"impact"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}
```

#### Control Plane → UnifiedEvent Conversion

```go
func (cp *ControlPlaneCollector) convertToUnifiedEvent(cpEvent *ControlPlaneEvent) *domain.UnifiedEvent {
    builder := domain.NewUnifiedEvent().
        WithSource("control-plane").
        WithType(domain.EventTypeSystem)
    
    // Add semantic context
    intent := getControlPlaneIntent(cpEvent.Type)
    category := "infrastructure"
    if isPerformanceEvent(cpEvent.Type) {
        category = "performance"
    }
    
    builder = builder.WithSemantic(intent, category, cpEvent.Component, "control-plane")
    
    // Add entity context - the control plane component
    builder = builder.WithEntity("control-plane-component", cpEvent.Component, "kube-system")
    
    // Add Kubernetes context
    k8sData := &domain.KubernetesData{
        EventType:   "Normal",
        Reason:      formatControlPlaneReason(cpEvent.Type),
        Message:     cpEvent.Message,
        Object:      fmt.Sprintf("component/%s", cpEvent.Component),
        ObjectKind:  "Component",
        
        // NEW: Story context
        WorkloadType:    "ControlPlane",
        BusinessService: "kubernetes-cluster",
        CriticalPath:    true, // Control plane is always critical
        HealthStatus:    cpEvent.Status,
    }
    
    // Calculate impact
    severity, businessImpact := calculateControlPlaneImpact(cpEvent)
    builder = builder.WithImpact(severity, businessImpact)
    
    event := builder.Build()
    
    // Add control plane specific metrics
    if event.Metrics == nil {
        event.Metrics = &domain.MetricsData{}
    }
    
    event.Metrics.Labels = map[string]string{
        "component": cpEvent.Component,
        "node":      cpEvent.Node,
        "status":    cpEvent.Status,
    }
    
    if cpEvent.Latency > 0 {
        event.Metrics.MetricName = fmt.Sprintf("%s_latency", cpEvent.Component)
        event.Metrics.Value = float64(cpEvent.Latency.Milliseconds())
        event.Metrics.Unit = "ms"
    }
    
    return event
}
```

#### Story Integration

**Control plane events provide cluster-wide context for pod stories:**

1. **etcd leader changes** → Explain scheduling delays, API server errors
2. **API server overload** → Explain kubectl timeouts, deployment failures
3. **Scheduler latency** → Explain pod pending states
4. **Controller lag** → Explain replica set scaling delays

## 📊 Implementation Priority

### Phase 1: K8s Story Enhancement (2-3 weeks)
1. **Enhanced KubernetesData structure** - Add story context fields
2. **Story template engine** - Implement common failure patterns  
3. **K8s hierarchical correlator** - Ownership chain correlation
4. **Business impact calculator** - SLO and service mapping
5. **Story builder integration** - Real-time story assembly

### Phase 2: CRI Collector (2 weeks)
1. **CRI client implementation** - gRPC connection to container runtime
2. **Event polling and streaming** - Container lifecycle monitoring
3. **UnifiedEvent conversion** - Map CRI events to standard format
4. **Story integration** - Enhance pod stories with runtime details

### Phase 3: Control Plane Collector (2 weeks)  
1. **etcd monitoring** - Health, performance, leader election
2. **API server monitoring** - Request latency, throttling, errors
3. **Component health checks** - Scheduler, controller manager
4. **Cluster-wide story context** - Infrastructure impact correlation

### Phase 4: Advanced Story Features (2-3 weeks)
1. **ML pattern discovery** - MCP integration for correlation learning
2. **Historical story analysis** - Pattern evolution over time
3. **Predictive insights** - Early warning system
4. **Advanced SRE dashboard** - Story-centric UI

## 🎯 Success Metrics

### Story Quality Metrics
- **Story accuracy**: % of stories that correctly identify root cause
- **Story completeness**: % of incidents covered by stories vs raw events
- **Time to insight**: Median time from incident start to story completion
- **SRE satisfaction**: User feedback on story usefulness

### Technical Performance
- **Story latency**: Time from event ingestion to story publication
- **Correlation accuracy**: % of correctly correlated events
- **False positive rate**: % of incorrect story classifications
- **System throughput**: Events processed per second with story building

### Business Impact
- **MTTR reduction**: Mean time to resolution improvement
- **Alert fatigue reduction**: % decrease in low-value alerts
- **SRE productivity**: Time saved on incident investigation
- **Service reliability**: Improvement in overall uptime

---

*Document created: 2025-01-26*  
*Status: Enhancement roadmap and collector specifications*  
*Next: Implementation planning and development*