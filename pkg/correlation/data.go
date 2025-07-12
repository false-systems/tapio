package correlation

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/yairfalse/tapio/pkg/types"
)

// SourceType represents the type of data source
type SourceType string

const (
	SourceKubernetes SourceType = "kubernetes"
	SourceEBPF       SourceType = "ebpf"
	SourceMetrics    SourceType = "metrics"
	SourceLogs       SourceType = "logs"
	SourceSystemd    SourceType = "systemd"
	SourceJournald   SourceType = "journald"
	SourceNetwork    SourceType = "network"
)


// AnalysisData contains all the data needed for correlation analysis
type AnalysisData struct {
	KubernetesData *KubernetesData `json:"kubernetes_data,omitempty"`
	EBPFData       *EBPFData       `json:"ebpf_data,omitempty"`
	SystemdData    *SystemdData    `json:"systemd_data,omitempty"`
	JournaldData   *JournaldData   `json:"journald_data,omitempty"`
	MetricsData    *MetricsData    `json:"metrics_data,omitempty"`
	NetworkData    *NetworkData    `json:"network_data,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
	TimeWindow     time.Duration   `json:"time_window"`
}

// DataSource defines the interface for retrieving data from different sources
type DataSource interface {
	// GetType returns the source type
	GetType() SourceType

	// IsAvailable checks if the source is available for querying
	IsAvailable() bool

	// GetData retrieves data of the specified type
	GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error)
}

// KubernetesData represents Kubernetes-specific data
type KubernetesData struct {
	Pods         []corev1.Pod                `json:"pods"`
	Events       []corev1.Event              `json:"events"`
	Deployments  []appsv1.Deployment         `json:"deployments"`
	Jobs         []batchv1.Job               `json:"jobs"`
	StatefulSets []appsv1.StatefulSet        `json:"statefulsets"`
	DaemonSets   []appsv1.DaemonSet          `json:"daemonsets"`
	ReplicaSets  []appsv1.ReplicaSet         `json:"replicasets"`
	Services     []corev1.Service            `json:"services"`
	Secrets      []corev1.Secret             `json:"secrets"`
	Logs         map[string][]LogEntry       `json:"logs"`
	Metrics      map[string]interface{}      `json:"metrics"`
	Problems     []types.Problem             `json:"problems"`
	Timestamp    time.Time                   `json:"timestamp"`
}

// EBPFData represents eBPF monitoring data
type EBPFData struct {
	ProcessStats  map[uint32]*ProcessMemoryStats `json:"process_stats"`
	SystemMetrics SystemMetrics                  `json:"system_metrics"`
	MemoryEvents  []MemoryEvent                  `json:"memory_events"`
	CPUEvents     []CPUEvent                     `json:"cpu_events"`
	IOEvents      []IOEvent                      `json:"io_events"`
	Timestamp     time.Time                      `json:"timestamp"`
}

// ProcessMemoryStats tracks memory usage for a process
type ProcessMemoryStats struct {
	PID            uint32            `json:"pid"`
	Command        string            `json:"command"`
	TotalAllocated uint64            `json:"total_allocated"`
	TotalFreed     uint64            `json:"total_freed"`
	CurrentUsage   uint64            `json:"current_usage"`
	AllocationRate float64           `json:"allocation_rate"` // bytes per second
	LastUpdate     time.Time         `json:"last_update"`
	InContainer    bool              `json:"in_container"`
	ContainerPID   uint32            `json:"container_pid"`
	GrowthPattern  []MemoryDataPoint `json:"growth_pattern"`
	IOBytesWritten uint64            `json:"io_bytes_written"`
	IOBytesRead    uint64            `json:"io_bytes_read"`
}

// MemoryDataPoint represents a point in time memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     uint64    `json:"usage"`
}

// MemoryEvent represents a memory-related event
type MemoryEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	PID         uint32    `json:"pid"`
	EventType   string    `json:"event_type"`
	Size        uint64    `json:"size"`
	TotalMemory uint64    `json:"total_memory"`
}

// CPUEvent represents a CPU-related event
type CPUEvent struct {
	Timestamp time.Time `json:"timestamp"`
	PID       uint32    `json:"pid"`
	EventType string    `json:"event_type"` // "throttle", "schedule", etc.
	Duration  uint64    `json:"duration"`   // Duration in nanoseconds
	CPUCore   int       `json:"cpu_core"`
}

// IOEvent represents an IO-related event
type IOEvent struct {
	Timestamp time.Time `json:"timestamp"`
	PID       uint32    `json:"pid"`
	EventType string    `json:"event_type"` // "read", "write", "sync", etc.
	Latency   uint64    `json:"latency"`    // Latency in nanoseconds
	Size      uint64    `json:"size"`       // IO size in bytes
	Device    string    `json:"device"`
	Path      string    `json:"path"`
}

// SystemMetrics represents system-wide metrics
type SystemMetrics struct {
	TotalMemory     uint64    `json:"total_memory"`
	AvailableMemory uint64    `json:"available_memory"`
	MemoryPressure  float64   `json:"memory_pressure"`
	CPUUsage        float64   `json:"cpu_usage"`
	CPUPressure     float64   `json:"cpu_pressure"`
	IOWait          float64   `json:"iowait"`
	Timestamp       time.Time `json:"timestamp"`
}

// MetricsData represents metrics from monitoring systems
type MetricsData struct {
	PodMetrics       map[string]PodMetrics       `json:"pod_metrics"`
	ContainerMetrics map[string]ContainerMetrics `json:"container_metrics"`
	NodeMetrics      NodeMetrics                 `json:"node_metrics"`
	Timestamp        time.Time                   `json:"timestamp"`
}

// PodMetrics represents pod-level metrics
type PodMetrics struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	CPU       ResourceMetrics        `json:"cpu"`
	Memory    ResourceMetrics        `json:"memory"`
	Custom    map[string]interface{} `json:"custom"`
}

// ContainerMetrics represents container-level metrics
type ContainerMetrics struct {
	PodName      string             `json:"pod_name"`
	Container    string             `json:"container"`
	Namespace    string             `json:"namespace"`
	CPU          CPUResourceMetrics `json:"cpu"`
	Memory       ResourceMetrics    `json:"memory"`
	RestartCount int32              `json:"restart_count"`
	VolumeUsage  float64            `json:"volume_usage"` // Volume usage percentage
	VolumePath   string             `json:"volume_path"`
	WriteIOPS    float64            `json:"write_iops"`
	ReadIOPS     float64            `json:"read_iops"`
}

// NodeMetrics represents node-level metrics
type NodeMetrics struct {
	Name           string          `json:"name"`
	CPU            ResourceMetrics `json:"cpu"`
	Memory         ResourceMetrics `json:"memory"`
	DiskPressure   bool            `json:"disk_pressure"`
	MemoryPressure bool            `json:"memory_pressure"`
	PIDPressure    bool            `json:"pid_pressure"`
}

// ResourceMetrics represents resource usage metrics
type ResourceMetrics struct {
	Current float64 `json:"current"`
	Limit   float64 `json:"limit"`
	Request float64 `json:"request"`
	Usage   float64 `json:"usage"` // Usage percentage
	Trend   float64 `json:"trend"` // Growth rate
}

// CPUResourceMetrics represents CPU-specific metrics
type CPUResourceMetrics struct {
	ResourceMetrics
	ThrottledTime uint64 `json:"throttled_time"` // Time throttled in nanoseconds
	TotalTime     uint64 `json:"total_time"`     // Total CPU time in nanoseconds
}

// LogsData represents log data from various sources
type LogsData struct {
	Entries   []LogEntry `json:"entries"`
	Timestamp time.Time  `json:"timestamp"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Severity  string                 `json:"severity"` // ERROR, FATAL, PANIC, etc.
	Message   string                 `json:"message"`
	Source    string                 `json:"source"`
	PodName   string                 `json:"pod_name"`
	Namespace string                 `json:"namespace"`
	Labels    map[string]string      `json:"labels"`
	Fields    map[string]interface{} `json:"fields"`
}

