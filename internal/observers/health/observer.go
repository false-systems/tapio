package health

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// HealthEvent represents a system health issue captured by eBPF
type HealthEvent struct {
	TimestampNs uint64
	PID         uint32
	PPID        uint32
	TID         uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	SyscallNr   int32
	ErrorCode   int32
	Category    uint8
	_pad        [3]uint8
	Comm        [16]byte
	Path        [256]byte
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Arg1        uint64
	Arg2        uint64
	Arg3        uint64
	ErrorCount  uint32
	_pad2       uint32
}

// ObserverStats represents observer statistics
type ObserverStats struct {
	TotalErrors       uint64
	ENOSPCCount       uint64
	ENOMEMCount       uint64
	ECONNREFUSEDCount uint64
	EIOCount          uint64
	EventsSent        uint64
	EventsDropped     uint64
}

// Observer implements the health observer
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health() methods
	*base.EventChannelManager // Handles event channel with drop counting
	*base.LifecycleManager    // Manages goroutines and graceful shutdown

	name   string
	logger *zap.Logger

	// eBPF state (platform-specific)
	ebpfState interface{}

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	enospcErrors    metric.Int64Counter
	enomemErrors    metric.Int64Counter
	econnrefErrors  metric.Int64Counter
	emfileErrors    metric.Int64Counter
	edquotErrors    metric.Int64Counter
	eventsDropped   metric.Int64Counter

	// Configuration
	config *Config

	// Error tracking for rate limiting
	lastErrorLogTime  time.Time
	errorLogInterval  time.Duration
	consecutiveErrors int
}

// Config holds observer configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories map[string]bool // Map for O(1) lookup
	RequireAllMetrics bool            // If true, fail startup when metrics can't be created
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:   8 * 1024 * 1024, // 8MB
		EventChannelSize: 10000,
		RateLimitMs:      100,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false, // Default to graceful degradation
	}
}

// NewObserver creates a new health observer
func NewObserver(logger *zap.Logger, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Initialize OpenTelemetry
	tracer := otel.Tracer("health-observer")
	meter := otel.Meter("health-observer")

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		"health_events_processed_total",
		metric.WithDescription("Total health events processed"),
	)
	if err != nil && config.RequireAllMetrics {
		return nil, fmt.Errorf("failed to create events counter: %w", err)
	}

	errorsTotal, err := meter.Int64Counter(
		"health_observer_errors_total",
		metric.WithDescription("Total errors in health observer"),
	)
	if err != nil && config.RequireAllMetrics {
		return nil, fmt.Errorf("failed to create errors counter: %w", err)
	}

	processingTime, err := meter.Float64Histogram(
		"health_processing_duration_ms",
		metric.WithDescription("Processing duration for health events in milliseconds"),
	)
	if err != nil && config.RequireAllMetrics {
		return nil, fmt.Errorf("failed to create processing time histogram: %w", err)
	}

	// Error-specific metrics
	enospcErrors, _ := meter.Int64Counter(
		"health_disk_space_errors_total",
		metric.WithDescription("Total disk space exhaustion errors captured"),
	)

	enomemErrors, _ := meter.Int64Counter(
		"health_memory_errors_total",
		metric.WithDescription("Total memory exhaustion errors captured"),
	)

	econnrefErrors, _ := meter.Int64Counter(
		"health_connection_refused_errors_total",
		metric.WithDescription("Total connection refused errors captured"),
	)

	emfileErrors, _ := meter.Int64Counter(
		"health_file_descriptor_errors_total",
		metric.WithDescription("Total file descriptor exhaustion errors captured"),
	)

	edquotErrors, _ := meter.Int64Counter(
		"health_disk_quota_errors_total",
		metric.WithDescription("Total disk quota exceeded errors captured"),
	)

	eventsDropped, _ := meter.Int64Counter(
		"health_events_dropped_total",
		metric.WithDescription("Total events dropped due to channel overflow"),
	)

	o := &Observer{
		BaseObserver:        base.NewBaseObserver("health", 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.EventChannelSize, "health", logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		name:                "health",
		logger:              logger,
		tracer:              tracer,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
		processingTime:      processingTime,
		enospcErrors:        enospcErrors,
		enomemErrors:        enomemErrors,
		econnrefErrors:      econnrefErrors,
		emfileErrors:        emfileErrors,
		edquotErrors:        edquotErrors,
		eventsDropped:       eventsDropped,
		config:              config,
		errorLogInterval:    time.Duration(config.RateLimitMs) * time.Millisecond,
	}

	// Start as unhealthy, become healthy only after Start() is called
	o.BaseObserver.SetHealthy(false)

	return o, nil
}

