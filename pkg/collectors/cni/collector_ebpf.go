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

	// Create eBPF spec
	spec := &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"trace_cni_exec": {
				Type:    ebpf.TracePoint,
				License: "GPL",
				Instructions: asm.Instructions{
					// Simplified - would be loaded from compiled BPF
					asm.Mov.Imm(asm.R0, 0),
					asm.Return(),
				},
			},
			"trace_clone": {
				Type:    ebpf.TracePoint,
				License: "GPL",
				Instructions: asm.Instructions{
					asm.Mov.Imm(asm.R0, 0),
					asm.Return(),
				},
			},
			"trace_setns": {
				Type:    ebpf.TracePoint,
				License: "GPL",
				Instructions: asm.Instructions{
					asm.Mov.Imm(asm.R0, 0),
					asm.Return(),
				},
			},
		},
		Maps: map[string]*ebpf.MapSpec{
			"cni_events": {
				Type:       ebpf.PerfEventArray,
				MaxEntries: uint32(c.config.BufferSize),
			},
		},
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
				// Log error but continue
				continue
			}

			// Parse event
			var event cniEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				continue
			}

			// Create raw event
			data, _ := json.Marshal(map[string]interface{}{
				"event_type": c.getEventTypeName(event.EventType),
				"pid":        event.Pid,
				"timestamp":  event.Timestamp,
				"data":       string(bytes.TrimRight(event.Data[:], "\x00")),
			})

			rawEvent := collectors.RawEvent{
				Timestamp: time.Unix(0, int64(event.Timestamp)),
				Type:      "cni",
				Data:      data,
				Metadata: map[string]string{
					"source":     "ebpf",
					"cni_plugin": c.detectedCNI,
					"event_type": c.getEventTypeName(event.EventType),
				},
			}

			select {
			case c.events <- rawEvent:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full
			}
		}
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
