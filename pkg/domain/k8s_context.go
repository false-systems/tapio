package domain

import (
	"time"
)

// K8sContext contains comprehensive Kubernetes context for correlation
type K8sContext struct {
	// Resource Identity
	APIVersion string `json:"api_version,omitempty"`
	Kind       string `json:"kind,omitempty"`
	UID        string `json:"uid,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`

	// Ownership & Management
	OwnerReferences []OwnerReference `json:"owner_references,omitempty"`
	Controller      *ControllerRef   `json:"controller,omitempty"`
	ManagedFields   []ManagedField   `json:"managed_fields,omitempty"`

	// Resource Metadata
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Generation      int64             `json:"generation,omitempty"`
	ResourceVersion string            `json:"resource_version,omitempty"`

	// Relationships
	Selectors    map[string]string    `json:"selectors,omitempty"`
	Dependencies []ResourceDependency `json:"dependencies,omitempty"`
	Consumers    []K8sResourceRef     `json:"consumers,omitempty"`

	// Placement & Topology
	NodeName    string `json:"node_name,omitempty"`
	Zone        string `json:"zone,omitempty"`
	Region      string `json:"region,omitempty"`
	ClusterName string `json:"cluster_name,omitempty"`

	// Workload Context
	WorkloadKind string `json:"workload_kind,omitempty"` // Deployment, StatefulSet
	WorkloadName string `json:"workload_name,omitempty"`
	ReplicaIndex *int   `json:"replica_index,omitempty"` // For StatefulSets

	// State Information
	Phase      string              `json:"phase,omitempty"`
	Conditions []ConditionSnapshot `json:"conditions,omitempty"`

	// Resource Specifications
	ResourceRequests ResourceList `json:"resource_requests,omitempty"`
	ResourceLimits   ResourceList `json:"resource_limits,omitempty"`
	QoSClass         string       `json:"qos_class,omitempty"`
}

// OwnerReference contains enough information to let you identify an owning object
type OwnerReference struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Controller         *bool  `json:"controller,omitempty"`
	BlockOwnerDeletion *bool  `json:"blockOwnerDeletion,omitempty"`
}

// ControllerRef contains a reference to the managing controller
type ControllerRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
}

// ManagedField is a workflow-id, a FieldSet and the group version of the resource
type ManagedField struct {
	Manager    string    `json:"manager,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	APIVersion string    `json:"apiVersion,omitempty"`
	Time       time.Time `json:"time,omitempty"`
	FieldsType string    `json:"fieldsType,omitempty"`
}

// ResourceDependency represents a dependency relationship
type ResourceDependency struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Type      string `json:"type"` // "config", "storage", "network", "service"
	Required  bool   `json:"required"`
	Status    string `json:"status,omitempty"` // "satisfied", "missing", "error"
}

// K8sResourceRef is a reference to another K8s resource
type K8sResourceRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	UID        string `json:"uid,omitempty"`
}

// ConditionSnapshot captures condition state at event time
type ConditionSnapshot struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	LastTransitionTime time.Time `json:"last_transition_time"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
}

// ResourceList is a map of resource name to quantity
type ResourceList map[string]string // e.g., "cpu": "100m", "memory": "128Mi"

// ResourceContext captures resource state and intent
type ResourceContext struct {
	// Desired State (from spec)
	DesiredState *ResourceState `json:"desired_state,omitempty"`

	// Actual State (from status)
	ActualState *ResourceState `json:"actual_state,omitempty"`

	// Divergence Analysis
	Divergences     []StateDivergence `json:"divergences,omitempty"`
	ReconcileStatus string            `json:"reconcile_status,omitempty"`

	// Historical Context
	PreviousState   *ResourceState   `json:"previous_state,omitempty"`
	StateTransition *StateTransition `json:"state_transition,omitempty"`
	UpdateHistory   []UpdateRecord   `json:"update_history,omitempty"`
}

// ResourceState represents a state snapshot
type ResourceState struct {
	Replicas        *ReplicaState    `json:"replicas,omitempty"`
	ContainerStates []ContainerState `json:"container_states,omitempty"`
	VolumeStates    []VolumeState    `json:"volume_states,omitempty"`
	NetworkState    *NetworkState    `json:"network_state,omitempty"`
	Custom          interface{}      `json:"custom,omitempty"` // For CRDs
}

// ReplicaState tracks replica information
type ReplicaState struct {
	Desired   int32 `json:"desired"`
	Current   int32 `json:"current"`
	Ready     int32 `json:"ready"`
	Updated   int32 `json:"updated,omitempty"`
	Available int32 `json:"available,omitempty"`
}

// ContainerState represents container state
type ContainerState struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	Image        string `json:"image"`
	ImageID      string `json:"image_id,omitempty"`
	ContainerID  string `json:"container_id,omitempty"`
	Started      *bool  `json:"started,omitempty"`
}

// VolumeState represents volume state
type VolumeState struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // configMap, secret, pvc, etc.
	Ready     bool   `json:"ready"`
	MountPath string `json:"mount_path,omitempty"`
	SourceRef string `json:"source_ref,omitempty"`
}

// NetworkState represents network state
type NetworkState struct {
	PodIP  string   `json:"pod_ip,omitempty"`
	PodIPs []string `json:"pod_ips,omitempty"`
	HostIP string   `json:"host_ip,omitempty"`
	Ports  []Port   `json:"ports,omitempty"`
}

