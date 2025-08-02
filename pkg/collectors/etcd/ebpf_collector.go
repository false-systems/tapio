//go:build linux
// +build linux

package etcd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang etcdMonitor ./bpf/etcd_monitor.c -- -I./bpf/headers -D__TARGET_ARCH_x86

// etcdEvent represents a raw event from eBPF
type etcdEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint8
	_         [3]byte // padding
	SrcIP     uint32
	DstIP     uint32
	SrcPort   uint16
	DstPort   uint16
	DataLen   uint32
	Data      [256]byte
}

// eBPF components
type ebpfState struct {
	objs   *etcdMonitorObjects
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
	objs := &etcdMonitorObjects{}
	if err := loadEtcdMonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create eBPF state
	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to write syscalls (etcd WAL writes)
	l1, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceSysEnterWrite, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching write tracepoint: %w", err)
	}
	state.links = append(state.links, l1)

	// Attach to fsync syscalls (etcd WAL syncs)
	l2, err := link.Tracepoint("syscalls", "sys_enter_fsync", objs.TraceSysEnterFsync, nil)
	if err != nil {
		state.cleanup()
		return fmt.Errorf("attaching fsync tracepoint: %w", err)
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

		// Parse the raw event
		var event etcdEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			continue
		}

		// Create raw event with NO business logic - just raw data
		eventData := map[string]interface{}{
			"timestamp": event.Timestamp,
			"pid":       event.PID,
			"tid":       event.TID,
			"type":      event.EventType, // Raw type, no interpretation
			"data_len":  event.DataLen,
		}

		// Add network info if present
		if event.SrcIP != 0 || event.DstIP != 0 {
			eventData["src_ip"] = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.SrcIP), byte(event.SrcIP>>8),
				byte(event.SrcIP>>16), byte(event.SrcIP>>24))
			eventData["dst_ip"] = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.DstIP), byte(event.DstIP>>8),
				byte(event.DstIP>>16), byte(event.DstIP>>24))
			eventData["src_port"] = event.SrcPort
			eventData["dst_port"] = event.DstPort
		}

		// Include raw data if present
		if event.DataLen > 0 && event.DataLen <= 256 {
			eventData["raw_data"] = event.Data[:event.DataLen]
		}

		rawEvent := c.createEvent("syscall", eventData)

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Buffer full, drop event
		}
	}
}
