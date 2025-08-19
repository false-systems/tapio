package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventPipeline manages the flow of events from collectors to NATS
type EventPipeline struct {
	collectors map[string]collectors.Collector
	publisher  *EnhancedNATSPublisher
	logger     *zap.Logger

	eventsChan chan domain.RawEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
}

// Config holds pipeline configuration
type Config struct {
	Workers    int
	BufferSize int
	NATSConfig *config.NATSConfig
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:    4,
		BufferSize: 10000,
		NATSConfig: config.DefaultNATSConfig(),
	}
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
