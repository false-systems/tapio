package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// ObserverOrchestrator is the central orchestration engine for all observers in Tapio.
//
// RESPONSIBILITIES:
// 1. OBSERVER REGISTRY: Maintains registry of all active observers
// 2. LIFECYCLE MANAGEMENT: Coordinates start/stop operations across all observers
// 3. EVENT AGGREGATION: Collects events from all observer channels into unified stream
// 4. WORKER POOL: Manages configurable worker goroutines for parallel processing
// 5. NATS PUBLISHING: Publishes aggregated events to NATS messaging system
// 6. HEALTH MONITORING: Tracks health status of all registered observers
// 7. GRACEFUL SHUTDOWN: Ensures clean shutdown with proper resource cleanup
//
// ARCHITECTURE:
// - Multiple observers (KubeAPI, Network, DNS, eBPF, etc.) register with orchestrator
// - Each observer runs independently and emits events to their own channels
// - Orchestrator spawns consumer goroutines to read from each observer's event channel
// - All events flow into a central aggregation channel (eventsChan)
// - Worker pool (default 4 workers) processes events from aggregation channel
// - Workers publish events to NATS with retry logic and backpressure handling
//
// CONCURRENCY DESIGN:
// - 1 consumer goroutine per registered observer
// - N worker goroutines for NATS publishing (configurable)
// - Proper coordination with WaitGroup and context cancellation
// - Safe channel operations with panic recovery
//
// This design provides scalability, fault isolation, and graceful degradation
// while maintaining a clean separation between observers and downstream systems.
type ObserverOrchestrator struct {
	observers map[string]observers.Observer
	publisher *EnhancedNATSPublisher
	logger    *zap.Logger

	eventsChan chan *domain.CollectorEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
}

// Config holds orchestrator configuration
type Config struct {
	// Workers specifies the number of goroutines processing events from the aggregation channel
	// Higher values increase throughput but consume more resources
	// Valid range: 1-64 workers
	// Default: 4 workers
	// Recommendations:
	//   - Low load (< 1K events/sec): 2-4 workers
	//   - Medium load (1K-10K events/sec): 4-8 workers
	//   - High load (> 10K events/sec): 8-16 workers
	//   - Very high load: 16+ workers (monitor CPU usage)
	Workers int

	// BufferSize sets the capacity of the central event aggregation channel
	// Larger buffers provide better burst handling but use more memory
	// Valid range: 100-100,000 events
	// Default: 10,000 events
	// Memory usage: ~1KB per event in buffer
	// Recommendations:
	//   - Low latency requirements: 1,000-5,000
	//   - High throughput: 10,000-50,000
	//   - Burst handling: 25,000-100,000
	BufferSize int

	// NATSConfig contains NATS connection and publishing settings
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

// RecommendedWorkerCount suggests optimal worker count based on observer count and expected load
func RecommendedWorkerCount(observerCount int, expectedEventsPerSecond int) int {
	// Base recommendation: 1 worker per 2 observers, minimum 2
	baseWorkers := max(2, observerCount/2+1)

	// Adjust for expected load
	if expectedEventsPerSecond > 10000 {
		return min(64, baseWorkers*4) // High load
	} else if expectedEventsPerSecond > 1000 {
		return min(32, baseWorkers*2) // Medium load
	}

	return min(16, baseWorkers) // Low load
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ObserverHealthStatus represents the health status of an observer
type ObserverHealthStatus struct {
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
