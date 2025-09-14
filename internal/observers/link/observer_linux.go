//go:build linux
// +build linux

package link

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

	// Attach to network functions for link failure detection
	linkFuncs := []struct {
		function string
		program  string
	}{
		// L4 - TCP connection tracking
		{"tcp_connect", "trace_tcp_connect"},       // Track SYN sent
		{"tcp_rcv_state", "trace_tcp_state"},       // Track SYN-ACK, RST
		{"tcp_retransmit", "trace_tcp_retransmit"}, // Track retransmissions
		{"tcp_send_reset", "trace_tcp_reset"},      // Track RST packets

		// L3 - IP layer tracking
		{"ip_rcv", "trace_ip_rcv"},
		{"icmp_rcv", "trace_icmp"}, // ICMP unreachable, etc

		// L2 - ARP tracking (if enabled)
		{"arp_send", "trace_arp_send"},
		{"arp_rcv", "trace_arp_rcv"},
	}

	for _, fn := range linkFuncs {
		if !o.shouldTrace(fn.function) {
			continue
		}

		if prog, ok := objs.Programs[fn.program]; ok {
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				o.logger.Warn("Failed to attach to link function",
					zap.String("function", fn.function),
					zap.Error(err))
				continue
			}
			state.links = append(state.links, l)
		}
	}

	o.ebpfState = state

	o.logger.Info("eBPF programs attached for link monitoring",
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
		if len(record.RawSample) < int(unsafe.Sizeof(LinkEvent{})) {
			continue
		}

		var event LinkEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			o.logger.Error("Failed to decode event", zap.Error(err))
			continue
		}

		// Process the event
		o.handleLinkEvent(&event)
	}
}

// handleLinkEvent processes a single link event
func (o *Observer) handleLinkEvent(event *LinkEvent) {
	switch event.EventType {
	case 1: // SYN sent
		o.handleSYNSent(event)
	case 2: // SYN-ACK received
		o.handleSYNACK(event)
	case 3: // RST received
		o.handleRST(event)
	case 4: // ARP request
		o.handleARPRequest(event)
	case 5: // ARP reply
		o.handleARPReply(event)
	case 6: // ICMP unreachable
		o.handleICMPUnreachable(event)
	case 7: // TCP retransmit
		o.handleRetransmit(event)
	}
}

// handleSYNSent tracks a new SYN packet
func (o *Observer) handleSYNSent(event *LinkEvent) {
	srcIP := formatIP(event.SrcIP[:], event.Protocol == 6)
	dstIP := formatIP(event.DstIP[:], event.Protocol == 6)

	syn := &SYNAttempt{
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		SrcIP:     srcIP,
		DstIP:     dstIP,
		SrcPort:   int32(event.SrcPort),
		DstPort:   int32(event.DstPort),
		SeqNum:    event.Flags, // Seq number stored in flags
	}

	// Store pending SYN
	o.mu.Lock()
	key := uint64(event.SrcPort)<<32 | uint64(event.DstPort)<<16 | uint64(event.Flags&0xFFFF)
	o.pendingSYNs[key] = syn
	o.mu.Unlock()
}

// handleSYNACK removes pending SYN (connection succeeded)
func (o *Observer) handleSYNACK(event *LinkEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Find and remove matching SYN
	key := uint64(event.DstPort)<<32 | uint64(event.SrcPort)<<16 | uint64((event.Flags-1)&0xFFFF)
	delete(o.pendingSYNs, key)
}

// handleRST handles TCP reset
func (o *Observer) handleRST(event *LinkEvent) {
	srcIP := formatIP(event.SrcIP[:], event.Protocol == 6)
	dstIP := formatIP(event.DstIP[:], event.Protocol == 6)

	if o.connectionResets != nil {
		o.connectionResets.Add(o.LifecycleManager.Context(), 1,
			metric.WithAttributes(
				attribute.String("src_ip", srcIP),
				attribute.String("dst_ip", dstIP),
			))
	}

	// Create RST event
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-rst-%d", event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.CollectorEventType("link.tcp_reset"),
		Source:    o.name,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "tcp_reset",
				Protocol:  "TCP",
				SrcIP:     srcIP,
				DstIP:     dstIP,
				SrcPort:   int32(event.SrcPort),
				DstPort:   int32(event.DstPort),
			},
			Process: &domain.ProcessData{
				PID:     int32(event.PID),
				Command: bytesToString(event.Comm[:]),
			},
			Custom: map[string]string{
				"failure_type": "connection_reset",
				"layer":        "L4",
			},
		},
	}

	o.SendEvent(domainEvent)
}

