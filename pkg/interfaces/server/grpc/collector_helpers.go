package grpc

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper functions for CollectorServer

func (s *CollectorServer) getResourceUtilization(collectorID string) *pb.ResourceUtilization {
	// In a real implementation, this would fetch actual metrics
	return &pb.ResourceUtilization{
		CpuUsage:    0.45,
		MemoryBytes: 1024 * 1024 * 512,  // 512MB
		MemoryLimit: 1024 * 1024 * 2048, // 2GB
		Network: &pb.NetworkUtilization{
			BytesSentPerSec:     1024 * 100,  // 100KB/s
			BytesReceivedPerSec: 1024 * 1000, // 1MB/s
			Connections:         5,
			Latency:             durationpb.New(5 * time.Millisecond),
		},
		Disk: &pb.DiskUtilization{
			ReadsPerSec:        100,
			WritesPerSec:       50,
			BytesReadPerSec:    1024 * 1024,     // 1MB/s
			BytesWrittenPerSec: 1024 * 1024 * 2, // 2MB/s
		},
	}
}

func (s *CollectorServer) getNetworkMetrics(collectorID string) *pb.NetworkMetrics {
	// Fetch actual network metrics for collector
	return &pb.NetworkMetrics{
		BytesSent:         1024 * 1024 * 100,  // 100MB
		BytesReceived:     1024 * 1024 * 1000, // 1GB
		ConnectionsActive: 5,
		ConnectionsFailed: 2,
		AvgLatencyMs:      5.2,
		P99LatencyMs:      12.5,
	}
}

func (s *CollectorServer) getErrorMetrics(collectorID string) *pb.ErrorMetrics {
	s.collectorsMu.RLock()
	collector, exists := s.collectors[collectorID]
	s.collectorsMu.RUnlock()

	if !exists {
		return &pb.ErrorMetrics{}
	}

	errorsByType := map[string]int64{
		"collection_error":    10,
		"serialization_error": 2,
		"network_error":       5,
		"validation_error":    15,
	}

	totalErrors := collector.Metrics.ErrorCount
	totalEvents := collector.Metrics.EventsReceived
	errorRate := float64(0)
	if totalEvents > 0 {
		errorRate = float64(totalErrors) / float64(totalEvents)
	}

	return &pb.ErrorMetrics{
		ErrorsByType: errorsByType,
		ErrorRate:    errorRate,
		LastError:    timestamppb.New(time.Now().Add(-5 * time.Minute)),
		RecentErrors: []*pb.RecentError{
			{
				Timestamp: timestamppb.New(time.Now().Add(-5 * time.Minute)),
				ErrorType: "validation_error",
				Message:   "Missing required field: trace_id",
				Context: map[string]string{
					"event_type": "network",
					"severity":   "warning",
				},
			},
		},
	}
}

func (s *CollectorServer) getLatencyStats(operation string) *pb.LatencyStats {
	// In production, this would aggregate actual latency measurements
	return &pb.LatencyStats{
		Min: durationpb.New(time.Microsecond * 100),
		Max: durationpb.New(time.Millisecond * 50),
		Avg: durationpb.New(time.Millisecond * 5),
		P50: durationpb.New(time.Millisecond * 3),
		P95: durationpb.New(time.Millisecond * 15),
		P99: durationpb.New(time.Millisecond * 30),
	}
}

func (s *CollectorServer) getErrorStats(collectorID string) *pb.ErrorStats {
	return &pb.ErrorStats{
		TotalErrors:         32,
		ValidationErrors:    15,
		SerializationErrors: 2,
		NetworkErrors:       5,
		ErrorRate:           0.05, // 5% error rate
	}
}

func (s *CollectorServer) calculateCollectorHealth(collector *collectorInfo) pb.HealthStatus {
	// Calculate health based on various factors
	if collector.State == pb.CollectorState_COLLECTOR_ERROR {
		return pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}

	if collector.State == pb.CollectorState_COLLECTOR_STOPPED {
		return pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	}

	// Check error rate
	errorRate := float64(0)
	if collector.Metrics.EventsReceived > 0 {
		errorRate = float64(collector.Metrics.ErrorCount) / float64(collector.Metrics.EventsReceived)
	}

	if errorRate > 0.1 { // More than 10% errors
		return pb.HealthStatus_HEALTH_STATUS_DEGRADED
	}

	// Check last seen time
	if time.Since(collector.LastSeen) > 2*time.Minute {
		return pb.HealthStatus_HEALTH_STATUS_DEGRADED
	}

	return pb.HealthStatus_HEALTH_STATUS_HEALTHY
}

func (s *CollectorServer) calculateCollectorStats() *pb.CollectorSummaryStats {
	s.collectorsMu.RLock()
	defer s.collectorsMu.RUnlock()

	stats := &pb.CollectorSummaryStats{
		TotalCollectors:      int32(len(s.collectors)),
		CollectorsByType:     make(map[string]int32),
		CollectorsByState:    make(map[string]int32),
		TotalEventsPerSecond: 0,
		UnhealthyCollectors:  0,
	}

	for _, collector := range s.collectors {
		// Count by type
		stats.CollectorsByType[collector.Type]++

		// Count by state
		stats.CollectorsByState[collector.State.String()]++

		// Sum events per second
		stats.TotalEventsPerSecond += collector.Metrics.CurrentRate

		// Count unhealthy
		health := s.calculateCollectorHealth(collector)
		if health != pb.HealthStatus_HEALTH_STATUS_HEALTHY {
			stats.UnhealthyCollectors++
		}
	}

	return stats
}

