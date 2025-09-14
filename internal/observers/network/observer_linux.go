//go:build linux
// +build linux

package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// NetworkEvent represents a network event from eBPF
type NetworkEvent struct {
	TimestampNs uint64
	PID         uint32
	TID         uint32
	EventType   uint32 // 1=connect, 2=accept, 3=send, 4=recv, 5=close
	Protocol    uint8  // 6=TCP, 17=UDP
	_pad        [3]uint8
	SrcIP       [16]byte // IPv4 or IPv6
	DstIP       [16]byte
	SrcPort     uint16
	DstPort     uint16
	Size        uint32
	Latency     uint64 // Connection setup time in ns
	Comm        [16]byte
	CgroupID    uint64
	Flags       uint32
	IsIPv6      uint8
	_pad2       [7]uint8
	PayloadSize uint32
	_pad3       uint32
	Payload     [256]byte // First bytes of payload for L7 parsing
}

// ebpfObjects contains eBPF objects
type ebpfObjects struct {
	Programs map[string]*ebpf.Program
	Maps     map[string]*ebpf.Map
}

// ebpfStateImpl contains eBPF-specific state
type ebpfStateImpl struct {
	objs       *ebpfObjects
	links      []link.Link
	perfReader *perf.Reader
}

// startEBPF initializes and attaches eBPF programs
func (o *Observer) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load embedded eBPF objects
	objs, err := o.loadEBPFObjects()
	if err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create state
	state := &ebpfStateImpl{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Create perf event reader
	eventsMap, ok := objs.Maps["events"]
	if !ok {
		return fmt.Errorf("events map not found")
	}

	reader, err := perf.NewReader(eventsMap, o.config.RingBufferSize)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	state.perfReader = reader

	// Attach to network functions
	networkFuncs := []struct {
		function string
		program  string
	}{
		{"tcp_connect", "trace_tcp_connect"},
		{"tcp_accept", "trace_tcp_accept"},
		{"tcp_sendmsg", "trace_tcp_send"},
		{"tcp_recvmsg", "trace_tcp_recv"},
		{"tcp_close", "trace_tcp_close"},
		{"udp_sendmsg", "trace_udp_send"},
		{"udp_recvmsg", "trace_udp_recv"},
	}

	for _, fn := range networkFuncs {
		if prog, ok := objs.Programs[fn.program]; ok {
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				o.logger.Warn("Failed to attach to network function",
					zap.String("function", fn.function),
					zap.Error(err))
				continue
			}
			state.links = append(state.links, l)
		}
	}

	o.ebpfState = state

	o.logger.Info("eBPF programs attached",
		zap.Int("programs", len(state.links)))

	return nil
}

// stopEBPF detaches and cleans up eBPF programs
func (o *Observer) stopEBPF() {
	if o.ebpfState == nil {
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
		return
	}

	// Close perf reader
	if state.perfReader != nil {
		state.perfReader.Close()
	}

	// Detach all programs
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		for _, prog := range state.objs.Programs {
			if prog != nil {
				prog.Close()
			}
		}
		for _, m := range state.objs.Maps {
			if m != nil {
				m.Close()
			}
		}
	}

	o.ebpfState = nil
}

// processEvents processes events from eBPF ring buffer
func (o *Observer) processEvents() {
	if o.ebpfState == nil {
		o.logger.Error("No eBPF state available")
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok || state.perfReader == nil {
		return
	}

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		default:
		}

		record, err := state.perfReader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			o.logger.Warn("Failed to read from ring buffer", zap.Error(err))
			continue
		}

		// Parse the event
		if len(record.RawSample) < int(unsafe.Sizeof(NetworkEvent{})) {
			continue
		}

		var event NetworkEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			o.logger.Error("Failed to decode event", zap.Error(err))
			continue
		}

		// Apply sampling
		if o.config.SamplingRate < 1.0 {
			// Simple sampling based on PID
			if float64(event.PID%100) >= o.config.SamplingRate*100 {
				continue
			}
		}

		// Process the event
		o.handleNetworkEvent(&event)
	}
}

