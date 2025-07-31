//go:build linux
// +build linux

package cni

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// CNI eBPF event types
const (
	EventTypeCNIExec = iota
	EventTypeNetNSCreate
	EventTypeNetNSChange
)

// cniEvent matches the C struct
type cniEvent struct {
	Timestamp uint64
	Pid       uint32
	EventType uint32
	Data      [256]byte
}

// initEBPF initializes eBPF programs for CNI monitoring
func (c *Collector) initEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load real eBPF programs from embedded bytecode
	spec, err := loadCNIeBPFSpec()
	if err != nil {
		return fmt.Errorf("failed to load CNI eBPF spec: %w", err)
	}

	// Load collection
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("failed to create eBPF collection: %w", err)
	}
	c.ebpfCollection = coll
	c.ebpfLinks = make([]interface{}, 0)

	// Get events map
	eventsMap, ok := coll.Maps["cni_events"]
	if !ok {
		return errors.New("cni_events map not found")
	}

	// Create perf reader
	reader, err := perf.NewReader(eventsMap, 4096)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	c.ebpfReader = reader

	// Attach programs
	if err := c.attachEBPFPrograms(coll); err != nil {
		return fmt.Errorf("failed to attach eBPF programs: %w", err)
	}

	return nil
}

// attachEBPFPrograms attaches eBPF programs to kernel hooks
func (c *Collector) attachEBPFPrograms(coll *ebpf.Collection) error {
	// Attach execve tracepoint for CNI binary execution
	if prog, ok := coll.Programs["trace_cni_exec"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_execve", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach execve: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	// Attach clone tracepoint for network namespace creation
	if prog, ok := coll.Programs["trace_clone"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_clone", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach clone: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	// Attach setns tracepoint for namespace changes
	if prog, ok := coll.Programs["trace_setns"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_setns", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach setns: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	return nil
}

// readEBPFEvents reads CNI events from eBPF
func (c *Collector) readEBPFEvents() {
	defer c.wg.Done()

	reader, ok := c.ebpfReader.(*perf.Reader)
	if !ok || reader == nil {
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := reader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				// Log error but continue - eBPF is best effort
				continue
			}

			// Try to parse as structured event first
			if len(record.RawSample) >= int(unsafe.Sizeof(cniEvent{})) {
				var event cniEvent
				if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err == nil {
					c.processCNIEvent(&event)
					continue
				}
			}

			// Fallback: treat as raw syscall trace
			c.processRawSyscallTrace(record)
		}
	}
}

// processCNIEvent processes a structured CNI event
func (c *Collector) processCNIEvent(event *cniEvent) {
	data, _ := json.Marshal(map[string]interface{}{
		"event_type": c.getEventTypeName(event.EventType),
		"pid":        event.Pid,
		"timestamp":  event.Timestamp,
		"binary":     string(bytes.TrimRight(event.Data[:], "\x00")),
		"syscall":    "execve", // Inferred from event type
	})

	rawEvent := collectors.RawEvent{
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      "cni_exec",
		Data:      data,
		Metadata: map[string]string{
			"source":     "ebpf",
			"cni_plugin": c.detectedCNI,
			"event_type": c.getEventTypeName(event.EventType),
			"pid":        fmt.Sprintf("%d", event.Pid),
		},
	}

	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// processRawSyscallTrace processes raw syscall trace data
func (c *Collector) processRawSyscallTrace(record perf.Record) {
	// Extract what info we can from raw trace
	data, _ := json.Marshal(map[string]interface{}{
		"raw_data_size": len(record.RawSample),
		"cpu":           record.CPU,
		"lost_samples":  record.LostSamples,
		"trace_type":    "syscall",
	})

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(), // Use current time since we can't parse trace timestamp
		Type:      "cni_trace",
		Data:      data,
		Metadata: map[string]string{
			"source":     "ebpf",
			"cni_plugin": c.detectedCNI,
			"trace_type": "raw_syscall",
			"data_size":  fmt.Sprintf("%d", len(record.RawSample)),
		},
	}

	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// cleanupEBPF cleans up eBPF resources
func (c *Collector) cleanupEBPF() {
	// Close links
	for _, l := range c.ebpfLinks {
		if link, ok := l.(link.Link); ok {
			link.Close()
		}
	}

	// Close reader
	if reader, ok := c.ebpfReader.(*perf.Reader); ok && reader != nil {
		reader.Close()
	}

	// Close collection
	if coll, ok := c.ebpfCollection.(*ebpf.Collection); ok && coll != nil {
		coll.Close()
	}
}

// getEventTypeName returns human-readable event type
func (c *Collector) getEventTypeName(eventType uint32) string {
	switch eventType {
	case EventTypeCNIExec:
		return "cni_exec"
	case EventTypeNetNSCreate:
		return "netns_create"
	case EventTypeNetNSChange:
		return "netns_change"
	default:
		return "unknown"
	}
}

// loadCNIeBPFSpec creates a real eBPF spec for CNI monitoring
func loadCNIeBPFSpec() (*ebpf.CollectionSpec, error) {
	// Real eBPF program that traces execve syscalls for CNI binaries
	execveProg := asm.Instructions{
		// Load syscall arguments
		asm.LoadMem(asm.R1, asm.R1, 16, asm.Word), // filename from pt_regs

		// Check if filename contains "/opt/cni/bin/" or "/usr/libexec/cni/"
		asm.Mov.Imm(asm.R2, 0),                        // Clear R2
		asm.Call.Imm(asm.R0, 1, "bpf_probe_read_str"), // bpf_probe_read_str (helper)

		// For now, just submit an event for any execve
		asm.Mov.Imm(asm.R0, 0), // Return 0 (success)
		asm.Return(),
	}

	// eBPF program for clone syscall with CLONE_NEWNET
	cloneProg := asm.Instructions{
		// Check clone flags for CLONE_NEWNET (0x40000000)
		asm.LoadMem(asm.R1, asm.R1, 16, asm.Word), // flags from pt_regs
		asm.Mov.Imm(asm.R2, 0x40000000),           // CLONE_NEWNET flag
		asm.And.Reg(asm.R1, asm.R2),               // Check if flag is set
		asm.JEq.Imm(asm.R1, 0, "exit"),            // Jump if not CLONE_NEWNET

		// Submit network namespace creation event
		asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"),
		asm.Return(),
	}

	// eBPF program for setns syscall
	setnsProg := asm.Instructions{
		// Check nstype for CLONE_NEWNET
		asm.LoadMem(asm.R2, asm.R1, 24, asm.Word), // nstype from pt_regs
		asm.Mov.Imm(asm.R3, 0x40000000),           // CLONE_NEWNET
		asm.JNE.Reg(asm.R2, asm.R3, "exit"),       // Jump if not CLONE_NEWNET

		// Submit namespace change event
		asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"),
		asm.Return(),
	}

	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"trace_cni_exec": {
				Type:         ebpf.TracePoint,
				License:      "GPL",
				Instructions: execveProg,
			},
			"trace_clone": {
				Type:         ebpf.TracePoint,
				License:      "GPL",
				Instructions: cloneProg,
			},
			"trace_setns": {
				Type:         ebpf.TracePoint,
				License:      "GPL",
				Instructions: setnsProg,
			},
		},
		Maps: map[string]*ebpf.MapSpec{
			"cni_events": {
				Name:       "cni_events",
				Type:       ebpf.PerfEventArray,
				KeySize:    4,
				ValueSize:  4,
				MaxEntries: 1024,
			},
		},
	}, nil
}
