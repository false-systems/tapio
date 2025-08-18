package domain

import "time"

// CollectorConfig is the ONLY way to configure collectors - NO map[string]interface{}
type CollectorConfig struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Enabled   bool              `json:"enabled"`
	Interval  time.Duration     `json:"interval"`
	BatchSize int               `json:"batch_size"`
	Labels    map[string]string `json:"labels"`

	// Type-specific configs (use ONE, not interface{})
	Kernel *KernelConfig `json:"kernel,omitempty"`
	ETCD   *ETCDConfig   `json:"etcd,omitempty"`
	DNS    *DNSConfig    `json:"dns,omitempty"`
	CRI    *CRIConfig    `json:"cri,omitempty"`
}

type KernelConfig struct {
	BufferSize    int    `json:"buffer_size"`
	PerfEventSize int    `json:"perf_event_size"`
	BPFPath       string `json:"bpf_path"`
}

type ETCDConfig struct {
	Endpoints []string      `json:"endpoints"`
	TLS       *TLSConfig    `json:"tls,omitempty"`
	Timeout   time.Duration `json:"timeout"`
}

type DNSConfig struct {
	ServerAddr string        `json:"server_addr"`
	Timeout    time.Duration `json:"timeout"`
	MaxRetries int           `json:"max_retries"`
}

type CRIConfig struct {
	SocketPath string        `json:"socket_path"`
	Timeout    time.Duration `json:"timeout"`
}

type TLSConfig struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

// CollectorStats is the ONLY way to return statistics - NO map[string]interface{}
type CollectorStats struct {
	EventsProcessed int64             `json:"events_processed"`
	ErrorCount      int64             `json:"error_count"`
	LastEventTime   time.Time         `json:"last_event_time"`
	Uptime          time.Duration     `json:"uptime"`
	CustomMetrics   map[string]string `json:"custom_metrics,omitempty"`
}

// EventAttributes for strongly-typed event data - NO interface{}
type StrictEventAttributes struct {
	StringAttrs map[string]string  `json:"string_attrs,omitempty"`
	IntAttrs    map[string]int64   `json:"int_attrs,omitempty"`
	FloatAttrs  map[string]float64 `json:"float_attrs,omitempty"`
	BoolAttrs   map[string]bool    `json:"bool_attrs,omitempty"`
}

// ServiceInfo replaces map[string]interface{} for service data
type ServiceInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels"`
	Selector  map[string]string `json:"selector"`
	Ports     []PortInfo        `json:"ports"`
}

type PortInfo struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// PipelineConfig replaces map[string]interface{} for pipeline configs
type PipelineConfig struct {
	BatchSize     int               `json:"batch_size"`
	FlushInterval time.Duration     `json:"flush_interval"`
	Processors    []ProcessorConfig `json:"processors"`
}

type ProcessorConfig struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
}

// LogFields replaces map[string]interface{} for structured logging
type LogFields struct {
	StringFields map[string]string    `json:"string_fields,omitempty"`
	IntFields    map[string]int64     `json:"int_fields,omitempty"`
	FloatFields  map[string]float64   `json:"float_fields,omitempty"`
	BoolFields   map[string]bool      `json:"bool_fields,omitempty"`
	TimeFields   map[string]time.Time `json:"time_fields,omitempty"`
}

// NewLogFields creates a new LogFields instance
func NewLogFields() *LogFields {
	return &LogFields{
		StringFields: make(map[string]string),
		IntFields:    make(map[string]int64),
		FloatFields:  make(map[string]float64),
		BoolFields:   make(map[string]bool),
		TimeFields:   make(map[string]time.Time),
	}
}

// AddString adds a string field
func (lf *LogFields) AddString(key, value string) *LogFields {
	if lf.StringFields == nil {
		lf.StringFields = make(map[string]string)
	}
	lf.StringFields[key] = value
	return lf
}

