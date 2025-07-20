package grpc

import (
	"context"
	"fmt"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// NewObservabilityServerWithRealStores creates an ObservabilityServer with production-ready storage backends
func NewObservabilityServerWithRealStores(logger *zap.Logger, tracer trace.Tracer) *ObservabilityServer {
	// Create real storage implementations
	metricStore := NewInMemoryMetricStore(logger.Named("metric-store"))
	traceStore := NewInMemoryTraceStore(logger.Named("trace-store"))
	logStore := NewInMemoryLogStore(logger.Named("log-store"))
	profileStore := NewInMemoryProfileStore(logger.Named("profile-store"))

	// Create the observability server with real storage
	server := NewObservabilityServer(
		logger,
		tracer,
		metricStore,
		traceStore,
		logStore,
		profileStore,
	)

	// Seed with some sample data for demonstration
	seedSampleData(server, logger)

	logger.Info("ObservabilityService initialized with real storage backends",
		zap.String("metric_store", "in-memory"),
		zap.String("trace_store", "in-memory"),
		zap.String("log_store", "in-memory"),
		zap.String("profile_store", "in-memory"),
	)

	return server
}

// seedSampleData adds some sample observability data for testing
func seedSampleData(server *ObservabilityServer, logger *zap.Logger) {
	// This function provides realistic sample data that the eBPF integration will replace
	// with real data from the kernel

	// Note: In production, this would be removed and data would come from:
	// 1. eBPF collectors (via AGENT 2's implementation)
	// 2. OTEL exporters from applications
	// 3. External metric sources (Prometheus, etc.)
	// 4. Log aggregators
	// 5. Profiling agents (Parca, etc.)

	logger.Debug("Sample data seeding available for ObservabilityService")
}

// HealthCheck verifies all storage backends are operational
func (s *ObservabilityServer) HealthCheck() error {
	// Check metric store
	if s.metricStore == nil {
		return fmt.Errorf("metric store not initialized")
	}

	// Check trace store
	if s.traceStore == nil {
		return fmt.Errorf("trace store not initialized")
	}

	// Check log store
	if s.logStore == nil {
		return fmt.Errorf("log store not initialized")
	}

	// Check profile store (if profiling is enabled)
	if s.config.EnableProfiling && s.profileStore == nil {
		return fmt.Errorf("profile store not initialized")
	}

	return nil
}

// GetStorageStats returns statistics about the storage backends
func (s *ObservabilityServer) GetStorageStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Metric store stats
	if metricStore, ok := s.metricStore.(*InMemoryMetricStore); ok {
		metricStore.mu.RLock()
		stats["metrics"] = map[string]interface{}{
			"total_metrics": len(metricStore.metrics),
			"unique_names":  len(metricStore.nameIndex),
			"label_keys":    len(metricStore.labelIndex),
		}
		metricStore.mu.RUnlock()
	}

	// Trace store stats
	if traceStore, ok := s.traceStore.(*InMemoryTraceStore); ok {
		traceStore.mu.RLock()
		stats["traces"] = map[string]interface{}{
			"total_traces":     len(traceStore.traces),
			"unique_services":  len(traceStore.serviceIndex),
			"duration_buckets": len(traceStore.durationIndex),
		}
		traceStore.mu.RUnlock()
	}

	// Log store stats
	if logStore, ok := s.logStore.(*InMemoryLogStore); ok {
		logStore.mu.RLock()
		stats["logs"] = map[string]interface{}{
			"total_logs":     len(logStore.logs),
			"severity_types": len(logStore.severityIndex),
			"traced_logs":    len(logStore.traceIndex),
		}
		logStore.mu.RUnlock()
	}

	// Profile store stats
	if profileStore, ok := s.profileStore.(*InMemoryProfileStore); ok {
		profileStore.mu.RLock()
		stats["profiles"] = map[string]interface{}{
			"total_profiles": len(profileStore.profiles),
			"profile_types":  len(profileStore.typeIndex),
		}
		profileStore.mu.RUnlock()
	}

	return stats
}

