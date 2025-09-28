package kernel

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer implements simple kernel monitoring via eBPF
type Observer struct {
	*base.BaseObserver        // Embed for Statistics() and Health()
	*base.EventChannelManager // Embed for event channel management
	*base.LifecycleManager    // Embed for lifecycle management

	logger *zap.Logger
	config *Config
	mu     sync.RWMutex

	// Mock mode for development
	mockMode bool

	// eBPF components (platform-specific)
	ebpfState interface{}

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	errorsTotal     metric.Int64Counter
	syscallsTracked metric.Int64Counter
	configAccesses  metric.Int64Counter
	secretAccesses  metric.Int64Counter
	accessFailures  metric.Int64Counter
	processingTime  metric.Float64Histogram
	eventSize       metric.Int64Histogram
}

// NewObserver creates a new simple kernel observer
func NewObserver(name string, cfg *Config) (*Observer, error) {
	// Use default config if nil
	if cfg == nil {
		cfg = NewDefaultConfig(name)
	}

	// Ensure buffer size is valid (non-negative and reasonable)
	if cfg.BufferSize < 0 {
		cfg.BufferSize = 0
	}
	// Cap buffer size to prevent allocation errors
	const maxBufferSize = 1000000 // 1 million events max
	if cfg.BufferSize > maxBufferSize {
		cfg.BufferSize = maxBufferSize
	}

	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Check for mock mode
	mockMode := os.Getenv("TAPIO_MOCK_MODE") == "true"
	if mockMode {
		logger.Info("Kernel observer running in MOCK MODE", zap.String("name", name))
	}

	// Initialize OpenTelemetry
	meter := otel.Meter("tapio.observers.kernel")
	tracer := otel.Tracer("tapio.observers.kernel")

	// Create metrics
	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total kernel events processed"),
	)
	eventsDropped, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription("Total kernel events dropped"),
	)
	errorsTotal, _ := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in kernel observer"),
	)
	syscallsTracked, _ := meter.Int64Counter(
		fmt.Sprintf("%s_syscalls_tracked_total", name),
		metric.WithDescription("Total syscalls tracked"),
	)
	configAccesses, _ := meter.Int64Counter(
		fmt.Sprintf("%s_config_accesses_total", name),
		metric.WithDescription("Total ConfigMap accesses tracked"),
	)
	secretAccesses, _ := meter.Int64Counter(
		fmt.Sprintf("%s_secret_accesses_total", name),
		metric.WithDescription("Total Secret accesses tracked"),
	)
	accessFailures, _ := meter.Int64Counter(
		fmt.Sprintf("%s_access_failures_total", name),
		metric.WithDescription("Total config/secret access failures"),
	)
	processingTime, _ := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_time_ms", name),
		metric.WithDescription("Event processing time in milliseconds"),
	)
	eventSize, _ := meter.Int64Histogram(
		fmt.Sprintf("%s_event_size_bytes", name),
		metric.WithDescription("Size of kernel events in bytes"),
	)

	// Initialize base components
	ctx := context.Background()
	baseObserver := base.NewBaseObserver("kernel", 5*time.Minute)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, "kernel", logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	c := &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		logger:              logger.Named(name),
		config:              cfg,
		mockMode:            mockMode,
		tracer:              tracer,
		eventsProcessed:     eventsProcessed,
		eventsDropped:       eventsDropped,
		errorsTotal:         errorsTotal,
		syscallsTracked:     syscallsTracked,
		configAccesses:      configAccesses,
		secretAccesses:      secretAccesses,
		accessFailures:      accessFailures,
		processingTime:      processingTime,
		eventSize:           eventSize,
	}

	c.logger.Info("Kernel observer created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return c, nil
}

