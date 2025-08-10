package security

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/security/bpf"
	"go.uber.org/zap"
)

// SecurityEvent represents a security event from eBPF
type SecurityEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	TargetPID uint32
	Comm      [16]byte
	CgroupID  uint64
	PodUID    [36]byte
	Data      [96]byte
}

// Collector implements security monitoring
type Collector struct {
	logger     *zap.Logger
	events     chan collectors.RawEvent
	ctx        context.Context
	cancel     context.CancelFunc
	reader     *ringbuf.Reader
	links      []link.Link
	objs       *bpf.SecuritymonitorObjects
	safeParser *collectors.SafeParser
}

// NewSecurityCollector creates a new security collector
func NewSecurityCollector(logger *zap.Logger) *Collector {
	return &Collector{
		logger:     logger,
		events:     make(chan collectors.RawEvent, 1000),
		safeParser: collectors.NewSafeParser(),
	}
}

// Start starts security monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF objects
	c.objs = &bpf.SecuritymonitorObjects{}
	if err := bpf.LoadSecuritymonitorObjects(c.objs, nil); err != nil {
		return fmt.Errorf("failed to load security monitor: %w", err)
	}

	// Attach tracepoints for security monitoring
	tpSetuid, err := link.Tracepoint("syscalls", "sys_enter_setuid", c.objs.TraceSetuid, nil)
	if err != nil {
		c.objs.Close()
		return fmt.Errorf("failed to attach setuid tracepoint: %w", err)
	}
	c.links = append(c.links, tpSetuid)

	tpSetgid, err := link.Tracepoint("syscalls", "sys_enter_setgid", c.objs.TraceSetgid, nil)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to attach setgid tracepoint: %w", err)
	}
	c.links = append(c.links, tpSetgid)

	// Attach kprobes for advanced security monitoring
	kpPtraceAttach, err := link.Kprobe("ptrace_attach", c.objs.TracePtraceAttach, nil)
	if err != nil {
		c.logger.Warn("Failed to attach ptrace_attach kprobe", zap.Error(err))
	} else {
		c.links = append(c.links, kpPtraceAttach)
	}

	kpModuleLoad, err := link.Kprobe("load_module", c.objs.TraceModuleLoad, nil)
	if err != nil {
		c.logger.Warn("Failed to attach load_module kprobe", zap.Error(err))
	} else {
		c.links = append(c.links, kpModuleLoad)
	}

	// Attach process_vm_readv tracepoint for process injection detection
	tpProcVmReadv, err := link.Tracepoint("syscalls", "sys_enter_process_vm_readv", c.objs.TraceProcessVmReadv, nil)
	if err != nil {
		c.logger.Warn("Failed to attach process_vm_readv tracepoint", zap.Error(err))
	} else {
		c.links = append(c.links, tpProcVmReadv)
	}

	// Set up ring buffer
	rd, err := ringbuf.NewReader(c.objs.SecurityEvents)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	c.reader = rd

	c.logger.Info("Security collector started",
		zap.Int("attached_links", len(c.links)),
		zap.String("ring_buffer", "security_events"))

	// Start event processing
	go c.processEvents()

	return nil
}

// Stop stops security monitoring
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	c.cleanup()

	c.logger.Info("Security collector stopped")
	return nil
}

// cleanup cleans up all resources
func (c *Collector) cleanup() {
	for _, l := range c.links {
		l.Close()
	}

	if c.reader != nil {
		c.reader.Close()
	}

	if c.objs != nil {
		c.objs.Close()
	}

	if c.events != nil {
		close(c.events)
	}
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// processEvents processes security events
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if c.reader == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			continue
		}

		// Parse security event
		event, err := c.parseSecurityEvent(record.RawSample)
		if err != nil {
			c.logger.Error("Failed to parse security event", zap.Error(err))
			continue
		}

		// Convert to RawEvent
		metadata := map[string]string{
			"collector":  "kernel-security",
			"pid":        fmt.Sprintf("%d", event.PID),
			"tid":        fmt.Sprintf("%d", event.TID),
			"target_pid": fmt.Sprintf("%d", event.TargetPID),
			"comm":       c.nullTerminatedString(event.Comm[:]),
			"cgroup_id":  fmt.Sprintf("%d", event.CgroupID),
			"pod_uid":    c.nullTerminatedString(event.PodUID[:]),
		}

		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample,
			Metadata:  metadata,
			TraceID:   collectors.GenerateTraceID(),
			SpanID:    collectors.GenerateSpanID(),
		}

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
		}
	}
}

// parseSecurityEvent parses a SecurityEvent from raw bytes with memory safety
func (c *Collector) parseSecurityEvent(rawBytes []byte) (*SecurityEvent, error) {
	// Use safe parsing with comprehensive validation
	event, err := collectors.SafeCast[SecurityEvent](c.safeParser, rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to safely parse SecurityEvent: %w", err)
	}

	// Validate security event type range (11-16)
	if err := c.safeParser.ValidateEventType(event.EventType, 11, 16); err != nil {
		return nil, fmt.Errorf("invalid security event type: %w", err)
	}

	// Validate string fields for corruption detection
	if err := c.safeParser.ValidateStringField(event.Comm[:], "comm"); err != nil {
		return nil, fmt.Errorf("invalid comm field: %w", err)
	}

	if err := c.safeParser.ValidateStringField(event.PodUID[:], "pod_uid"); err != nil {
		return nil, fmt.Errorf("invalid pod_uid field: %w", err)
	}

	// Additional security-specific validation
	if event.PID == 0 && event.EventType != 12 { // PID 0 only valid for kernel module events
		return nil, fmt.Errorf("invalid PID 0 for security event type %d", event.EventType)
	}

	// Validate TargetPID for events that should have one
	if (event.EventType == 14 || event.EventType == 16) && event.TargetPID == 0 {
		return nil, fmt.Errorf("security event type %d requires valid TargetPID", event.EventType)
	}

	return event, nil
}

// eventTypeToString converts security event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 11:
		return "privilege_escalation"
	case 12:
		return "kernel_module_load"
	case 13:
		return "kernel_module_unload"
	case 14:
		return "process_injection"
	case 15:
		return "core_dump"
	case 16:
		return "ptrace_attach"
	default:
		return "security_unknown"
	}
}

// nullTerminatedString safely converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	return c.safeParser.StringFromByteArray(b)
}
