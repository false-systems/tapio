//go:build linux
// +build linux

package dns

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -Wall -Werror" dnsMonitor ./bpf/dns_monitor.c

// DNSeBPFProgram manages the eBPF DNS monitoring program
type DNSeBPFProgram struct {
	objs   *dnsMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
	logger *zap.Logger

	// Configuration
	slowThresholdMs    float64
	timeoutThresholdMs float64
	coreDNSPort        uint16
	enableTCP          bool
}

// NewDNSeBPFProgram creates a new eBPF DNS monitor
func NewDNSeBPFProgram(logger *zap.Logger) *DNSeBPFProgram {
	return &DNSeBPFProgram{
		logger:             logger,
		slowThresholdMs:    100,  // 100ms is slow
		timeoutThresholdMs: 5000, // 5s is timeout
		coreDNSPort:        9153, // CoreDNS metrics port
		enableTCP:          true,
	}
}

// Load loads the eBPF programs into the kernel
func (p *DNSeBPFProgram) Load() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load pre-compiled eBPF objects
	objs := &dnsMonitorObjects{}
	if err := loadDnsMonitorObjects(objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			p.logger.Error("eBPF verifier error", zap.String("error", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	p.objs = objs

	// Configure the program
	if err := p.updateConfig(); err != nil {
		p.objs.Close()
		return fmt.Errorf("failed to update config: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(p.objs.DnsEvents)
	if err != nil {
		p.objs.Close()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	p.reader = reader

	p.logger.Info("eBPF DNS monitor loaded successfully")
	return nil
}

// Attach attaches the eBPF programs to their hooks
func (p *DNSeBPFProgram) Attach() error {
	// Attach UDP DNS monitoring
	udpSendLink, err := link.KprobeMulti(&link.KprobeMultiOptions{
		Symbols: []string{"udp_sendmsg"},
		Program: p.objs.TraceUdpSendmsg,
	})
	if err != nil {
		return fmt.Errorf("failed to attach UDP send: %w", err)
	}
	p.links = append(p.links, udpSendLink)

	udpRecvLink, err := link.KprobeMulti(&link.KprobeMultiOptions{
		Symbols: []string{"udp_recvmsg"},
		Program: p.objs.TraceUdpRecvmsg,
	})
	if err != nil {
		return fmt.Errorf("failed to attach UDP recv: %w", err)
	}
	p.links = append(p.links, udpRecvLink)

	// Attach TCP DNS monitoring if enabled
	if p.enableTCP {
		tcpLink, err := link.Kprobe("tcp_connect", p.objs.TraceTcpConnect, nil)
		if err != nil {
			// TCP is optional, just log warning
			p.logger.Warn("Failed to attach TCP DNS monitoring", zap.Error(err))
		} else {
			p.links = append(p.links, tcpLink)
		}
	}

	p.logger.Info("eBPF programs attached",
		zap.Int("num_hooks", len(p.links)),
		zap.Bool("tcp_enabled", p.enableTCP))

	return nil
}

// ReadEvents reads DNS events from the ring buffer
func (p *DNSeBPFProgram) ReadEvents() (<-chan *DNSEvent, error) {
	events := make(chan *DNSEvent, 100)

	go func() {
		defer close(events)

		for {
			record, err := p.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					p.logger.Debug("Ring buffer closed")
					return
				}
				p.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the raw event
			if len(record.RawSample) < int(unsafe.Sizeof(kernelDNSEvent{})) {
				p.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
				continue
			}

			// Convert kernel event to Go struct
			kernelEvent := (*kernelDNSEvent)(unsafe.Pointer(&record.RawSample[0]))
			event := p.kernelEventToGo(kernelEvent)

			// Send to channel
			select {
			case events <- event:
				// Event sent
			default:
				// Channel full, drop event
				p.logger.Debug("Event channel full, dropping DNS event")
			}
		}
	}()

	return events, nil
}

// SetCoreDNSPIDs registers PIDs as CoreDNS processes
func (p *DNSeBPFProgram) SetCoreDNSPIDs(pids []uint32) error {
	if p.objs == nil {
		return errors.New("eBPF objects not loaded")
	}

	// Clear existing entries
	var pid uint32
	iter := p.objs.CorednsPids.Iterate()
	for iter.Next(&pid, nil) {
		_ = p.objs.CorednsPids.Delete(pid)
	}

	// Add new PIDs
	for _, pid := range pids {
		val := uint8(1)
		if err := p.objs.CorednsPids.Put(pid, val); err != nil {
			p.logger.Warn("Failed to add CoreDNS PID",
				zap.Uint32("pid", pid),
				zap.Error(err))
		}
	}

	p.logger.Info("CoreDNS PIDs updated", zap.Int("count", len(pids)))
	return nil
}

// GetStats returns DNS monitoring statistics
func (p *DNSeBPFProgram) GetStats() (*DNSStats, error) {
	if p.objs == nil {
		return nil, errors.New("eBPF objects not loaded")
	}

	stats := &DNSStats{
		ActiveQueries: 0,
		EventsDropped: 0,
	}

	// Count active queries
	var key dnsQueryKey
	iter := p.objs.ActiveQueries.Iterate()
	for iter.Next(&key, nil) {
		stats.ActiveQueries++
	}

	return stats, nil
}

// Close cleans up the eBPF program
func (p *DNSeBPFProgram) Close() error {
	// Detach all links
	for _, l := range p.links {
		if err := l.Close(); err != nil {
			p.logger.Warn("Failed to close link", zap.Error(err))
		}
	}
	p.links = nil

	// Close ring buffer reader
	if p.reader != nil {
		if err := p.reader.Close(); err != nil {
			p.logger.Warn("Failed to close ring buffer", zap.Error(err))
		}
		p.reader = nil
	}

	// Close eBPF objects
	if p.objs != nil {
		if err := p.objs.Close(); err != nil {
			p.logger.Warn("Failed to close eBPF objects", zap.Error(err))
		}
		p.objs = nil
	}

	p.logger.Info("eBPF DNS monitor closed")
	return nil
}

// updateConfig updates the eBPF program configuration
func (p *DNSeBPFProgram) updateConfig() error {
	config := dnsConfig{
		SlowThresholdNs: uint64(p.slowThresholdMs * 1_000_000),
		TimeoutNs:       uint64(p.timeoutThresholdMs * 1_000_000),
		CoreDNSPort:     p.coreDNSPort,
		EnableTCP:       1,
		EnableK8s:       1,
	}

	if !p.enableTCP {
		config.EnableTCP = 0
	}

	key := uint32(0)
	if err := p.objs.Config.Put(key, config); err != nil {
		return fmt.Errorf("failed to update config map: %w", err)
	}

	return nil
}

// kernelEventToGo converts kernel DNS event to Go struct
func (p *DNSeBPFProgram) kernelEventToGo(ke *kernelDNSEvent) *DNSEvent {
	event := &DNSEvent{
		Timestamp:    ke.TimestampNs,
		LatencyNs:    ke.LatencyNs,
		PID:          ke.PID,
		TID:          ke.TID,
		UID:          ke.UID,
		GID:          ke.GID,
		ProblemType:  DNSProblemType(ke.ProblemType),
		ResponseCode: ke.ResponseCode,
		Retries:      ke.Retries,
	}

	// Convert byte arrays to strings
	event.QueryName = bytesToString(ke.QueryName[:])
	event.Comm = bytesToString(ke.Comm[:])

	// Parse server IP
	if ke.ServerIP[0] != 0 {
		// Simplified - check if IPv4 or IPv6
		if ke.ServerIP[12] == 0 && ke.ServerIP[13] == 0 &&
			ke.ServerIP[14] == 0 && ke.ServerIP[15] == 0 {
			// IPv4
			event.ServerIP = ke.ServerIP[0:4]
		} else {
			// IPv6
			event.ServerIP = ke.ServerIP[:]
		}
	}

	return event
}

// bytesToString converts null-terminated byte array to string
func bytesToString(b []byte) [253]byte {
	var result [253]byte
	for i, v := range b {
		if v == 0 || i >= 253 {
			break
		}
		result[i] = v
	}
	return result
}

// Kernel structures (must match C definitions)
type kernelDNSEvent struct {
	TimestampNs     uint64
	LatencyNs       uint64
	PID             uint32
	TID             uint32
	QueryID         uint16
	QueryType       uint16
	ProblemType     uint8
	ResponseCode    uint8
	QueryName       [253]byte
	ServerIP        [16]byte
	Comm            [16]byte
	Protocol        uint8
	IsCoreDNS       uint8
	TCPFlags        uint16
	K8sService      [128]byte
	K8sNamespace    [64]byte
	CorednsCacheHit uint32
	UID             uint32
	GID             uint32
	Retries         uint8
	_               [3]byte // Padding
}

type dnsQueryKey struct {
	PID      uint32
	QueryID  uint16
	Protocol uint8
	_        uint8 // Padding
}

type dnsConfig struct {
	SlowThresholdNs uint64
	TimeoutNs       uint64
	CoreDNSPort     uint16
	EnableTCP       uint8
	EnableK8s       uint8
	_               [4]byte // Padding
}

// DNSStats holds DNS monitoring statistics
type DNSStats struct {
	ActiveQueries uint64
	EventsDropped uint64
	EventsTotal   uint64
}

// CheckKernelSupport checks if the kernel supports our eBPF features
func CheckKernelSupport() error {
	// Check if we're running on Linux
	if _, err := os.Stat("/proc/sys/kernel/osrelease"); err != nil {
		return errors.New("not running on Linux")
	}

	// Check kernel version (need at least 5.8 for ringbuf)
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fmt.Errorf("failed to read kernel version: %w", err)
	}

	version := string(bytes.TrimSpace(data))
	p.logger.Info("Kernel version", zap.String("version", version))

	// Try to remove memlock (requires CAP_SYS_ADMIN)
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("insufficient privileges (need CAP_SYS_ADMIN): %w", err)
	}

	return nil
}
