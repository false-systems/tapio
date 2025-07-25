package core

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface for CNI event collection
// This follows Tapio's 5-level hierarchy architecture and returns UnifiedEvent
// directly from source, eliminating conversion overhead and enabling rich
// semantic correlation from the beginning.
type Collector interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error

	// Event streaming - Returns UnifiedEvent for direct analytics integration
	Events() <-chan domain.UnifiedEvent

	// Health and monitoring
	Health() domain.HealthStatus
	Statistics() Statistics

	// Configuration
	Configure(config Config) error
}

// Config defines CNI collector configuration
// Supports multiple CNI plugins and monitoring approaches for comprehensive
// container network observability.
type Config struct {
	// Basic settings
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	EventBufferSize int    `json:"event_buffer_size"`

	// CNI monitoring configuration
	CNIBinPath       string   `json:"cni_bin_path"`      // Path to CNI binaries directory
	CNIConfPath      string   `json:"cni_conf_path"`     // Path to CNI configuration directory
	MonitoredPlugins []string `json:"monitored_plugins"` // Specific CNI plugins to monitor

	// Monitoring approaches
	EnableLogMonitoring     bool `json:"enable_log_monitoring"`     // Monitor CNI plugin logs
	EnableProcessMonitoring bool `json:"enable_process_monitoring"` // Monitor CNI binary executions
	EnableEventMonitoring   bool `json:"enable_event_monitoring"`   // Monitor Kubernetes CNI events
	EnableFileMonitoring    bool `json:"enable_file_monitoring"`    // Monitor CNI config file changes

	// Plugin-specific configurations
	CiliumConfig  *CiliumConfig  `json:"cilium_config,omitempty"`
	CalicoConfig  *CalicoConfig  `json:"calico_config,omitempty"`
	FlannelConfig *FlannelConfig `json:"flannel_config,omitempty"`
	AWSVPCConfig  *AWSVPCConfig  `json:"aws_vpc_config,omitempty"`

	// Performance tuning
	PollInterval       time.Duration `json:"poll_interval"`
	EventRateLimit     int           `json:"event_rate_limit"`
	MaxConcurrentWatch int           `json:"max_concurrent_watch"`

	// Kubernetes integration
	KubeConfig    string `json:"kubeconfig,omitempty"`
	InCluster     bool   `json:"in_cluster"`
	Namespace     string `json:"namespace,omitempty"` // Watch specific namespace
	LabelSelector string `json:"label_selector,omitempty"`

	// Correlation settings
	EnableTraceCorrelation bool          `json:"enable_trace_correlation"`
	CorrelationTimeout     time.Duration `json:"correlation_timeout"`
}

// Plugin-specific configurations for advanced monitoring
type CiliumConfig struct {
	HubbleEndpoint    string `json:"hubble_endpoint,omitempty"`
	EnableFlowMonitor bool   `json:"enable_flow_monitor"`
	EnablePolicyAudit bool   `json:"enable_policy_audit"`
}

type CalicoConfig struct {
	FelixMetricsPort int    `json:"felix_metrics_port"`
	EnableBGPMonitor bool   `json:"enable_bgp_monitor"`
	PolicyLogLevel   string `json:"policy_log_level"`
}

type FlannelConfig struct {
	EtcdEndpoints []string `json:"etcd_endpoints,omitempty"`
	SubnetFile    string   `json:"subnet_file,omitempty"`
}

type AWSVPCConfig struct {
	EnableENIMonitor  bool   `json:"enable_eni_monitor"`
	Region            string `json:"region,omitempty"`
	ClusterName       string `json:"cluster_name,omitempty"`
	EnableIPAMMetrics bool   `json:"enable_ipam_metrics"`
}

// Health represents collector health status
type Health struct {
	Status             HealthStatus       `json:"status"`
	Message            string             `json:"message"`
	LastEventTime      time.Time          `json:"last_event_time"`
	EventsProcessed    uint64             `json:"events_processed"`
	EventsDropped      uint64             `json:"events_dropped"`
	ErrorCount         uint64             `json:"error_count"`
	CNIPluginsDetected []string           `json:"cni_plugins_detected"`
	ActiveMonitors     int                `json:"active_monitors"`
	K8sConnected       bool               `json:"k8s_connected"`
	Metrics            map[string]float64 `json:"metrics"`
}

// HealthStatusAdapter wraps Health to implement domain.HealthStatus interface
type HealthStatusAdapter struct {
	health Health
}

// NewHealthStatusAdapter creates an adapter for the Health struct
func NewHealthStatusAdapter(h Health) domain.HealthStatus {
	return &HealthStatusAdapter{health: h}
}

// Status returns the health status value
func (a *HealthStatusAdapter) Status() domain.HealthStatusValue {
	switch a.health.Status {
	case HealthStatusHealthy:
		return domain.HealthHealthy
	case HealthStatusDegraded:
		return domain.HealthDegraded
	case HealthStatusUnhealthy:
		return domain.HealthUnhealthy
	default:
		return domain.HealthUnknown
	}
}

// Message returns the health message
func (a *HealthStatusAdapter) Message() string {
	return a.health.Message
}