// handleNetworkEvent processes a single network event
func (o *Observer) handleNetworkEvent(event *NetworkEvent) {
	// Update metrics
	if o.packetsProcessed != nil {
		o.packetsProcessed.Add(o.LifecycleManager.Context(), 1)
	}

	// Extract IPs
	var srcIP, dstIP string
	if event.IsIPv6 == 1 {
		srcIP = formatIPv6(event.SrcIP[:])
		dstIP = formatIPv6(event.DstIP[:])
	} else {
		srcIP = formatIPv4(event.SrcIP[:4])
		dstIP = formatIPv4(event.DstIP[:4])
	}

	// Get protocol name
	protocol := getProtocolName(event.Protocol)

	// Update specific metrics based on event type
	switch event.EventType {
	case 1: // connect
		if o.connectionsTotal != nil {
			o.connectionsTotal.Add(o.LifecycleManager.Context(), 1,
				metric.WithAttributes(
					attribute.String("protocol", protocol),
					attribute.String("type", "outbound")))
		}
	case 2: // accept
		if o.connectionsTotal != nil {
			o.connectionsTotal.Add(o.LifecycleManager.Context(), 1,
				metric.WithAttributes(
					attribute.String("protocol", protocol),
					attribute.String("type", "inbound")))
		}
	case 3, 4: // send/recv
		if o.bytesTransferred != nil {
			o.bytesTransferred.Add(o.LifecycleManager.Context(), int64(event.Size),
				metric.WithAttributes(
					attribute.String("protocol", protocol),
					attribute.String("direction", getDirection(event.EventType))))
		}
	}

	// L7 parsing if enabled and we have payload
	var l7Data *domain.NetworkL7Data
	if o.config.EnableL7Parse && event.PayloadSize > 0 {
		l7Data = o.parseL7Data(event)
	}

	// Create domain event
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("network-%d-%d", event.PID, event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.CollectorEventType(domain.EventTypeNetwork),
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType:   getEventTypeName(event.EventType),
				Protocol:    protocol,
				SrcIP:       srcIP,
				DstIP:       dstIP,
				SrcPort:     int32(event.SrcPort),
				DstPort:     int32(event.DstPort),
				PayloadSize: int64(event.Size),
				Direction:   getDirection(event.EventType),
				L7Protocol:  getL7Protocol(event.DstPort),
				L7Data:      l7Data,
			},
			Process: &domain.ProcessData{
				PID:     int32(event.PID),
				TID:     int32(event.TID),
				Command: bytesToString(event.Comm[:]),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"protocol":  protocol,
				"direction": getDirection(event.EventType),
			},
		},
	}

	o.SendEvent(domainEvent)
}

// parseL7Data attempts to parse L7 protocol data
func (o *Observer) parseL7Data(event *NetworkEvent) *domain.NetworkL7Data {
	payload := event.Payload[:event.PayloadSize]
	connID := fmt.Sprintf("%d-%d-%d-%d", event.SrcIP[0], event.SrcPort, event.DstIP[0], event.DstPort)

	// Check if it's HTTP
	if o.l7Parser.IsHTTPPort(event.DstPort) || o.l7Parser.IsHTTPPort(event.SrcPort) {
		if req, err := o.l7Parser.ParseHTTPRequest(connID, payload); err == nil && req != nil {
			if o.httpRequests != nil {
				o.httpRequests.Add(o.LifecycleManager.Context(), 1,
					metric.WithAttributes(
						attribute.String("method", req.Method),
						attribute.String("path", req.Path)))
			}
			return &domain.NetworkL7Data{
				Protocol: "HTTP",
				HTTPData: &domain.HTTPRequestData{
					Method:  req.Method,
					URL:     req.URL,
					Path:    req.Path,
					Headers: req.Headers,
				},
			}
		}
	}

	// Check if it's DNS
	if o.l7Parser.IsDNSPort(event.DstPort) || o.l7Parser.IsDNSPort(event.SrcPort) {
		// Simplified DNS detection
		if o.dnsQueries != nil {
			o.dnsQueries.Add(o.LifecycleManager.Context(), 1)
		}
		return &domain.NetworkL7Data{
			Protocol: "DNS",
		}
	}

	return nil
}

// loadEBPFObjects loads pre-compiled eBPF objects
func (o *Observer) loadEBPFObjects() (*ebpfObjects, error) {
	// This would normally load from embedded bytecode
	// For now, returning placeholder
	return &ebpfObjects{
		Programs: make(map[string]*ebpf.Program),
		Maps:     make(map[string]*ebpf.Map),
	}, nil
}

// Helper functions
func bytesToString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func formatIPv4(ip []byte) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

func formatIPv6(ip []byte) string {
	// Simplified IPv6 formatting
	return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
		binary.BigEndian.Uint16(ip[0:2]),
		binary.BigEndian.Uint16(ip[2:4]),
		binary.BigEndian.Uint16(ip[4:6]),
		binary.BigEndian.Uint16(ip[6:8]),
		binary.BigEndian.Uint16(ip[8:10]),
		binary.BigEndian.Uint16(ip[10:12]),
		binary.BigEndian.Uint16(ip[12:14]),
		binary.BigEndian.Uint16(ip[14:16]))
}

func getProtocolName(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("proto_%d", proto)
	}
}

func getEventTypeName(eventType uint32) string {
	switch eventType {
	case 1:
		return "connect"
	case 2:
		return "accept"
	case 3:
		return "send"
	case 4:
		return "recv"
	case 5:
		return "close"
	default:
		return fmt.Sprintf("event_%d", eventType)
	}
}

func getDirection(eventType uint32) string {
	switch eventType {
	case 1, 3: // connect, send
		return "outbound"
	case 2, 4: // accept, recv
		return "inbound"
	default:
		return "unknown"
	}
}

func getL7Protocol(port uint16) string {
	switch port {
	case 80, 8080, 8081:
		return "HTTP"
	case 443, 8443:
		return "HTTPS"
	case 53:
		return "DNS"
	case 3306:
		return "MySQL"
	case 5432:
		return "PostgreSQL"
	case 6379:
		return "Redis"
	case 27017:
		return "MongoDB"
	default:
		return ""
	}
}
