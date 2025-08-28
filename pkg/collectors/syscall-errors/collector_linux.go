//go:build linux
// +build linux

package syscallerrors

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 syscallmonitor ./bpf_src/syscall_monitor.c -- -I./bpf_src

// SyscallErrorEvent represents a syscall error captured by eBPF
type SyscallErrorEvent struct {
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

// CollectorStats represents collector statistics
type CollectorStats struct {
	TotalErrors       uint64
	ENOSPCCount       uint64
	ENOMEMCount       uint64
	ECONNREFUSEDCount uint64
	EIOCount          uint64
	EventsSent        uint64
	EventsDropped     uint64
}

// Collector implements the syscall error collector
type Collector struct {
	name   string
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// eBPF components
	objs   *syscallmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Event processing
	eventChan chan *domain.ObservationEvent
	stopOnce  sync.Once

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

// Config holds collector configuration
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

// NewCollector creates a new syscall error collector
func NewCollector(logger *zap.Logger, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OpenTelemetry
	tracer := otel.Tracer("syscall-errors-collector")
	meter := otel.Meter("syscall-errors-collector")

	eventsProcessed, err := meter.Int64Counter(
		"syscall_errors_events_processed_total",
		metric.WithDescription("Total syscall error events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		"syscall_errors_collector_errors_total",
		metric.WithDescription("Total errors in syscall error collector"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		"syscall_errors_processing_duration_ms",
		metric.WithDescription("Processing duration for syscall errors in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	enospcErrors, err := meter.Int64Counter(
		"syscall_errors_enospc_total",
		metric.WithDescription("Total ENOSPC errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ENOSPC counter", zap.Error(err))
	}

	enomemErrors, err := meter.Int64Counter(
		"syscall_errors_enomem_total",
		metric.WithDescription("Total ENOMEM errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ENOMEM counter", zap.Error(err))
	}

	econnrefErrors, err := meter.Int64Counter(
		"syscall_errors_econnrefused_total",
		metric.WithDescription("Total ECONNREFUSED errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ECONNREFUSED counter", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		"syscall_errors_events_dropped_total",
		metric.WithDescription("Total events dropped due to channel overflow"),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
	}

	// Check if all required metrics were created
	if config.RequireAllMetrics {
		if eventsProcessed == nil || errorsTotal == nil || processingTime == nil {
			return nil, fmt.Errorf("failed to create required metrics, aborting startup")
		}
		logger.Info("All required metrics initialized successfully")
	}

	return &Collector{
		name:              "syscall-errors",
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		eventChan:         make(chan *domain.ObservationEvent, config.EventChannelSize),
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		enospcErrors:      enospcErrors,
		enomemErrors:      enomemErrors,
		econnrefErrors:    econnrefErrors,
		emfileErrors:      emfileErrors,
		edquotErrors:      edquotErrors,
		eventsDropped:     eventsDropped,
		config:            config,
		errorLogInterval:  time.Minute, // Log errors at most once per minute
		lastErrorLogTime:  time.Time{},
		consecutiveErrors: 0,
	}, nil
}

// Start begins collecting syscall errors
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "syscall_errors.start")
	defer span.End()

	c.logger.Info("Starting syscall error collector")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memory limit", zap.Error(err))
	}

	// Load eBPF programs
	objs := &syscallmonitorObjects{}
	if err := loadSyscallmonitorObjects(objs, nil); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}
	c.objs = objs

	// Attach tracepoints
	if err := c.attachTracepoints(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching tracepoints: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	c.reader = reader

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	c.logger.Info("Syscall error collector started successfully")
	return nil
}

// attachTracepoints attaches the eBPF programs to kernel tracepoints
func (c *Collector) attachTracepoints() error {
	// Attach sys_enter tracepoint
	enterLink, err := link.Tracepoint("raw_syscalls", "sys_enter", c.objs.TraceSysEnter, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_enter tracepoint: %w", err)
	}
	c.links = append(c.links, enterLink)

	// Attach sys_exit tracepoint
	exitLink, err := link.Tracepoint("raw_syscalls", "sys_exit", c.objs.TraceSysExit, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_exit tracepoint: %w", err)
	}
	c.links = append(c.links, exitLink)

	return nil
}

// processEvents reads and processes events from the ring buffer
func (c *Collector) processEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping syscall error event processing")
			return
		default:
			record, err := c.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "ring_buffer_read"),
					))
				}

				// Rate-limited error logging to prevent log spam
				c.consecutiveErrors++
				now := time.Now()
				if now.Sub(c.lastErrorLogTime) > c.errorLogInterval {
					c.logger.Error("Failed to read from ring buffer",
						zap.Error(err),
						zap.Int("consecutive_errors", c.consecutiveErrors))
					c.lastErrorLogTime = now
					c.consecutiveErrors = 0
				}

				// Add a small delay to avoid busy-waiting on persistent errors
				time.Sleep(50 * time.Millisecond)
				continue
			}

			// Reset error counter on successful read
			c.consecutiveErrors = 0

			// Process the event
			if err := c.processRawEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to process event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "event_processing_failed"),
					))
				}
			}
		}
	}
}

