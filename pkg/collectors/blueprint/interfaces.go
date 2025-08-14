package blueprint

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// CollectorBlueprint defines the enhanced interface all collectors should implement
// This extends the basic collectors.Collector interface with production-ready features
type CollectorBlueprint interface {
	collectors.Collector

	// Enhanced health reporting with detailed status
	Health() (bool, map[string]interface{})

	// Statistics returns comprehensive performance metrics
	Statistics() map[string]interface{}

	// UpdateConfig dynamically updates configuration (hot reload)
	UpdateConfig(config interface{}) error

	// GetConfig returns current configuration
	GetConfig() interface{}
}

// ContainerIntegrator provides container runtime correlation capabilities
type ContainerIntegrator interface {
	// UpdatePodInfo updates pod correlation data in eBPF maps
	UpdatePodInfo(cgroupID uint64, podUID, namespace, podName string) error

	// RemovePodInfo removes pod correlation data
	RemovePodInfo(cgroupID uint64) error

	// UpdateContainerInfo updates container correlation data
	UpdateContainerInfo(pid uint32, containerID, podUID, image string) error

	// RemoveContainerInfo removes container correlation data
	RemoveContainerInfo(pid uint32) error

	// UpdateServiceEndpoint updates service endpoint correlation
	UpdateServiceEndpoint(ip string, port uint16, serviceName, namespace, clusterIP string) error

	// RemoveServiceEndpoint removes service endpoint correlation
	RemoveServiceEndpoint(ip string, port uint16) error
}

// EBPFCollector provides eBPF program management capabilities
type EBPFCollector interface {
	// LoadEBPFProgram loads and validates an eBPF program
	LoadEBPFProgram(programName string) error

	// AttachEBPFProgram attaches eBPF program to kernel hooks
	AttachEBPFProgram(programName string, hookPoints []string) error

	// DetachEBPFProgram detaches eBPF program from kernel hooks
	DetachEBPFProgram(programName string) error

	// GetEBPFStats returns eBPF program statistics
	GetEBPFStats() map[string]interface{}
}

// ResourceMonitor provides resource usage monitoring
type ResourceMonitor interface {
	// GetResourceUsage returns current resource consumption
	GetResourceUsage() ResourceUsage

	// SetResourceLimits configures resource limits
	SetResourceLimits(limits ResourceLimits) error

	// IsWithinLimits checks if resource usage is within configured limits
	IsWithinLimits() bool
}

// ConfigurableCollector provides dynamic configuration management
type ConfigurableCollector interface {
	// ValidateConfig validates configuration before applying
	ValidateConfig(config interface{}) error

	// ApplyConfig applies new configuration
	ApplyConfig(config interface{}) error

	// GetConfigSchema returns JSON schema for configuration validation
	GetConfigSchema() map[string]interface{}

	// ResetToDefaults resets configuration to default values
	ResetToDefaults() error
}

// MetricsReporter provides comprehensive metrics reporting
type MetricsReporter interface {
	// RegisterMetrics registers collector-specific metrics
	RegisterMetrics() error

	// RecordEventProcessed records successful event processing
	RecordEventProcessed(eventType string, duration time.Duration)

	// RecordEventDropped records dropped event with reason
	RecordEventDropped(eventType string, reason string)

	// RecordError records error with context
	RecordError(operation string, err error)

	// UpdateHealthMetrics updates health status metrics
	UpdateHealthMetrics(healthy bool, details map[string]interface{})
}

// TraceProvider provides distributed tracing capabilities
type TraceProvider interface {
	// StartSpan creates a new tracing span
	StartSpan(ctx context.Context, operationName string) (context.Context, func())

	// AddSpanEvent adds event to current span
	AddSpanEvent(ctx context.Context, name string, attributes map[string]interface{})

	// SetSpanError records error in current span
	SetSpanError(ctx context.Context, err error)

	// GetTraceID returns current trace ID
	GetTraceID(ctx context.Context) string

	// InjectTraceContext injects trace context into metadata
	InjectTraceContext(ctx context.Context, metadata map[string]string)
}

// K8sMetadataExtractor provides Kubernetes metadata extraction
type K8sMetadataExtractor interface {
	// ExtractPodMetadata extracts pod metadata from various sources
	ExtractPodMetadata(data interface{}) *K8sMetadata

	// ExtractServiceMetadata extracts service metadata
	ExtractServiceMetadata(data interface{}) *K8sServiceMetadata

	// ExtractNodeMetadata extracts node metadata
	ExtractNodeMetadata(data interface{}) *K8sNodeMetadata

	// ResolveOwnerReferences resolves Kubernetes owner references
	ResolveOwnerReferences(namespace, name, kind string) []K8sOwnerRef
}

// EventEnricher provides event enrichment capabilities
type EventEnricher interface {
	// EnrichEvent adds context to raw event data
	EnrichEvent(ctx context.Context, event *collectors.RawEvent) error

	// AddK8sContext adds Kubernetes context to event
	AddK8sContext(event *collectors.RawEvent, metadata *K8sMetadata)

	// AddContainerContext adds container context to event
	AddContainerContext(event *collectors.RawEvent, containerID, image string)

	// AddNetworkContext adds network context to event
	AddNetworkContext(event *collectors.RawEvent, srcIP, dstIP string, srcPort, dstPort uint16)
}