// NewObserverWithConfig creates a new kernel observer with provided config and logger
// This function maintains backward compatibility with existing tests
func NewObserverWithConfig(cfg *Config, logger *zap.Logger) (*Observer, error) {
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

	// Create the observer using NewObserver
	c, err := NewObserver(cfg.Name, cfg)
	if err != nil {
		return nil, err
	}

	// Replace the logger if one was provided
	if logger != nil {
		c.logger = logger.Named(cfg.Name)
	}

	c.logger.Info("Kernel observer created with config",
		zap.String("name", cfg.Name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return c, nil
}

// Name returns observer name
func (c *Observer) Name() string {
	return c.config.Name
}

// Start starts the eBPF monitoring
func (c *Observer) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "kernel.observer.start")
	defer span.End()

	c.logger.Info("Starting kernel observer",
		zap.Bool("enable_ebpf", c.config.EnableEBPF),
		zap.Bool("mock_mode", c.mockMode))

	span.SetAttributes(
		attribute.Bool("mock_mode", c.mockMode),
		attribute.Bool("enable_ebpf", c.config.EnableEBPF),
		attribute.String("observer_name", c.config.Name),
	)

	// Check if we're in mock mode
	if c.mockMode {
		c.logger.Info("Starting kernel observer in mock mode")
		c.LifecycleManager.Start("mock-generator", func() {
			c.generateMockEvents()
		})
		c.BaseObserver.SetHealthy(true)
		span.SetStatus(codes.Ok, "Started in mock mode")
		return nil
	}

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.BaseObserver.RecordError(err)
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
			span.RecordError(err)
			span.SetStatus(codes.Error, "Failed to start eBPF")
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		c.LifecycleManager.Start("event-processor", func() {
			c.processEvents()
		})
	}

	c.BaseObserver.SetHealthy(true)
	c.logger.Info("Kernel observer started successfully")
	span.SetStatus(codes.Ok, "Started successfully")
	return nil
}

// Stop stops the observer
func (c *Observer) Stop() error {
	_, span := c.tracer.Start(context.Background(), "kernel.observer.stop")
	defer span.End()

	c.logger.Info("Stopping kernel observer")

	// First stop the lifecycle manager to signal goroutines to exit
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Then stop eBPF after goroutines have exited
	c.stopEBPF()

	// Close event channel
	c.EventChannelManager.Close()
	c.BaseObserver.SetHealthy(false)

	c.logger.Info("Kernel observer stopped successfully")
	span.SetStatus(codes.Ok, "Stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Observer) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// Statistics delegates to base observer
func (c *Observer) Statistics() *domain.CollectorStats {
	return c.BaseObserver.Statistics()
}

// Health delegates to base observer
func (c *Observer) Health() *domain.HealthStatus {
	return c.BaseObserver.Health()
}

// generateMockEvents generates fake kernel events for development/testing
func (c *Observer) generateMockEvents() {
	ctx := c.LifecycleManager.Context()
	_, span := c.tracer.Start(ctx, "kernel.generate_mock_events")
	defer span.End()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	processes := []string{
		"nginx", "redis", "postgres", "kubelet", "containerd",
		"dockerd", "systemd", "sshd", "etcd", "kube-apiserver",
	}

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			// Generate random kernel event
			eventType := rand.Intn(3)

			// Create mock kernel event with proper types
			mockEvent := &KernelEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       uint32(1000 + rand.Intn(10000)),
				TID:       uint32(1000 + rand.Intn(10000)),
				EventType: uint32(1 + rand.Intn(4)), // Random event type 1-4
				CgroupID:  uint64(rand.Intn(1000)),
			}

			// Set process name
			comm := processes[rand.Intn(len(processes))]
			copy(mockEvent.Comm[:], comm)

			// Generate pod UID for some events
			if rand.Intn(3) == 0 {
				podUID := fmt.Sprintf("pod-%d", rand.Intn(1000))
				copy(mockEvent.PodUID[:], podUID)
			}

			switch eventType {
			case 0: // ConfigMap access
				mockEvent.EventType = uint32(EventTypeConfigMapAccess)

			case 1: // Secret access
				mockEvent.EventType = uint32(EventTypeSecretAccess)

			case 2: // Failed config access
				mockEvent.EventType = uint32(EventTypeConfigAccessFailed)
			}

			// Convert to ObserverEvent
			observerEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("kernel-mock-%d", mockEvent.PID),
				Type:      domain.EventTypeKernelSyscall, // Use syscall type for kernel events
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("kernel-%s", c.config.Name), // Prefix with "kernel-" for validator
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Kernel: &domain.KernelData{
						EventType: fmt.Sprintf("mock_event_%d", mockEvent.EventType),
						PID:       int32(mockEvent.PID),
						Command:   comm,
						CgroupID:  mockEvent.CgroupID,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer":   "kernel", // Validator expects "kernel"
						"version":    "1.0.0",
						"mock":       "true",
						"event_type": fmt.Sprintf("%d", mockEvent.EventType),
						"pid":        fmt.Sprintf("%d", mockEvent.PID),
						"command":    comm,
					},
				},
			}

			// Send event using EventChannelManager
			if c.EventChannelManager.SendEvent(observerEvent) {
				c.BaseObserver.RecordEvent()
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", fmt.Sprintf("%d", mockEvent.EventType)),
					attribute.Bool("mock", true),
				))
			} else {
				c.BaseObserver.RecordDrop()
				c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
					attribute.String("reason", "channel_full"),
					attribute.Bool("mock", true),
				))
			}

			c.logger.Debug("Generated mock kernel event")
		}
	}
}