// processRawEvent processes a single raw eBPF event
func (c *Collector) processRawEvent(data []byte) error {
	ctx, span := c.tracer.Start(c.ctx, "syscall_errors.process_event")
	defer span.End()

	start := time.Now()
	defer func() {
		if c.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000
			c.processingTime.Record(ctx, duration)
		}
	}()

	// Parse the event
	expectedSize := int(unsafe.Sizeof(SyscallErrorEvent{}))
	if len(data) < expectedSize {
		return fmt.Errorf("event data too small: got %d bytes, expected %d", len(data), expectedSize)
	}

	// Validate exact size match to ensure struct alignment
	if len(data) != expectedSize {
		c.logger.Warn("Event size mismatch, potential struct alignment issue",
			zap.Int("got_size", len(data)),
			zap.Int("expected_size", expectedSize))
	}

	var event SyscallErrorEvent
	reader := bytes.NewReader(data[:expectedSize]) // Only read expected bytes
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Filter by category if enabled
	if !c.isCategoryEnabled(event.Category) {
		// Silently drop events from disabled categories
		return nil
	}

	// Convert to ObservationEvent
	obsEvent := c.convertToObservationEvent(&event)

	// Update metrics
	if c.eventsProcessed != nil {
		c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", c.getErrorName(event.ErrorCode)),
			attribute.String("category", c.getCategoryName(event.Category)),
		))
	}

	// Update specific error counters
	c.updateErrorMetrics(ctx, event.ErrorCode)

	// Send to event channel
	select {
	case c.eventChan <- obsEvent:
		span.SetAttributes(
			attribute.String("syscall", c.getSyscallName(event.SyscallNr)),
			attribute.String("error", c.getErrorName(event.ErrorCode)),
			attribute.Int("pid", int(event.PID)),
		)
	case <-c.ctx.Done():
		return nil
	default:
		// Channel full, drop event
		if c.eventsDropped != nil {
			c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
				attribute.String("category", c.getCategoryName(event.Category)),
				attribute.String("syscall", c.getSyscallName(event.SyscallNr)),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "channel_full"),
			))
		}

		// Log warning with rate limiting
		now := time.Now()
		if now.Sub(c.lastErrorLogTime) > c.errorLogInterval {
			c.logger.Warn("Event channel full, dropping events",
				zap.String("syscall", c.getSyscallName(event.SyscallNr)),
				zap.String("error", c.getErrorName(event.ErrorCode)),
				zap.Int("pid", int(event.PID)))
			c.lastErrorLogTime = now
		}
	}

	return nil
}

