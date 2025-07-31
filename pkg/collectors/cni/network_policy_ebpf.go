//go:build linux
// +build linux

package cni

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I./bpf -I./bpf/headers" -target amd64,arm64 -type policy_event,policy_rule,pod_metadata networkPolicy ./bpf/network_policy.c -- -I./bpf -I./bpf/headers

// NetworkPolicyCollector enhances CNI collector with eBPF-based policy monitoring
type NetworkPolicyCollector struct {
	*Collector

	// eBPF objects
	objs   *networkPolicyObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Metrics
	metrics PolicyMetrics
}

// PolicyMetrics tracks network policy enforcement
type PolicyMetrics struct {
	PacketsAllowed uint64
	PacketsDropped uint64
	PacketsLogged  uint64
	PolicyMatches  uint64
	PolicyMisses   uint64
}

// PolicyEvent represents a network policy decision
type PolicyEvent struct {
	Timestamp  time.Time
	SourceIP   net.IP
	DestIP     net.IP
	SourcePort uint16
	DestPort   uint16
	Protocol   string
	Action     string
	Direction  string
	PodName    string
	Namespace  string
	PolicyName string
	CNIPlugin  string
}

// EnhanceWithNetworkPolicy adds network policy monitoring to CNI collector
func (c *Collector) EnhanceWithNetworkPolicy() error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &networkPolicyObjects{}
	if err := loadNetworkPolicyObjects(objs, nil); err != nil {
		return fmt.Errorf("loading network policy objects: %w", err)
	}

	// Create enhanced collector
	npc := &NetworkPolicyCollector{
		Collector: c,
		objs:      objs,
		links:     make([]link.Link, 0),
	}

	// Attach programs based on detected CNI
	if err := npc.attachPrograms(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching programs: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.PolicyEvents)
	if err != nil {
		npc.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	npc.reader = reader

	// Replace the simple eBPF reader with our enhanced one
	c.ebpfReader = reader
	c.ebpfCollection = objs

	// Start enhanced event collection
	c.wg.Add(1)
	go npc.collectPolicyEvents()

	return nil
}

// attachPrograms attaches eBPF programs based on CNI type
func (npc *NetworkPolicyCollector) attachPrograms() error {
	switch npc.detectedCNI {
	case "calico":
		// Calico uses TC (Traffic Control) for policy enforcement
		// In production, would attach to actual interfaces
		// For now, we'll attach to kprobes as demonstration

		// Attach to netfilter hook (used by Calico)
		l, err := link.Kprobe("nf_hook_slow", npc.objs.KprobeNfHookSlow, nil)
		if err != nil {
			return fmt.Errorf("attaching nf_hook_slow kprobe: %w", err)
		}
		npc.links = append(npc.links, l)

	case "cilium":
		// Cilium uses XDP for high-performance policy enforcement
		// Would attach XDP program to network interfaces
		// For demonstration, using kprobe

		l, err := link.Kprobe("nf_hook_slow", npc.objs.KprobeNfHookSlow, nil)
		if err != nil {
			return fmt.Errorf("attaching cilium kprobe: %w", err)
		}
		npc.links = append(npc.links, l)

	case "flannel":
		// Flannel typically uses iptables
		l, err := link.Kprobe("nf_hook_slow", npc.objs.KprobeNfHookSlow, nil)
		if err != nil {
			return fmt.Errorf("attaching flannel kprobe: %w", err)
		}
		npc.links = append(npc.links, l)

	default:
		// Generic CNI - monitor iptables
		l, err := link.Kprobe("nf_hook_slow", npc.objs.KprobeNfHookSlow, nil)
		if err != nil {
			return fmt.Errorf("attaching generic kprobe: %w", err)
		}
		npc.links = append(npc.links, l)
	}

	return nil
}

// collectPolicyEvents reads policy events from ring buffer
func (npc *NetworkPolicyCollector) collectPolicyEvents() {
	defer npc.wg.Done()

	for {
		record, err := npc.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			continue
		}

		// Parse event
		event, err := npc.parseEvent(record.RawSample)
		if err != nil {
			continue
		}

		// Update metrics
		npc.updateMetrics(event)

		// Convert to RawEvent
		data, _ := json.Marshal(event)

		rawEvent := collectors.RawEvent{
			Timestamp: event.Timestamp,
			Type:      "cni_policy",
			Data:      data,
			Metadata: map[string]string{
				"source":      "ebpf",
				"cni_plugin":  npc.detectedCNI,
				"action":      event.Action,
				"direction":   event.Direction,
				"policy_name": event.PolicyName,
			},
		}

		select {
		case npc.events <- rawEvent:
		case <-npc.ctx.Done():
			return
		default:
			// Buffer full
		}
	}
}

// parseEvent parses raw eBPF event data
func (npc *NetworkPolicyCollector) parseEvent(data []byte) (*PolicyEvent, error) {
	if len(data) < 152 { // Size of policy_event struct
		return nil, fmt.Errorf("event data too small")
	}

	var raw networkPolicyPolicyEvent
	buf := bytes.NewReader(data)
	if err := binary.Read(buf, binary.LittleEndian, &raw); err != nil {
		return nil, err
	}

	event := &PolicyEvent{
		Timestamp:  time.Unix(0, int64(raw.Timestamp)),
		SourceIP:   intToIP(raw.SrcIp),
		DestIP:     intToIP(raw.DstIp),
		SourcePort: raw.SrcPort,
		DestPort:   raw.DstPort,
		Protocol:   protocolToString(raw.Protocol),
		Action:     actionToString(raw.Action),
		Direction:  directionToString(raw.Direction),
		PodName:    nullTerminatedString(raw.PodName[:]),
		Namespace:  nullTerminatedString(raw.Namespace[:]),
		PolicyName: nullTerminatedString(raw.PolicyName[:]),
		CNIPlugin:  npc.detectedCNI,
	}

	return event, nil
}

