//go:build linux
// +build linux

package cni

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 cniMonitor ./bpf_src/cni_monitor.c -- -I../bpf_common

// cniEvent represents a network event from eBPF
type cniEvent struct {
	Timestamp uint64
	PID       uint32
	Netns     uint32
	EventType uint32
	Comm      [16]byte
	Data      [64]byte
}

// eBPF components
type ebpfState struct {
	objs   *cniMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring
func (c *Collector) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &cniMonitorObjects{}
	if err := loadCniMonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create eBPF state
	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to network namespace operations
	l1, err := link.Tracepoint("syscalls", "sys_enter_setns", objs.TraceSysEnterSetns, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching setns tracepoint: %w", err)
	}
	state.links = append(state.links, l1)

	// Attach to unshare (new network namespace)
	l2, err := link.Tracepoint("syscalls", "sys_enter_unshare", objs.TraceSysEnterUnshare, nil)
	if err != nil {
		state.cleanup()
		return fmt.Errorf("attaching unshare tracepoint: %w", err)
	}
	state.links = append(state.links, l2)

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		state.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	// Store state and start reading events
	c.ebpfState = state
	go c.readEBPFEvents()

	return nil
}

// stopEBPF cleans up eBPF resources
func (c *Collector) stopEBPF() {
	if state, ok := c.ebpfState.(*ebpfState); ok && state != nil {
		state.cleanup()
		c.ebpfState = nil
	}
}

// cleanup releases all eBPF resources
func (s *ebpfState) cleanup() {
	if s.reader != nil {
		s.reader.Close()
	}
	for _, l := range s.links {
		l.Close()
	}
	if s.objs != nil {
		s.objs.Close()
	}
}

// readEBPFEvents reads events from eBPF ring buffer
func (c *Collector) readEBPFEvents() {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return
	}

	for {
		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			// Log error and continue
			continue
		}

		// Parse the event
		var event cniEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			continue
		}

		// Create raw event
		eventData := map[string]interface{}{
			"timestamp": event.Timestamp,
			"pid":       event.PID,
			"netns":     event.Netns,
			"type":      eventTypeToString(event.EventType),
			"comm":      nullTerminatedString(event.Comm[:]),
			"data":      nullTerminatedString(event.Data[:]),
		}

		rawEvent := c.createEvent("network_namespace", eventData)

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Buffer full, drop event
		}
	}
}

// Helper to convert event type to string
func eventTypeToString(t uint32) string {
	switch t {
	case 1:
		return "netns_enter"
	case 2:
		return "netns_create"
	case 3:
		return "netns_exit"
	default:
		return fmt.Sprintf("unknown_%d", t)
	}
}

// Helper to convert null-terminated byte array to string
func nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
