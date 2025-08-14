package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
)

// NATSPublisherInterface defines the interface for NATS publishers
type NATSPublisherInterface interface {
	Publish(event collectors.RawEvent) error // Unified method for publishing RawEvent
	Close()
	IsHealthy() bool
}

// EventPipeline manages the flow of events from collectors to NATS
type EventPipeline struct {
	collectors map[string]collectors.Collector
	enricher   *K8sEnricher
	publisher  NATSPublisherInterface
	logger     *zap.Logger

	eventsChan chan *collectors.RawEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
}

// Config holds pipeline configuration
type Config struct {
	Workers         int
	BufferSize      int
	NATSConfig      *config.NATSConfig
	UseEnhancedNATS bool // Use enhanced NATS publisher with backpressure
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:         4,
		BufferSize:      10000,
		NATSConfig:      config.DefaultNATSConfig(),
		UseEnhancedNATS: true, // Default to enhanced NATS for production
	}
}

// EnrichedEvent represents an event with added context
type EnrichedEvent struct {
	Raw       *collectors.RawEvent
	K8sObject *K8sObjectInfo
	TraceID   string
	SpanID    string
	ParentID  string
}

// K8sObjectInfo contains Kubernetes object information
type K8sObjectInfo struct {
	Kind      string
	Name      string
	Namespace string
	UID       string
	Labels    map[string]string
}

// CollectorHealthStatus represents the health status of a collector
type CollectorHealthStatus struct {
	Healthy   bool
	Error     string
	LastEvent time.Time
}

// HealthDetails provides structured health information instead of map[string]interface{}
type HealthDetails struct {
	Healthy   bool          `json:"healthy"`
	Error     string        `json:"error,omitempty"`
	LastEvent time.Time     `json:"last_event,omitempty"`
	Uptime    time.Duration `json:"uptime,omitempty"`
}

// convertMetadataToStringMap converts metadata while preserving type safety
func convertMetadataToStringMap(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}

	result := make(map[string]string, len(metadata))
	for k, v := range metadata {
		result[k] = v
	}
	return result
}
