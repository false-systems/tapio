//go:build linux
// +build linux

package kernel

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements simple kernel monitoring via eBPF
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  *Config
	mu      sync.RWMutex

	// Mock mode for development
	mockMode bool

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Essential OTEL Metrics (5 core metrics)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge
}

// NewCollector creates a new simple kernel collector
func NewCollector(name string, cfg *Config) (*Collector, error) {
	// Use default config if nil
	if cfg == nil {
		cfg = NewDefaultConfig(name)
	}

	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Check for mock mode
	mockMode := os.Getenv("TAPIO_MOCK_MODE") == "true"
	if mockMode {
		logger.Info("Kernel collector running in MOCK MODE", zap.String("name", name))
	}

	// Initialize minimal OTEL components
	tracer := otel.Tracer("kernel-collector")
	meter := otel.Meter("kernel-collector")

	// Only essential metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	c := &Collector{
		name:            name,
		logger:          logger.Named(name),
		tracer:          tracer,
		config:          cfg,
		mockMode:        mockMode,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		healthy:         true, // Start as healthy - will be updated when Start() is called
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
	}

	c.logger.Info("Kernel collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return c, nil
}

// NewCollectorWithConfig creates a new kernel collector with provided config and logger
// This function maintains backward compatibility with existing tests
func NewCollectorWithConfig(cfg *Config, logger *zap.Logger) (*Collector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Use provided logger or create a default one
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OTEL components
	tracer := otel.Tracer("kernel-collector")
	meter := otel.Meter("kernel-collector")

	// Create metrics with descriptive names
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", cfg.Name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", cfg.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", cfg.Name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", cfg.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", cfg.Name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", cfg.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", cfg.Name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", cfg.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", cfg.Name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", cfg.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	c := &Collector{
		name:            cfg.Name,
		logger:          logger.Named(cfg.Name),
		tracer:          tracer,
		config:          cfg,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		healthy:         true, // Start as healthy for backward compatibility
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
	}

	c.logger.Info("Kernel collector created with config",
		zap.String("name", cfg.Name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "kernel.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.logger.Info("Starting kernel collector",
		zap.Bool("enable_ebpf", c.config.EnableEBPF),
		zap.Bool("mock_mode", c.mockMode))

	// Check if we're in mock mode
	if c.mockMode {
		c.logger.Info("Starting kernel collector in mock mode")
		go c.generateMockEvents()
		c.healthy = true
		return nil
	}

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			span.SetAttributes(attribute.String("error", "ebpf_start_failed"))
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		go c.processEvents()
	}

	c.healthy = true
	c.logger.Info("Kernel collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Prevent multiple stops
	if !c.healthy {
		c.logger.Debug("Collector already stopped")
		return nil
	}

	c.logger.Info("Stopping kernel collector")

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Only close channel once
	if c.events != nil {
		close(c.events)
		c.events = nil
	}
	c.healthy = false

	c.logger.Info("Kernel collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// generateMockEvents generates fake kernel events for development/testing
func (c *Collector) generateMockEvents() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	syscalls := []string{
		"open", "read", "write", "close", "mmap", "munmap",
		"socket", "connect", "accept", "sendto", "recvfrom",
		"fork", "execve", "exit", "kill", "ptrace",
	}

	processes := []string{
		"nginx", "redis", "postgres", "kubelet", "containerd",
		"dockerd", "systemd", "sshd", "etcd", "kube-apiserver",
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Generate random kernel event
			eventType := rand.Intn(3)

			// Create mock kernel event with proper types
			mockEvent := &KernelEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       uint32(1000 + rand.Intn(10000)),
				TID:       uint32(1000 + rand.Intn(10000)),
				UID:       uint32(rand.Intn(1000)),
				GID:       uint32(rand.Intn(1000)),
			}

			// Set process name
			comm := processes[rand.Intn(len(processes))]
			copy(mockEvent.Comm[:], comm)

			switch eventType {
			case 0: // Syscall event
				mockEvent.EventType = 1                    // KERNEL_EVENT_SYSCALL
				mockEvent.Syscall = uint32(rand.Intn(400)) // Random syscall number
				mockEvent.Retval = int64(rand.Intn(2) - 1) // -1 or 0

			case 1: // Process event
				mockEvent.EventType = 2 // KERNEL_EVENT_PROCESS
				mockEvent.PPID = uint32(1 + rand.Intn(1000))

			case 2: // Security event
				mockEvent.EventType = 3                // KERNEL_EVENT_SECURITY
				mockEvent.Retval = int64(rand.Intn(3)) // Security violation type
			}

			// Convert to CollectorEvent
			collectorEvent := &domain.CollectorEvent{
				Type:      domain.EventTypeKernel,
				Timestamp: time.Now(),
				Source:    c.name,
				Priority:  domain.PriorityNormal,
				Data:      mockEvent,
				Metadata: domain.EventMetadata{
					Component: c.name,
					Host:      "mock-host",
					Attributes: map[string]string{
						"mock":       "true",
						"event_type": fmt.Sprintf("%d", mockEvent.EventType),
						"pid":        fmt.Sprintf("%d", mockEvent.PID),
					},
				},
			}

			// Send event
			select {
			case c.events <- collectorEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(c.ctx, 1)
				}
				c.logger.Debug("Generated mock kernel event")
			case <-c.ctx.Done():
				return
			}
		}
	}
}