// handleARPRequest tracks ARP requests
func (o *Observer) handleARPRequest(event *LinkEvent) {
	if !o.config.EnableL2Track {
		return
	}

	srcIP := formatIP(event.SrcIP[:4], false)
	dstIP := formatIP(event.DstIP[:4], false)

	arp := &ARPRequest{
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		SrcIP:     srcIP,
		TargetIP:  dstIP,
		Interface: fmt.Sprintf("if%d", event.Flags&0xFF),
	}

	// Store pending ARP
	o.mu.Lock()
	key := binary.BigEndian.Uint32(event.DstIP[:4])
	o.pendingARPs[key] = arp
	o.mu.Unlock()
}

// handleARPReply removes pending ARP request
func (o *Observer) handleARPReply(event *LinkEvent) {
	if !o.config.EnableL2Track {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	// Remove matching ARP request
	key := binary.BigEndian.Uint32(event.SrcIP[:4])
	delete(o.pendingARPs, key)
}

// handleICMPUnreachable handles ICMP unreachable messages
func (o *Observer) handleICMPUnreachable(event *LinkEvent) {
	if !o.config.EnableL3Track {
		return
	}

	srcIP := formatIP(event.SrcIP[:], false)
	dstIP := formatIP(event.DstIP[:], false)

	// Create ICMP unreachable event
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-icmp-%d", event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.CollectorEventType("link.icmp_unreachable"),
		Source:    o.name,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "icmp_unreachable",
				Protocol:  "ICMP",
				SrcIP:     srcIP,
				DstIP:     dstIP,
			},
			Custom: map[string]string{
				"failure_type": "unreachable",
				"layer":        "L3",
				"icmp_type":    fmt.Sprintf("%d", event.ErrorCode),
			},
		},
	}

	o.SendEvent(domainEvent)
}

// handleRetransmit tracks TCP retransmissions
func (o *Observer) handleRetransmit(event *LinkEvent) {
	if o.retransmissions != nil {
		o.retransmissions.Add(o.LifecycleManager.Context(), 1,
			metric.WithAttributes(
				attribute.Int("retries", int(event.Flags&0xFF)),
			))
	}

	// Check if exceeds threshold
	if int(event.Flags&0xFF) >= o.config.MaxRetransmits {
		srcIP := formatIP(event.SrcIP[:], event.Protocol == 6)
		dstIP := formatIP(event.DstIP[:], event.Protocol == 6)

		// Excessive retransmits indicate link problems
		domainEvent := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("link-retransmit-%d", event.TimestampNs),
			Timestamp: time.Unix(0, int64(event.TimestampNs)),
			Type:      domain.CollectorEventType("link.excessive_retransmit"),
			Source:    o.name,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					EventType: "excessive_retransmit",
					Protocol:  "TCP",
					SrcIP:     srcIP,
					DstIP:     dstIP,
					SrcPort:   int32(event.SrcPort),
					DstPort:   int32(event.DstPort),
				},
				Custom: map[string]string{
					"retransmit_count": fmt.Sprintf("%d", event.Flags&0xFF),
					"threshold":        fmt.Sprintf("%d", o.config.MaxRetransmits),
				},
			},
		}

		o.SendEvent(domainEvent)
	}
}

// shouldTrace determines if we should trace a function
func (o *Observer) shouldTrace(function string) bool {
	switch function {
	case "arp_send", "arp_rcv":
		return o.config.EnableL2Track
	case "ip_rcv", "icmp_rcv":
		return o.config.EnableL3Track
	case "tcp_connect", "tcp_rcv_state", "tcp_retransmit", "tcp_send_reset":
		return o.config.EnableL4Track
	default:
		return true
	}
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

func formatIP(ip []byte, isIPv6 bool) string {
	if isIPv6 && len(ip) >= 16 {
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
	if len(ip) >= 4 {
		return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
	}
	return ""
}