// SystemdData represents systemd service monitoring data
type SystemdData struct {
	Timestamp     time.Time                      `json:"timestamp"`
	ServiceStates map[string]interface{}        `json:"service_states"`
	UnitInfo      map[string]interface{}        `json:"unit_info"`
	Patterns      interface{}                   `json:"patterns"`
	Events        interface{}                   `json:"events"`
	Statistics    map[string]interface{}        `json:"statistics"`
}

// JournaldData represents journald log monitoring data  
type JournaldData struct {
	Timestamp       time.Time              `json:"timestamp"`
	Events          interface{}           `json:"events"`
	PatternMatches  map[string]interface{} `json:"pattern_matches"`
	Classifications map[string]interface{} `json:"classifications"`
	Statistics      map[string]interface{} `json:"statistics"`
}

// DataCollection provides unified access to multiple data sources
type DataCollection struct {
	sources  map[SourceType]DataSource
	cache    map[string]*CachedData
	cacheTTL time.Duration
}

// CachedData represents cached data with expiration
type CachedData struct {
	Data      interface{}
	ExpiresAt time.Time
}

// NewDataCollection creates a new data collection with specified sources
func NewDataCollection(sources map[SourceType]DataSource) *DataCollection {
	return &DataCollection{
		sources:  sources,
		cache:    make(map[string]*CachedData),
		cacheTTL: 30 * time.Second, // Default cache TTL
	}
}

