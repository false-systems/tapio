//go:build linux
// +build linux

package services

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// KernelConnection represents a network connection event from eBPF
type KernelConnection struct {
	SourceIP    uint32
	DestIP      uint32
	SourcePort  uint16
	DestPort    uint16
	Protocol    uint8
	Direction   uint8 // 0=unknown, 1=outbound, 2=inbound
	State       uint8 // TCP state
	BytesSent   uint64
	BytesRecv   uint64
	Latency     uint64 // RTT in nanoseconds
	Retransmits uint32
	Resets      uint32
	Timestamp   uint64
}

// ebpfObjects contains all eBPF objects loaded from the compiled program
type ebpfObjects struct {
	Programs map[string]*ebpf.Program
	Maps     map[string]*ebpf.Map
}

// ebpfStateImpl contains the eBPF programs and maps
type ebpfStateImpl struct {
	objs   *ebpfObjects
	links  []link.Link
	reader *perf.Reader
}

// initializeEBPF initializes eBPF monitoring for connection tracking
func (o *Observer) initializeEBPF(ctx context.Context) error {
	o.logger.Info("Initializing eBPF connection tracking")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// For now, we'll create placeholder eBPF state
	// In production, this would load actual eBPF programs
	state := &ebpfStateImpl{
		objs: &ebpfObjects{
			Programs: make(map[string]*ebpf.Program),
			Maps:     make(map[string]*ebpf.Map),
		},
		links: make([]link.Link, 0),
	}

	o.ebpfState = state

	// Start eBPF event processor
	o.LifecycleManager.Start("ebpf-processor", func() {
		o.processEBPFEvents(ctx)
	})

	o.logger.Info("eBPF connection tracking initialized")
	return nil
}

// cleanupEBPF cleans up eBPF resources
func (o *Observer) cleanupEBPF() {
	if o.ebpfState == nil {
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
		return
	}

	o.logger.Info("Cleaning up eBPF resources")

	// Close reader if exists
	if state.reader != nil {
		state.reader.Close()
	}

	// Detach all links
	for _, link := range state.links {
		link.Close()
	}

	// Close all programs
	for name, prog := range state.objs.Programs {
		o.logger.Debug("Closing eBPF program", zap.String("program", name))
		prog.Close()
	}

	// Close all maps
	for name, m := range state.objs.Maps {
		o.logger.Debug("Closing eBPF map", zap.String("map", name))
		m.Close()
	}

	o.ebpfState = nil
	o.logger.Info("eBPF resources cleaned up")
}

// processEBPFEvents processes connection events from eBPF
func (o *Observer) processEBPFEvents(ctx context.Context) {
	if o.ebpfState == nil {
		o.logger.Warn("eBPF state not initialized")
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok || state.reader == nil {
		// In production, we would have a real perf reader
		// For now, simulate with a ticker
		o.simulateConnectionEvents(ctx)
		return
	}

	o.logger.Info("Starting eBPF event processor")

	for {
		select {
		case <-ctx.Done():
			o.logger.Info("eBPF event processor stopped")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			o.logger.Error("Failed to read eBPF event", zap.Error(err))
			if o.errorsTotal != nil {
				o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_read_failed"),
				))
			}
			continue
		}

		// Parse kernel event
		if len(record.RawSample) < int(unsafe.Sizeof(KernelConnection{})) {
			o.logger.Warn("Invalid eBPF event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*KernelConnection)(unsafe.Pointer(&record.RawSample[0]))
		o.processConnectionEvent(ctx, event)
	}
}

// simulateConnectionEvents simulates connection events for testing
func (o *Observer) simulateConnectionEvents(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	o.logger.Info("Running in simulation mode (no real eBPF events)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Simulate discovering connections between services
			o.mu.RLock()
			serviceCount := len(o.services)
			o.mu.RUnlock()

			if serviceCount > 1 {
				// Create simulated connections
				o.simulateServiceConnections(ctx)
			}
		}
	}
}

// simulateServiceConnections creates simulated connections for testing
func (o *Observer) simulateServiceConnections(ctx context.Context) {
	o.mu.RLock()
	services := make([]*Service, 0, len(o.services))
	for _, svc := range o.services {
		services = append(services, svc)
	}
	o.mu.RUnlock()

	if len(services) < 2 {
		return
	}

	// Simulate a connection from first to second service
	src := services[0]
	dst := services[1]

	if len(src.Endpoints) > 0 && len(dst.Endpoints) > 0 {
		srcEp := src.Endpoints[0]
		dstEp := dst.Endpoints[0]

		// Create simulated kernel connection
		conn := &KernelConnection{
			SourceIP:    ipToUint32(srcEp.IP),
			DestIP:      ipToUint32(dstEp.IP),
			SourcePort:  uint16(srcEp.Port),
			DestPort:    uint16(dstEp.Port),
			Protocol:    6, // TCP
			Direction:   1, // Outbound
			State:       2, // Established
			BytesSent:   1024,
			BytesRecv:   2048,
			Latency:     5000000, // 5ms
			Retransmits: 0,
			Resets:      0,
			Timestamp:   uint64(time.Now().UnixNano()),
		}

		o.processConnectionEvent(ctx, conn)
		o.logger.Debug("Simulated connection",
			zap.String("source", src.Name),
			zap.String("dest", dst.Name))
	}
}

