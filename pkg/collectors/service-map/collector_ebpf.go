//go:build linux

package servicemap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/collectors/service-map/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfState holds eBPF components for Linux
type ebpfState struct {
	objs   *bpf.ServicemonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// ConnectionEvent represents a connection event from eBPF (matches C struct)
type ConnectionEvent struct {
	SrcIP     uint32    `json:"src_ip"`
	DstIP     uint32    `json:"dst_ip"`
	SrcPort   uint16    `json:"src_port"`
	DstPort   uint16    `json:"dst_port"`
	Protocol  uint8     `json:"protocol"`
	EventType uint8     `json:"event_type"` // 0=new, 1=close
	Timestamp uint64    `json:"timestamp"`
	BytesSent uint64    `json:"bytes_sent"`
	BytesRecv uint64    `json:"bytes_recv"`
	PID       uint32    `json:"pid"`
	UID       uint32    `json:"uid"`
	Comm      [16]int8  `json:"comm"`
}

// startEBPF initializes and starts eBPF programs for connection tracking
func (c *Collector) startEBPF() error {
	c.logger.Info("Starting eBPF connection tracking for service map")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memory limit: %w", err)
	}

	// Load pre-compiled eBPF objects
	objs := &bpf.ServicemonitorObjects{}
	if err := bpf.LoadServicemonitorObjects(objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			c.logger.Error("eBPF verifier error", zap.String("details", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach kprobes for connection tracking
	probes := map[string]string{
		"tcp_connect":        "kprobe/tcp_connect",
		"inet_csk_accept":    "kprobe/inet_csk_accept", 
		"tcp_sendmsg":        "kprobe/tcp_sendmsg",
		"tcp_cleanup_rbuf":   "kprobe/tcp_cleanup_rbuf",
		"tcp_close":          "kprobe/tcp_close",
		"udp_sendmsg":        "kprobe/udp_sendmsg",
	}

	for symbol, progName := range probes {
		var prog *ebpf.Program
		switch progName {
		case "kprobe/tcp_connect":
			prog = objs.TraceTcpConnect
		case "kprobe/inet_csk_accept":
			prog = objs.TraceTcpAccept
		case "kprobe/tcp_sendmsg":
			prog = objs.TraceTcpSendmsg
		case "kprobe/tcp_cleanup_rbuf":
			prog = objs.TraceTcpCleanupRbuf
		case "kprobe/tcp_close":
			prog = objs.TraceTcpClose
		case "kprobe/udp_sendmsg":
			prog = objs.TraceUdpSendmsg
		default:
			continue
		}

		l, err := link.Kprobe(symbol, prog, nil)
		if err != nil {
			c.logger.Warn("Failed to attach kprobe", 
				zap.String("symbol", symbol),
				zap.Error(err))
			// Continue with other probes
			continue
		}
		state.links = append(state.links, l)
	}

	if len(state.links) == 0 {
		objs.Close()
		return errors.New("failed to attach any eBPF programs")
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.closeEBPF(state)
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	state.reader = reader

	c.ebpfState = state
	c.logger.Info("eBPF connection tracking started successfully", 
		zap.Int("attached_probes", len(state.links)))

	return nil
}

// stopEBPF stops and cleans up eBPF programs
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	c.closeEBPF(state)
	c.ebpfState = nil
	c.logger.Info("eBPF connection tracking stopped")
}

// closeEBPF closes all eBPF resources
func (c *Collector) closeEBPF(state *ebpfState) {
	if state.reader != nil {
		state.reader.Close()
	}
	
	for _, l := range state.links {
		l.Close()
	}
	
	if state.objs != nil {
		state.objs.Close()
	}
}

// processEBPFEvents processes events from the eBPF ring buffer
func (c *Collector) processEBPFEvents(ctx context.Context) {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	c.logger.Info("Starting eBPF event processing")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.StopChannel():
			return
		default:
		}

		// Read from ring buffer with timeout
		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				c.logger.Info("Ring buffer closed, stopping event processing")
				return
			}
			c.RecordError(err)
			continue
		}

		// Parse connection event
		if len(record.RawSample) < int(unsafe.Sizeof(ConnectionEvent{})) {
			c.logger.Warn("Invalid eBPF event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*ConnectionEvent)(unsafe.Pointer(&record.RawSample[0]))
		
		// Process the connection event
		c.processConnectionEvent(ctx, event)
	}
}

// processConnectionEvent processes a single connection event from eBPF
func (c *Collector) processConnectionEvent(ctx context.Context, event *ConnectionEvent) {
	// Start tracing span for this connection event
	ctx, span := c.StartSpan(ctx, "process-connection-event")
	defer span.End()

	start := time.Now()

	// Convert to Connection struct
	conn := &Connection{
		SourceIP:    event.SrcIP,
		DestIP:      event.DstIP,
		SourcePort:  event.SrcPort,
		DestPort:    event.DstPort,
		Protocol:    event.Protocol,
		Direction:   c.detectConnectionDirection(event),
		State:       c.detectConnectionState(event),
		Timestamp:   time.Unix(0, int64(event.Timestamp)),
		BytesSent:   event.BytesSent,
		BytesRecv:   event.BytesRecv,
		Latency:     0, // TODO: Calculate from connect->accept timing
		Retransmits: 0, // TODO: Get from TCP info
		Resets:      0, // TODO: Track RST packets
	}
	
	// Calculate L4 quality score
	conn.Quality = conn.CalculateQuality()

	// Create connection key
	connKey := fmt.Sprintf("%s:%d->%s:%d", 
		intToIP(conn.SourceIP), conn.SourcePort,
		intToIP(conn.DestIP), conn.DestPort)

	c.mu.Lock()
	
	if event.EventType == 0 { // New connection
		c.connections[connKey] = conn
		c.logger.Debug("New connection tracked",
			zap.String("connection", connKey),
			zap.String("protocol", protocolToString(conn.Protocol)),
			zap.Uint32("pid", event.PID))
	} else { // Connection close
		if existing, ok := c.connections[connKey]; ok {
			// Update final stats
			existing.BytesSent = conn.BytesSent
			existing.BytesRecv = conn.BytesRecv
			
			// Keep connection for a bit longer for service mapping
			// Actual cleanup happens in cleanupConnections()
		}
	}
	
	c.mu.Unlock()

	// Check if this connection involves known services
	c.updateServiceConnections(conn, event)

	// Record metrics
	c.RecordProcessingDuration(ctx, time.Since(start))
	c.RecordEventWithContext(ctx)

	// Emit connection event if ring buffer consumer is registered
	c.emitConnectionEvent(ctx, conn, event)
}

// detectConnectionDirection determines who initiated the connection
func (c *Collector) detectConnectionDirection(event *ConnectionEvent) ConnDirection {
	// EventType field encodes the kprobe source
	// 0 = tcp_connect (outbound)
	// 1 = inet_csk_accept (inbound) 
	// Higher bits might encode other info
	
	eventSource := event.EventType & 0x0F // Lower 4 bits for event source
	
	switch eventSource {
	case 0: // tcp_connect - we initiated
		return DirectionOutbound
	case 1: // accept - we received
		return DirectionInbound
	default:
		return DirectionUnknown
	}
}

// detectConnectionState determines the connection state
func (c *Collector) detectConnectionState(event *ConnectionEvent) ConnState {
	// Upper bits of EventType encode state
	stateCode := (event.EventType >> 4) & 0x0F
	
	switch stateCode {
	case 0:
		return StateEstablished
	case 1:
		return StateSynSent
	case 2:
		return StateFinWait
	case 3:
		return StateClosed
	case 4:
		return StateReset
	default:
		return StateUnknown
	}
}

// updateServiceConnections updates service connection statistics
func (c *Collector) updateServiceConnections(conn *Connection, event *ConnectionEvent) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Get all possible source services
	srcServices := c.getServicesForIP(intToIP(conn.SourceIP), conn.SourcePort)
	dstServices := c.getServicesForIP(intToIP(conn.DestIP), conn.DestPort)

	// Update connections for all service pairs
	for _, srcService := range srcServices {
		for _, dstService := range dstServices {
			if srcService != "" && dstService != "" && srcService != dstService {
				// Use connection direction to determine dependency
				if conn.Direction == DirectionOutbound {
					// We (srcService) called them (dstService)
					if src, ok := c.services[srcService]; ok {
						if dep, exists := src.Dependencies[dstService]; exists {
							dep.CallRate += 1
							dep.LastSeen = time.Now()
							if dep.Protocol == "" {
								dep.Protocol = protocolToString(conn.Protocol)
							}
						}
					}
					if dst, ok := c.services[dstService]; ok {
						if dep, exists := dst.Dependents[srcService]; exists {
							dep.CallRate += 1
							dep.LastSeen = time.Now()
						}
					}
				} else if conn.Direction == DirectionInbound {
					// They (srcService) called us (dstService) 
					// Reverse the dependency direction
					if dst, ok := c.services[dstService]; ok {
						if dep, exists := dst.Dependencies[srcService]; exists {
							dep.CallRate += 1
							dep.LastSeen = time.Now()
							if dep.Protocol == "" {
								dep.Protocol = protocolToString(conn.Protocol)
							}
						}
					}
					if src, ok := c.services[srcService]; ok {
						if dep, exists := src.Dependents[dstService]; exists {
							dep.CallRate += 1
							dep.LastSeen = time.Now()
						}
					}
				}
			}
		}
	}
}

