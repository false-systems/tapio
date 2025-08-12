package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// InstrumentedStorage wraps a Storage implementation with OpenTelemetry instrumentation
// This demonstrates how storage adapters should implement OTEL directly
type InstrumentedStorage struct {
	storage Storage
	logger  *zap.Logger

	// OTEL instrumentation - REQUIRED fields
	tracer          trace.Tracer
	storageOpsCtr   metric.Int64Counter
	errorsTotalCtr  metric.Int64Counter
	opDurationHist  metric.Float64Histogram
	recordsSizeHist metric.Float64Histogram
	activeOpGauge   metric.Int64UpDownCounter
}

// NewInstrumentedStorage creates a new instrumented storage wrapper
func NewInstrumentedStorage(storage Storage, logger *zap.Logger) (*InstrumentedStorage, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer("correlation-storage")
	meter := otel.Meter("correlation-storage")

	// Create metrics with descriptive names and descriptions
	storageOpsCtr, err := meter.Int64Counter(
		"correlation_storage_operations_total",
		metric.WithDescription("Total storage operations by correlation storage"),
	)
	if err != nil {
		logger.Warn("Failed to create storage ops counter", zap.Error(err))
	}

	errorsTotalCtr, err := meter.Int64Counter(
		"correlation_storage_errors_total",
		metric.WithDescription("Total errors in correlation storage"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	opDurationHist, err := meter.Float64Histogram(
		"correlation_storage_operation_duration_ms",
		metric.WithDescription("Storage operation duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create operation duration histogram", zap.Error(err))
	}

	recordsSizeHist, err := meter.Float64Histogram(
		"correlation_storage_record_size_bytes",
		metric.WithDescription("Size of stored correlation records in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create record size histogram", zap.Error(err))
	}

	activeOpGauge, err := meter.Int64UpDownCounter(
		"correlation_storage_active_operations",
		metric.WithDescription("Number of active storage operations"),
	)
	if err != nil {
		logger.Warn("Failed to create active operations gauge", zap.Error(err))
	}

	return &InstrumentedStorage{
		storage:         storage,
		logger:          logger,
		tracer:          tracer,
		storageOpsCtr:   storageOpsCtr,
		errorsTotalCtr:  errorsTotalCtr,
		opDurationHist:  opDurationHist,
		recordsSizeHist: recordsSizeHist,
		activeOpGauge:   activeOpGauge,
	}, nil
}

// Store saves a correlation result with instrumentation
func (s *InstrumentedStorage) Store(ctx context.Context, result *CorrelationResult) error {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.store")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "store"),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "store"),
		attribute.String("correlation.id", result.ID),
		attribute.String("correlation.type", result.Type),
		attribute.Float64("correlation.confidence", result.Confidence),
	)

	// Estimate record size (simplified)
	recordSize := float64(len(result.ID) + len(result.Type) + 100) // Rough estimate
	if s.recordsSizeHist != nil {
		s.recordsSizeHist.Record(ctx, recordSize, metric.WithAttributes(
			attribute.String("correlation.type", result.Type),
		))
	}

	// Execute the actual storage operation
	err := s.storage.Store(ctx, result)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "store_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "store_failed"),
				attribute.String("operation", "store"),
			))
		}
		return fmt.Errorf("failed to store correlation: %w", err)
	}

	// Record success metrics
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "store"),
			attribute.String("status", "success"),
		))
	}

	return nil
}

// GetRecent retrieves recent correlations with instrumentation
func (s *InstrumentedStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.get_recent")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "get_recent"),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "get_recent"),
		attribute.Int("limit", limit),
	)

	// Execute the actual storage operation
	results, err := s.storage.GetRecent(ctx, limit)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "get_recent_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "get_recent_failed"),
				attribute.String("operation", "get_recent"),
			))
		}
		return nil, fmt.Errorf("failed to get recent correlations: %w", err)
	}

	// Record success metrics
	span.SetAttributes(attribute.Int("results.count", len(results)))
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "get_recent"),
			attribute.String("status", "success"),
			attribute.Int("results_count", len(results)),
		))
	}

	return results, nil
}

// GetByTraceID retrieves correlations for a specific trace with instrumentation
func (s *InstrumentedStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.get_by_trace")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "get_by_trace"),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "get_by_trace"),
		attribute.String("trace.id", traceID),
	)

	// Execute the actual storage operation
	results, err := s.storage.GetByTraceID(ctx, traceID)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "get_by_trace_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "get_by_trace_failed"),
				attribute.String("operation", "get_by_trace"),
			))
		}
		return nil, fmt.Errorf("failed to get correlations by trace ID: %w", err)
	}

	// Record success metrics
	span.SetAttributes(attribute.Int("results.count", len(results)))
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "get_by_trace"),
			attribute.String("status", "success"),
			attribute.Int("results_count", len(results)),
		))
	}

	return results, nil
}

