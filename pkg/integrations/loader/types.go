package loader

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// LoaderMetrics contains metrics for the Neo4j loader service
type LoaderMetrics struct {
	EventsReceived      int64     `json:"events_received"`
	EventsProcessed     int64     `json:"events_processed"`
	EventsFailed        int64     `json:"events_failed"`
	BatchesProcessed    int64     `json:"batches_processed"`
	BatchesFailed       int64     `json:"batches_failed"`
	ProcessingLatency   float64   `json:"processing_latency_ms"`
	StorageLatency      float64   `json:"storage_latency_ms"`
	LastProcessedTime   time.Time `json:"last_processed_time"`
	BacklogSize         int       `json:"backlog_size"`
	ActiveWorkers       int       `json:"active_workers"`
	HealthStatus        string    `json:"health_status"`
	ErrorRate           float64   `json:"error_rate"`
	ThroughputPerSecond float64   `json:"throughput_per_second"`
}

// BatchJob represents a batch of observation events to be processed
type BatchJob struct {
	ID        string                     `json:"id"`
	Events    []*domain.ObservationEvent `json:"events"`
	CreatedAt time.Time                  `json:"created_at"`
	Retries   int                        `json:"retries"`
}

// ProcessingResult represents the result of processing a batch
type ProcessingResult struct {
	BatchID              string        `json:"batch_id"`
	Success              bool          `json:"success"`
	EventsProcessed      int           `json:"events_processed"`
	ProcessingTime       time.Duration `json:"processing_time"`
	Error                error         `json:"error,omitempty"`
	NodesCreated         int64         `json:"nodes_created"`
	RelationshipsCreated int64         `json:"relationships_created"`
}

// HealthStatus represents the health state of the loader service
type HealthStatus struct {
	Status         string            `json:"status"` // "healthy", "degraded", "unhealthy"
	LastCheck      time.Time         `json:"last_check"`
	NATSConnected  bool              `json:"nats_connected"`
	Neo4jConnected bool              `json:"neo4j_connected"`
	Metrics        LoaderMetrics     `json:"metrics"`
	Errors         []string          `json:"errors,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
	Details        map[string]string `json:"details,omitempty"`
}

// NodeCreationRequest represents a request to create a node in Neo4j
type NodeCreationRequest struct {
	ObservationID string                   `json:"observation_id"`
	Event         *domain.ObservationEvent `json:"event"`
	Labels        []string                 `json:"labels"`
	Properties    map[string]interface{}   `json:"properties"`
}

// RelationshipCreationRequest represents a request to create a relationship in Neo4j
type RelationshipCreationRequest struct {
	FromObservationID string                 `json:"from_observation_id"`
	ToObservationID   string                 `json:"to_observation_id"`
	RelationType      string                 `json:"relationship_type"`
	Properties        map[string]interface{} `json:"properties"`
}

// StorageStats contains statistics about storage operations
type StorageStats struct {
	NodesCreated         int64         `json:"nodes_created"`
	RelationshipsCreated int64         `json:"relationships_created"`
	StorageTime          time.Duration `json:"storage_time"`
	BatchSize            int           `json:"batch_size"`
}
