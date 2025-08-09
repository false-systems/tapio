package network

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// NetworkEvent represents a network event from eBPF
type NetworkEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	DataLen   uint32
	Comm      [16]byte
	CgroupID  uint64
	PodUID    [36]byte
	NetInfo   NetworkInfo
	Data      [132]byte
}

// NetworkInfo represents network connection information
type NetworkInfo struct {
	SAddr     uint32 // Source IP (IPv4)
	DAddr     uint32 // Destination IP (IPv4)
	SPort     uint16 // Source port
	DPort     uint16 // Destination port
	Protocol  uint8  // IPPROTO_TCP or IPPROTO_UDP
	State     uint8  // Connection state
	Direction uint8  // 0=outgoing, 1=incoming
	_         uint8  // Padding
}

// Collector implements network monitoring
type Collector struct {
	logger *zap.Logger
	events chan collectors.RawEvent
	ctx    context.Context
	cancel context.CancelFunc
	reader *ringbuf.Reader
	links  []link.Link
}

// NewNetworkCollector creates a new network collector
func NewNetworkCollector(logger *zap.Logger) *Collector {
	return &Collector{
		logger: logger,
		events: make(chan collectors.RawEvent, 3000),
	}
}

// Start starts network monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load network eBPF programs
	// Note: Network monitor eBPF objects need to be generated first
	c.logger.Info("loading network eBPF programs")

	// For now, skip eBPF loading until compilation issues are resolved
	// The programs will be loaded once the C compilation is fixed

	c.logger.Info("Network collector started")

	// Start event processing
	go c.processEvents()

	return nil
}

// Stop stops network monitoring
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	for _, l := range c.links {
		l.Close()
	}

	if c.reader != nil {
		c.reader.Close()
	}

	if c.events != nil {
		close(c.events)
	}

	c.logger.Info("Network collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// processEvents processes network events
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

		// Parse network event
		event, err := c.parseNetworkEvent(record.RawSample)
		if err != nil {
			c.logger.Error("Failed to parse network event", zap.Error(err))
			continue
		}

		// Convert to RawEvent with network-specific metadata
		metadata := map[string]string{
			"collector": "kernel-network",
			"pid":       fmt.Sprintf("%d", event.PID),
			"tid":       fmt.Sprintf("%d", event.TID),
			"comm":      c.nullTerminatedString(event.Comm[:]),
			"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
			"pod_uid":   c.nullTerminatedString(event.PodUID[:]),
			"src_ip":    c.ipToString(event.NetInfo.SAddr),
			"dst_ip":    c.ipToString(event.NetInfo.DAddr),
			"src_port":  fmt.Sprintf("%d", event.NetInfo.SPort),
			"dst_port":  fmt.Sprintf("%d", event.NetInfo.DPort),
			"protocol":  c.protocolToString(event.NetInfo.Protocol),
			"direction": c.directionToString(event.NetInfo.Direction),
			"state":     fmt.Sprintf("%d", event.NetInfo.State),
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

// parseNetworkEvent parses a NetworkEvent from raw bytes
func (c *Collector) parseNetworkEvent(rawBytes []byte) (*NetworkEvent, error) {
	expectedSize := int(unsafe.Sizeof(NetworkEvent{}))

	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	event := *(*NetworkEvent)(unsafe.Pointer(&rawBytes[0]))

	if event.EventType < 5 || event.EventType > 20 {
		return nil, fmt.Errorf("invalid network event type: %d", event.EventType)
	}

	return &event, nil
}

// eventTypeToString converts network event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 5:
		return "network_conn"
	case 6:
		return "network_accept"
	case 7:
		return "network_close"
	case 19:
		return "dns_request"
	case 20:
		return "dns_response"
	default:
		return "network_unknown"
	}
}

// ipToString converts uint32 IP to string
func (c *Collector) ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xff,
		(ip>>16)&0xff,
		(ip>>8)&0xff,
		ip&0xff)
}

// protocolToString converts protocol number to string
func (c *Collector) protocolToString(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("%d", proto)
	}
}

// directionToString converts direction flag to string
func (c *Collector) directionToString(dir uint8) string {
	if dir == 0 {
		return "outgoing"
	}
	return "incoming"
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
