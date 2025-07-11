//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 networkmonitor ../../ebpf/network_monitor.c -- -I../../ebpf
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 packetanalyzer ../../ebpf/packet_analyzer.c -- -I../../ebpf
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 dnsmonitor ../../ebpf/dns_monitor.c -- -I../../ebpf
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 protocolanalyzer ../../ebpf/protocol_analyzer.c -- -I../../ebpf

// EnhancedCollector manages multiple eBPF programs for comprehensive system monitoring
type EnhancedCollector struct {
	// Original memory monitoring
	memoryObjs   oomdetectorObjects
	memoryReader *ringbuf.Reader
	memoryEvents chan *MemoryEvent

	// Network monitoring  
	networkObjs   networkmonitorObjects
	networkReader *ringbuf.Reader
	networkEvents chan *NetworkEvent

	// Packet analysis
	packetObjs   packetanalyzerObjects
	packetReader *ringbuf.Reader
	packetEvents chan *PacketEvent

	// DNS monitoring
	dnsObjs   dnsmonitorObjects
	dnsReader *ringbuf.Reader
	dnsEvents chan *DNSEvent

	// Protocol analysis
	protocolObjs   protocolanalyzerObjects
	protocolReader *ringbuf.Reader
	protocolEvents chan *ProtocolEvent

	// Unified event stream
	unifiedEvents chan SystemEvent

	// Ring buffer manager
	ringBufferManager *RingBufferManager

	// Error handler
	errorHandler *ErrorHandler

	// State tracking
	processStats map[uint32]*ProcessMemoryStats
	networkStats map[string]*NetworkConnectionStats
	dnsStats     map[string]*DNSQueryStats
	protocolStats map[string]*ProtocolStats
	statsMutex   sync.RWMutex

	// Lifecycle management
	links  []link.Link
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	started atomic.Bool

	// Performance monitoring
	eventCount    atomic.Uint64
	droppedEvents atomic.Uint64
	parseErrors   atomic.Uint64
}

// NetworkEvent represents a network-related event
type NetworkEvent struct {
	Timestamp   time.Time
	PID         uint32
	TGID        uint32
	UID         uint32
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	EventType   uint8
	LatencyUS   uint32
	Bytes       uint32
	ErrorCode   uint16
	Command     string
	ContainerID string
}

// PacketEvent represents a packet-level event
type PacketEvent struct {
	Timestamp   time.Time
	PID         uint32
	TGID        uint32
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	EventType   uint8
	LatencyUS   uint32
	PacketSize  uint32
	SequenceNum uint32
	AckNum      uint32
	WindowSize  uint16
	TCPFlags    uint8
	Command     string
	Interface   string
}

// DNSEvent represents a DNS-related event
type DNSEvent struct {
	Timestamp    time.Time
	PID          uint32
	TGID         uint32
	UID          uint32
	SrcIP        uint32
	DstIP        uint32
	QueryID      uint16
	QueryType    uint16
	QueryClass   uint16
	EventType    uint8
	ResponseCode uint8
	LatencyMS    uint32
	AnswerCount  uint32
	Domain       string
	Command      string
	ContainerID  string
}

// ProtocolEvent represents a protocol-level event
type ProtocolEvent struct {
	Timestamp    time.Time
	PID          uint32
	TGID         uint32
	UID          uint32
	SrcIP        uint32
	DstIP        uint32
	SrcPort      uint16
	DstPort      uint16
	ProtocolType uint8
	EventType    uint8
	StatusCode   uint16
	LatencyUS    uint32
	PayloadSize  uint32
	RequestID    uint32
	Method       string
	Path         string
	UserAgent    string
	ErrorMsg     string
	Command      string
	ContainerID  string
}

// SystemEvent is a unified event structure
type SystemEvent struct {
	Type      string
	Timestamp time.Time
	PID       uint32
	Data      interface{}
}

// Statistics structures
type NetworkConnectionStats struct {
	StartTime       time.Time
	LastSeen        time.Time
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
	Retransmits     uint64
	RTTMin          uint32
	RTTMax          uint32
	RTTAvg          uint32
	State           uint8
	Failed          bool
}

type DNSQueryStats struct {
	QueryCount    uint64
	ResponseCount uint64
	TimeoutCount  uint64
	ErrorCount    uint64
	NXDomainCount uint64
	AvgLatencyMS  uint32
}

type ProtocolStats struct {
	RequestCount  uint64
	ResponseCount uint64
	ErrorCount    uint64
	SlowCount     uint64
	AvgLatencyUS  uint32
	StatusCodes   map[uint16]uint64
}

