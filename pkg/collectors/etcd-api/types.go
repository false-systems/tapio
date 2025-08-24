package etcdapi

import "time"

// EtcdEventData represents strongly-typed etcd event data from API monitoring
type EtcdEventData struct {
	Key            string `json:"key"`
	Value          string `json:"value"`
	ModRevision    int64  `json:"mod_revision"`
	CreateRevision int64  `json:"create_revision"`
	Version        int64  `json:"version"`
	ResourceType   string `json:"resource_type"`
}

// CollectorStats represents strongly-typed collector statistics
type CollectorStats struct {
	EventsProcessed int64             `json:"events_processed"`
	ErrorCount      int64             `json:"error_count"`
	LastEventTime   time.Time         `json:"last_event_time"`
	Uptime          time.Duration     `json:"uptime"`
	CustomMetrics   map[string]string `json:"custom_metrics,omitempty"`
}

// HealthStatus represents strongly-typed health status
type HealthStatus struct {
	Healthy       bool              `json:"healthy"`
	Message       string            `json:"message"`
	LastCheck     time.Time         `json:"last_check"`
	ComponentInfo map[string]string `json:"component_info,omitempty"`
}