// Port represents a network port
type Port struct {
	Name          string `json:"name,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	ContainerPort int32  `json:"container_port"`
	HostPort      int32  `json:"host_port,omitempty"`
}

// StateDivergence represents a specific divergence
type StateDivergence struct {
	Field        string      `json:"field"`
	DesiredValue interface{} `json:"desired_value"`
	ActualValue  interface{} `json:"actual_value"`
	Reason       string      `json:"reason,omitempty"`
	Impact       string      `json:"impact,omitempty"`
	Since        time.Time   `json:"since"`
}

// StateTransition represents a state change
type StateTransition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// UpdateRecord represents an update event
type UpdateRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // "scale", "image", "config"
	Field     string    `json:"field"`
	OldValue  string    `json:"old_value,omitempty"`
	NewValue  string    `json:"new_value"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// OperationalContext captures runtime behavior
type OperationalContext struct {
	// Performance Metrics
	ResourceUtilization *ResourceUtilization `json:"resource_utilization,omitempty"`
	LatencyProfile      *LatencyProfile      `json:"latency_profile,omitempty"`
	ThroughputMetrics   *ThroughputMetrics   `json:"throughput_metrics,omitempty"`

	// Reliability Indicators
	HealthStatus        string               `json:"health_status"`
	AvailabilityMetrics *AvailabilityMetrics `json:"availability_metrics,omitempty"`
	ErrorMetrics        *K8sErrorMetrics     `json:"error_metrics,omitempty"`

	// Behavioral Patterns
	ScalingBehavior *ScalingBehavior `json:"scaling_behavior,omitempty"`
	RestartPatterns *RestartPatterns `json:"restart_patterns,omitempty"`
	TrafficPatterns *TrafficPatterns `json:"traffic_patterns,omitempty"`
}

// ResourceUtilization tracks resource usage
type ResourceUtilization struct {
	CPU     *ResourceMetric `json:"cpu,omitempty"`
	Memory  *ResourceMetric `json:"memory,omitempty"`
	Disk    *ResourceMetric `json:"disk,omitempty"`
	Network *NetworkMetric  `json:"network,omitempty"`
}

// ResourceMetric represents a resource metric
type ResourceMetric struct {
	Current    float64         `json:"current"`
	Average    float64         `json:"average"`
	Peak       float64         `json:"peak"`
	Trend      string          `json:"trend"`                // "increasing", "stable", "decreasing"
	Percentile map[int]float64 `json:"percentile,omitempty"` // 50, 90, 95, 99
}

// NetworkMetric represents network metrics
type NetworkMetric struct {
	BytesIn    int64   `json:"bytes_in"`
	BytesOut   int64   `json:"bytes_out"`
	PacketsIn  int64   `json:"packets_in"`
	PacketsOut int64   `json:"packets_out"`
	ErrorRate  float64 `json:"error_rate"`
	Latency    float64 `json:"latency_ms"`
}

// LatencyProfile tracks latency characteristics
type LatencyProfile struct {
	P50    float64 `json:"p50_ms"`
	P90    float64 `json:"p90_ms"`
	P95    float64 `json:"p95_ms"`
	P99    float64 `json:"p99_ms"`
	Mean   float64 `json:"mean_ms"`
	StdDev float64 `json:"std_dev_ms"`
}

// ThroughputMetrics tracks throughput
type ThroughputMetrics struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	BytesPerSecond    float64 `json:"bytes_per_second"`
	ActiveConnections int64   `json:"active_connections"`
}

// AvailabilityMetrics tracks availability
type AvailabilityMetrics struct {
	Uptime           float64       `json:"uptime_percent"`
	LastDowntime     *time.Time    `json:"last_downtime,omitempty"`
	DowntimeDuration time.Duration `json:"downtime_duration,omitempty"`
	RestartCount     int32         `json:"restart_count"`
}

// K8sErrorMetrics tracks errors for K8s resources
type K8sErrorMetrics struct {
	ErrorRate  float64          `json:"error_rate"`
	ErrorCount int64            `json:"error_count"`
	ErrorTypes map[string]int64 `json:"error_types,omitempty"`
	LastError  *time.Time       `json:"last_error,omitempty"`
}

// ScalingBehavior tracks scaling patterns
type ScalingBehavior struct {
	ScaleUpCount    int64      `json:"scale_up_count"`
	ScaleDownCount  int64      `json:"scale_down_count"`
	LastScaleEvent  *time.Time `json:"last_scale_event,omitempty"`
	AverageReplicas float64    `json:"average_replicas"`
	ScalingVelocity float64    `json:"scaling_velocity"` // changes per hour
}

// RestartPatterns tracks restart behavior
type RestartPatterns struct {
	TotalRestarts  int32          `json:"total_restarts"`
	RecentRestarts int32          `json:"recent_restarts"` // last hour
	RestartRate    float64        `json:"restart_rate"`    // per hour
	LastRestart    *time.Time     `json:"last_restart,omitempty"`
	RestartReasons map[string]int `json:"restart_reasons,omitempty"`
}

// TrafficPatterns tracks traffic characteristics
type TrafficPatterns struct {
	Pattern         string  `json:"pattern"` // "steady", "spiky", "periodic"
	PeakHour        int     `json:"peak_hour,omitempty"`
	BaselineTraffic float64 `json:"baseline_traffic"`
	PeakTraffic     float64 `json:"peak_traffic"`
	Seasonality     string  `json:"seasonality,omitempty"` // "daily", "weekly"
}
