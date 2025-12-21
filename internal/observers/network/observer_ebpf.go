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

	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/internal/observers/network/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
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

	// Load eBPF objects (bpf2go-generated)
	objs := &bpf.NetworkObjects{}
	if err := bpf.LoadNetworkObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	defer func() {
		if err := objs.Close(); err != nil {
			log.Printf("[%s] Error closing eBPF objects: %v", n.Name(), err)
		}
	}()

	// Store map reference for reading connection stats
	n.connStatsMap = objs.ConnStats

	// Create BaseEBPFManager. We pass nil because bpf2go generates typed structs
	// (NetworkObjects, NetworkPrograms, NetworkMaps) but doesn't expose the underlying
	// *ebpf.Collection required by NewEBPFManagerFromCollection. We use the manager
	// only for tracepoint attachment; individual programs/maps accessed via objs.
	n.ebpfMgr = base.NewEBPFManagerFromCollection(nil)
	defer func() {
		if err := n.ebpfMgr.Close(); err != nil {
			log.Printf("[%s] Error closing eBPF manager: %v", n.Name(), err)
		}
	}()

	// Attach to inet_sock_set_state tracepoint (TCP state transitions)
	if err := n.ebpfMgr.AttachTracepointWithProgram(objs.TraceInetSockSetState, "sock", "inet_sock_set_state"); err != nil {
		return fmt.Errorf("failed to attach inet_sock_set_state: %w", err)
	}

	// Attach to tcp_receive_reset tracepoint (RST packets)
	if err := n.ebpfMgr.AttachTracepointWithProgram(objs.TraceTcpReceiveReset, "tcp", "tcp_receive_reset"); err != nil {
		return fmt.Errorf("failed to attach tcp_receive_reset: %w", err)
	}

	// Attach to tcp_retransmit_skb tracepoint (packet retransmissions)
	if err := n.ebpfMgr.AttachTracepointWithProgram(objs.TraceTcpRetransmitSkb, "tcp", "tcp_retransmit_skb"); err != nil {
		return fmt.Errorf("failed to attach tcp_retransmit_skb: %w", err)
	}

	log.Printf("[%s] eBPF programs loaded and attached (3 tracepoints)", n.Name())

	// Read ring buffer events (bpf2go-generated objects give us direct map access)
	// Monitor context and close ring buffer when cancelled
	go func() {
		<-ctx.Done()
		if err := n.ebpfMgr.Close(); err != nil {
			log.Printf("[%s] Error closing eBPF manager in cleanup goroutine: %v", n.Name(), err)
		}
	}()

	// Read ring buffer until closed
	rb := objs.Events
	reader, err := ringbuf.NewReader(rb)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("[%s] Error closing ring buffer reader: %v", n.Name(), err)
		}
	}()

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				log.Printf("[%s] Ring buffer closed, shutting down", n.Name())
				return nil
			}
			log.Printf("[%s] Error reading ring buffer: %v", n.Name(), err)
			n.RecordError(ctx, nil) // nil event: internal ring buffer error, not a domain event
			continue
		}

		// Parse event
		var evt NetworkEventBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
			log.Printf("[%s] Error parsing event: %v", n.Name(), err)
			n.RecordError(ctx, nil) // nil event: binary parsing error, no domain event created yet
			continue
		}

		// Send to processing stage (non-blocking)
		select {
		case eventCh <- evt:
			// Event sent
		default:
			// Channel full - drop event
			n.RecordDrop(ctx, "network_event")
		}
	}
}