// convertToObservationEvent converts eBPF event to domain ObservationEvent
func (c *Collector) convertToObservationEvent(event *SyscallErrorEvent) *domain.ObservationEvent {
	timestamp := time.Unix(0, int64(event.TimestampNs))

	// Extract strings from fixed-size arrays
	comm := bytesToString(event.Comm[:])
	path := bytesToString(event.Path[:])

	// Build custom data map
	customData := map[string]string{
		"pid":         fmt.Sprintf("%d", event.PID),
		"ppid":        fmt.Sprintf("%d", event.PPID),
		"tid":         fmt.Sprintf("%d", event.TID),
		"uid":         fmt.Sprintf("%d", event.UID),
		"gid":         fmt.Sprintf("%d", event.GID),
		"cgroup_id":   fmt.Sprintf("%d", event.CgroupID),
		"command":     comm,
		"syscall":     c.getSyscallName(event.SyscallNr),
		"error_code":  fmt.Sprintf("%d", event.ErrorCode),
		"error_name":  c.getErrorName(event.ErrorCode),
		"category":    c.getCategoryName(event.Category),
		"error_count": fmt.Sprintf("%d", event.ErrorCount),
		"description": fmt.Sprintf("Syscall %s failed with %s",
			c.getSyscallName(event.SyscallNr),
			c.getErrorName(event.ErrorCode)),
	}

	// Add path if present
	if path != "" {
		customData["path"] = path
	}

	// Determine severity based on error type
	severity := c.getSeverityForError(event.ErrorCode)

	// Create collector event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("syscall-error-%d-%d", timestamp.UnixNano(), event.PID),
		Type:      domain.EventTypeKernelSyscall,
		Timestamp: timestamp,
		Source:    "syscall-errors",
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				EventType:    "syscall_error",
				PID:          int32(event.PID),
				PPID:         int32(event.PPID),
				UID:          int32(event.UID),
				GID:          int32(event.GID),
				Command:      comm,
				CgroupID:     event.CgroupID,
				Syscall:      c.getSyscallName(event.SyscallNr),
				ReturnCode:   event.ErrorCode,
				ErrorMessage: c.getErrorName(event.ErrorCode),
			},
			SystemCall: &domain.SystemCallData{
				Number:    int64(event.SyscallNr),
				Name:      c.getSyscallName(event.SyscallNr),
				PID:       int32(event.PID),
				TID:       int32(event.TID),
				UID:       int32(event.UID),
				GID:       int32(event.GID),
				Arguments: []domain.SystemCallArg{},
				RetValue:  int64(event.ErrorCode),
				ErrorCode: event.ErrorCode,
			},
			Custom: customData,
		},
		Metadata: domain.EventMetadata{},
	}
}

// updateErrorMetrics updates specific error counters
func (c *Collector) updateErrorMetrics(ctx context.Context, errorCode int32) {
	switch -errorCode {
	case 28: // ENOSPC
		if c.enospcErrors != nil {
			c.enospcErrors.Add(ctx, 1)
		}
	case 12: // ENOMEM
		if c.enomemErrors != nil {
			c.enomemErrors.Add(ctx, 1)
		}
	case 111: // ECONNREFUSED
		if c.econnrefErrors != nil {
			c.econnrefErrors.Add(ctx, 1)
		}
	}
}

// getSeverityForError determines severity based on error type
func (c *Collector) getSeverityForError(errorCode int32) domain.EventSeverity {
	switch -errorCode {
	case 28, 12: // ENOSPC, ENOMEM - critical resource exhaustion
		return domain.EventSeverityCritical
	case 111, 110: // ECONNREFUSED, ETIMEDOUT - service connectivity issues
		return domain.EventSeverityHigh
	case 5: // EIO - I/O errors
		return domain.EventSeverityHigh
	case 13, 1: // EACCES, EPERM - permission issues
		return domain.EventSeverityMedium
	default:
		return domain.EventSeverityLow
	}
}

