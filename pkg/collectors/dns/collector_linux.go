//go:build linux
// +build linux

package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/dns/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 dnsMonitor ./bpf_src/dns_monitor.c -- -I../bpf_common

// BPFDNSEvent matches the C struct dns_event exactly for proper memory alignment
type BPFDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64

	EventType uint8
	Protocol  uint8
	IPVersion uint8
	Pad1      uint8

	// Source address (IPv4 or IPv6)
	SrcAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	// Destination address (IPv4 or IPv6)
	DstAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	SrcPort uint16
	DstPort uint16

	// DNS info
	DNSID    uint16
	DNSFlags uint16
	Opcode   uint8
	Rcode    uint8
	QType    uint16

	DataLen   uint32
	LatencyNs uint32

	QueryName [128]byte // MAX_DNS_NAME_LEN from C
	Data      [512]byte // MAX_DNS_DATA from C
}

// eBPF components - Linux-specific
type ebpfState struct {
	objs   *bpf.DnsMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "dns.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := bpf.DnsMonitorObjects{}
	if err := bpf.LoadDnsMonitorObjects(&objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: &objs}

	// Dynamically detect network interfaces and attach XDP
	interfaces, err := c.getActiveNetworkInterfaces()
	if err != nil {
		c.logger.Warn("Failed to get network interfaces, trying defaults",
			zap.Error(err))
		interfaces = []string{"eth0", "ens33", "enp0s3", "wlan0", "docker0", "br0"}
	}

	var attachedLinks []link.Link
	var attachedInterfaces []string

	for _, iface := range interfaces {
		// Try to attach XDP program to each interface
		// Using AttachXDP with interface index for better compatibility
		iface_obj, err := net.InterfaceByName(iface)
		if err != nil {
			c.logger.Debug("Failed to get interface object",
				zap.String("interface", iface),
				zap.Error(err))
			continue
		}

		xdpLink, err := link.AttachXDP(link.XDPOptions{
			Interface: iface_obj.Index,
			Program:   objs.XdpDnsMonitor,
			Flags:     0, // Let kernel choose the best mode
		})
		if err != nil {
			c.logger.Debug("Failed to attach XDP to interface",
				zap.String("interface", iface),
				zap.Error(err))
			continue
		}
		attachedLinks = append(attachedLinks, xdpLink)
		attachedInterfaces = append(attachedInterfaces, iface)
	}

	if len(attachedLinks) == 0 {
		objs.Close()
		return fmt.Errorf("failed to attach XDP DNS monitor to any interface")
	}

	c.ebpfState.(*ebpfState).reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		udpLink.Close()
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.ebpfState.(*ebpfState).links = attachedLinks

	c.logger.Info("DNS eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(attachedLinks)),
		zap.Strings("interfaces", attachedInterfaces),
	)

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, link := range state.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.logger.Info("DNS eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events - Linux only
func (c *Collector) readEBPFEvents() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	if state.reader == nil {
		return
	}

	ctx := c.ctx
	for {
		select {
		case <-ctx.Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the eBPF event
			if len(record.RawSample) < int(unsafe.Sizeof(BPFDNSEvent{})) {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				continue
			}

			var bpfEvent BPFDNSEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &bpfEvent); err != nil {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to parse BPF DNS event", zap.Error(err))
				continue
			}

			// Extract and validate DNS query information
			queryName := c.extractQueryName(bpfEvent.QueryName[:])
			if queryName == "" {
				// Skip empty queries
				continue
			}

			// Convert BPF event to DNS event with query name
			event := c.convertBPFEventToDNSEvent(&bpfEvent)
			event.QueryName = queryName

			// Log interesting DNS queries for debugging
			if c.logger.Core().Enabled(zap.DebugLevel) {
				c.logger.Debug("DNS query captured",
					zap.String("query", queryName),
					zap.Uint16("qtype", bpfEvent.QType),
					zap.String("protocol", c.getProtocolName(bpfEvent.Protocol)),
					zap.Uint32("pid", bpfEvent.PID),
				)
			}

			// Convert to raw event
			rawEvent := c.convertToRawEvent(event)

			// Send to event channel
			select {
			case c.events <- rawEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(ctx, 1)
				}
			case <-ctx.Done():
				return
			default:
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
			}
		}
	}
}