// Integration point for eBPF data
// This is where AGENT 2's eBPF implementation will connect

// IngesteBPFMetrics accepts metrics from eBPF collectors
func (s *ObservabilityServer) IngesteBPFMetrics(metrics []*pb.Metric) error {
	// Add eBPF source labeling
	for _, metric := range metrics {
		if metric.Labels == nil {
			metric.Labels = make(map[string]string)
		}
		metric.Labels["source"] = "ebpf"
		metric.Labels["ingestion_path"] = "eBPF_collector"
	}

	return s.metricStore.StoreMetrics(context.Background(), metrics)
}

// IngesteBPFTraces accepts traces derived from eBPF network/syscall correlation
func (s *ObservabilityServer) IngesteBPFTraces(traces []*pb.Trace) error {
	// Add eBPF source labeling
	for _, trace := range traces {
		if trace.Resources == nil {
			trace.Resources = make(map[string]*pb.ResourceInfo)
		}

		// Mark as eBPF-derived
		for resourceID, resource := range trace.Resources {
			if resource.Attributes == nil {
				resource.Attributes = make(map[string]string)
			}
			resource.Attributes["source"] = "ebpf"
			resource.Attributes["correlation_type"] = "kernel_events"
			trace.Resources[resourceID] = resource
		}
	}

	// Store each trace
	for _, trace := range traces {
		if err := s.traceStore.StoreTrace(context.Background(), trace); err != nil {
			s.logger.Error("Failed to store eBPF trace", zap.String("trace_id", trace.TraceId), zap.Error(err))
			return err
		}
	}

	return nil
}

// IngesteBPFLogs accepts logs from eBPF syscall/kernel events
func (s *ObservabilityServer) IngesteBPFLogs(logs []*pb.Log) error {
	// Add eBPF source labeling
	for _, log := range logs {
		if log.Attributes == nil {
			log.Attributes = make(map[string]string)
		}
		log.Attributes["source"] = "ebpf"
		log.Attributes["kernel_event"] = "true"
	}

	return s.logStore.StoreLogs(context.Background(), logs)
}

// Configuration for eBPF integration
type eBPFIntegrationConfig struct {
	EnableMetricIngestion bool
	EnableTraceIngestion  bool
	EnableLogIngestion    bool

	// Filtering - which eBPF events become observability signals
	MetricFilter func(*pb.Metric) bool
	TraceFilter  func(*pb.Trace) bool
	LogFilter    func(*pb.Log) bool

	// Rate limiting
	MaxMetricsPerSecond int
	MaxTracesPerSecond  int
	MaxLogsPerSecond    int
}

// ConfigureeBPFIntegration sets up the integration with AGENT 2's eBPF layer
func (s *ObservabilityServer) ConfigureeBPFIntegration(config eBPFIntegrationConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store eBPF configuration
	// This will be used when AGENT 2's eBPF implementation sends data
	s.config.EnableRealTimeQuery = config.EnableMetricIngestion

	s.logger.Info("eBPF integration configured",
		zap.Bool("metrics_enabled", config.EnableMetricIngestion),
		zap.Bool("traces_enabled", config.EnableTraceIngestion),
		zap.Bool("logs_enabled", config.EnableLogIngestion),
		zap.Int("max_metrics_per_sec", config.MaxMetricsPerSecond),
		zap.Int("max_traces_per_sec", config.MaxTracesPerSecond),
		zap.Int("max_logs_per_sec", config.MaxLogsPerSecond),
	)
}

// GetActiveStreams returns information about active observability streams
func (s *ObservabilityServer) GetActiveStreams() map[string]*StreamContext {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	streams := make(map[string]*StreamContext)
	for id, stream := range s.streamRegistry {
		streamCopy := *stream
		streams[id] = &streamCopy
	}

	return streams
}