// Details returns health details
func (a *HealthStatusAdapter) Details() map[string]interface{} {
	return map[string]interface{}{
		"last_event_time":      a.health.LastEventTime,
		"events_processed":     a.health.EventsProcessed,
		"events_dropped":       a.health.EventsDropped,
		"error_count":          a.health.ErrorCount,
		"cni_plugins_detected": a.health.CNIPluginsDetected,
		"active_monitors":      a.health.ActiveMonitors,
		"k8s_connected":        a.health.K8sConnected,
		"metrics":              a.health.Metrics,
	}
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Statistics represents runtime statistics
type Statistics struct {
	StartTime            time.Time                `json:"start_time"`
	EventsCollected      uint64                   `json:"events_collected"`
	EventsDropped        uint64                   `json:"events_dropped"`
	CNIOperationsTotal   uint64                   `json:"cni_operations_total"`
	CNIOperationsFailed  uint64                   `json:"cni_operations_failed"`
	IPAllocationsTotal   uint64                   `json:"ip_allocations_total"`
	IPDeallocationsTotal uint64                   `json:"ip_deallocations_total"`
	PolicyEventsTotal    uint64                   `json:"policy_events_total"`
	PluginExecutionTime  map[string]time.Duration `json:"plugin_execution_time"`
	MonitoringErrors     uint64                   `json:"monitoring_errors"`
	K8sEventsProcessed   uint64                   `json:"k8s_events_processed"`
	Custom               map[string]interface{}   `json:"custom"`
}

// CNIMonitor monitors CNI plugin operations
type CNIMonitor interface {
	// Start monitoring CNI operations
	Start(ctx context.Context) error

	// Stop monitoring
	Stop() error

	// Events channel for raw CNI events
	Events() <-chan CNIRawEvent

	// Monitor type (log, process, event, file)
	MonitorType() string
}

// EventProcessor processes raw CNI events into UnifiedEvents
// This is the key component that creates semantic correlation context
// and eliminates the need for downstream event conversion.
type EventProcessor interface {
	ProcessEvent(ctx context.Context, raw CNIRawEvent) (*domain.UnifiedEvent, error)
}

// CNIRawEvent represents a raw CNI operation or event
// This captures all the essential information from CNI plugin execution
// or related network configuration changes.
type CNIRawEvent struct {
	// Event identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // log, process, k8s-event, file

	// CNI operation details
	Operation    CNIOperation  `json:"operation"`   // ADD, DEL, CHECK
	PluginName   string        `json:"plugin_name"` // cilium, calico, flannel, etc.
	Command      string        `json:"command"`     // Full command executed
	ExitCode     int           `json:"exit_code"`
	Duration     time.Duration `json:"duration"`
	Success      bool          `json:"success"`
	ErrorMessage string        `json:"error_message,omitempty"`

	// Container/Pod context
	ContainerID  string `json:"container_id,omitempty"`
	PodUID       string `json:"pod_uid,omitempty"`
	PodName      string `json:"pod_name,omitempty"`
	PodNamespace string `json:"pod_namespace,omitempty"`
	NetworkNS    string `json:"network_ns,omitempty"`

	// Network configuration
	InterfaceName string            `json:"interface_name,omitempty"`
	AssignedIP    string            `json:"assigned_ip,omitempty"`
	Subnet        string            `json:"subnet,omitempty"`
	Gateway       string            `json:"gateway,omitempty"`
	Routes        []RouteInfo       `json:"routes,omitempty"`
	DNS           []string          `json:"dns,omitempty"`
	Capabilities  map[string]string `json:"capabilities,omitempty"`

	// CNI configuration
	CNIVersion   string                 `json:"cni_version,omitempty"`
	PluginConfig map[string]interface{} `json:"plugin_config,omitempty"`

	// Additional context for correlation
	NodeName    string            `json:"node_name,omitempty"`
	ClusterName string            `json:"cluster_name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Raw data for debugging
	RawStdout string `json:"raw_stdout,omitempty"`
	RawStderr string `json:"raw_stderr,omitempty"`
	RawConfig string `json:"raw_config,omitempty"`
}

// CNIOperation represents the type of CNI operation
type CNIOperation string

const (
	CNIOperationAdd   CNIOperation = "ADD"
	CNIOperationDel   CNIOperation = "DEL"
	CNIOperationCheck CNIOperation = "CHECK"
	CNIOperationOther CNIOperation = "OTHER"
)

// RouteInfo represents network routing information
type RouteInfo struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Interface   string `json:"interface"`
	Metric      int    `json:"metric,omitempty"`
}

// Validate validates the configuration and sets defaults
func (c *Config) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 1000
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.MaxConcurrentWatch <= 0 {
		c.MaxConcurrentWatch = 10
	}
	if c.CorrelationTimeout <= 0 {
		c.CorrelationTimeout = 30 * time.Second
	}

	// Set default paths if not specified
	if c.CNIBinPath == "" {
		c.CNIBinPath = "/opt/cni/bin"
	}
	if c.CNIConfPath == "" {
		c.CNIConfPath = "/etc/cni/net.d"
	}

	// Enable at least one monitoring approach
	if !c.EnableLogMonitoring && !c.EnableProcessMonitoring &&
		!c.EnableEventMonitoring && !c.EnableFileMonitoring {
		c.EnableLogMonitoring = true
		c.EnableEventMonitoring = true
	}

	return nil
}
