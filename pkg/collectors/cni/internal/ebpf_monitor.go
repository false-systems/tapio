//go:build linux
// +build linux

package internal

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

// eBPF program to monitor CNI network operations
const ebpfProgram = `
#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/if_packet.h>
#include <linux/ip.h>
#include <linux/in.h>
#include <linux/tcp.h>
#include <linux/udp.h>

struct cni_event {
    __u64 timestamp;
    __u32 pid;
    __u32 netns;
    __u32 ifindex;
    __u8  operation; // 0=create, 1=delete, 2=modify
    __u8  family;    // AF_INET or AF_INET6
    __u8  proto;     // IPPROTO_TCP, IPPROTO_UDP, etc
    __u8  pad;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    char  comm[16];
    char  ifname[16];
};

BPF_PERF_OUTPUT(events);

// Monitor network namespace operations
SEC("kprobe/create_new_namespaces")
int trace_netns_create(struct pt_regs *ctx)
{
    struct cni_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.operation = 0; // create
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    events.perf_submit(ctx, &event, sizeof(event));
    return 0;
}

// Monitor network interface operations
SEC("kprobe/dev_change_net_namespace")
int trace_dev_change_netns(struct pt_regs *ctx)
{
    struct cni_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.operation = 2; // modify
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    
    events.perf_submit(ctx, &event, sizeof(event));
    return 0;
}

// Monitor veth pair creation (common in container networking)
SEC("kprobe/veth_newlink")
int trace_veth_create(struct pt_regs *ctx)
{
    struct cni_event event = {};
    
    event.timestamp = bpf_ktime_get_ns();
    event.pid = bpf_get_current_pid_tgid() >> 32;
    event.operation = 0; // create
    
    bpf_get_current_comm(&event.comm, sizeof(event.comm));
    bpf_probe_read_str(&event.ifname, sizeof(event.ifname), (void *)PT_REGS_PARM1(ctx));
    
    events.perf_submit(ctx, &event, sizeof(event));
    return 0;
}
`

// CNIEvent represents a kernel-level CNI event
type CNIEvent struct {
	Timestamp uint64
	PID       uint32
	NetNS     uint32
	IfIndex   uint32
	Operation uint8
	Family    uint8
	Proto     uint8
	Pad       uint8
	SrcIP     uint32
	DstIP     uint32
	SrcPort   uint16
	DstPort   uint16
	Comm      [16]byte
	IfName    [16]byte
}

// EBPFMonitor monitors CNI operations at the kernel level
type EBPFMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// eBPF objects
	collection *ebpf.Collection
	perfReader *perf.Reader
	links      []link.Link
}

// NewEBPFMonitor creates a new eBPF-based CNI monitor
func NewEBPFMonitor(config core.Config) (*EBPFMonitor, error) {
	// Check if we can use eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock limit: %w", err)
	}

	monitor := &EBPFMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
		links:     make([]link.Link, 0),
	}
	return monitor, nil
}

func (m *EBPFMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Load eBPF program
	spec, err := ebpf.LoadCollectionSpecFromReader(strings.NewReader(ebpfProgram))
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("failed to create eBPF collection: %w", err)
	}
	m.collection = coll

	// Attach kprobes
	if err := m.attachProbes(); err != nil {
		m.collection.Close()
		return fmt.Errorf("failed to attach probes: %w", err)
	}

	// Open perf event reader
	perfMap, ok := m.collection.Maps["events"]
	if !ok {
		return fmt.Errorf("events map not found in eBPF program")
	}

	reader, err := perf.NewReader(perfMap, 4096)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	m.perfReader = reader

	// Start event processing
	m.wg.Add(1)
	go m.processEvents()

	return nil
}

func (m *EBPFMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	// Detach probes
	for _, l := range m.links {
		l.Close()
	}

	// Close perf reader
	if m.perfReader != nil {
		m.perfReader.Close()
	}

	// Close eBPF collection
	if m.collection != nil {
		m.collection.Close()
	}

	// Wait for goroutines
	m.wg.Wait()

	close(m.eventChan)
	return nil
}

func (m *EBPFMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *EBPFMonitor) MonitorType() string {
	return "ebpf"
}

func (m *EBPFMonitor) attachProbes() error {
	// Attach to network namespace creation
	prog, ok := m.collection.Programs["trace_netns_create"]
	if ok {
		l, err := link.Kprobe("create_new_namespaces", prog, nil)
		if err == nil {
			m.links = append(m.links, l)
		}
	}

	// Attach to device namespace changes
	prog, ok = m.collection.Programs["trace_dev_change_netns"]
	if ok {
		l, err := link.Kprobe("dev_change_net_namespace", prog, nil)
		if err == nil {
			m.links = append(m.links, l)
		}
	}

	// Attach to veth creation
	prog, ok = m.collection.Programs["trace_veth_create"]
	if ok {
		l, err := link.Kprobe("veth_newlink", prog, nil)
		if err == nil {
			m.links = append(m.links, l)
		}
	}

	if len(m.links) == 0 {
		return fmt.Errorf("failed to attach any probes")
	}

	return nil
}

