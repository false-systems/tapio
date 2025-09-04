package kernel

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// Collector implements simple kernel monitoring via eBPF
type Collector struct {
	*base.BaseCollector       // Embed for Statistics() and Health()
	*base.EventChannelManager // Embed for event channel management
	*base.LifecycleManager    // Embed for lifecycle management

	logger *zap.Logger
	config *Config
	mu     sync.RWMutex

	// Mock mode for development
	mockMode bool

	// eBPF components (platform-specific)
	ebpfState interface{}
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

	// Initialize base components
	ctx := context.Background()
	baseCollector := base.NewBaseCollector("kernel", 5*time.Minute)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, "kernel", logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		logger:              logger.Named(name),
		config:              cfg,
		mockMode:            mockMode,
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

	// Create the collector using NewCollector
	c, err := NewCollector(cfg.Name, cfg)
	if err != nil {
		return nil, err
	}

	// Replace the logger if one was provided
	if logger != nil {
		c.logger = logger.Named(cfg.Name)
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
	return c.config.Name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	tracer := c.BaseCollector.GetTracer()
	ctx, span := tracer.Start(ctx, "kernel.collector.start")
	defer span.End()

	c.logger.Info("Starting kernel collector",
		zap.Bool("enable_ebpf", c.config.EnableEBPF),
		zap.Bool("mock_mode", c.mockMode))

	// Check if we're in mock mode
	if c.mockMode {
		c.logger.Info("Starting kernel collector in mock mode")
		c.LifecycleManager.Start("mock-generator", func() {
			c.generateMockEvents()
		})
		c.BaseCollector.SetHealthy(true)
		return nil
	}

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.BaseCollector.RecordError(err)
			span.SetAttributes(attribute.String("error", "ebpf_start_failed"))
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		c.LifecycleManager.Start("event-processor", func() {
			c.processEvents()
		})
	}

	c.BaseCollector.SetHealthy(true)
	c.logger.Info("Kernel collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping kernel collector")

	// Stop eBPF if running
	c.stopEBPF()

	// Shutdown lifecycle manager
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	c.EventChannelManager.Close()
	c.BaseCollector.SetHealthy(false)

	c.logger.Info("Kernel collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// Statistics delegates to base collector
func (c *Collector) Statistics() *domain.CollectorStats {
	return c.BaseCollector.Statistics()
}

// Health delegates to base collector
func (c *Collector) Health() *domain.HealthStatus {
	return c.BaseCollector.Health()
}

// generateMockEvents generates fake kernel events for development/testing
func (c *Collector) generateMockEvents() {
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

			// Convert to CollectorEvent
			collectorEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("kernel-mock-%d", mockEvent.PID),
				Type:      domain.EventTypeKernelProcess,
				Timestamp: time.Now(),
				Source:    c.Name(),
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
						"collector":  c.Name(),
						"mock":       "true",
						"event_type": fmt.Sprintf("%d", mockEvent.EventType),
						"pid":        fmt.Sprintf("%d", mockEvent.PID),
						"command":    comm,
					},
				},
			}

			// Send event using EventChannelManager
			if c.EventChannelManager.SendEvent(collectorEvent) {
				c.BaseCollector.RecordEvent()
			} else {
				c.BaseCollector.RecordDrop()
			}

			c.logger.Debug("Generated mock kernel event")
		}
	}
}