// updateMetrics updates policy enforcement metrics
func (npc *NetworkPolicyCollector) updateMetrics(event *PolicyEvent) {
	npc.mu.Lock()
	defer npc.mu.Unlock()

	switch event.Action {
	case "allow":
		npc.metrics.PacketsAllowed++
	case "drop":
		npc.metrics.PacketsDropped++
	case "log":
		npc.metrics.PacketsLogged++
	}

	if event.PolicyName != "" {
		npc.metrics.PolicyMatches++
	} else {
		npc.metrics.PolicyMisses++
	}
}

// GetPolicyMetrics returns current policy metrics
func (npc *NetworkPolicyCollector) GetPolicyMetrics() PolicyMetrics {
	npc.mu.RLock()
	defer npc.mu.RUnlock()
	return npc.metrics
}

// cleanup releases eBPF resources
func (npc *NetworkPolicyCollector) cleanup() {
	// Close links
	for _, l := range npc.links {
		if l != nil {
			l.Close()
		}
	}

	// Close reader
	if npc.reader != nil {
		npc.reader.Close()
	}

	// Close objects
	if npc.objs != nil {
		npc.objs.Close()
	}
}

// Helper functions

func intToIP(ip uint32) net.IP {
	return net.IPv4(
		byte(ip),
		byte(ip>>8),
		byte(ip>>16),
		byte(ip>>24),
	)
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
		return fmt.Sprintf("Unknown(%d)", proto)
	}
}

func actionToString(action uint8) string {
	switch action {
	case 1:
		return "allow"
	case 2:
		return "drop"
	case 3:
		return "log"
	default:
		return "unknown"
	}
}

func directionToString(dir uint8) string {
	if dir == 0 {
		return "ingress"
	}
	return "egress"
}

func nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// UpdatePodMetadata updates the pod metadata map
func (npc *NetworkPolicyCollector) UpdatePodMetadata(podName, namespace string, ip net.IP) error {
	if npc.objs == nil || npc.objs.PodMetadataMap == nil {
		return fmt.Errorf("eBPF objects not initialized")
	}

	ipInt := binary.BigEndian.Uint32(ip.To4())

	metadata := networkPolicyPodMetadata{}
	copy(metadata.PodName[:], podName)
	copy(metadata.Namespace[:], namespace)
	metadata.Ip = ipInt

	return npc.objs.PodMetadataMap.Update(ipInt, metadata, ebpf.UpdateAny)
}

// AddPolicyRule adds a network policy rule to the eBPF map
func (npc *NetworkPolicyCollector) AddPolicyRule(rule PolicyRule) error {
	if npc.objs == nil || npc.objs.ActivePolicies == nil {
		return fmt.Errorf("eBPF objects not initialized")
	}

	bpfRule := networkPolicyPolicyRule{
		PolicyId: rule.ID,
		SrcCidr:  ipToUint32(rule.SourceCIDR),
		SrcMask:  ipToUint32(rule.SourceMask),
		DstCidr:  ipToUint32(rule.DestCIDR),
		DstMask:  ipToUint32(rule.DestMask),
		Port:     rule.Port,
		Protocol: rule.Protocol,
		Action:   rule.Action,
	}
	copy(bpfRule.Name[:], rule.Name)

	return npc.objs.ActivePolicies.Update(rule.ID, bpfRule, ebpf.UpdateAny)
}

// PolicyRule represents a network policy rule
type PolicyRule struct {
	ID         uint32
	Name       string
	SourceCIDR net.IP
	SourceMask net.IP
	DestCIDR   net.IP
	DestMask   net.IP
	Port       uint16
	Protocol   uint8
	Action     uint8
}

func ipToUint32(ip net.IP) uint32 {
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip4)
}

// Temporary structs until generated by bpf2go
type networkPolicyObjects struct {
	PolicyEvents     *ebpf.Map
	ActivePolicies   *ebpf.Map
	PodMetadataMap   *ebpf.Map
	KprobeNfHookSlow *ebpf.Program
}

func (o *networkPolicyObjects) Close() error {
	// TODO: Implement proper cleanup
	return nil
}

type networkPolicyPolicyEvent struct {
	Timestamp  uint64
	SrcIp      uint32
	DstIp      uint32
	SrcPort    uint16
	DstPort    uint16
	Protocol   uint8
	Action     uint8
	Direction  uint8
	EventType  uint8
	PodName    [64]byte
	Namespace  [64]byte
	PolicyName [64]byte
}

type networkPolicyPolicyRule struct {
	PolicyId uint32
	SrcCidr  uint32
	SrcMask  uint32
	DstCidr  uint32
	DstMask  uint32
	Port     uint16
	Protocol uint8
	Action   uint8
	Name     [64]byte
}

type networkPolicyPodMetadata struct {
	PodName   [64]byte
	Namespace [64]byte
	Ip        uint32
}

func loadNetworkPolicyObjects(obj *networkPolicyObjects, opts *ebpf.CollectionOptions) error {
	// TODO: This will be replaced by generated code
	return fmt.Errorf("network policy eBPF objects not yet generated - run go generate")
}