// processConnectionEvent processes a single connection event from eBPF
func (o *Observer) processConnectionEvent(ctx context.Context, event *KernelConnection) {
	// Record eBPF event metric
	if o.ebpfEvents != nil {
		o.ebpfEvents.Add(ctx, 1)
	}

	// Convert kernel event to domain connection
	conn := &Connection{
		SourceIP:    event.SourceIP,
		DestIP:      event.DestIP,
		SourcePort:  event.SourcePort,
		DestPort:    event.DestPort,
		Protocol:    event.Protocol,
		Direction:   ConnDirection(event.Direction),
		State:       ConnState(event.State),
		Timestamp:   time.Unix(0, int64(event.Timestamp)),
		BytesSent:   event.BytesSent,
		BytesRecv:   event.BytesRecv,
		Latency:     event.Latency,
		Retransmits: event.Retransmits,
		Resets:      event.Resets,
	}

	// Calculate connection quality
	conn.Quality = conn.CalculateQuality()

	// Store connection
	connKey := fmt.Sprintf("%s:%d->%s:%d",
		uint32ToIP(conn.SourceIP), conn.SourcePort,
		uint32ToIP(conn.DestIP), conn.DestPort)

	o.mu.Lock()
	o.connections[connKey] = conn
	o.mu.Unlock()

	// Record connection metric
	if o.connectionsTracked != nil {
		o.connectionsTracked.Add(ctx, 1, metric.WithAttributes(
			attribute.String("protocol", protocolToString(conn.Protocol)),
			attribute.String("state", stateToString(conn.State)),
		))
	}

	// Try to correlate with services
	o.correlateConnectionWithServices(ctx, conn)
}

// correlateConnectionWithServices correlates a connection with known services
func (o *Observer) correlateConnectionWithServices(ctx context.Context, conn *Connection) {
	srcIP := uint32ToIP(conn.SourceIP)
	dstIP := uint32ToIP(conn.DestIP)

	o.mu.RLock()
	srcServices := o.ipToService[srcIP]
	dstServices := o.ipToService[dstIP]
	o.mu.RUnlock()

	if len(srcServices) == 0 || len(dstServices) == 0 {
		// Can't correlate without service info
		return
	}

	// For simplicity, use first matching service
	srcService := srcServices[0]
	dstService := dstServices[0]

	o.mu.Lock()
	defer o.mu.Unlock()

	src, srcExists := o.services[srcService]
	dst, dstExists := o.services[dstService]

	if !srcExists || !dstExists {
		return
	}

	// Update dependency from source to destination
	if src.Dependencies == nil {
		src.Dependencies = make(map[string]*Dependency)
	}

	dep, exists := src.Dependencies[dstService]
	if !exists {
		dep = &Dependency{
			Target:    dstService,
			Protocol:  protocolToString(conn.Protocol),
			FirstSeen: conn.Timestamp,
		}
		src.Dependencies[dstService] = dep

		// Record new dependency
		if o.dependenciesDetected != nil {
			o.dependenciesDetected.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source", srcService),
				attribute.String("target", dstService),
			))
		}

		// Track as significant change
		o.pendingChanges <- ChangeEvent{
			Type:      ChangeNewDependency,
			Service:   srcService,
			Target:    dstService,
			Timestamp: time.Now(),
		}
	}

	// Update dependency metrics
	dep.LastSeen = conn.Timestamp
	dep.CallRate++ // Simplified - in production would calculate rate

	// Update latency stats (simplified)
	if conn.Latency > 0 {
		latencyMs := float64(conn.Latency) / 1000000.0
		dep.Latency.P50 = latencyMs // Simplified - would need proper percentile calculation
		dep.Latency.P95 = latencyMs * 1.5
		dep.Latency.P99 = latencyMs * 2
		dep.Latency.Max = latencyMs * 3
	}

	// Update dependent on destination
	if dst.Dependents == nil {
		dst.Dependents = make(map[string]*Dependent)
	}

	dependent, exists := dst.Dependents[srcService]
	if !exists {
		dependent = &Dependent{
			Source:    srcService,
			FirstSeen: conn.Timestamp,
		}
		dst.Dependents[srcService] = dependent
	}

	dependent.LastSeen = conn.Timestamp
	dependent.CallRate++ // Simplified

	o.logger.Debug("Correlated connection with services",
		zap.String("source", srcService),
		zap.String("dest", dstService),
		zap.Float64("quality", conn.Quality))
}

// Helper functions

func ipToUint32(ip string) uint32 {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return 0
	}
	if v4 := parsed.To4(); v4 != nil {
		return binary.BigEndian.Uint32(v4)
	}
	return 0
}

func uint32ToIP(ip uint32) string {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, ip)
	return net.IP(bytes).String()
}

func protocolToString(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("PROTO_%d", proto)
	}
}

func stateToString(state ConnState) string {
	switch state {
	case StateSynSent:
		return "SYN_SENT"
	case StateEstablished:
		return "ESTABLISHED"
	case StateFinWait:
		return "FIN_WAIT"
	case StateClosed:
		return "CLOSED"
	case StateReset:
		return "RESET"
	default:
		return "UNKNOWN"
	}
}