// GetByTimeRange retrieves correlations within a time range with instrumentation
func (s *InstrumentedStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.get_by_time_range")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "get_by_time_range"),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "get_by_time_range"),
		attribute.String("time.start", start.Format(time.RFC3339)),
		attribute.String("time.end", end.Format(time.RFC3339)),
		attribute.Float64("time.range_hours", end.Sub(start).Hours()),
	)

	// Execute the actual storage operation
	results, err := s.storage.GetByTimeRange(ctx, start, end)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "get_by_time_range_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "get_by_time_range_failed"),
				attribute.String("operation", "get_by_time_range"),
			))
		}
		return nil, fmt.Errorf("failed to get correlations by time range: %w", err)
	}

	// Record success metrics
	span.SetAttributes(attribute.Int("results.count", len(results)))
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "get_by_time_range"),
			attribute.String("status", "success"),
			attribute.Int("results_count", len(results)),
		))
	}

	return results, nil
}

// GetByResource retrieves correlations affecting a specific resource with instrumentation
func (s *InstrumentedStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.get_by_resource")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "get_by_resource"),
				attribute.String("resource_type", resourceType),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "get_by_resource"),
		attribute.String("resource.type", resourceType),
		attribute.String("resource.namespace", namespace),
		attribute.String("resource.name", name),
	)

	// Execute the actual storage operation
	results, err := s.storage.GetByResource(ctx, resourceType, namespace, name)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "get_by_resource_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "get_by_resource_failed"),
				attribute.String("operation", "get_by_resource"),
				attribute.String("resource_type", resourceType),
			))
		}
		return nil, fmt.Errorf("failed to get correlations by resource: %w", err)
	}

	// Record success metrics
	span.SetAttributes(attribute.Int("results.count", len(results)))
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "get_by_resource"),
			attribute.String("status", "success"),
			attribute.String("resource_type", resourceType),
			attribute.Int("results_count", len(results)),
		))
	}

	return results, nil
}

// Cleanup removes old correlations with instrumentation
func (s *InstrumentedStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	// Always start spans for operations
	ctx, span := s.tracer.Start(ctx, "correlation.storage.cleanup")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record operation duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if s.opDurationHist != nil {
			s.opDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "cleanup"),
			))
		}
	}()

	// Track active operations
	if s.activeOpGauge != nil {
		s.activeOpGauge.Add(ctx, 1)
		defer s.activeOpGauge.Add(ctx, -1)
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "correlation-storage"),
		attribute.String("operation", "cleanup"),
		attribute.Float64("older_than_hours", olderThan.Hours()),
	)

	// Execute the actual storage operation
	err := s.storage.Cleanup(ctx, olderThan)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "cleanup_failed"),
		)
		// Record error metrics
		if s.errorsTotalCtr != nil {
			s.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "cleanup_failed"),
				attribute.String("operation", "cleanup"),
			))
		}
		return fmt.Errorf("failed to cleanup old correlations: %w", err)
	}

	// Record success metrics
	if s.storageOpsCtr != nil {
		s.storageOpsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "cleanup"),
			attribute.String("status", "success"),
		))
	}

	return nil
}

// MemoryStorage is a simple in-memory storage implementation for testing
// This demonstrates a basic storage implementation without breaking the architecture
type MemoryStorage struct {
	mu      sync.RWMutex
	results map[string]*CorrelationResult
	logger  *zap.Logger
}

// NewMemoryStorage creates a new in-memory storage
func NewMemoryStorage(logger *zap.Logger) *MemoryStorage {
	return &MemoryStorage{
		results: make(map[string]*CorrelationResult),
		logger:  logger,
	}
}

// Store saves a correlation result in memory
func (m *MemoryStorage) Store(ctx context.Context, result *CorrelationResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.results[result.ID] = result
	return nil
}

// GetRecent retrieves recent correlations from memory
func (m *MemoryStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*CorrelationResult, 0, limit)
	for _, result := range m.results {
		if len(results) >= limit {
			break
		}
		results = append(results, result)
	}
	return results, nil
}

// GetByTraceID retrieves correlations for a specific trace from memory
func (m *MemoryStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*CorrelationResult
	for _, result := range m.results {
		if result.TraceID == traceID {
			results = append(results, result)
		}
	}
	return results, nil
}

// GetByTimeRange retrieves correlations within a time range from memory
func (m *MemoryStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*CorrelationResult
	for _, result := range m.results {
		if result.StartTime.After(start) && result.EndTime.Before(end) {
			results = append(results, result)
		}
	}
	return results, nil
}

// GetByResource retrieves correlations affecting a specific resource from memory
func (m *MemoryStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*CorrelationResult
	for _, result := range m.results {
		// Check in related events for matching resources
		for _, event := range result.Related {
			if event != nil && event.Entity != nil {
				if event.Entity.Type == resourceType &&
					event.Entity.Namespace == namespace &&
					event.Entity.Name == name {
					results = append(results, result)
					break
				}
			}
		}
	}
	return results, nil
}

// Cleanup removes old correlations from memory
func (m *MemoryStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for id, result := range m.results {
		if result.EndTime.Before(cutoff) {
			delete(m.results, id)
		}
	}
	return nil
}