// processEventsStage processes events from channel and outputs them
func (n *NetworkObserver) processEventsStage(ctx context.Context, eventCh chan NetworkEventBPF) error {
	// Initialize processors (Design Doc 003 - processor pattern)
	linkProc := NewLinkProcessor()
	dnsProc := NewDNSProcessor()
	statusProc := NewStatusProcessor()

	// Periodically report ringbuffer utilization
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] Shutting down event processor", n.Name())
			return nil

		case <-ticker.C:
			// Report ringbuffer utilization (% of channel capacity used)
			var utilization float64
			if cap(eventCh) > 0 {
				utilization = float64(len(eventCh)) / float64(cap(eventCh)) * 100
			} else {
				utilization = 0
			}
			n.ringbufferUtilization.Record(ctx, utilization)

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
				n.RecordError(ctx, nil) // nil event: validation error before domain event creation
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

			// Try processors in order (Design Doc 003 - processor pattern)
			if domainEvent := linkProc.Process(ctx, evt); domainEvent != nil {
				n.emitDomainEvent(ctx, domainEvent)
				continue
			}

			if domainEvent := dnsProc.Process(ctx, evt); domainEvent != nil {
				n.emitDomainEvent(ctx, domainEvent)
				continue
			}

			if domainEvent := statusProc.Process(ctx, evt); domainEvent != nil {
				n.emitDomainEvent(ctx, domainEvent)
				continue
			}

			// Handle different event types
			if evt.EventType == EventTypeRSTReceived {
				// RST received - already marked in eBPF LRU map by tcp_receive_reset handler
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

			if evt.EventType == EventTypeRTTSpike {
				// RTT spike event - process latency degradation
				n.processRTTSpikeEvent(ctx, evt, connKey, srcIP, dstIP)
				continue // Don't emit state change event for RTT spikes
			}

			// State change event - convert to domain representation
			eventType := stateToEventType(evt.OldState, evt.NewState, connKey, n)
			_ = extractComm(evt.Comm) // comm used for debug output (disabled)

			// Record network-specific metrics based on event type
			switch eventType {
			case "connection_refused":
				n.connectionRefused.Add(ctx, 1)
			case "connection_syn_timeout":
				n.synTimeouts.Add(ctx, 1)
			}

			// Record base metrics
			n.RecordEvent(ctx)
			n.RecordProcessingTime(ctx, nil, float64(time.Since(startTime).Milliseconds())) // nil event: state change processing, not a specific domain event
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
	_ = evt.OldState        // total_retrans from eBPF (used for debug output, disabled)
	sndCwnd := evt.NewState // snd_cwnd from eBPF
	comm := extractComm(evt.Comm)

	// Read stats from eBPF LRU map (already updated by tcp_retransmit_skb handler)
	var key ebpfConnKey
	var stats ebpfRetransmitStats
	if n.connStatsMap != nil {
		key = ebpfConnKey{
			SrcAddr: ipv4StringToUint32(srcIP),
			DstAddr: ipv4StringToUint32(dstIP),
			SrcPort: evt.SrcPort,
			DstPort: evt.DstPort,
		}
		// Lookup stats from eBPF map - if entry not found, stats remain zero-initialized
		// This is expected for new connections and not an error condition
		_ = n.connStatsMap.Lookup(&key, &stats) // Ignore: missing entries are expected
	}

	// Track eBPF map size (number of tracked connections)
	if n.connStatsMap != nil {
		var count uint32
		iter := n.connStatsMap.Iterate()
		var k ebpfConnKey
		var v ebpfRetransmitStats
		for iter.Next(&k, &v) {
			count++
		}
		n.ebpfMapSize.Record(ctx, int64(count))
	}

	// Record retransmit metric
	n.retransmitsTotal.Add(ctx, 1)

	// Calculate retransmit rate if we have enough data
	const minPacketsForRate = 100
	if stats.TotalPackets >= minPacketsForRate {
		retxRate := float64(stats.Retransmits) / float64(stats.TotalPackets)
		n.retransmitRate.Record(ctx, retxRate)

		// Detect high retransmit rate (>0.05 = 5% indicates network issues)
		const highRetransmitThreshold = 0.05
		if retxRate > highRetransmitThreshold {
			// Record congestion event
			n.congestionEvents.Add(ctx, 1)

			// Create and emit domain event for high retransmit rate
			n.emitDomainEvent(ctx, &domain.ObserverEvent{
				Type:    "packet_loss",
				Subtype: "high_retransmit_rate",
				NetworkData: &domain.NetworkEventData{
					Protocol:         "tcp",
					SrcIP:            srcIP,
					DstIP:            dstIP,
					SrcPort:          evt.SrcPort,
					DstPort:          evt.DstPort,
					RetransmitCount:  uint32(stats.Retransmits),
					RetransmitRate:   retxRate * 100, // Convert to percentage
					CongestionWindow: uint32(sndCwnd),
					ProcessName:      comm,
				},
			})

		}
	}

	// Record processing time
	n.RecordEvent(ctx)
}