// Start begins observing system health
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting health observer",
		zap.Int("ringBufferSize", o.config.RingBufferSize),
		zap.Int("eventChannelSize", o.config.EventChannelSize),
		zap.Any("enabledCategories", o.config.EnabledCategories),
	)

	// Start eBPF (platform-specific)
	if err := o.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processor using LifecycleManager
	o.LifecycleManager.Start("event-reader", func() {
		o.readEvents()
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Health observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping health observer")

	// Stop eBPF (platform-specific)
	o.stopEBPF()

	// Stop goroutines and wait
	o.LifecycleManager.Stop(5 * time.Second)

	// Close event channel
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Health observer stopped")
	return nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns the health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// GetStats retrieves observer statistics (platform-specific implementation)
func (o *Observer) GetStats() (*ObserverStats, error) {
	// Implemented in platform-specific files
	return o.getStatsImpl()
}

// convertToCollectorEvent converts eBPF event to domain event
func (o *Observer) convertToCollectorEvent(event *HealthEvent) *domain.CollectorEvent {
	_, span := o.tracer.Start(o.LifecycleManager.Context(), "convertToCollectorEvent")
	defer span.End()

	// Convert basic fields
	pid := int32(event.PID)
	comm := bytesToString(event.Comm[:])
	path := bytesToString(event.Path[:])

	// Map syscall number to name
	syscallName := getSyscallName(event.SyscallNr)

	// Map error code to name and severity
	errorName := getErrorName(event.ErrorCode)
	severity := getErrorSeverity(event.ErrorCode)

	// Extract network context if applicable
	var customData map[string]string
	if event.Category == 2 && event.SrcIP != 0 { // network category
		customData = map[string]string{
			"src_ip":   formatIP(event.SrcIP),
			"dst_ip":   formatIP(event.DstIP),
			"src_port": fmt.Sprintf("%d", event.SrcPort),
			"dst_port": fmt.Sprintf("%d", event.DstPort),
		}
	}

	// Create collector event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("health-%d-%d", event.PID, event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.EventTypeKernelSyscall,
		Source:    o.name,
		Severity:  mapSeverity(severity),
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				EventType:    "health_issue",
				PID:          pid,
				PPID:         int32(event.PPID),
				UID:          int32(event.UID),
				GID:          int32(event.GID),
				Command:      comm,
				CgroupID:     event.CgroupID,
				Syscall:      syscallName,
				ReturnCode:   event.ErrorCode,
				ErrorMessage: errorName,
			},
			Custom: customData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":    o.name,
				"version":     "1.0.0",
				"error_count": fmt.Sprintf("%d", event.ErrorCount),
				"category":    getCategoryName(event.Category),
				"path":        path,
			},
		},
	}
}

// Helper functions
func bytesToString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func formatIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

func getCategoryName(category uint8) string {
	switch category {
	case 1:
		return "file"
	case 2:
		return "network"
	case 3:
		return "memory"
	case 4:
		return "process"
	default:
		return "unknown"
	}
}

func mapSeverity(severity string) domain.EventSeverity {
	switch severity {
	case "critical":
		return domain.EventSeverityCritical
	case "high":
		return domain.EventSeverityError
	case "medium":
		return domain.EventSeverityWarning
	default:
		return domain.EventSeverityInfo
	}
}

// Error name mapping
func getErrorName(code int32) string {
	errorNames := map[int32]string{
		-1:   "EPERM",
		-2:   "ENOENT",
		-5:   "EIO",
		-12:  "ENOMEM",
		-13:  "EACCES",
		-16:  "EBUSY",
		-22:  "EINVAL",
		-24:  "EMFILE",
		-28:  "ENOSPC",
		-110: "ETIMEDOUT",
		-111: "ECONNREFUSED",
		-122: "EDQUOT",
	}

	if name, ok := errorNames[code]; ok {
		return name
	}
	return fmt.Sprintf("ERROR_%d", code)
}

// Error severity classification
func getErrorSeverity(code int32) string {
	switch code {
	case -12, -28, -122, -24: // ENOMEM, ENOSPC, EDQUOT, EMFILE
		return "critical"
	case -5, -111, -110, -104: // EIO, ECONNREFUSED, ETIMEDOUT, ECONNRESET
		return "high"
	case -13, -1, -16: // EACCES, EPERM, EBUSY
		return "medium"
	default:
		return "low"
	}
}

// Syscall name mapping (partial list for brevity)
func getSyscallName(nr int32) string {
	syscallNames := map[int32]string{
		0:   "read",
		1:   "write",
		2:   "open",
		3:   "close",
		41:  "socket",
		42:  "connect",
		43:  "accept",
		59:  "execve",
		83:  "mkdir",
		87:  "unlink",
		257: "openat",
	}

	if name, ok := syscallNames[nr]; ok {
		return name
	}
	return fmt.Sprintf("syscall_%d", nr)
}