// emitConnectionEvent emits a connection event via ring buffer if consumers are registered
func (c *Collector) emitConnectionEvent(ctx context.Context, conn *Connection, ebpfEvent *ConnectionEvent) {
	// Create domain event for the connection
	commStr := string(bytes.Trim((*(*[]byte)(unsafe.Pointer(&ebpfEvent.Comm)))[:16], "\x00"))
	
	connectionData := map[string]string{
		"source_ip":   intToIP(conn.SourceIP),
		"dest_ip":     intToIP(conn.DestIP),
		"source_port": fmt.Sprintf("%d", conn.SourcePort),
		"dest_port":   fmt.Sprintf("%d", conn.DestPort),
		"protocol":    protocolToString(conn.Protocol),
		"bytes_sent":  fmt.Sprintf("%d", conn.BytesSent),
		"bytes_recv":  fmt.Sprintf("%d", conn.BytesRecv),
		"pid":         fmt.Sprintf("%d", ebpfEvent.PID),
		"uid":         fmt.Sprintf("%d", ebpfEvent.UID),
		"process":     commStr,
		"event_type":  fmt.Sprintf("%d", ebpfEvent.EventType),
	}

	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("connection-%d", ebpfEvent.Timestamp),
		Source:    c.Name(),
		Type:      domain.EventTypeNetworkConnection,
		Timestamp: time.Unix(0, int64(ebpfEvent.Timestamp)),
		Severity:  domain.EventSeverityInfo, // Use EventSeverityInfo instead of SeverityDebug
		EventData: domain.EventDataContainer{
			Custom: connectionData,
		},
	}

	// Filter the event
	if !c.ShouldProcess(event) {
		return
	}

	// Record event size
	eventSize := int64(200 + len(commStr)) // Rough estimate
	c.RecordEventSize(ctx, eventSize)

	// Send via ring buffer if available
	if c.IsRingBufferEnabled() {
		c.WriteToRingBuffer(event)
	}
}