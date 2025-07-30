//go:build linux

package cni

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// cniEvent matches the C structure
type cniEvent struct {
	PID       uint32
	TGID      uint32
	Timestamp uint64
	Type      uint8
	_         [7]byte // padding
	Comm      [16]byte
	Data      [64]byte
}

// initEBPF initializes eBPF programs for CNI syscall tracing
func (c *Collector) initEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Parse eBPF program
	spec, err := ebpf.LoadCollectionSpecFromReader(strings.NewReader(cniTraceProgramSource))
	if err != nil {
		// If eBPF not supported, continue without it
		c.ebpfEnabled = false
		return nil
	}

	// Load collection
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		c.ebpfEnabled = false
		return nil
	}
	c.ebpfColl = coll

	// Get events map
	eventsMap, ok := coll.Maps["events"]
	if !ok {
		c.ebpfColl.Close()
		return errors.New("events map not found")
	}

	// Create perf reader
	reader, err := perf.NewReader(eventsMap, 4096)
	if err != nil {
		c.ebpfColl.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	c.ebpfReader = reader

	// Attach programs
	if err := c.attachEBPFPrograms(coll); err != nil {
		reader.Close()
		coll.Close()
		return err
	}

	c.ebpfEnabled = true
	return nil
}

// attachEBPFPrograms attaches the eBPF programs to their hooks
func (c *Collector) attachEBPFPrograms(coll *ebpf.Collection) error {
	// Attach execve tracer
	if prog, ok := coll.Programs["trace_cni_exec"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_execve", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach execve tracer: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	// Attach clone tracer
	if prog, ok := coll.Programs["trace_netns_create"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_clone", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach clone tracer: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	// Attach setns tracer
	if prog, ok := coll.Programs["trace_netns_enter"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_setns", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach setns tracer: %w", err)
		}
		c.ebpfLinks = append(c.ebpfLinks, l)
	}

	return nil
}

// readEBPFEvents reads events from eBPF programs
func (c *Collector) readEBPFEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.ebpfReader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				continue
			}

			// Parse event
			if len(record.RawSample) < int(unsafe.Sizeof(cniEvent{})) {
				continue
			}

			var event cniEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				continue
			}

			// Convert to JSON
			eventType := c.getEventTypeName(event.Type)
			comm := string(bytes.TrimRight(event.Comm[:], "\x00"))
			data := string(bytes.TrimRight(event.Data[:], "\x00"))

			// Only process CNI-related events
			if !c.isCNIRelated(eventType, data, comm) {
				continue
			}

			jsonData, _ := json.Marshal(map[string]interface{}{
				"type":      "syscall",
				"syscall":   eventType,
				"pid":       event.PID,
				"process":   comm,
				"details":   data,
				"timestamp": event.Timestamp,
			})

			rawEvent := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "cni",
				Data:      jsonData,
				Metadata: map[string]string{
					"source":     "ebpf",
					"cni_plugin": c.detectedCNI,
					"syscall":    eventType,
				},
			}

			select {
			case c.events <- rawEvent:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop
			}
		}
	}
}

// getEventTypeName converts event type to readable name
func (c *Collector) getEventTypeName(eventType uint8) string {
	switch eventType {
	case 0:
		return "exec"
	case 1:
		return "netns"
	case 2:
		return "veth"
	case 3:
		return "route"
	default:
		return "unknown"
	}
}

// isCNIRelated checks if the event is CNI-related
func (c *Collector) isCNIRelated(eventType, data, comm string) bool {
	// Check if exec contains CNI path
	if eventType == "exec" && strings.Contains(data, "cni") {
		return true
	}

	// Check if process name suggests CNI operation
	cniProcesses := []string{"calico", "cilium", "flannel", "weave", "cni", "bridge", "host-device"}
	lowerComm := strings.ToLower(comm)
	for _, proc := range cniProcesses {
		if strings.Contains(lowerComm, proc) {
			return true
		}
	}

	// Network namespace operations from CNI-related processes
	if eventType == "netns" && c.isLikelyCNIProcess(comm) {
		return true
	}

	return false
}

// isLikelyCNIProcess checks if process is likely CNI-related
func (c *Collector) isLikelyCNIProcess(comm string) bool {
	// Common CNI-related processes
	patterns := []string{
		"kube", "docker", "containerd", "crio",
		"calico", "cilium", "flannel", "weave",
	}

	lower := strings.ToLower(comm)
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// cleanupEBPF cleans up eBPF resources
func (c *Collector) cleanupEBPF() {
	if c.ebpfReader != nil {
		c.ebpfReader.Close()
	}

	for _, l := range c.ebpfLinks {
		l.Close()
	}

	if c.ebpfColl != nil {
		c.ebpfColl.Close()
	}
}