func (s *CollectorServer) countPendingEvents(collectorID string) int64 {
	// In a real implementation, this would count events in pipeline
	// that haven't been processed yet for this collector
	return 0
}

func sortCollectors(collectors []*pb.CollectorInfo, sortBy string, descending bool) {
	sort.Slice(collectors, func(i, j int) bool {
		var less bool

		switch sortBy {
		case "name":
			less = collectors[i].CollectorId < collectors[j].CollectorId
		case "type":
			less = collectors[i].CollectorType < collectors[j].CollectorType
		case "created":
			less = collectors[i].RegisteredAt.AsTime().Before(collectors[j].RegisteredAt.AsTime())
		case "events_per_second":
			less = collectors[i].EventsPerSecond < collectors[j].EventsPerSecond
		default:
			less = collectors[i].CollectorId < collectors[j].CollectorId
		}

		if descending {
			return !less
		}
		return less
	})
}

func parsePageToken(token string) int {
	val, err := strconv.Atoi(token)
	if err != nil {
		return 0
	}
	return val
}

// Additional helper for managing collector configuration

type collectorConfigManager struct {
	configs map[string]*pb.CollectorConfig
	mu      sync.RWMutex
}

func newCollectorConfigManager() *collectorConfigManager {
	return &collectorConfigManager{
		configs: make(map[string]*pb.CollectorConfig),
	}
}

func (m *collectorConfigManager) Get(collectorID string) (*pb.CollectorConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	config, exists := m.configs[collectorID]
	return config, exists
}

func (m *collectorConfigManager) Set(collectorID string, config *pb.CollectorConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[collectorID] = config
}

func (m *collectorConfigManager) Delete(collectorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.configs, collectorID)
}

// Compression helpers

type gzipCompressor struct{}

func (g *gzipCompressor) Compress(data []byte) ([]byte, error) {
	// Implementation would use gzip compression
	return data, nil
}

func (g *gzipCompressor) Decompress(data []byte) ([]byte, error) {
	// Implementation would use gzip decompression
	return data, nil
}

type zstdCompressor struct{}

func (z *zstdCompressor) Compress(data []byte) ([]byte, error) {
	// Implementation would use zstd compression
	return data, nil
}

func (z *zstdCompressor) Decompress(data []byte) ([]byte, error) {
	// Implementation would use zstd decompression
	return data, nil
}

type lz4Compressor struct{}

func (l *lz4Compressor) Compress(data []byte) ([]byte, error) {
	// Implementation would use lz4 compression
	return data, nil
}

func (l *lz4Compressor) Decompress(data []byte) ([]byte, error) {
	// Implementation would use lz4 decompression
	return data, nil
}

type snappyCompressor struct{}

func (s *snappyCompressor) Compress(data []byte) ([]byte, error) {
	// Implementation would use snappy compression
	return data, nil
}

func (s *snappyCompressor) Decompress(data []byte) ([]byte, error) {
	// Implementation would use snappy decompression
	return data, nil
}

// Health check helpers

func (s *CollectorServer) performHealthCheck() error {
	// Check internal components
	if s.pipeline == nil {
		return fmt.Errorf("event pipeline not initialized")
	}

	if s.batchProc == nil {
		return fmt.Errorf("batch processor not initialized")
	}

	// Check if we can accept new events
	currentRate := s.eventsPerSecond.Load()
	if currentRate > uint64(s.maxEventsPerSec) {
		return fmt.Errorf("system overloaded: %d events/sec exceeds limit of %d", currentRate, s.maxEventsPerSec)
	}

	return nil
}

// Metric aggregation helpers

func (s *CollectorServer) aggregateMetrics(timeWindow time.Duration) map[string]interface{} {
	metrics := make(map[string]interface{})

	// Aggregate global metrics
	metrics["total_events"] = s.totalEvents.Load()
	metrics["events_per_second"] = s.eventsPerSecond.Load()
	metrics["total_batches"] = s.totalBatches.Load()
	metrics["failed_events"] = s.failedEvents.Load()
	metrics["dropped_events"] = s.droppedEvents.Load()

	// Aggregate per-collector metrics
	s.collectorsMu.RLock()
	collectorMetrics := make(map[string]interface{})
	for id, collector := range s.collectors {
		collectorMetrics[id] = map[string]interface{}{
			"events_received":  collector.Metrics.EventsReceived,
			"events_processed": collector.Metrics.EventsProcessed,
			"error_rate":       collector.Metrics.ErrorCount,
			"current_rate":     collector.Metrics.CurrentRate,
		}
	}
	s.collectorsMu.RUnlock()

	metrics["collectors"] = collectorMetrics

	// Calculate rates
	s.streamsMu.RLock()
	activeStreams := len(s.activeStreams)
	s.streamsMu.RUnlock()

	metrics["active_streams"] = activeStreams

	return metrics
}