// BufferManager provides event buffer management
type BufferManager interface {
	// GetBufferStats returns buffer utilization statistics
	GetBufferStats() BufferStats

	// ConfigureBuffer configures buffer size and behavior
	ConfigureBuffer(size int, dropPolicy DropPolicy) error

	// FlushBuffer forces buffer flush
	FlushBuffer() error

	// IsBufferFull returns true if buffer is at capacity
	IsBufferFull() bool
}

// SecurityValidator provides security validation
type SecurityValidator interface {
	// ValidateEvent validates event data for security concerns
	ValidateEvent(event *collectors.RawEvent) error

	// FilterSensitiveData removes sensitive data from event
	FilterSensitiveData(event *collectors.RawEvent)

	// CheckPermissions verifies collector has required permissions
	CheckPermissions() error

	// ValidateSource validates event source is trusted
	ValidateSource(source string) error
}

// Data structures for interfaces

// ResourceUsage represents current resource consumption
type ResourceUsage struct {
	MemoryMB       float64   `json:"memory_mb"`
	CPUPercent     float64   `json:"cpu_percent"`
	DiskUsageMB    float64   `json:"disk_usage_mb"`
	NetworkBytesRx uint64    `json:"network_bytes_rx"`
	NetworkBytesTx uint64    `json:"network_bytes_tx"`
	OpenFiles      int       `json:"open_files"`
	Goroutines     int       `json:"goroutines"`
	LastUpdated    time.Time `json:"last_updated"`
}

// ResourceLimits defines resource consumption limits
type ResourceLimits struct {
	MaxMemoryMB    int `json:"max_memory_mb" validate:"min=10,max=1000"`
	MaxCPUPercent  int `json:"max_cpu_percent" validate:"min=1,max=100"`
	MaxDiskUsageMB int `json:"max_disk_usage_mb" validate:"min=10,max=10000"`
	MaxOpenFiles   int `json:"max_open_files" validate:"min=10,max=10000"`
	MaxGoroutines  int `json:"max_goroutines" validate:"min=1,max=1000"`
}

// K8sMetadata represents Kubernetes object metadata
type K8sMetadata struct {
	Namespace    string            `json:"namespace"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	UID          string            `json:"uid"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	OwnerRefs    []K8sOwnerRef     `json:"owner_refs,omitempty"`
	CreationTime time.Time         `json:"creation_time,omitempty"`
}

// K8sServiceMetadata represents Kubernetes service metadata
type K8sServiceMetadata struct {
	K8sMetadata
	ClusterIP   string            `json:"cluster_ip"`
	ExternalIPs []string          `json:"external_ips,omitempty"`
	Ports       []K8sServicePort  `json:"ports,omitempty"`
	Selector    map[string]string `json:"selector,omitempty"`
	ServiceType string            `json:"service_type"`
	Endpoints   []K8sEndpoint     `json:"endpoints,omitempty"`
}

// K8sNodeMetadata represents Kubernetes node metadata
type K8sNodeMetadata struct {
	K8sMetadata
	NodeIP           string             `json:"node_ip"`
	InternalIP       string             `json:"internal_ip"`
	ExternalIP       string             `json:"external_ip,omitempty"`
	Hostname         string             `json:"hostname"`
	KubeletVersion   string             `json:"kubelet_version"`
	ContainerRuntime string             `json:"container_runtime"`
	Conditions       []K8sNodeCondition `json:"conditions,omitempty"`
	Capacity         map[string]string  `json:"capacity,omitempty"`
	Allocatable      map[string]string  `json:"allocatable,omitempty"`
}

// K8sOwnerRef represents Kubernetes owner reference
type K8sOwnerRef struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller *bool  `json:"controller,omitempty"`
}

// K8sServicePort represents Kubernetes service port
type K8sServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"target_port"`
	NodePort   int32  `json:"node_port,omitempty"`
}

// K8sEndpoint represents Kubernetes endpoint
type K8sEndpoint struct {
	IP       string           `json:"ip"`
	Hostname string           `json:"hostname,omitempty"`
	Ports    []K8sServicePort `json:"ports,omitempty"`
	Ready    bool             `json:"ready"`
}

// K8sNodeCondition represents Kubernetes node condition
type K8sNodeCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	LastHeartbeatTime  time.Time `json:"last_heartbeat_time,omitempty"`
	LastTransitionTime time.Time `json:"last_transition_time,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
}

// BufferStats represents buffer utilization statistics
type BufferStats struct {
	Size          int       `json:"size"`
	Capacity      int       `json:"capacity"`
	Utilization   float64   `json:"utilization"`
	DroppedEvents int64     `json:"dropped_events"`
	TotalEvents   int64     `json:"total_events"`
	LastDropTime  time.Time `json:"last_drop_time,omitempty"`
	DropPolicy    string    `json:"drop_policy"`
}

// DropPolicy defines buffer overflow behavior
type DropPolicy string

const (
	DropPolicyOldest DropPolicy = "oldest"
	DropPolicyNewest DropPolicy = "newest"
	DropPolicyRandom DropPolicy = "random"
	DropPolicyBlock  DropPolicy = "block"
)
