package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
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
	IPVersion uint8     // 4 for IPv4, 6 for IPv6
	Protocol  uint8     // IPPROTO_TCP or IPPROTO_UDP
	State     uint8     // Connection state
	Direction uint8     // 0=outgoing, 1=incoming
	SPort     uint16    // Source port
	DPort     uint16    // Destination port
	SAddrV4   uint32    // Source IP (IPv4)
	DAddrV4   uint32    // Destination IP (IPv4)
	SAddrV6   [4]uint32 // Source IP (IPv6)
	DAddrV6   [4]uint32 // Destination IP (IPv6)
}

// Collector implements network monitoring
type Collector struct {
	logger     *zap.Logger
	events     chan domain.RawEvent
	ctx        context.Context
	cancel     context.CancelFunc
	reader     *ringbuf.Reader
	links      []link.Link
	safeParser *collectors.SafeParser
	stopped    bool
	mu         sync.RWMutex
}

// NewNetworkCollector creates a new network collector
func NewNetworkCollector(logger *zap.Logger) *Collector {
	return &Collector{
		logger:     logger,
		events:     make(chan domain.RawEvent, 3000),
		safeParser: collectors.NewSafeParser(),
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopped {
		return nil
	}

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

	c.stopped = true
	c.logger.Info("Network collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan domain.RawEvent {
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

		// Convert to RawEvent
		rawEvent := domain.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Source:    "network",
			Data:      record.RawSample,
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

// parseNetworkEvent parses a NetworkEvent from raw bytes with memory safety
func (c *Collector) parseNetworkEvent(rawBytes []byte) (*NetworkEvent, error) {
	// Use safe parsing with comprehensive validation
	event, err := collectors.SafeCast[NetworkEvent](c.safeParser, rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to safely parse NetworkEvent: %w", err)
	}

	// Validate network event type range (5-20)
	if err := c.safeParser.ValidateEventType(event.EventType, 5, 20); err != nil {
		return nil, fmt.Errorf("invalid network event type: %w", err)
	}

	// Validate string fields for corruption detection
	if err := c.safeParser.ValidateStringField(event.Comm[:], "comm"); err != nil {
		return nil, fmt.Errorf("invalid comm field: %w", err)
	}

	if err := c.safeParser.ValidateStringField(event.PodUID[:], "pod_uid"); err != nil {
		return nil, fmt.Errorf("invalid pod_uid field: %w", err)
	}

	// Validate network-specific data
	if err := c.safeParser.ValidateNetworkData(event.NetInfo.Protocol, event.NetInfo.Direction); err != nil {
		return nil, fmt.Errorf("invalid network data: %w", err)
	}

	// Additional network-specific validation
	if event.PID == 0 && event.EventType != 19 && event.EventType != 20 { // PID 0 only valid for DNS events
		return nil, fmt.Errorf("invalid PID 0 for network event type %d", event.EventType)
	}

	// Validate port ranges (0 is valid for some cases like ICMP)
	if event.NetInfo.SPort > 65535 || event.NetInfo.DPort > 65535 {
		return nil, fmt.Errorf("invalid port values: src=%d, dst=%d", event.NetInfo.SPort, event.NetInfo.DPort)
	}

	// Validate DataLen field
	if event.DataLen > uint32(len(event.Data)) {
		return nil, fmt.Errorf("invalid data length: %d exceeds buffer size %d", event.DataLen, len(event.Data))
	}

	return event, nil
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

// nullTerminatedString safely converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	return c.safeParser.StringFromByteArray(b)
}