// getErrorName returns human-readable error name
func (c *Collector) getErrorName(errorCode int32) string {
	// Comprehensive mapping of Linux error codes
	errorNames := map[int32]string{
		1:   "EPERM",
		2:   "ENOENT",
		3:   "ESRCH",
		4:   "EINTR",
		5:   "EIO",
		6:   "ENXIO",
		7:   "E2BIG",
		8:   "ENOEXEC",
		9:   "EBADF",
		10:  "ECHILD",
		11:  "EAGAIN",
		12:  "ENOMEM",
		13:  "EACCES",
		14:  "EFAULT",
		15:  "ENOTBLK",
		16:  "EBUSY",
		17:  "EEXIST",
		18:  "EXDEV",
		19:  "ENODEV",
		20:  "ENOTDIR",
		21:  "EISDIR",
		22:  "EINVAL",
		23:  "ENFILE",
		24:  "EMFILE",
		25:  "ENOTTY",
		26:  "ETXTBSY",
		27:  "EFBIG",
		28:  "ENOSPC",
		29:  "ESPIPE",
		30:  "EROFS",
		31:  "EMLINK",
		32:  "EPIPE",
		33:  "EDOM",
		34:  "ERANGE",
		35:  "EDEADLK",
		36:  "ENAMETOOLONG",
		37:  "ENOLCK",
		38:  "ENOSYS",
		39:  "ENOTEMPTY",
		40:  "ELOOP",
		42:  "ENOMSG",
		43:  "EIDRM",
		61:  "ENODATA",
		62:  "ETIME",
		63:  "ENOSR",
		71:  "EPROTO",
		95:  "EOPNOTSUPP",
		98:  "EADDRINUSE",
		99:  "EADDRNOTAVAIL",
		100: "ENETDOWN",
		101: "ENETUNREACH",
		102: "ENETRESET",
		103: "ECONNABORTED",
		104: "ECONNRESET",
		105: "ENOBUFS",
		106: "EISCONN",
		107: "ENOTCONN",
		108: "ESHUTDOWN",
		110: "ETIMEDOUT",
		111: "ECONNREFUSED",
		112: "EHOSTDOWN",
		113: "EHOSTUNREACH",
		114: "EALREADY",
		115: "EINPROGRESS",
		116: "ESTALE",
		122: "EDQUOT",
	}

	if name, ok := errorNames[-errorCode]; ok {
		return name
	}
	return fmt.Sprintf("ERROR_%d", -errorCode)
}

// getCategoryName returns category name
func (c *Collector) getCategoryName(category uint8) string {
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
		return "other"
	}
}

// getSyscallName returns syscall name
func (c *Collector) getSyscallName(syscallNr int32) string {
	// Comprehensive mapping of x86_64 syscall numbers
	syscallNames := map[int32]string{
		0:   "read",
		1:   "write",
		2:   "open",
		3:   "close",
		4:   "stat",
		5:   "fstat",
		6:   "lstat",
		7:   "poll",
		8:   "lseek",
		9:   "mmap",
		10:  "mprotect",
		11:  "munmap",
		12:  "brk",
		13:  "rt_sigaction",
		14:  "rt_sigprocmask",
		16:  "ioctl",
		17:  "pread64",
		18:  "pwrite64",
		19:  "readv",
		20:  "writev",
		21:  "access",
		22:  "pipe",
		23:  "select",
		24:  "sched_yield",
		25:  "mremap",
		26:  "msync",
		27:  "mincore",
		28:  "madvise",
		32:  "dup",
		33:  "dup2",
		35:  "nanosleep",
		39:  "getpid",
		40:  "sendfile",
		41:  "socket",
		42:  "connect",
		43:  "accept",
		44:  "sendto",
		45:  "recvfrom",
		46:  "sendmsg",
		47:  "recvmsg",
		48:  "shutdown",
		49:  "bind",
		50:  "listen",
		51:  "getsockname",
		52:  "getpeername",
		53:  "socketpair",
		54:  "setsockopt",
		55:  "getsockopt",
		56:  "clone",
		57:  "fork",
		59:  "execve",
		60:  "exit",
		61:  "wait4",
		62:  "kill",
		72:  "fcntl",
		73:  "flock",
		74:  "fsync",
		75:  "fdatasync",
		76:  "truncate",
		77:  "ftruncate",
		78:  "getdents",
		79:  "getcwd",
		80:  "chdir",
		81:  "fchdir",
		82:  "rename",
		83:  "mkdir",
		84:  "rmdir",
		85:  "creat",
		86:  "link",
		87:  "unlink",
		88:  "symlink",
		89:  "readlink",
		90:  "chmod",
		91:  "fchmod",
		92:  "chown",
		93:  "fchown",
		94:  "lchown",
		102: "getuid",
		104: "getgid",
		107: "geteuid",
		108: "getegid",
		110: "getppid",
		111: "getpgrp",
		112: "setsid",
		186: "gettid",
		217: "getdents64",
		231: "exit_group",
		232: "epoll_wait",
		233: "epoll_ctl",
		257: "openat",
		258: "mkdirat",
		259: "mknodat",
		260: "fchownat",
		261: "futimesat",
		262: "newfstatat",
		263: "unlinkat",
		264: "renameat",
		265: "linkat",
		266: "symlinkat",
		267: "readlinkat",
		268: "fchmodat",
		269: "faccessat",
		270: "pselect6",
		271: "ppoll",
		281: "epoll_pwait",
		288: "accept4",
		293: "pipe2",
		295: "preadv",
		296: "pwritev",
		302: "prlimit64",
		318: "getrandom",
		332: "statx",
	}

	if name, ok := syscallNames[syscallNr]; ok {
		return name
	}
	return fmt.Sprintf("syscall_%d", syscallNr)
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.stopOnce.Do(func() {
		c.logger.Info("Stopping syscall error collector")
		c.cancel()

		// Close ring buffer reader
		if c.reader != nil {
			c.reader.Close()
		}

		// Wait for event processing to complete
		c.wg.Wait()

		// Cleanup eBPF resources
		c.cleanup()

		// Close event channel
		close(c.eventChan)
	})
	return nil
}