// getActiveNetworkInterfaces returns a list of active network interfaces
func (c *Collector) getActiveNetworkInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("getting network interfaces: %w", err)
	}

	var activeInterfaces []string
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Check if interface has any addresses
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}

		activeInterfaces = append(activeInterfaces, iface.Name)
	}

	return activeInterfaces, nil
}

// convertBPFEventToDNSEvent converts BPF event structure to DNSEvent
func (c *Collector) convertBPFEventToDNSEvent(bpfEvent *BPFDNSEvent) *DNSEvent {
	event := &DNSEvent{
		Timestamp:  time.Unix(0, int64(bpfEvent.Timestamp)),
		QueryID:    uint32(bpfEvent.DNSID),
		PID:        bpfEvent.PID,
		TID:        bpfEvent.TID,
		CgroupID:   bpfEvent.CgroupID,
		ClientPort: bpfEvent.SrcPort,
		ServerPort: bpfEvent.DstPort,
		LatencyMs:  bpfEvent.LatencyNs / 1000000, // Convert ns to ms
	}

	// Parse event type
	switch bpfEvent.EventType {
	case 1: // DNS_EVENT_QUERY
		event.EventType = DNSEventTypeQuery
	case 2: // DNS_EVENT_RESPONSE
		event.EventType = DNSEventTypeResponse
		event.ResponseCode = DNSResponseCode(bpfEvent.Rcode)
		event.Success = bpfEvent.Rcode == 0
	case 3: // DNS_EVENT_TIMEOUT
		event.EventType = DNSEventTypeTimeout
	case 4: // DNS_EVENT_ERROR
		event.EventType = DNSEventTypeError
	}

	// Parse protocol
	if bpfEvent.Protocol == 17 { // IPPROTO_UDP
		event.Protocol = DNSProtocolUDP
	} else if bpfEvent.Protocol == 6 { // IPPROTO_TCP
		event.Protocol = DNSProtocolTCP
	}

	// Parse query type
	switch bpfEvent.QType {
	case 1:
		event.QueryType = DNSQueryTypeA
	case 28:
		event.QueryType = DNSQueryTypeAAAA
	case 5:
		event.QueryType = DNSQueryTypeCNAME
	case 15:
		event.QueryType = DNSQueryTypeMX
	case 2:
		event.QueryType = DNSQueryTypeNS
	case 12:
		event.QueryType = DNSQueryTypePTR
	case 6:
		event.QueryType = DNSQueryTypeSOA
	case 16:
		event.QueryType = DNSQueryTypeTXT
	case 33:
		event.QueryType = DNSQueryTypeSRV
	}

	// Parse IP addresses based on version
	if bpfEvent.IPVersion == 4 {
		// IPv4 addresses are stored in first 4 bytes
		srcIP := net.IP(bpfEvent.SrcAddr[:4])
		dstIP := net.IP(bpfEvent.DstAddr[:4])
		event.ClientIP = srcIP.String()
		event.ServerIP = dstIP.String()
	} else if bpfEvent.IPVersion == 6 {
		// IPv6 uses all 16 bytes
		srcIP := net.IP(bpfEvent.SrcAddr[:])
		dstIP := net.IP(bpfEvent.DstAddr[:])
		event.ClientIP = srcIP.String()
		event.ServerIP = dstIP.String()
	}

	// Parse query name (null-terminated string)
	queryNameEnd := bytes.IndexByte(bpfEvent.QueryName[:], 0)
	if queryNameEnd > 0 {
		event.QueryName = string(bpfEvent.QueryName[:queryNameEnd])
	}

	// Extract container ID from cgroup path if available
	if bpfEvent.CgroupID != 0 {
		// Container ID extraction would go here based on cgroup ID
		// This requires additional mapping which can be implemented later
	}

	return event
}

// convertToRawEvent converts DNS event to raw event format - Linux only
func (c *Collector) convertToRawEvent(event *DNSEvent) domain.RawEvent {
	// Convert the DNS event to bytes for transmission
	eventBytes := (*[unsafe.Sizeof(*event)]byte)(unsafe.Pointer(event))[:]
	dataCopy := make([]byte, len(eventBytes))
	copy(dataCopy, eventBytes)

	return domain.RawEvent{
		Timestamp: event.Timestamp,
		Source:    c.name,
		Data:      dataCopy,
	}
}