// parseConfigPath extracts config type, name, and pod UID from mount path
func (c *Observer) parseConfigPath(mountPath string) (configType, configName, podUID string) {
	// Check if this is a Kubernetes volume mount path
	if !strings.Contains(mountPath, "/kubelet/pods/") {
		return "", "", ""
	}

	// Extract pod UID from path
	// Format: /var/lib/kubelet/pods/{pod-uid}/volumes/kubernetes.io~{type}/{name}
	parts := strings.Split(mountPath, "/")
	for i, part := range parts {
		if part == "pods" && i+1 < len(parts) {
			podUID = parts[i+1]
		}
		if strings.HasPrefix(part, "kubernetes.io~") {
			// Extract volume type
			volParts := strings.Split(part, "~")
			if len(volParts) == 2 {
				configType = volParts[1]
			}
			// Next part should be the config name
			if i+1 < len(parts) {
				configName = parts[i+1]
			}
		}
	}

	// Handle volume-subpaths format
	if configType == "" && strings.Contains(mountPath, "volume-subpaths") {
		for i, part := range parts {
			if strings.HasPrefix(part, "kubernetes.io~") {
				volParts := strings.Split(part, "~")
				if len(volParts) == 2 {
					configType = volParts[1]
				}
				if i+1 < len(parts) {
					configName = parts[i+1]
				}
			}
		}
	}

	return configType, configName, podUID
}

// getErrorDescription returns human-readable error description
func (c *Observer) getErrorDescription(errorCode int32) string {
	switch errorCode {
	case 0:
		return "Success"
	case 2:
		return "No such file or directory"
	case 5:
		return "I/O error"
	case 13:
		return "Permission denied"
	case 28:
		return "No space left on device"
	case 30:
		return "Read-only file system"
	default:
		return fmt.Sprintf("Unknown error (%d)", errorCode)
	}
}

// convertKernelEvent converts kernel event to domain event (simplified for non-Linux)
func (c *Observer) convertKernelEvent(event *KernelEvent) *domain.CollectorEvent {
	if event == nil {
		return nil
	}

	// Extract command name from Comm field
	comm := string(bytes.TrimRight(event.Comm[:], "\x00"))

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("kernel-%d-%d", event.PID, event.Timestamp),
		Type:      domain.EventTypeKernelSyscall,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Source:    c.Name(),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				PID:       int32(event.PID),
				Command:   comm,
				CgroupID:  event.CgroupID,
				EventType: "config_access",
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "kernel",
				"version":  "1.0.0",
				"pid":      fmt.Sprintf("%d", event.PID),
				"command":  comm,
			},
		},
	}
}
