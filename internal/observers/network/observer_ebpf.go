//go:build linux
// +build linux

package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/internal/observers/network/bpf"
)

// tcpStateNames maps TCP state constants to human-readable names
var tcpStateNames = map[uint8]string{
	TCP_ESTABLISHED: "ESTABLISHED",
	TCP_SYN_SENT:    "SYN_SENT",
	TCP_SYN_RECV:    "SYN_RECV",
	TCP_FIN_WAIT1:   "FIN_WAIT1",
	TCP_FIN_WAIT2:   "FIN_WAIT2",
	TCP_TIME_WAIT:   "TIME_WAIT",
	TCP_CLOSE:       "CLOSE",
	TCP_CLOSE_WAIT:  "CLOSE_WAIT",
	TCP_LAST_ACK:    "LAST_ACK",
	TCP_LISTEN:      "LISTEN",
	TCP_CLOSING:     "CLOSING",
}

// Start implements the Observer interface - sets up pipeline stages
func (n *NetworkObserver) Start(ctx context.Context) error {
	// Create event channel for ring buffer → processor communication
	channelSize := n.config.EventChannelSize
	if channelSize <= 0 {
		channelSize = 1000 // Default buffer size
	}
	eventCh := make(chan NetworkEventBPF, channelSize)

	// Stage 1: Load and attach eBPF program
	n.AddStage(func(ctx context.Context) error {
		return n.loadAndAttachStage(ctx, eventCh)
	})

	// Stage 2: Process events from channel
	n.AddStage(func(ctx context.Context) error {
		return n.processEventsStage(ctx, eventCh)
	})

	// Let BaseObserver run the pipeline
	return n.BaseObserver.Start(ctx)
}

// loadAndAttachStage loads eBPF program, attaches to tracepoint, reads ring buffer
func (n *NetworkObserver) loadAndAttachStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
	// Close channel when exiting to signal processor stage
	defer close(eventCh)

	// Load eBPF objects
	objs := &bpf.NetworkObjects{}
	if err := bpf.LoadNetworkObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	defer objs.Close()

	// Attach to inet_sock_set_state tracepoint (TCP state transitions)
	tpState, err := link.Tracepoint("sock", "inet_sock_set_state", objs.TraceInetSockSetState, nil)
	if err != nil {
		return fmt.Errorf("failed to attach inet_sock_set_state tracepoint: %w", err)
	}
	defer tpState.Close()

	// Attach to tcp_receive_reset tracepoint (RST packets)
	tpRST, err := link.Tracepoint("tcp", "tcp_receive_reset", objs.TraceTcpReceiveReset, nil)
	if err != nil {
		return fmt.Errorf("failed to attach tcp_receive_reset tracepoint: %w", err)
	}
	defer tpRST.Close()

	// Attach to tcp_retransmit_skb tracepoint (packet retransmissions)
	tpRetx, err := link.Tracepoint("tcp", "tcp_retransmit_skb", objs.TraceTcpRetransmitSkb, nil)
	if err != nil {
		return fmt.Errorf("failed to attach tcp_retransmit_skb tracepoint: %w", err)
	}
	defer tpRetx.Close()

	// Open ring buffer reader
	rb, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	log.Printf("[%s] eBPF programs loaded and attached (inet_sock_set_state + tcp_receive_reset)", n.Name())

	// Monitor context and close ring buffer when cancelled
	// This unblocks rb.Read() immediately on shutdown
	go func() {
		<-ctx.Done()
		rb.Close()
	}()

	// Ensure cleanup on exit

	// Read ring buffer until closed
	for {
		// Read event from ring buffer (blocks until event or Close())
		record, err := rb.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				log.Printf("[%s] Shutting down eBPF reader", n.Name())
				return nil // Clean shutdown
			}
			log.Printf("[%s] Error reading from ring buffer: %v", n.Name(), err)
			n.RecordError(ctx)
			continue
		}

		// Parse event
		var evt NetworkEventBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
			log.Printf("[%s] Error parsing event: %v", n.Name(), err)
			n.RecordError(ctx)
			continue
		}

		// Send to processing stage (non-blocking)
		select {
		case eventCh <- evt:
			// Event sent
		default:
			// Channel full - drop event
			n.RecordDrop(ctx)
		}
	}
}

