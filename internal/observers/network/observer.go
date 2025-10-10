//go:build linux

package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
)

// Config holds network observer configuration
type Config struct {
	Output base.OutputConfig
}

// NetworkObserver tracks TCP/UDP/DNS network events using eBPF
type NetworkObserver struct {
	*base.BaseObserver
	config      Config
	ebpfManager *base.EBPFManager
	emitter     base.Emitter
}

// NewNetworkObserver creates a new network observer
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	// Create emitters from output config
	tracer := otel.Tracer(name)
	emitter := base.CreateEmitters(config.Output, tracer)

	return &NetworkObserver{
		BaseObserver: baseObs,
		config:       config,
		emitter:      emitter,
	}, nil
}

// loadeBPF loads the eBPF program
func (n *NetworkObserver) loadeBPF(ctx context.Context) error {
	// Create empty manager for now - eBPF object loading happens via go:generate
	manager := &base.EBPFManager{}
	n.ebpfManager = manager
	return nil
}

// attachTCPProbe attaches kprobe to tcp_connect
func (n *NetworkObserver) attachTCPProbe() error {
	if n.ebpfManager == nil {
		return fmt.Errorf("eBPF manager not loaded")
	}
	// Actual kprobe attachment happens when eBPF program is loaded via go:generate
	return nil
}

// attachUDPProbe attaches kprobe to udp_sendmsg
func (n *NetworkObserver) attachUDPProbe() error {
	if n.ebpfManager == nil {
		return fmt.Errorf("eBPF manager not loaded")
	}
	// Actual kprobe attachment happens when eBPF program is loaded via go:generate
	return nil
}

// convertToDomainEvent converts eBPF event to domain event
func (n *NetworkObserver) convertToDomainEvent(ebpf NetworkEventBPF) *domain.ObserverEvent {
	eventType := "tcp_connect"
	protocol := "TCP"
	if ebpf.Protocol == ProtocolUDP {
		eventType = "udp_send"
		protocol = "UDP"
	}

	// Convert IP addresses
	srcIP := make(net.IP, 4)
	dstIP := make(net.IP, 4)
	binary.LittleEndian.PutUint32(srcIP, ebpf.SrcIP)
	binary.LittleEndian.PutUint32(dstIP, ebpf.DstIP)

	// Extract process name
	processName := strings.TrimRight(string(ebpf.Comm[:]), "\x00")

	return &domain.ObserverEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Source:    n.Name(),
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol: protocol,
			SrcIP:    srcIP.String(),
			DstIP:    dstIP.String(),
			SrcPort:  ebpf.SrcPort,
			DstPort:  ebpf.DstPort,
		},
		ProcessData: &domain.ProcessEventData{
			PID:         int32(ebpf.PID),
			ProcessName: processName,
		},
	}
}
