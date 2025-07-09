package correlation

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/falseyair/tapio/pkg/types"
)

// SourceType represents the type of data source
type SourceType string

const (
	SourceKubernetes SourceType = "kubernetes"
	SourceEBPF       SourceType = "ebpf"
	SourceMetrics    SourceType = "metrics"
	SourceLogs       SourceType = "logs"
)

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
	Pods      []corev1.Pod           `json:"pods"`
	Events    []corev1.Event         `json:"events"`
	Metrics   map[string]interface{} `json:"metrics"`
	Problems  []types.Problem        `json:"problems"`
	Timestamp time.Time              `json:"timestamp"`
}

// EBPFData represents eBPF monitoring data
type EBPFData struct {
	ProcessStats  map[uint32]*ProcessMemoryStats `json:"process_stats"`
	SystemMetrics SystemMetrics                  `json:"system_metrics"`
	MemoryEvents  []MemoryEvent                  `json:"memory_events"`
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

// SystemMetrics represents system-wide metrics
type SystemMetrics struct {
	TotalMemory     uint64    `json:"total_memory"`
	AvailableMemory uint64    `json:"available_memory"`
	MemoryPressure  float64   `json:"memory_pressure"`
	CPUUsage        float64   `json:"cpu_usage"`
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
	PodName      string          `json:"pod_name"`
	Container    string          `json:"container"`
	CPU          ResourceMetrics `json:"cpu"`
	Memory       ResourceMetrics `json:"memory"`
	RestartCount int32           `json:"restart_count"`
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

// LogsData represents log data from various sources
type LogsData struct {
	Entries   []LogEntry `json:"entries"`
	Timestamp time.Time  `json:"timestamp"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Source    string                 `json:"source"`
	Labels    map[string]string      `json:"labels"`
	Fields    map[string]interface{} `json:"fields"`
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
func (dc *DataCollection) GetLogsData(ctx context.Context, timeRange time.Duration) (*LogsData, error) {
	source, exists := dc.sources[SourceLogs]
	if !exists {
		return nil, NewSourceNotAvailableError(SourceLogs)
	}

	params := map[string]interface{}{
		"time_range": timeRange,
	}

	data, err := source.GetData(ctx, "recent", params)
	if err != nil {
		return nil, err
	}

	return data.(*LogsData), nil
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