// AddInt adds an integer field
func (lf *LogFields) AddInt(key string, value int64) *LogFields {
	if lf.IntFields == nil {
		lf.IntFields = make(map[string]int64)
	}
	lf.IntFields[key] = value
	return lf
}

// AddFloat adds a float field
func (lf *LogFields) AddFloat(key string, value float64) *LogFields {
	if lf.FloatFields == nil {
		lf.FloatFields = make(map[string]float64)
	}
	lf.FloatFields[key] = value
	return lf
}

// AddBool adds a boolean field
func (lf *LogFields) AddBool(key string, value bool) *LogFields {
	if lf.BoolFields == nil {
		lf.BoolFields = make(map[string]bool)
	}
	lf.BoolFields[key] = value
	return lf
}

// AddTime adds a time field
func (lf *LogFields) AddTime(key string, value time.Time) *LogFields {
	if lf.TimeFields == nil {
		lf.TimeFields = make(map[string]time.Time)
	}
	lf.TimeFields[key] = value
	return lf
}

// BatchMetadata replaces map[string]interface{} for batch metadata
type BatchMetadata struct {
	BatchID        string            `json:"batch_id"`
	ProcessingTime time.Duration     `json:"processing_time"`
	EventCount     int               `json:"event_count"`
	Source         string            `json:"source"`
	Labels         map[string]string `json:"labels,omitempty"`
	Metrics        *BatchMetrics     `json:"metrics,omitempty"`
}

// BatchMetrics provides structured metrics for batches
type BatchMetrics struct {
	BytesProcessed   int64         `json:"bytes_processed"`
	EventsDropped    int64         `json:"events_dropped"`
	ErrorCount       int64         `json:"error_count"`
	AverageLatency   time.Duration `json:"average_latency"`
	ThroughputPerSec float64       `json:"throughput_per_sec"`
}

// LoaderProperties replaces map[string]interface{} for loader configuration
type LoaderProperties struct {
	// Common properties
	BatchSize     int           `json:"batch_size,omitempty"`
	Timeout       time.Duration `json:"timeout,omitempty"`
	RetryAttempts int           `json:"retry_attempts,omitempty"`

	// Connection properties
	Endpoints []string   `json:"endpoints,omitempty"`
	Username  string     `json:"username,omitempty"`
	Password  string     `json:"password,omitempty"`
	TLS       *TLSConfig `json:"tls,omitempty"`

	// Performance properties
	PoolSize      int           `json:"pool_size,omitempty"`
	QueueSize     int           `json:"queue_size,omitempty"`
	FlushInterval time.Duration `json:"flush_interval,omitempty"`

	// Feature flags
	EnableMetrics bool `json:"enable_metrics,omitempty"`
	EnableTracing bool `json:"enable_tracing,omitempty"`

	// Additional configuration
	Labels map[string]string `json:"labels,omitempty"`
	Tags   []string          `json:"tags,omitempty"`
}

// K8sEventData replaces map[string]interface{} for K8s watcher events
type K8sEventData struct {
	Type      string            `json:"type"`
	Object    *ObjectData       `json:"object,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Source    string            `json:"source"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ObjectData represents Kubernetes object data
type ObjectData struct {
	Kind        string            `json:"kind"`
	APIVersion  string            `json:"api_version"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	UID         string            `json:"uid"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// MonitoringStatus replaces map[string]interface{} for production monitoring
type MonitoringStatus struct {
	Component       string            `json:"component"`
	Status          string            `json:"status"`
	Timestamp       time.Time         `json:"timestamp"`
	Uptime          time.Duration     `json:"uptime"`
	CPUUsage        float64           `json:"cpu_usage"`
	MemoryUsage     int64             `json:"memory_usage"`
	EventsProcessed int64             `json:"events_processed"`
	ErrorCount      int64             `json:"error_count"`
	LastError       string            `json:"last_error,omitempty"`
	Details         map[string]string `json:"details,omitempty"`
}
