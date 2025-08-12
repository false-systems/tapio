package correlation

import "time"

// MetricsData represents the correlation engine metrics
// This replaces the map[string]interface{} to comply with CLAUDE.md requirements
type MetricsData struct {
	// Event processing metrics
	EventsProcessed   int64 `json:"events_processed"`
	CorrelationsFound int64 `json:"correlations_found"`

	// Queue metrics
	EventQueueSize   int `json:"event_queue_size"`
	ResultQueueSize  int `json:"result_queue_size"`
	StorageQueueSize int `json:"storage_queue_size"`

	// Storage metrics
	StorageProcessed int64 `json:"storage_processed"`
	StorageRejected  int64 `json:"storage_rejected"`

	// Configuration metrics
	CorrelatorsCount int `json:"correlators_count"`
	WorkersCount     int `json:"workers_count"`
	StorageWorkers   int `json:"storage_workers"`

	// Timing metrics
	LastReportTime time.Time `json:"last_report_time,omitempty"`

	// Health metrics
	IsHealthy bool   `json:"is_healthy"`
	Status    string `json:"status,omitempty"`
}

// EngineMetrics provides detailed metrics about the correlation engine
type EngineMetrics struct {
	MetricsData

	// Performance metrics
	EventsPerSecond       float64       `json:"events_per_second,omitempty"`
	CorrelationsPerSecond float64       `json:"correlations_per_second,omitempty"`
	AverageProcessingTime time.Duration `json:"average_processing_time,omitempty"`

	// Resource utilization
	MemoryUsedBytes uint64 `json:"memory_used_bytes,omitempty"`
	GoroutineCount  int    `json:"goroutine_count,omitempty"`

	// Error tracking
	ErrorCount      int64     `json:"error_count,omitempty"`
	LastErrorTime   time.Time `json:"last_error_time,omitempty"`
	LastErrorReason string    `json:"last_error_reason,omitempty"`
}

// CorrelatorMetrics provides metrics for individual correlators
type CorrelatorMetrics struct {
	Name              string        `json:"name"`
	Version           string        `json:"version"`
	EventsProcessed   int64         `json:"events_processed"`
	CorrelationsFound int64         `json:"correlations_found"`
	ErrorCount        int64         `json:"error_count"`
	AverageLatency    time.Duration `json:"average_latency"`
	LastProcessedTime time.Time     `json:"last_processed_time,omitempty"`
}

// HealthStatus represents comprehensive health information
type HealthStatus struct {
	Timestamp    time.Time                   `json:"timestamp"`
	IsHealthy    bool                        `json:"is_healthy"`
	Component    string                      `json:"component"`
	Version      string                      `json:"version"`
	Dependencies map[string]DependencyHealth `json:"dependencies"`
	QueueHealth  QueueHealth                 `json:"queue_health"`
}

// DependencyHealth represents the health of a dependency
type DependencyHealth struct {
	Name      string `json:"name"`
	IsHealthy bool   `json:"is_healthy"`
	Message   string `json:"message,omitempty"`
}

// QueueHealth represents the health of processing queues
type QueueHealth struct {
	EventQueue   QueueStatus  `json:"event_queue"`
	ResultQueue  QueueStatus  `json:"result_queue"`
	StorageQueue *QueueStatus `json:"storage_queue,omitempty"`
}

// QueueStatus represents the status of a specific queue
type QueueStatus struct {
	Size     int     `json:"size"`
	Capacity int     `json:"capacity"`
	Usage    float64 `json:"usage_percent"`
}