func (m *EBPFMonitor) processEvents() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		record, err := m.perfReader.Read()
		if err != nil {
			if err == perf.ErrClosed {
				return
			}
			continue
		}

		// Parse the event
		if len(record.RawSample) < int(unsafe.Sizeof(CNIEvent{})) {
			continue
		}

		var event CNIEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			continue
		}

		// Convert to CNI raw event
		cniEvent := m.convertToCNIEvent(&event)
		if cniEvent != nil {
			select {
			case m.eventChan <- *cniEvent:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *EBPFMonitor) convertToCNIEvent(event *CNIEvent) *core.CNIRawEvent {
	operation := m.getOperation(event.Operation)

	// Extract command name
	comm := string(event.Comm[:])
	if idx := strings.IndexByte(comm, 0); idx >= 0 {
		comm = comm[:idx]
	}

	// Extract interface name
	ifname := string(event.IfName[:])
	if idx := strings.IndexByte(ifname, 0); idx >= 0 {
		ifname = ifname[:idx]
	}

	// Determine if this is a CNI-related operation
	if !m.isCNIRelated(comm, ifname) {
		return nil
	}

	annotations := map[string]string{
		"pid":       fmt.Sprintf("%d", event.PID),
		"command":   comm,
		"interface": ifname,
	}

	// Add network information if available
	if event.SrcIP != 0 {
		annotations["src_ip"] = m.uint32ToIP(event.SrcIP)
	}
	if event.DstIP != 0 {
		annotations["dst_ip"] = m.uint32ToIP(event.DstIP)
	}
	if event.SrcPort != 0 {
		annotations["src_port"] = fmt.Sprintf("%d", event.SrcPort)
	}
	if event.DstPort != 0 {
		annotations["dst_port"] = fmt.Sprintf("%d", event.DstPort)
	}
	if event.NetNS != 0 {
		annotations["netns"] = fmt.Sprintf("%d", event.NetNS)
	}
	if event.IfIndex != 0 {
		annotations["ifindex"] = fmt.Sprintf("%d", event.IfIndex)
	}

	return &core.CNIRawEvent{
		ID:          fmt.Sprintf("ebpf_%d_%d", event.PID, event.Timestamp),
		Timestamp:   time.Unix(0, int64(event.Timestamp)),
		Source:      "ebpf",
		Operation:   operation,
		Success:     true, // Kernel operations are assumed successful if they're traced
		PluginName:  m.detectPlugin(comm, ifname),
		Command:     comm,
		Annotations: annotations,
	}
}

func (m *EBPFMonitor) getOperation(op uint8) core.CNIOperation {
	switch op {
	case 0:
		return core.CNIOperationAdd
	case 1:
		return core.CNIOperationDel
	case 2:
		return core.CNIOperationOther
	default:
		return core.CNIOperationOther
	}
}

func (m *EBPFMonitor) isCNIRelated(comm, ifname string) bool {
	// Check if command is CNI-related
	cniCommands := []string{
		"bridge", "macvlan", "ipvlan", "ptp", "host-local",
		"dhcp", "cilium", "calico", "flannel", "weave",
		"containerd", "dockerd", "crio", "runc",
	}

	lowerComm := strings.ToLower(comm)
	for _, cmd := range cniCommands {
		if strings.Contains(lowerComm, cmd) {
			return true
		}
	}

	// Check if interface name suggests CNI
	if strings.HasPrefix(ifname, "veth") ||
		strings.HasPrefix(ifname, "cni") ||
		strings.HasPrefix(ifname, "docker") ||
		strings.HasPrefix(ifname, "cali") ||
		strings.HasPrefix(ifname, "cilium") {
		return true
	}

	return false
}

func (m *EBPFMonitor) detectPlugin(comm, ifname string) string {
	// Try to detect CNI plugin from command or interface name
	plugins := map[string][]string{
		"cilium":  {"cilium"},
		"calico":  {"calico", "cali"},
		"flannel": {"flannel"},
		"weave":   {"weave"},
		"bridge":  {"bridge"},
		"macvlan": {"macvlan"},
		"ipvlan":  {"ipvlan"},
	}

	lower := strings.ToLower(comm + " " + ifname)
	for plugin, patterns := range plugins {
		for _, pattern := range patterns {
			if strings.Contains(lower, pattern) {
				return plugin
			}
		}
	}

	return "unknown"
}

func (m *EBPFMonitor) uint32ToIP(ip uint32) string {
	return net.IPv4(
		byte(ip),
		byte(ip>>8),
		byte(ip>>16),
		byte(ip>>24),
	).String()
}

// FallbackEBPFMonitor is used when eBPF is not available
type FallbackEBPFMonitor struct {
	*ProcessMonitor
}

// NewFallbackEBPFMonitor creates a fallback monitor when eBPF is not available
func NewFallbackEBPFMonitor(config core.Config) (*FallbackEBPFMonitor, error) {
	processMonitor, err := NewProcessMonitor(config)
	if err != nil {
		return nil, err
	}
	return &FallbackEBPFMonitor{ProcessMonitor: processMonitor}, nil
}

func (m *FallbackEBPFMonitor) MonitorType() string {
	return "ebpf-fallback"
}