// AddSource adds a new data source
func (dc *DataCollection) AddSource(sourceType SourceType, source DataSource) {
	dc.sources[sourceType] = source
}

// IsSourceAvailable checks if a specific source type is available
func (dc *DataCollection) IsSourceAvailable(sourceType SourceType) bool {
	source, exists := dc.sources[sourceType]
	if !exists {
		return false
	}
	return source.IsAvailable()
}

// GetAvailableSources returns a list of available source types
func (dc *DataCollection) GetAvailableSources() []SourceType {
	var available []SourceType
	for sourceType, source := range dc.sources {
		if source.IsAvailable() {
			available = append(available, sourceType)
		}
	}
	return available
}

// GetKubernetesData retrieves Kubernetes data with caching
func (dc *DataCollection) GetKubernetesData(ctx context.Context) (*KubernetesData, error) {
	cacheKey := "kubernetes_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*KubernetesData), nil
	}

	source, exists := dc.sources[SourceKubernetes]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceKubernetes)
	}

	data, err := source.GetData(ctx, "full", nil)
	if err != nil {
		return nil, err
	}

	k8sData := data.(*KubernetesData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      k8sData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return k8sData, nil
}

// GetEBPFData retrieves eBPF data with caching
func (dc *DataCollection) GetEBPFData(ctx context.Context) (*EBPFData, error) {
	cacheKey := "ebpf_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*EBPFData), nil
	}

	source, exists := dc.sources[SourceEBPF]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceEBPF)
	}

	data, err := source.GetData(ctx, "memory_stats", nil)
	if err != nil {
		return nil, err
	}

	ebpfData := data.(*EBPFData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      ebpfData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return ebpfData, nil
}

// GetMetricsData retrieves metrics data with caching
func (dc *DataCollection) GetMetricsData(ctx context.Context) (*MetricsData, error) {
	cacheKey := "metrics_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*MetricsData), nil
	}

	source, exists := dc.sources[SourceMetrics]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceMetrics)
	}

	data, err := source.GetData(ctx, "current", nil)
	if err != nil {
		return nil, err
	}

	metricsData := data.(*MetricsData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      metricsData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return metricsData, nil
}