// processRTTSpikeEvent handles RTT spike events from eBPF (Stage 3)
func (n *NetworkObserver) processRTTSpikeEvent(ctx context.Context, evt NetworkEventBPF, connKey, srcIP, dstIP string) {
	// Extract RTT data from reused fields
	baselineMs := float64(evt.OldState) // Baseline RTT in ms
	currentMs := float64(evt.NewState)  // Current RTT in ms
	comm := extractComm(evt.Comm)

	// Calculate degradation percentage
	degradation := 0.0
	if baselineMs > 0 {
		degradation = (currentMs - baselineMs) / baselineMs
	}

	// Record OTEL metrics
	n.rttSpikesTotal.Add(ctx, 1)
	n.rttCurrentMs.Record(ctx, currentMs)
	n.rttDegradationPct.Record(ctx, degradation)

	// Create and emit domain event
	n.emitDomainEvent(ctx, &domain.ObserverEvent{
		Type:    "rtt_spike",
		Subtype: "latency_degradation",
		NetworkData: &domain.NetworkEventData{
			Protocol:       "tcp",
			SrcIP:          srcIP,
			DstIP:          dstIP,
			SrcPort:        evt.SrcPort,
			DstPort:        evt.DstPort,
			RTTBaseline:    baselineMs,
			RTTCurrent:     currentMs,
			RTTDegradation: degradation * 100,
			ProcessName:    comm,
		},
	})

}

// emitDomainEvent outputs domain events from processors
func (n *NetworkObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) {
	// Record OTEL metrics
	n.RecordEvent(ctx)

	// Validate event has network data
	if evt.NetworkData == nil {
		log.Printf("[%s] %s.%s: missing network data", n.Name(), evt.Type, evt.Subtype)
		return
	}

	// Enrich with K8s context (if service configured)
	if n.config.K8sContextService != nil {
		n.enrichWithK8sContext(evt)
	}

	// Send to OTLP (Community path - structured logs)
	n.SendObserverEvent(ctx, evt)
}

// enrichWithK8sContext lookups pod metadata by IP and populates NetworkEventData fields
// Also publishes TapioEvent with graph entities to NATS (Enterprise path)
func (n *NetworkObserver) enrichWithK8sContext(evt *domain.ObserverEvent) {
	if evt.NetworkData == nil {
		return
	}

	// Lookup source pod by IP
	if evt.NetworkData.SrcIP != "" {
		podInfo, err := n.config.K8sContextService.GetPodByIP(evt.NetworkData.SrcIP)
		if err == nil {
			// Populate K8s fields in NetworkEventData (for OTLP)
			evt.NetworkData.PodName = podInfo.Name
			evt.NetworkData.Namespace = podInfo.Namespace

			// Build K8sContext for Enterprise graph enrichment
			k8sCtx := &domain.K8sContext{
				PodName:      podInfo.Name,
				PodNamespace: podInfo.Namespace,
				PodLabels:    podInfo.Labels,
				PodIP:        podInfo.PodIP,
				HostIP:       podInfo.HostIP,
			}

			// Enrich to TapioEvent with graph entities (Enterprise path)
			tapioEvent, err := domain.EnrichWithK8sContext(evt, k8sCtx)
			if err == nil {
				// Publish to NATS (NoOp in OSS, real NATS in Enterprise)
				if err := n.PublishEvent(context.Background(), "tapio.events.network", tapioEvent); err != nil {
					log.Printf("[%s] failed to publish TapioEvent: %v", n.Name(), err)
				}
			}
		}
	}
}