// cleanup cleans up eBPF resources
func (c *Collector) cleanup() {
	// Close all links
	for _, l := range c.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
	}
}

// GetEventChannel returns the event channel
func (c *Collector) GetEventChannel() <-chan *domain.ObservationEvent {
	return c.eventChan
}

// GetName returns the collector name
func (c *Collector) GetName() string {
	return c.name
}

// IsHealthy checks if the collector is healthy
func (c *Collector) IsHealthy() bool {
	// Check if context is still active
	select {
	case <-c.ctx.Done():
		return false
	default:
		return true
	}
}

// GetStats returns current collector statistics
func (c *Collector) GetStats() (*CollectorStats, error) {
	if c.objs == nil || c.objs.Stats == nil {
		return nil, fmt.Errorf("eBPF objects not initialized")
	}

	var key uint32 = 0
	var stats CollectorStats

	// Read from per-CPU map and aggregate
	values, err := c.objs.Stats.Lookup(&key)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup stats: %w", err)
	}

	// Type assertion with safety check
	switch v := values.(type) {
	case []CollectorStats:
		// Expected type for per-CPU map
		for _, cpuStat := range v {
			stats.TotalErrors += cpuStat.TotalErrors
			stats.ENOSPCCount += cpuStat.ENOSPCCount
			stats.ENOMEMCount += cpuStat.ENOMEMCount
			stats.ECONNREFUSEDCount += cpuStat.ECONNREFUSEDCount
			stats.EIOCount += cpuStat.EIOCount
			stats.EventsSent += cpuStat.EventsSent
			stats.EventsDropped += cpuStat.EventsDropped
		}
	case CollectorStats:
		// Single value (non per-CPU map)
		stats = v
	default:
		return nil, fmt.Errorf("unexpected type from Stats map: %T", values)
	}

	return &stats, nil
}

// bytesToString converts null-terminated byte array to string
// isCategoryEnabled checks if a category is enabled for collection
func (c *Collector) isCategoryEnabled(category uint8) bool {
	categoryName := c.getCategoryName(category)

	// If no categories specified, enable all
	if len(c.config.EnabledCategories) == 0 {
		return true
	}

	// Check if category is in enabled map
	return c.config.EnabledCategories[categoryName]
}

func bytesToString(data []byte) string {
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n])
}
