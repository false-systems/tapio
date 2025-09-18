//go:build linux
// +build linux

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/network/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// networkEBPF contains eBPF-specific state for CO-RE implementation
type networkEBPF struct {
	objs   *bpf.NetworkObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Connection tracking
	connections      map[string]*ConnectionInfo
	connectionsMutex sync.RWMutex

	// K8s enricher
	k8sEnricher *K8sEnricher

	// Metrics
	eventsProcessed    metric.Int64Counter
	eventsDropped      metric.Int64Counter
	processingTime     metric.Float64Histogram
	bytesProcessed     metric.Int64Counter
	connectionsTracked metric.Int64Counter
	l7EventsParsed     metric.Int64Counter

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config *Config
}

// startEBPF initializes and attaches CO-RE eBPF programs
func (o *Observer) startEBPF() error {
	o.logger.Info("Starting network observer with CO-RE eBPF support")
	return o.loadEBPF()
}

// loadEBPF loads CO-RE eBPF programs
func (o *Observer) loadEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for Network observer")

	objs, err := o.loadBPFObjects()
	if err != nil {
		return err
	}

	ebpfState := o.createEBPFState(&objs)
	o.ebpfState = ebpfState

	if err := o.setupEBPFEnvironment(&objs); err != nil {
		objs.Close()
		return err
	}

	o.startEBPFWorkers(ebpfState)
	o.logger.Info("CO-RE eBPF programs loaded successfully for Network observer")
	return nil
}

// loadBPFObjects loads the BPF objects from bytecode
func (o *Observer) loadBPFObjects() (bpf.NetworkObjects, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return bpf.NetworkObjects{}, fmt.Errorf("removing memlock: %w", err)
	}

	var objs bpf.NetworkObjects
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogSize: 64 * 1024 * 1024,
		},
	}

	err := bpf.LoadNetworkObjects(&objs, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error", zap.String("error", ve.Error()))
			return objs, fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return objs, fmt.Errorf("loading BPF objects: %w", err)
	}
	return objs, nil
}

// createEBPFState creates and initializes the eBPF state
func (o *Observer) createEBPFState(objs *bpf.NetworkObjects) *networkEBPF {
	k8sEnricher, err := NewK8sEnricher(o.logger)
	if err != nil {
		o.logger.Warn("K8s enricher not available", zap.Error(err))
	}

	return &networkEBPF{
		objs:               objs,
		links:              make([]link.Link, 0),
		connections:        make(map[string]*ConnectionInfo),
		k8sEnricher:        k8sEnricher,
		eventsProcessed:    o.eventsProcessed,
		eventsDropped:      o.errorsTotal,
		processingTime:     o.requestLatency,
		bytesProcessed:     o.bytesTransferred,
		connectionsTracked: o.connectionsTotal,
		l7EventsParsed:     o.httpRequests,
		logger:             o.logger,
		config:             o.config,
	}
}

// setupEBPFEnvironment attaches probes and configures the eBPF programs
func (o *Observer) setupEBPFEnvironment(objs *bpf.NetworkObjects) error {
	if err := o.attachNetworkProbes(); err != nil {
		return fmt.Errorf("attaching probes: %w", err)
	}

	reader, err := ringbuf.NewReader(objs.NetworkEvents)
	if err != nil {
		o.closeEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}
	o.ebpfState.(*networkEBPF).reader = reader

	if err := o.configureL7Ports(); err != nil {
		o.logger.Warn("Failed to configure L7 ports", zap.Error(err))
	}
	return nil
}

// startEBPFWorkers starts the background workers for eBPF processing
func (o *Observer) startEBPFWorkers(ebpfState *networkEBPF) {
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(3)
	go o.processNetworkEvents(ctx)
	go o.trackConnections(ctx)
	go o.collectNetworkMetrics(ctx)
}

// stopEBPF detaches and cleans up eBPF programs
func (o *Observer) stopEBPF() {
	o.closeEBPF()
}