// processEventsStage processes events from channel and outputs them
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] Shutting down event processor", n.Name())
			return nil

		case evt, ok := <-eventCh:
			if !ok {
				// Channel closed by loadAndAttachStage - exit gracefully
				log.Printf("[%s] Event channel closed, processor exiting", n.Name())
				return nil
			}
			startTime := time.Now()

			// Validate address family
			if evt.Family != AF_INET && evt.Family != AF_INET6 {
				log.Printf("[%s] Invalid address family %d, skipping event", n.Name(), evt.Family)
				n.RecordError(ctx)
				continue
			}

			// Convert IP addresses
			var srcIP, dstIP string
			if evt.Family == AF_INET {
				srcIP = convertIPv4(evt.SrcIP)
				dstIP = convertIPv4(evt.DstIP)
			} else { // AF_INET6 (already validated above)
				srcIP = convertIPv6(evt.SrcIPv6)
				dstIP = convertIPv6(evt.DstIPv6)
			}

			// Connection key for tracking RST
			connKey := fmt.Sprintf("%s:%d:%s:%d", srcIP, evt.SrcPort, dstIP, evt.DstPort)

			// Handle different event types
			if evt.EventType == EventTypeRSTReceived {
				// RST received - mark this connection as refused
				n.rstConnections.Store(connKey, true)
				log.Printf("[%s] RST received for %s (state=%s)", n.Name(), connKey, tcpStateName(evt.OldState))

				// Record RST metric
				n.connectionResets.Add(ctx, 1)
				continue // Don't emit event yet, wait for state transition
			}

			if evt.EventType == EventTypeRetransmit {
				// Retransmit event - process packet loss
				n.processRetransmitEvent(ctx, evt, connKey, srcIP, dstIP)
				continue // Don't emit state change event for retransmits
			}

			// State change event - convert to domain representation
			eventType := stateToEventType(evt.OldState, evt.NewState, connKey, n)
			comm := extractComm(evt.Comm)

			// Record network-specific metrics based on event type
			switch eventType {
			case "connection_refused":
				n.connectionRefused.Add(ctx, 1)
			case "connection_syn_timeout":
				n.synTimeouts.Add(ctx, 1)
			}

			// Output event (if stdout enabled)
			if n.config.Output.Stdout {
				log.Printf("[%s] %s: %s (%d) %s:%d -> %s:%d [%s->%s]",
					n.Name(), eventType, comm, evt.PID,
					srcIP, evt.SrcPort, dstIP, evt.DstPort,
					tcpStateName(evt.OldState), tcpStateName(evt.NewState))
			}

			// Record base metrics
			n.RecordEvent(ctx)
			n.RecordProcessingTime(ctx, float64(time.Since(startTime).Milliseconds()))
		}
	}
}

// tcpStateName returns human-readable TCP state name
func tcpStateName(state uint8) string {
	if name, ok := tcpStateNames[state]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", state)
}

// processRetransmitEvent handles TCP retransmission events
func (n *NetworkObserver) processRetransmitEvent(ctx context.Context, evt NetworkEventBPF, connKey, srcIP, dstIP string) {
	// Extract retransmit data from reused fields
	totalRetrans := evt.OldState // total_retrans from eBPF
	sndCwnd := evt.NewState      // snd_cwnd from eBPF
	comm := extractComm(evt.Comm)

	// Update retransmit stats for this connection
	statsInterface, _ := n.retransmitStats.LoadOrStore(connKey, &retransmitStats{})
	stats := statsInterface.(*retransmitStats)
	stats.retransmits++
	stats.totalPackets++ // Approximate: increment on each retransmit
	stats.lastRetransmit = time.Now()

	// Record retransmit metric
	n.retransmitsTotal.Add(ctx, 1)

	// Calculate retransmit rate if we have enough data
	const minPacketsForRate = 100
	if stats.totalPackets >= minPacketsForRate {
		retxRate := float64(stats.retransmits) / float64(stats.totalPackets) * 100
		n.retransmitRate.Record(ctx, retxRate)

		// Detect high retransmit rate (>5% indicates network issues)
		const highRetransmitThreshold = 5.0
		if retxRate > highRetransmitThreshold {
			// Record congestion event
			n.congestionEvents.Add(ctx, 1)

			// Output warning if stdout enabled
			if n.config.Output.Stdout {
				log.Printf("[%s] HIGH RETRANSMIT RATE: %s (%s) %.1f%% (retx=%d, total=%d, cwnd=%d)",
					n.Name(), connKey, comm, retxRate,
					stats.retransmits, stats.totalPackets, sndCwnd)
			}
		}
	}

	// Output retransmit event if stdout enabled
	if n.config.Output.Stdout {
		log.Printf("[%s] RETRANSMIT: %s (%d) %s:%d -> %s:%d (total_retrans=%d, cwnd=%d)",
			n.Name(), comm, evt.PID,
			srcIP, evt.SrcPort, dstIP, evt.DstPort,
			totalRetrans, sndCwnd)
	}

	// Record processing time
	n.RecordEvent(ctx)
}
