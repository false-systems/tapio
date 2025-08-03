package systemd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// SystemdEvent represents a systemd event from eBPF
type SystemdEvent struct {
	Timestamp uint64
	PID       uint32
	PPID      uint32
	EventType uint32
	ExitCode  uint32
	Comm      [16]byte
	Filename  [256]byte
}

// Collector implements minimal systemd monitoring via eBPF
type Collector struct {
	name    string
	objs    *systemdMonitorObjects
	links   []link.Link
	reader  *ringbuf.Reader
	events  chan collectors.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  Config
}

// NewCollector creates a new minimal systemd collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	return &Collector{
		name:   name,
		config: cfg,
		events: make(chan collectors.RawEvent, cfg.BufferSize),
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start journal reader if enabled
	if c.config.EnableJournal {
		if err := c.startJournalReader(); err != nil {
			return fmt.Errorf("failed to start journal reader: %w", err)
		}
	}

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		// Load eBPF program
		spec, err := loadSystemdMonitor()
		if err != nil {
			return fmt.Errorf("failed to load eBPF spec: %w", err)
		}

		c.objs = &systemdMonitorObjects{}
		if err := spec.LoadAndAssign(c.objs, nil); err != nil {
			return fmt.Errorf("failed to load eBPF objects: %w", err)
		}

		// Populate systemd PIDs
		if err := c.populateSystemdPIDs(); err != nil {
			return fmt.Errorf("failed to populate systemd PIDs: %w", err)
		}

		// Attach tracepoints
		execLink, err := link.Tracepoint("syscalls", "sys_enter_execve", c.objs.TraceExec, nil)
		if err != nil {
			return fmt.Errorf("failed to attach execve tracepoint: %w", err)
		}
		c.links = append(c.links, execLink)

		exitLink, err := link.Tracepoint("syscalls", "sys_enter_exit", c.objs.TraceExit, nil)
		if err != nil {
			return fmt.Errorf("failed to attach exit tracepoint: %w", err)
		}
		c.links = append(c.links, exitLink)

		// Open ring buffer
		c.reader, err = ringbuf.NewReader(c.objs.Events)
		if err != nil {
			return fmt.Errorf("failed to open ring buffer: %w", err)
		}

		// Start event processing
		go c.processEvents()
	}

	c.healthy = true
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Close links
	for _, l := range c.links {
		l.Close()
	}

	// Close ring buffer
	if c.reader != nil {
		c.reader.Close()
	}

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
	}

	close(c.events)
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// populateSystemdPIDs finds and adds systemd-related PIDs to the map
func (c *Collector) populateSystemdPIDs() error {
	// Find systemd PIDs (PID 1 and its children)
	pids := []uint32{1} // systemd is always PID 1

	// Add some common systemd PIDs by scanning /proc
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}

		pid, err := strconv.ParseUint(proc.Name(), 10, 32)
		if err != nil {
			continue
		}

		// Read comm to check if it's systemd-related
		commPath := fmt.Sprintf("/proc/%d/comm", pid)
		comm, err := os.ReadFile(commPath)
		if err != nil {
			continue
		}

		commStr := strings.TrimSpace(string(comm))
		if strings.Contains(commStr, "systemd") {
			pids = append(pids, uint32(pid))
		}
	}

	// Add PIDs to eBPF map
	var value uint8 = 1
	for _, pid := range pids {
		if err := c.objs.SystemdPids.Put(pid, value); err != nil {
			// Log but don't fail - just skip this PID
			continue
		}
	}

	return nil
}

// processEvents processes events from the ring buffer
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(SystemdEvent{})) {
			continue
		}

		var event SystemdEvent
		// Simple binary unmarshaling from raw bytes
		if len(record.RawSample) != int(unsafe.Sizeof(event)) {
			continue
		}
		event = *(*SystemdEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to RawEvent - NO BUSINESS LOGIC
		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample, // Raw eBPF event data
			Metadata: map[string]string{
				"collector": "systemd",
				"pid":       fmt.Sprintf("%d", event.PID),
				"ppid":      fmt.Sprintf("%d", event.PPID),
				"comm":      c.nullTerminatedString(event.Comm[:]),
				"filename":  c.nullTerminatedString(event.Filename[:]),
				"exit_code": fmt.Sprintf("%d", event.ExitCode),
			},
			// Generate new trace ID for systemd events
			// TODO: Extract from journal metadata if available
			TraceID: collectors.GenerateTraceID(),
			SpanID:  collectors.GenerateSpanID(),
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

// eventTypeToString converts event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 1:
		return "exec"
	case 2:
		return "exit"
	case 3:
		return "kill"
	default:
		return "unknown"
	}
}

// nullTerminatedString converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