// GetLogsData retrieves logs data
func (dc *DataCollection) GetLogsData(ctx context.Context) (*LogsData, error) {
	cacheKey := "logs_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*LogsData), nil
	}

	source, exists := dc.sources[SourceLogs]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceLogs)
	}

	data, err := source.GetData(ctx, "recent", nil)
	if err != nil {
		return nil, err
	}

	logsData := data.(*LogsData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      logsData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return logsData, nil
}

// GetSystemdData retrieves systemd data with caching
func (dc *DataCollection) GetSystemdData(ctx context.Context) (*SystemdData, error) {
	cacheKey := "systemd_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*SystemdData), nil
	}

	source, exists := dc.sources[SourceSystemd]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceSystemd)
	}

	data, err := source.GetData(ctx, "service_states", nil)
	if err != nil {
		return nil, err
	}

	systemdData := data.(*SystemdData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      systemdData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return systemdData, nil
}

// GetJournaldData retrieves journald data with caching
func (dc *DataCollection) GetJournaldData(ctx context.Context) (*JournaldData, error) {
	cacheKey := "journald_data"

	// Check cache first
	if cached, exists := dc.cache[cacheKey]; exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Data.(*JournaldData), nil
	}

	source, exists := dc.sources[SourceJournald]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceJournald)
	}

	data, err := source.GetData(ctx, "events", nil)
	if err != nil {
		return nil, err
	}

	journaldData := data.(*JournaldData)

	// Cache the data
	dc.cache[cacheKey] = &CachedData{
		Data:      journaldData,
		ExpiresAt: time.Now().Add(dc.cacheTTL),
	}

	return journaldData, nil
}

// ClearCache clears all cached data
func (dc *DataCollection) ClearCache() {
	dc.cache = make(map[string]*CachedData)
}

// SetCacheTTL sets the cache time-to-live duration
func (dc *DataCollection) SetCacheTTL(ttl time.Duration) {
	dc.cacheTTL = ttl
}

// SourceNotAvailableError represents an error when a data source is not available
type SourceNotAvailableError struct {
	SourceType SourceType
}

func (e *SourceNotAvailableError) Error() string {
	return "data source not available: " + string(e.SourceType)
}

// NewSourceNotAvailableError creates a new source not available error
func NewSourceNotAvailableError(sourceType SourceType) *SourceNotAvailableError {
	return &SourceNotAvailableError{SourceType: sourceType}
}


// NetworkData represents network-related monitoring data
type NetworkData struct {
	Connections []NetworkConnection       `json:"connections"`
	Traffic     []NetworkTrafficSample    `json:"traffic"`
	DNS         []DNSQuery                `json:"dns"`
	Timestamp   time.Time                 `json:"timestamp"`
}

// NetworkConnection represents a network connection
type NetworkConnection struct {
	LocalAddr  string    `json:"local_addr"`
	RemoteAddr string    `json:"remote_addr"`
	Protocol   string    `json:"protocol"`
	State      string    `json:"state"`
	PID        uint32    `json:"pid,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// NetworkTrafficSample represents network traffic data
type NetworkTrafficSample struct {
	Interface    string    `json:"interface"`
	BytesIn      uint64    `json:"bytes_in"`
	BytesOut     uint64    `json:"bytes_out"`
	PacketsIn    uint64    `json:"packets_in"`
	PacketsOut   uint64    `json:"packets_out"`
	DroppedIn    uint64    `json:"dropped_in"`
	DroppedOut   uint64    `json:"dropped_out"`
	Timestamp    time.Time `json:"timestamp"`
}

// DNSQuery represents a DNS query
type DNSQuery struct {
	Query     string    `json:"query"`
	Response  string    `json:"response,omitempty"`
	QueryType string    `json:"query_type"`
	PID       uint32    `json:"pid,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
