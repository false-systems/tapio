//go:build linux
// +build linux

package etcd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 etcdMonitor bpf/etcd_monitor.c -- -I./bpf -I./bpf/headers

// EBPFCollector implements eBPF-based etcd monitoring
type EBPFCollector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// eBPF objects
	objs   etcdMonitorObjects
	tcLink link.Link
	reader *ringbuf.Reader
	links  []link.Link

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	healthy bool
}

// NewEBPFCollector creates a new eBPF-based etcd collector
func NewEBPFCollector(config collectors.CollectorConfig) (*EBPFCollector, error) {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %w", err)
	}

	return &EBPFCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
		links:   make([]link.Link, 0),
	}, nil
}

// Name returns the collector name
func (c *EBPFCollector) Name() string {
	return "etcd-ebpf"
}

// Start begins eBPF collection
func (c *EBPFCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF programs
	if err := loadEtcdMonitorObjects(&c.objs, nil); err != nil {
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	// Create ring buffer reader
	var err error
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		c.objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	// Attach programs
	if err := c.attachPrograms(); err != nil {
		c.cleanup()
		return fmt.Errorf("attaching programs: %w", err)
	}

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	return nil
}

// Stop gracefully shuts down
func (c *EBPFCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()
	c.cleanup()
	close(c.events)

	return nil
}

// Events returns the event channel
func (c *EBPFCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *EBPFCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// attachPrograms attaches all eBPF programs
func (c *EBPFCollector) attachPrograms() error {
	// Attach TC program for network monitoring
	// Note: In production, this would attach to the actual network interface
	// For now, we'll attach tracepoints

	// Attach syscall tracepoints
	writeTP, err := link.Tracepoint("syscalls", "sys_enter_write", c.objs.TraceEtcdWrite, nil)
	if err != nil {
		return fmt.Errorf("attaching write tracepoint: %w", err)
	}
	c.links = append(c.links, writeTP)

	fsyncTP, err := link.Tracepoint("syscalls", "sys_enter_fsync", c.objs.TraceEtcdFsync, nil)
	if err != nil {
		return fmt.Errorf("attaching fsync tracepoint: %w", err)
	}
	c.links = append(c.links, fsyncTP)

	return nil
}

// cleanup releases all resources
func (c *EBPFCollector) cleanup() {
	if c.reader != nil {
		c.reader.Close()
	}

	for _, l := range c.links {
		l.Close()
	}

	if c.tcLink != nil {
		c.tcLink.Close()
	}

	c.objs.Close()
}

// processEvents reads events from ring buffer
func (c *EBPFCollector) processEvents() {
	defer c.wg.Done()

	for {
		record, err := c.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			// Log error but continue
			continue
		}

		// Parse event
		var event etcdEvent
		if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.LittleEndian, &event); err != nil {
			continue
		}

		// Convert to RawEvent
		rawEvent := c.convertToRawEvent(&event)

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Buffer full, drop event
		}
	}
}

// convertToRawEvent converts eBPF event to RawEvent
func (c *EBPFCollector) convertToRawEvent(event *etcdEvent) collectors.RawEvent {
	// Create event data
	data := map[string]interface{}{
		"timestamp":  event.Timestamp,
		"pid":        event.Pid,
		"tid":        event.Tid,
		"type":       c.getEventType(event.Type),
		"operation":  c.getOperation(event.Operation),
		"latency_ms": event.LatencyMs,
		"key_size":   event.KeySize,
		"value_size": event.ValueSize,
	}

	// Add network info if available
	if event.Type == 1 { // EVENT_NETWORK
		data["src_ip"] = intToIP(event.SrcIp)
		data["dst_ip"] = intToIP(event.DstIp)
		data["src_port"] = event.SrcPort
		data["dst_port"] = event.DstPort
	}

	// Extract key if present
	if event.KeySize > 0 {
		key := string(bytes.TrimRight(event.Key[:], "\x00"))
		if len(key) > 0 {
			data["key"] = key
		}
	}

	// Marshal to JSON
	jsonData, _ := json.Marshal(data)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      fmt.Sprintf("etcd.%s", c.getOperation(event.Operation)),
		Data:      jsonData,
		Metadata: map[string]string{
			"collector":     "etcd-ebpf",
			"capture_point": c.getEventType(event.Type),
		},
	}
}

// getEventType converts event type to string
func (c *EBPFCollector) getEventType(t uint8) string {
	switch t {
	case 1:
		return "network"
	case 2:
		return "syscall"
	case 3:
		return "file_op"
	default:
		return "unknown"
	}
}

// getOperation converts operation code to string
func (c *EBPFCollector) getOperation(op uint8) string {
	switch op {
	case 1:
		return "get"
	case 2:
		return "put"
	case 3:
		return "delete"
	case 4:
		return "watch"
	case 5:
		return "lease"
	case 6:
		return "txn"
	case 7:
		return "wal_sync"
	default:
		return "unknown"
	}
}

// intToIP converts uint32 to IP string
func intToIP(ip uint32) string {
	return net.IPv4(byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)).String()
}

// etcdEvent matches the BPF event structure
type etcdEvent struct {
	Timestamp uint64
	Pid       uint32
	Tid       uint32
	Type      uint8
	Operation uint8
	LatencyMs uint16
	SrcIp     uint32
	DstIp     uint32
	SrcPort   uint16
	DstPort   uint16
	KeySize   uint32
	ValueSize uint32
	Key       [64]byte
}
