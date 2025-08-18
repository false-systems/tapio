// Package bpf_common provides shared eBPF functionality
package bpf_common

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// BPFStatistics tracks comprehensive eBPF program performance and health metrics
type BPFStatistics struct {
	// Program identification
	ProgramName string
	ProgramID   uint32
	ProgramType string

	// Event counters
	EventsReceived  uint64 `json:"events_received"`
	EventsProcessed uint64 `json:"events_processed"`
	EventsDropped   uint64 `json:"events_dropped"`
	EventsFiltered  uint64 `json:"events_filtered"`
	EventsSampled   uint64 `json:"events_sampled"`
	EventsBatched   uint64 `json:"events_batched"`

	// Performance metrics
	ProcessingTimeNs  uint64 `json:"processing_time_ns"`
	AverageLatencyNs  uint64 `json:"average_latency_ns"`
	Peak1MinLatencyNs uint64 `json:"peak_1min_latency_ns"`
	Peak5MinLatencyNs uint64 `json:"peak_5min_latency_ns"`

	// eBPF-specific metrics
	RingBufferSize        uint64  `json:"ring_buffer_size"`
	RingBufferUsed        uint64  `json:"ring_buffer_used"`
	RingBufferUtilization float64 `json:"ring_buffer_utilization"`
	MapMemoryUsage        uint64  `json:"map_memory_usage"`
	InstructionCount      uint64  `json:"instruction_count"`
	VerifierLogSize       uint32  `json:"verifier_log_size"`

	// Health indicators
	ProgramErrors        uint64 `json:"program_errors"`
	VerificationFailures uint64 `json:"verification_failures"`
	AttachFailures       uint64 `json:"attach_failures"`
	DetachEvents         uint64 `json:"detach_events"`

	// Filter statistics
	NamespaceFilters uint64 `json:"namespace_filters"`
	ProcessFilters   uint64 `json:"process_filters"`
	NetworkFilters   uint64 `json:"network_filters"`

	// Sampling statistics
	SamplingRate      float64           `json:"sampling_rate"`
	SamplingActive    bool              `json:"sampling_active"`
	SampledEventTypes map[string]uint64 `json:"sampled_event_types"`

	// Batch processing
	BatchesSent        uint64  `json:"batches_sent"`
	AverageBatchSize   float64 `json:"average_batch_size"`
	BatchFullEvents    uint64  `json:"batch_full_events"`
	BatchTimeoutEvents uint64  `json:"batch_timeout_events"`

	// Timing
	LastUpdate    time.Time `json:"last_update"`
	StartTime     time.Time `json:"start_time"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
}

// BPFStatsCollector manages statistics collection for eBPF programs
type BPFStatsCollector struct {
	mu     sync.RWMutex
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// OTEL instrumentation
	meter metric.Meter

	// Statistics storage
	programs       map[string]*BPFStatistics
	updateInterval time.Duration

	// Metrics
	programsActive        metric.Int64UpDownCounter
	eventsReceivedTotal   metric.Int64Counter
	eventsProcessedTotal  metric.Int64Counter
	eventsDroppedTotal    metric.Int64Counter
	eventsFilteredTotal   metric.Int64Counter
	eventsSampledTotal    metric.Int64Counter
	eventsBatchedTotal    metric.Int64Counter
	processingDuration    metric.Float64Histogram
	ringBufferUtilization metric.Float64Gauge
	mapMemoryUsage        metric.Int64UpDownCounter
	programErrors         metric.Int64Counter
	verificationFailures  metric.Int64Counter
	attachFailures        metric.Int64Counter
	batchesSentTotal      metric.Int64Counter
	averageBatchSize      metric.Float64Gauge
	samplingRateGauge     metric.Float64Gauge
	filterHitsTotal       metric.Int64Counter
}

// NewBPFStatsCollector creates a new eBPF statistics collector
func NewBPFStatsCollector(logger *zap.Logger, updateInterval time.Duration) (*BPFStatsCollector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	if updateInterval <= 0 {
		updateInterval = 5 * time.Second
	}

	meter := otel.Meter("tapio.bpf.stats")

	// Create OTEL metrics
	programsActive, err := meter.Int64UpDownCounter(
		"bpf_programs_active",
		metric.WithDescription("Number of active eBPF programs"),
	)
	if err != nil {
		logger.Warn("Failed to create programs_active metric", zap.Error(err))
	}

	eventsReceivedTotal, err := meter.Int64Counter(
		"bpf_events_received_total",
		metric.WithDescription("Total eBPF events received"),
	)
	if err != nil {
		logger.Warn("Failed to create events_received_total metric", zap.Error(err))
	}

	eventsProcessedTotal, err := meter.Int64Counter(
		"bpf_events_processed_total",
		metric.WithDescription("Total eBPF events processed successfully"),
	)
	if err != nil {
		logger.Warn("Failed to create events_processed_total metric", zap.Error(err))
	}

	eventsDroppedTotal, err := meter.Int64Counter(
		"bpf_events_dropped_total",
		metric.WithDescription("Total eBPF events dropped"),
	)
	if err != nil {
		logger.Warn("Failed to create events_dropped_total metric", zap.Error(err))
	}

	eventsFilteredTotal, err := meter.Int64Counter(
		"bpf_events_filtered_total",
		metric.WithDescription("Total eBPF events filtered out"),
	)
	if err != nil {
		logger.Warn("Failed to create events_filtered_total metric", zap.Error(err))
	}

	eventsSampledTotal, err := meter.Int64Counter(
		"bpf_events_sampled_total",
		metric.WithDescription("Total eBPF events included via sampling"),
	)
	if err != nil {
		logger.Warn("Failed to create events_sampled_total metric", zap.Error(err))
	}

	eventsBatchedTotal, err := meter.Int64Counter(
		"bpf_events_batched_total",
		metric.WithDescription("Total eBPF events sent in batches"),
	)
	if err != nil {
		logger.Warn("Failed to create events_batched_total metric", zap.Error(err))
	}

	processingDuration, err := meter.Float64Histogram(
		"bpf_processing_duration_ms",
		metric.WithDescription("eBPF event processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing_duration metric", zap.Error(err))
	}

	ringBufferUtilization, err := meter.Float64Gauge(
		"bpf_ring_buffer_utilization_ratio",
		metric.WithDescription("eBPF ring buffer utilization ratio (0.0-1.0)"),
	)
	if err != nil {
		logger.Warn("Failed to create ring_buffer_utilization metric", zap.Error(err))
	}

	mapMemoryUsage, err := meter.Int64UpDownCounter(
		"bpf_map_memory_usage_bytes",
		metric.WithDescription("eBPF map memory usage in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create map_memory_usage metric", zap.Error(err))
	}

	programErrors, err := meter.Int64Counter(
		"bpf_program_errors_total",
		metric.WithDescription("Total eBPF program errors"),
	)
	if err != nil {
		logger.Warn("Failed to create program_errors metric", zap.Error(err))
	}

	verificationFailures, err := meter.Int64Counter(
		"bpf_verification_failures_total",
		metric.WithDescription("Total eBPF program verification failures"),
	)
	if err != nil {
		logger.Warn("Failed to create verification_failures metric", zap.Error(err))
	}

	attachFailures, err := meter.Int64Counter(
		"bpf_attach_failures_total",
		metric.WithDescription("Total eBPF program attach failures"),
	)
	if err != nil {
		logger.Warn("Failed to create attach_failures metric", zap.Error(err))
	}

	batchesSentTotal, err := meter.Int64Counter(
		"bpf_batches_sent_total",
		metric.WithDescription("Total number of event batches sent"),
	)
	if err != nil {
		logger.Warn("Failed to create batches_sent_total metric", zap.Error(err))
	}

	averageBatchSize, err := meter.Float64Gauge(
		"bpf_average_batch_size",
		metric.WithDescription("Average number of events per batch"),
	)
	if err != nil {
		logger.Warn("Failed to create average_batch_size metric", zap.Error(err))
	}

	samplingRateGauge, err := meter.Float64Gauge(
		"bpf_sampling_rate",
		metric.WithDescription("Current eBPF sampling rate (0.0-1.0)"),
	)
	if err != nil {
		logger.Warn("Failed to create sampling_rate metric", zap.Error(err))
	}

	filterHitsTotal, err := meter.Int64Counter(
		"bpf_filter_hits_total",
		metric.WithDescription("Total eBPF filter hits by type"),
	)
	if err != nil {
		logger.Warn("Failed to create filter_hits_total metric", zap.Error(err))
	}

	return &BPFStatsCollector{
		logger:                logger,
		programs:              make(map[string]*BPFStatistics),
		updateInterval:        updateInterval,
		meter:                 meter,
		programsActive:        programsActive,
		eventsReceivedTotal:   eventsReceivedTotal,
		eventsProcessedTotal:  eventsProcessedTotal,
		eventsDroppedTotal:    eventsDroppedTotal,
		eventsFilteredTotal:   eventsFilteredTotal,
		eventsSampledTotal:    eventsSampledTotal,
		eventsBatchedTotal:    eventsBatchedTotal,
		processingDuration:    processingDuration,
		ringBufferUtilization: ringBufferUtilization,
		mapMemoryUsage:        mapMemoryUsage,
		programErrors:         programErrors,
		verificationFailures:  verificationFailures,
		attachFailures:        attachFailures,
		batchesSentTotal:      batchesSentTotal,
		averageBatchSize:      averageBatchSize,
		samplingRateGauge:     samplingRateGauge,
		filterHitsTotal:       filterHitsTotal,
	}, nil
}

// Start begins statistics collection
func (c *BPFStatsCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("stats collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start metrics update loop
	go c.metricsUpdateLoop()

	c.logger.Info("BPF statistics collector started",
		zap.Duration("update_interval", c.updateInterval),
	)

	return nil
}

// Stop gracefully shuts down the statistics collector
func (c *BPFStatsCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	c.logger.Info("BPF statistics collector stopped")
	return nil
}

// RegisterProgram registers a new eBPF program for monitoring
func (c *BPFStatsCollector) RegisterProgram(name, programType string, programID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := &BPFStatistics{
		ProgramName:       name,
		ProgramID:         programID,
		ProgramType:       programType,
		StartTime:         time.Now(),
		LastUpdate:        time.Now(),
		SampledEventTypes: make(map[string]uint64),
	}

	c.programs[name] = stats

	// Update active programs metric
	if c.programsActive != nil {
		c.programsActive.Add(c.ctx, 1, metric.WithAttributes(
			attribute.String("program_name", name),
			attribute.String("program_type", programType),
		))
	}

	c.logger.Info("Registered eBPF program for monitoring",
		zap.String("name", name),
		zap.String("type", programType),
		zap.Uint32("program_id", programID),
	)
}

// UnregisterProgram removes a program from monitoring
func (c *BPFStatsCollector) UnregisterProgram(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if stats, exists := c.programs[name]; exists {
		delete(c.programs, name)

		// Update active programs metric
		if c.programsActive != nil {
			c.programsActive.Add(c.ctx, -1, metric.WithAttributes(
				attribute.String("program_name", name),
				attribute.String("program_type", stats.ProgramType),
			))
		}

		c.logger.Info("Unregistered eBPF program from monitoring",
			zap.String("name", name),
		)
	}
}

// UpdateStats updates statistics for a specific program
func (c *BPFStatsCollector) UpdateStats(name string, update func(*BPFStatistics)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if stats, exists := c.programs[name]; exists {
		update(stats)
		stats.LastUpdate = time.Now()
		stats.UptimeSeconds = uint64(time.Since(stats.StartTime).Seconds())

		// Update ring buffer utilization
		if stats.RingBufferSize > 0 {
			stats.RingBufferUtilization = float64(stats.RingBufferUsed) / float64(stats.RingBufferSize)
		}

		// Update average batch size
		if stats.BatchesSent > 0 {
			stats.AverageBatchSize = float64(stats.EventsBatched) / float64(stats.BatchesSent)
		}
	}
}

// GetStats returns a copy of statistics for a program
func (c *BPFStatsCollector) GetStats(name string) (*BPFStatistics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if stats, exists := c.programs[name]; exists {
		// Create a deep copy
		copy := *stats
		copy.SampledEventTypes = make(map[string]uint64)
		for k, v := range stats.SampledEventTypes {
			copy.SampledEventTypes[k] = v
		}
		return &copy, true
	}

	return nil, false
}

// GetAllStats returns copies of all program statistics
func (c *BPFStatsCollector) GetAllStats() map[string]*BPFStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*BPFStatistics)
	for name, stats := range c.programs {
		copy := *stats
		copy.SampledEventTypes = make(map[string]uint64)
		for k, v := range stats.SampledEventTypes {
			copy.SampledEventTypes[k] = v
		}
		result[name] = &copy
	}

	return result
}

// IncrementEventCounter atomically increments an event counter
func (c *BPFStatsCollector) IncrementEventCounter(programName string, counterType CounterType, delta uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, exists := c.programs[programName]
	if !exists {
		return
	}

	switch counterType {
	case CounterEventsReceived:
		atomic.AddUint64(&stats.EventsReceived, delta)
		if c.eventsReceivedTotal != nil {
			c.eventsReceivedTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterEventsProcessed:
		atomic.AddUint64(&stats.EventsProcessed, delta)
		if c.eventsProcessedTotal != nil {
			c.eventsProcessedTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterEventsDropped:
		atomic.AddUint64(&stats.EventsDropped, delta)
		if c.eventsDroppedTotal != nil {
			c.eventsDroppedTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterEventsFiltered:
		atomic.AddUint64(&stats.EventsFiltered, delta)
		if c.eventsFilteredTotal != nil {
			c.eventsFilteredTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterEventsSampled:
		atomic.AddUint64(&stats.EventsSampled, delta)
		if c.eventsSampledTotal != nil {
			c.eventsSampledTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterEventsBatched:
		atomic.AddUint64(&stats.EventsBatched, delta)
		if c.eventsBatchedTotal != nil {
			c.eventsBatchedTotal.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	case CounterProgramErrors:
		atomic.AddUint64(&stats.ProgramErrors, delta)
		if c.programErrors != nil {
			c.programErrors.Add(c.ctx, int64(delta), metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	}
}

// RecordProcessingTime records processing time for performance tracking
func (c *BPFStatsCollector) RecordProcessingTime(programName string, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, exists := c.programs[programName]
	if !exists {
		return
	}

	durationNs := uint64(duration.Nanoseconds())
	atomic.AddUint64(&stats.ProcessingTimeNs, durationNs)

	// Update average latency
	totalEvents := atomic.LoadUint64(&stats.EventsProcessed)
	if totalEvents > 0 {
		stats.AverageLatencyNs = atomic.LoadUint64(&stats.ProcessingTimeNs) / totalEvents
	}

	// Update peak latencies (simplified approach)
	if durationNs > stats.Peak1MinLatencyNs {
		stats.Peak1MinLatencyNs = durationNs
	}
	if durationNs > stats.Peak5MinLatencyNs {
		stats.Peak5MinLatencyNs = durationNs
	}

	// Record OTEL metric
	if c.processingDuration != nil {
		durationMs := float64(duration.Nanoseconds()) / 1e6
		c.processingDuration.Record(c.ctx, durationMs, metric.WithAttributes(
			attribute.String("program_name", programName),
		))
	}
}

// UpdateRingBufferStats updates ring buffer utilization metrics
func (c *BPFStatsCollector) UpdateRingBufferStats(programName string, size, used uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, exists := c.programs[programName]
	if !exists {
		return
	}

	stats.RingBufferSize = size
	stats.RingBufferUsed = used

	if size > 0 {
		stats.RingBufferUtilization = float64(used) / float64(size)

		if c.ringBufferUtilization != nil {
			c.ringBufferUtilization.Record(c.ctx, stats.RingBufferUtilization, metric.WithAttributes(
				attribute.String("program_name", programName),
			))
		}
	}
}

// metricsUpdateLoop periodically updates OTEL metrics
func (c *BPFStatsCollector) metricsUpdateLoop() {
	ticker := time.NewTicker(c.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateMetrics()
		}
	}
}

// updateMetrics updates all OTEL metrics
func (c *BPFStatsCollector) updateMetrics() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for name, stats := range c.programs {
		attrs := metric.WithAttributes(attribute.String("program_name", name))

		// Update gauge metrics
		if c.ringBufferUtilization != nil {
			c.ringBufferUtilization.Record(c.ctx, stats.RingBufferUtilization, attrs)
		}

		if c.mapMemoryUsage != nil {
			c.mapMemoryUsage.Add(c.ctx, int64(stats.MapMemoryUsage), attrs)
		}

		if c.averageBatchSize != nil {
			c.averageBatchSize.Record(c.ctx, stats.AverageBatchSize, attrs)
		}

		if c.samplingRateGauge != nil {
			c.samplingRateGauge.Record(c.ctx, stats.SamplingRate, attrs)
		}
	}
}

// CounterType defines the types of counters that can be incremented
type CounterType int

const (
	CounterEventsReceived CounterType = iota
	CounterEventsProcessed
	CounterEventsDropped
	CounterEventsFiltered
	CounterEventsSampled
	CounterEventsBatched
	CounterProgramErrors
)