// NewEnhancedCollector creates a new enhanced eBPF collector
func NewEnhancedCollector() (*EnhancedCollector, error) {
	ctx, cancel := context.WithCancel(context.Background())

	collector := &EnhancedCollector{
		memoryEvents:   make(chan *MemoryEvent, 10000),
		networkEvents:  make(chan *NetworkEvent, 10000),
		packetEvents:   make(chan *PacketEvent, 10000),
		dnsEvents:      make(chan *DNSEvent, 10000),
		protocolEvents: make(chan *ProtocolEvent, 10000),
		unifiedEvents:  make(chan SystemEvent, 50000),
		processStats:   make(map[uint32]*ProcessMemoryStats),
		networkStats:   make(map[string]*NetworkConnectionStats),
		dnsStats:       make(map[string]*DNSQueryStats),
		protocolStats:  make(map[string]*ProtocolStats),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Initialize memory monitoring
	if err := collector.initMemoryMonitoring(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to initialize memory monitoring: %w", err)
	}

	// Initialize network monitoring
	if err := collector.initNetworkMonitoring(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to initialize network monitoring: %w", err)
	}

	// Initialize packet analysis
	if err := collector.initPacketAnalysis(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to initialize packet analysis: %w", err)
	}

	// Initialize DNS monitoring
	if err := collector.initDNSMonitoring(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to initialize DNS monitoring: %w", err)
	}

	// Initialize protocol analysis
	if err := collector.initProtocolAnalysis(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to initialize protocol analysis: %w", err)
	}

	// Start event processing
	go collector.processMemoryEvents()
	go collector.processNetworkEvents()
	go collector.processPacketEvents()
	go collector.processDNSEvents()
	go collector.processProtocolEvents()
	go collector.unifyEvents()
	go collector.cleanupStats()

	return collector, nil
}

// initMemoryMonitoring initializes the memory monitoring eBPF program
func (c *EnhancedCollector) initMemoryMonitoring() error {
	spec, err := loadOomdetector()
	if err != nil {
		return fmt.Errorf("failed to load memory monitoring spec: %w", err)
	}

	if err := spec.LoadAndAssign(&c.memoryObjs, nil); err != nil {
		return fmt.Errorf("failed to load memory monitoring objects: %w", err)
	}

	reader, err := ringbuf.NewReader(c.memoryObjs.Events)
	if err != nil {
		c.memoryObjs.Close()
		return fmt.Errorf("failed to create memory ring buffer reader: %w", err)
	}
	c.memoryReader = reader

	return c.attachMemoryPrograms()
}

// initNetworkMonitoring initializes the network monitoring eBPF program
func (c *EnhancedCollector) initNetworkMonitoring() error {
	spec, err := loadNetworkmonitor()
	if err != nil {
		return fmt.Errorf("failed to load network monitoring spec: %w", err)
	}

	if err := spec.LoadAndAssign(&c.networkObjs, nil); err != nil {
		return fmt.Errorf("failed to load network monitoring objects: %w", err)
	}

	reader, err := ringbuf.NewReader(c.networkObjs.Events)
	if err != nil {
		c.networkObjs.Close()
		return fmt.Errorf("failed to create network ring buffer reader: %w", err)
	}
	c.networkReader = reader

	return c.attachNetworkPrograms()
}

// initPacketAnalysis initializes the packet analysis eBPF program
func (c *EnhancedCollector) initPacketAnalysis() error {
	spec, err := loadPacketanalyzer()
	if err != nil {
		return fmt.Errorf("failed to load packet analyzer spec: %w", err)
	}

	if err := spec.LoadAndAssign(&c.packetObjs, nil); err != nil {
		return fmt.Errorf("failed to load packet analyzer objects: %w", err)
	}

	reader, err := ringbuf.NewReader(c.packetObjs.Events)
	if err != nil {
		c.packetObjs.Close()
		return fmt.Errorf("failed to create packet ring buffer reader: %w", err)
	}
	c.packetReader = reader

	return c.attachPacketPrograms()
}

// initDNSMonitoring initializes the DNS monitoring eBPF program
func (c *EnhancedCollector) initDNSMonitoring() error {
	spec, err := loadDnsmonitor()
	if err != nil {
		return fmt.Errorf("failed to load DNS monitoring spec: %w", err)
	}

	if err := spec.LoadAndAssign(&c.dnsObjs, nil); err != nil {
		return fmt.Errorf("failed to load DNS monitoring objects: %w", err)
	}

	reader, err := ringbuf.NewReader(c.dnsObjs.Events)
	if err != nil {
		c.dnsObjs.Close()
		return fmt.Errorf("failed to create DNS ring buffer reader: %w", err)
	}
	c.dnsReader = reader

	return c.attachDNSPrograms()
}

// initProtocolAnalysis initializes the protocol analysis eBPF program
func (c *EnhancedCollector) initProtocolAnalysis() error {
	spec, err := loadProtocolanalyzer()
	if err != nil {
		return fmt.Errorf("failed to load protocol analyzer spec: %w", err)
	}

	if err := spec.LoadAndAssign(&c.protocolObjs, nil); err != nil {
		return fmt.Errorf("failed to load protocol analyzer objects: %w", err)
	}

	reader, err := ringbuf.NewReader(c.protocolObjs.Events)
	if err != nil {
		c.protocolObjs.Close()
		return fmt.Errorf("failed to create protocol ring buffer reader: %w", err)
	}
	c.protocolReader = reader

	return c.attachProtocolPrograms()
}

// Attach program methods (simplified for brevity)
func (c *EnhancedCollector) attachMemoryPrograms() error {
	// Similar to original collector
	l1, err := link.Tracepoint("kmem", "mm_page_alloc", c.memoryObjs.TrackMemoryAlloc, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l1)

	l2, err := link.Tracepoint("kmem", "mm_page_free", c.memoryObjs.TrackMemoryFree, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l2)

	l3, err := link.Tracepoint("oom", "oom_score_adj_update", c.memoryObjs.TrackOomKill, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l3)

	l4, err := link.Tracepoint("sched", "sched_process_exit", c.memoryObjs.TrackProcessExit, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l4)

	return nil
}

func (c *EnhancedCollector) attachNetworkPrograms() error {
	// Attach network monitoring kprobes
	l1, err := link.Kprobe("tcp_v4_connect", c.networkObjs.TraceConnect, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l1)

	l2, err := link.Kretprobe("tcp_v4_connect", c.networkObjs.TraceConnect, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l2)

	l3, err := link.Kprobe("tcp_close", c.networkObjs.TraceClose, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l3)

	l4, err := link.Kprobe("tcp_retransmit_skb", c.networkObjs.TraceRetransmit, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l4)

	l5, err := link.Tracepoint("skb", "kfree_skb", c.networkObjs.TracePacketDrop, nil)
	if err != nil {
		return err
	}
	c.links = append(c.links, l5)

	return nil
}

func (c *EnhancedCollector) attachPacketPrograms() error {
	// Attach packet analysis programs to TC
	// Note: TC attachment requires additional setup and is simplified here
	return nil
}

func (c *EnhancedCollector) attachDNSPrograms() error {
	// Attach DNS monitoring programs
	return nil
}

func (c *EnhancedCollector) attachProtocolPrograms() error {
	// Attach protocol analysis programs
	return nil
}

// Event processing methods
func (c *EnhancedCollector) processMemoryEvents() {
	defer close(c.memoryEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.memoryReader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.incrementDroppedEvents()
				continue
			}

			event, err := parseRawMemoryEvent(record.RawSample)
			if err != nil {
				c.incrementDroppedEvents()
				continue
			}

			c.incrementEventCount()
			select {
			case c.memoryEvents <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *EnhancedCollector) processNetworkEvents() {
	defer close(c.networkEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.networkReader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.incrementDroppedEvents()
				continue
			}

			event, err := parseRawNetworkEvent(record.RawSample)
			if err != nil {
				c.incrementDroppedEvents()
				continue
			}

			c.incrementEventCount()
			select {
			case c.networkEvents <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *EnhancedCollector) processPacketEvents() {
	defer close(c.packetEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.packetReader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.incrementDroppedEvents()
				continue
			}

			event, err := parseRawPacketEvent(record.RawSample)
			if err != nil {
				c.incrementDroppedEvents()
				continue
			}

			c.incrementEventCount()
			select {
			case c.packetEvents <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *EnhancedCollector) processDNSEvents() {
	defer close(c.dnsEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.dnsReader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.incrementDroppedEvents()
				continue
			}

			event, err := parseRawDNSEvent(record.RawSample)
			if err != nil {
				c.incrementDroppedEvents()
				continue
			}

			c.incrementEventCount()
			select {
			case c.dnsEvents <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *EnhancedCollector) processProtocolEvents() {
	defer close(c.protocolEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.protocolReader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.incrementDroppedEvents()
				continue
			}

			event, err := parseRawProtocolEvent(record.RawSample)
			if err != nil {
				c.incrementDroppedEvents()
				continue
			}

			c.incrementEventCount()
			select {
			case c.protocolEvents <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// unifyEvents combines all event streams into a unified stream
func (c *EnhancedCollector) unifyEvents() {
	defer close(c.unifiedEvents)

	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-c.memoryEvents:
			if event != nil {
				c.unifiedEvents <- SystemEvent{
					Type:      "memory",
					Timestamp: event.Timestamp,
					PID:       event.PID,
					Data:      event,
				}
			}
		case event := <-c.networkEvents:
			if event != nil {
				c.unifiedEvents <- SystemEvent{
					Type:      "network",
					Timestamp: event.Timestamp,
					PID:       event.PID,
					Data:      event,
				}
			}
		case event := <-c.packetEvents:
			if event != nil {
				c.unifiedEvents <- SystemEvent{
					Type:      "packet",
					Timestamp: event.Timestamp,
					PID:       event.PID,
					Data:      event,
				}
			}
		case event := <-c.dnsEvents:
			if event != nil {
				c.unifiedEvents <- SystemEvent{
					Type:      "dns",
					Timestamp: event.Timestamp,
					PID:       event.PID,
					Data:      event,
				}
			}
		case event := <-c.protocolEvents:
			if event != nil {
				c.unifiedEvents <- SystemEvent{
					Type:      "protocol",
					Timestamp: event.Timestamp,
					PID:       event.PID,
					Data:      event,
				}
			}
		}
	}
}

// cleanupStats periodically cleans up old statistics
func (c *EnhancedCollector) cleanupStats() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanupOldStats()
		}
	}
}

func (c *EnhancedCollector) cleanupOldStats() {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)

	// Clean up process stats
	for pid, stats := range c.processStats {
		if stats.LastUpdate.Before(cutoff) {
			delete(c.processStats, pid)
		}
	}

	// Clean up network stats
	for key, stats := range c.networkStats {
		if stats.LastSeen.Before(cutoff) {
			delete(c.networkStats, key)
		}
	}
}

// Performance monitoring
func (c *EnhancedCollector) incrementEventCount() {
	c.eventCount.Add(1)
}

func (c *EnhancedCollector) incrementDroppedEvents() {
	c.droppedEvents.Add(1)
}

// GetStats returns performance statistics
func (c *EnhancedCollector) GetStats() map[string]interface{} {
	c.statsMutex.RLock()
	defer c.statsMutex.RUnlock()

	return map[string]interface{}{
		"event_count":     c.eventCount.Load(),
		"dropped_events":  c.droppedEvents.Load(),
		"process_count":   len(c.processStats),
		"network_flows":   len(c.networkStats),
		"dns_queries":     len(c.dnsStats),
		"protocol_flows":  len(c.protocolStats),
	}
}

// GetUnifiedEvents returns the unified event stream
func (c *EnhancedCollector) GetUnifiedEvents() <-chan SystemEvent {
	return c.unifiedEvents
}

// Close stops the collector and cleans up resources
func (c *EnhancedCollector) Close() error {
	c.cancel()

	// Close ring buffer readers
	if c.memoryReader != nil {
		c.memoryReader.Close()
	}
	if c.networkReader != nil {
		c.networkReader.Close()
	}
	if c.packetReader != nil {
		c.packetReader.Close()
	}
	if c.dnsReader != nil {
		c.dnsReader.Close()
	}
	if c.protocolReader != nil {
		c.protocolReader.Close()
	}

	// Detach all links
	for _, l := range c.links {
		l.Close()
	}

	// Close eBPF objects
	c.memoryObjs.Close()
	c.networkObjs.Close()
	c.packetObjs.Close()
	c.dnsObjs.Close()
	c.protocolObjs.Close()

	return nil
}

// parseRawMemoryEvent is already defined in events.go, no need to redefine

func parseRawNetworkEvent(data []byte) (*NetworkEvent, error) {
	parser := NewNetworkEventParser()
	result, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}
	return result.(*NetworkEvent), nil
}

func parseRawPacketEvent(data []byte) (*PacketEvent, error) {
	parser := NewPacketEventParser()
	result, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}
	return result.(*PacketEvent), nil
}

func parseRawDNSEvent(data []byte) (*DNSEvent, error) {
	parser := NewDNSEventParser()
	result, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}
	return result.(*DNSEvent), nil
}

func parseRawProtocolEvent(data []byte) (*ProtocolEvent, error) {
	parser := NewProtocolEventParser()
	result, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}
	return result.(*ProtocolEvent), nil
}