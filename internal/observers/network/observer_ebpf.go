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
	objs := &bpf.networkObjects{}
	if err := bpf.loadNetworkObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	defer objs.Close()

	// Attach to tracepoint
	tp, err := link.Tracepoint("sock", "inet_sock_set_state", objs.TraceInetSockSetState, nil)
	if err != nil {
		return fmt.Errorf("failed to attach tracepoint: %w", err)
	}
	defer tp.Close()

	// Open ring buffer reader
	rb, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}
	defer rb.Close()

	log.Printf("[%s] eBPF program loaded and attached", n.Name())

	// Monitor context and close ring buffer when cancelled
	go func() {
		<-ctx.Done()
		rb.Close()
	}()

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

			// Convert event to domain representation
			eventType := stateToEventType(evt.OldState, evt.NewState)

			var srcIP, dstIP string
			if evt.Family == AF_INET {
				srcIP = convertIPv4(evt.SrcIP)
				dstIP = convertIPv4(evt.DstIP)
			} else if evt.Family == AF_INET6 {
				srcIP = convertIPv6(evt.SrcIPv6)
				dstIP = convertIPv6(evt.DstIPv6)
			}

			comm := extractComm(evt.Comm)

			// Output event (if stdout enabled)
			if n.config.Output.Stdout {
				log.Printf("[%s] %s: %s (%d) %s:%d -> %s:%d [%s->%s]",
					n.Name(), eventType, comm, evt.PID,
					srcIP, evt.SrcPort, dstIP, evt.DstPort,
					tcpStateName(evt.OldState), tcpStateName(evt.NewState))
			}

			// Record metrics
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