// closeEBPF closes eBPF resources
func (o *Observer) closeEBPF() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*networkEBPF)

	// Cancel context
	if ebpfState.cancel != nil {
		ebpfState.cancel()
	}

	// Close reader
	if ebpfState.reader != nil {
		ebpfState.reader.Close()
	}

	// Wait for goroutines
	ebpfState.wg.Wait()

	// Detach probes
	for _, l := range ebpfState.links {
		l.Close()
	}

	// Close objects
	if ebpfState.objs != nil {
		ebpfState.objs.Close()
	}

	// Close K8s enricher
	if ebpfState.k8sEnricher != nil {
		ebpfState.k8sEnricher.Close()
	}

	o.logger.Info("CO-RE eBPF programs closed for Network observer")
	o.ebpfState = nil
}

// processNetworkEvents processes events from eBPF ring buffer
func (o *Observer) processNetworkEvents(ctx context.Context) {
	defer o.ebpfState.(*networkEBPF).wg.Done()

	ebpfState := o.ebpfState.(*networkEBPF)

	o.logger.Info("Started reading network eBPF events")
	defer o.logger.Info("Stopped reading network eBPF events")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := ebpfState.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			o.logger.Debug("Error reading from ring buffer", zap.Error(err))
			continue
		}

		// Parse the raw event - must match C struct exactly
		if len(record.RawSample) < int(unsafe.Sizeof(BPFNetworkEvent{})) {
			o.logger.Warn("Invalid network event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		// Cast to BPF event struct
		bpfEvent := (*BPFNetworkEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Apply sampling
		if o.config.SamplingRate < 1.0 {
			if float64(bpfEvent.PID%100) >= o.config.SamplingRate*100 {
				continue
			}
		}

		// Convert to domain event
		networkEvent := o.convertBPFEvent(bpfEvent)
		domainEvent := o.createDomainEvent(ctx, networkEvent)

		// Enrich with K8s metadata
		if ebpfState.k8sEnricher != nil {
			ebpfState.k8sEnricher.EnrichEvent(domainEvent)
		}

		// Update connection tracking
		o.updateConnectionTracking(networkEvent)

		// Update metrics
		o.updateNetworkMetrics(ctx, networkEvent)

		// Send event
		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
			if ebpfState.eventsProcessed != nil {
				ebpfState.eventsProcessed.Add(ctx, 1)
			}
		} else {
			o.BaseObserver.RecordDrop()
			if ebpfState.eventsDropped != nil {
				ebpfState.eventsDropped.Add(ctx, 1)
			}
		}
	}
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

// attachNetworkProbes attaches eBPF programs to kernel tracepoints
func (o *Observer) attachNetworkProbes() error {
	ebpfState := o.ebpfState.(*networkEBPF)

	// Attach TCP connect probes
	tcpV4Connect, err := link.AttachTracing(link.TracingOptions{
		Program: ebpfState.objs.TraceTcpV4Connect,
	})
	if err != nil {
		return fmt.Errorf("attaching TCP v4 connect probe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, tcpV4Connect)

	tcpV6Connect, err := link.AttachTracing(link.TracingOptions{
		Program: ebpfState.objs.TraceTcpV6Connect,
	})
	if err != nil {
		return fmt.Errorf("attaching TCP v6 connect probe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, tcpV6Connect)

	// Attach TCP sendmsg probe
	tcpSendmsg, err := link.AttachTracing(link.TracingOptions{
		Program: ebpfState.objs.TraceTcpSendmsg,
	})
	if err != nil {
		return fmt.Errorf("attaching TCP sendmsg probe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, tcpSendmsg)

	// Attach UDP sendmsg probe
	udpSendmsg, err := link.AttachTracing(link.TracingOptions{
		Program: ebpfState.objs.TraceUdpSendmsg,
	})
	if err != nil {
		return fmt.Errorf("attaching UDP sendmsg probe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, udpSendmsg)

	o.logger.Info("Network probes attached successfully")
	return nil
}

// configureL7Ports configures L7 protocol detection ports
func (o *Observer) configureL7Ports() error {
	ebpfState := o.ebpfState.(*networkEBPF)

	// Configure HTTP ports
	for _, port := range o.config.HTTPPorts {
		key := uint16(port)
		val := uint8(1) // L7_PROTOCOL_HTTP
		if err := ebpfState.objs.L7PortMap.Put(key, val); err != nil {
			return fmt.Errorf("configuring HTTP port %d: %w", port, err)
		}
	}

	// Configure DNS port
	dnsKey := uint16(o.config.DNSPort)
	dnsVal := uint8(3) // L7_PROTOCOL_DNS
	if err := ebpfState.objs.L7PortMap.Put(dnsKey, dnsVal); err != nil {
		return fmt.Errorf("configuring DNS port: %w", err)
	}

	return nil
}

// convertBPFEvent converts raw BPF event to NetworkEvent
func (o *Observer) convertBPFEvent(bpfEvent *BPFNetworkEvent) *NetworkEvent {
	// Extract IP addresses based on version
	var srcIP, dstIP net.IP
	if bpfEvent.IPVersion == IPVersion6 {
		srcIP = net.IP(bpfEvent.SrcAddr[:])
		dstIP = net.IP(bpfEvent.DstAddr[:])
	} else {
		srcIP = net.IPv4(bpfEvent.SrcAddr[0], bpfEvent.SrcAddr[1],
			bpfEvent.SrcAddr[2], bpfEvent.SrcAddr[3])
		dstIP = net.IPv4(bpfEvent.DstAddr[0], bpfEvent.DstAddr[1],
			bpfEvent.DstAddr[2], bpfEvent.DstAddr[3])
	}

	return &NetworkEvent{
		EventID:     fmt.Sprintf("net-%d-%d", bpfEvent.PID, bpfEvent.Timestamp),
		Timestamp:   time.Unix(0, int64(bpfEvent.Timestamp)),
		EventType:   GetEventTypeName(bpfEvent.EventType),
		PID:         bpfEvent.PID,
		TID:         bpfEvent.TID,
		UID:         bpfEvent.UID,
		GID:         bpfEvent.GID,
		Command:     bytesToString(bpfEvent.Comm[:]),
		Protocol:    GetProtocolName(bpfEvent.Protocol),
		IPVersion:   bpfEvent.IPVersion,
		SrcIP:       srcIP,
		DstIP:       dstIP,
		SrcPort:     bpfEvent.SrcPort,
		DstPort:     bpfEvent.DstPort,
		Direction:   GetDirectionName(bpfEvent.Direction),
		ConnState:   GetConnStateName(bpfEvent.ConnState),
		BytesSent:   bpfEvent.BytesSent,
		BytesRecv:   bpfEvent.BytesRecv,
		PacketsSent: bpfEvent.PacketsSent,
		PacketsRecv: bpfEvent.PacketsRecv,
		Latency:     time.Duration(bpfEvent.LatencyNs),
		Duration:    time.Duration(bpfEvent.DurationNs),
		L7Protocol:  GetL7ProtocolName(bpfEvent.L7Protocol),
		CgroupID:    bpfEvent.CgroupID,
		PodUID:      bytesToString(bpfEvent.PodUID[:]),
		InterfaceID: bpfEvent.IfIndex,
	}
}

// createDomainEvent creates a domain event from network event
func (o *Observer) createDomainEvent(ctx context.Context, netEvent *NetworkEvent) *domain.CollectorEvent {
	return &domain.CollectorEvent{
		EventID:   netEvent.EventID,
		Timestamp: netEvent.Timestamp,
		Type:      "network",
		Source:    o.name,
		Severity:  "info",
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType:   netEvent.EventType,
				Protocol:    netEvent.Protocol,
				SrcIP:       netEvent.SrcIP.String(),
				DstIP:       netEvent.DstIP.String(),
				SrcPort:     int32(netEvent.SrcPort),
				DstPort:     int32(netEvent.DstPort),
				PayloadSize: int64(netEvent.BytesSent + netEvent.BytesRecv),
				Direction:   netEvent.Direction,
				L7Protocol:  netEvent.L7Protocol,
			},
			Process: &domain.ProcessData{
				PID:     int32(netEvent.PID),
				TID:     int32(netEvent.TID),
				Command: netEvent.Command,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"protocol":  netEvent.Protocol,
				"direction": netEvent.Direction,
				"pod_uid":   netEvent.PodUID,
			},
		},
	}
}

// updateConnectionTracking updates connection state tracking
func (o *Observer) updateConnectionTracking(event *NetworkEvent) {
	ebpfState := o.ebpfState.(*networkEBPF)

	flowKey := event.GetFlowKey()
	connKey := fmt.Sprintf("%s:%d->%s:%d",
		flowKey.SrcIP, flowKey.SrcPort,
		flowKey.DstIP, flowKey.DstPort)

	ebpfState.connectionsMutex.Lock()
	defer ebpfState.connectionsMutex.Unlock()

	conn, exists := ebpfState.connections[connKey]
	if !exists {
		conn = &ConnectionInfo{
			FlowKey:      flowKey,
			State:        event.ConnState,
			StartTime:    event.Timestamp,
			LastActivity: event.Timestamp,
			ProcessInfo: &ProcessInfo{
				PID:     event.PID,
				TID:     event.TID,
				UID:     event.UID,
				GID:     event.GID,
				Command: event.Command,
			},
		}
		ebpfState.connections[connKey] = conn
	}

	// Update metrics
	conn.BytesSent = event.BytesSent
	conn.BytesRecv = event.BytesRecv
	conn.PacketsSent = event.PacketsSent
	conn.PacketsRecv = event.PacketsRecv
	conn.LastActivity = event.Timestamp
	conn.State = event.ConnState
	conn.L7Protocol = event.L7Protocol
}

// trackConnections periodically cleans up stale connections
func (o *Observer) trackConnections(ctx context.Context) {
	defer o.ebpfState.(*networkEBPF).wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.cleanupStaleConnections()
		}
	}
}

// cleanupStaleConnections removes inactive connections
func (o *Observer) cleanupStaleConnections() {
	ebpfState := o.ebpfState.(*networkEBPF)
	now := time.Now()
	staleTimeout := 5 * time.Minute

	ebpfState.connectionsMutex.Lock()
	defer ebpfState.connectionsMutex.Unlock()

	for key, conn := range ebpfState.connections {
		if now.Sub(conn.LastActivity) > staleTimeout {
			delete(ebpfState.connections, key)
		}
	}

	o.logger.Debug("Connection cleanup completed",
		zap.Int("active_connections", len(ebpfState.connections)))
}

// collectNetworkMetrics collects and exports network metrics
func (o *Observer) collectNetworkMetrics(ctx context.Context) {
	defer o.ebpfState.(*networkEBPF).wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.exportNetworkMetrics(ctx)
		}
	}
}

// exportNetworkMetrics exports current network metrics
func (o *Observer) exportNetworkMetrics(ctx context.Context) {
	ebpfState := o.ebpfState.(*networkEBPF)

	ebpfState.connectionsMutex.RLock()
	activeConns := len(ebpfState.connections)
	ebpfState.connectionsMutex.RUnlock()

	if ebpfState.connectionsTracked != nil {
		ebpfState.connectionsTracked.Add(ctx, int64(activeConns))
	}
}

// updateNetworkMetrics updates network-specific metrics
func (o *Observer) updateNetworkMetrics(ctx context.Context, event *NetworkEvent) {
	ebpfState := o.ebpfState.(*networkEBPF)

	// Update bytes transferred
	if ebpfState.bytesProcessed != nil {
		totalBytes := event.BytesSent + event.BytesRecv
		ebpfState.bytesProcessed.Add(ctx, int64(totalBytes))
	}

	// Update L7 metrics
	if event.L7Protocol != "" && ebpfState.l7EventsParsed != nil {
		ebpfState.l7EventsParsed.Add(ctx, 1)
	}

	// Record processing time
	if ebpfState.processingTime != nil && event.Latency > 0 {
		ebpfState.processingTime.Record(ctx, float64(event.Latency.Milliseconds()))
	}
}
