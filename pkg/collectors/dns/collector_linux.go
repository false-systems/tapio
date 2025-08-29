//go:build linux
// +build linux

package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/dns/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

// eBPFState concrete type for Linux eBPF state (NO interface{})
type eBPFState struct {
	objs   *bpf.DnsmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
	mu     sync.RWMutex
}

// getEBPFState returns the eBPF state with proper type safety
func (c *Collector) getEBPFState() *eBPFState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.ebpfState == nil {
		return nil
	}
	return c.ebpfState
}

// setEBPFState sets the eBPF state with proper type safety
func (c *Collector) setEBPFState(state *eBPFState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ebpfState = state
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "dns.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		span.SetAttributes(attribute.Bool("ebpf.supported", false))
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "memlock_removal_failed"),
			))
		}
		span.RecordError(err)
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := &bpf.DnsmonitorObjects{}
	if err := bpf.LoadDnsmonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		span.RecordError(err)
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create state object
	state := &eBPFState{
		objs: objs,
	}

	// Dynamically detect network interfaces and attach XDP
	interfaces, err := c.getActiveNetworkInterfaces()
	if err != nil {
		c.logger.Warn("Failed to get network interfaces, trying defaults",
			zap.Error(err))
		interfaces = []string{"eth0", "ens33", "enp0s3", "wlan0", "docker0", "br0"}
	}

	var attachedLinks []link.Link
	var attachedInterfaces []string

	// Clean up function for partial attachments
	cleanup := func() {
		for _, l := range attachedLinks {
			if l != nil {
				l.Close()
			}
		}
		objs.Close()
	}

	for _, iface := range interfaces {
		// Try to attach tracepoint programs instead of XDP for now
		// XDP requires specific network interface support
		c.logger.Debug("Skipping XDP attachment for now",
			zap.String("interface", iface))
		continue
	}

	if len(attachedLinks) == 0 {
		c.logger.Warn("No DNS tracepoints attached, monitoring may have limited functionality")
		// Don't fail if no tracepoints are attached - we can still work in test mode
	}

	// Create ring buffer reader for DnsEvents map
	if objs.DnsEvents != nil {
		state.reader, err = ringbuf.NewReader(objs.DnsEvents)
		if err != nil {
			cleanup() // Clean up all attachments on error
			return fmt.Errorf("creating ring buffer reader: %w", err)
		}
	} else {
		c.logger.Warn("DnsEvents map not found, continuing without ring buffer")
	}

	// Try to attach tracepoints for DNS monitoring
	if objs.TraceDnsRecvfrom != nil {
		recvLink, err := link.Tracepoint("syscalls", "sys_enter_recvfrom", objs.TraceDnsRecvfrom, nil)
		if err != nil {
			c.logger.Debug("Failed to attach recvfrom tracepoint", zap.Error(err))
		} else {
			attachedLinks = append(attachedLinks, recvLink)
			attachedInterfaces = append(attachedInterfaces, "tracepoint:recvfrom")
		}
	}

	if objs.TraceDnsSendto != nil {
		sendLink, err := link.Tracepoint("syscalls", "sys_enter_sendto", objs.TraceDnsSendto, nil)
		if err != nil {
			c.logger.Debug("Failed to attach sendto tracepoint", zap.Error(err))
		} else {
			attachedLinks = append(attachedLinks, sendLink)
			attachedInterfaces = append(attachedInterfaces, "tracepoint:sendto")
		}
	}

	state.links = attachedLinks
	c.setEBPFState(state)

	// Set span attributes for successful start
	span.SetAttributes(
		attribute.Bool("ebpf.attached", true),
		attribute.Int("interfaces.count", len(attachedInterfaces)),
		attribute.StringSlice("interfaces.names", attachedInterfaces),
	)

	c.logger.Info("DNS eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Strings("attachments", attachedInterfaces),
		zap.Bool("ring_buffer_enabled", state.reader != nil))

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *Collector) stopEBPF() {
	state := c.getEBPFState()
	if state == nil {
		return
	}

	// Close ring buffer reader first
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all XDP attachments
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.setEBPFState(nil)
	c.logger.Info("DNS eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events - Linux only
func (c *Collector) readEBPFEvents() {
	state := c.getEBPFState()
	if state == nil || state.reader == nil {
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
				if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
					return
				}

				// Rate-limited error logging
				c.handleReadError(ctx, err)

				// Add delay to prevent busy-waiting on persistent errors
				time.Sleep(50 * time.Millisecond)
				continue
			}

			// Reset error counter on successful read
			c.resetErrorCounter()

			// Process the event
			if err := c.processBPFEvent(ctx, record.RawSample); err != nil {
				c.logger.Debug("Failed to process BPF event", zap.Error(err))
			}
		}
	}
}

// processBPFEvent processes a single BPF event
func (c *Collector) processBPFEvent(ctx context.Context, data []byte) error {
	// Start span for event processing
	ctx, span := c.tracer.Start(ctx, "dns.process_event")
	defer span.End()

	startTime := time.Now()
	defer func() {
		if c.processingTime != nil {
			duration := time.Since(startTime).Seconds() * 1000
			c.processingTime.Record(ctx, duration)
		}
	}()

	// Validate event size
	expectedSize := int(unsafe.Sizeof(BPFDNSEvent{}))
	if len(data) < expectedSize {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "invalid_event_size"),
			))
		}
		return fmt.Errorf("event data too small: got %d bytes, expected %d", len(data), expectedSize)
	}

	// Parse the BPF event safely
	var bpfEvent BPFDNSEvent
	reader := bytes.NewReader(data[:expectedSize])
	if err := binary.Read(reader, binary.LittleEndian, &bpfEvent); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "parse_error"),
			))
		}
		span.RecordError(err)
		return fmt.Errorf("failed to parse BPF DNS event: %w", err)
	}

	// Extract and validate DNS query information
	queryName := c.extractQueryName(bpfEvent.QueryName[:])
	if queryName == "" {
		// Skip empty queries
		return nil
	}

	// Convert to domain event
	event := c.convertToDomainEvent(&bpfEvent, queryName)

	// Set span attributes
	span.SetAttributes(
		attribute.String("dns.query", queryName),
		attribute.Int("dns.qtype", int(bpfEvent.QType)),
		attribute.String("dns.protocol", c.getProtocolName(bpfEvent.Protocol)),
		attribute.Int("dns.pid", int(bpfEvent.PID)),
		attribute.Int64("dns.cgroup_id", int64(bpfEvent.CgroupID)),
	)

	// Update buffer usage gauge
	if c.bufferUsage != nil {
		c.bufferUsage.Record(ctx, int64(len(c.events)))
	}

	// Send to event channel with non-blocking send
	select {
	case c.events <- event:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("query_type", c.getQueryTypeName(bpfEvent.QType)),
				attribute.String("protocol", c.getProtocolName(bpfEvent.Protocol)),
				attribute.String("response_code", c.getRcodeName(bpfEvent.Rcode)),
			))
		}
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Buffer full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
		c.handleDroppedEvent(ctx, queryName)
	}

	return nil
}

// convertToDomainEvent converts BPF event to domain CollectorEvent with intelligent processing
func (c *Collector) convertToDomainEvent(bpfEvent *BPFDNSEvent, queryName string) *domain.CollectorEvent {
	timestamp := time.Unix(0, int64(bpfEvent.Timestamp))

	// Determine event type and severity
	eventType := domain.EventTypeDNS
	severity := domain.EventSeverityInfo

	if bpfEvent.Rcode != 0 {
		severity = domain.EventSeverityWarning
		if bpfEvent.Rcode == 3 { // NXDOMAIN
			severity = domain.EventSeverityMedium
		}
	}

	// Convert IP addresses
	srcIP := c.extractIP(bpfEvent.SrcAddr[:], bpfEvent.IPVersion)
	dstIP := c.extractIP(bpfEvent.DstAddr[:], bpfEvent.IPVersion)

	// Extract container ID from cgroup ID
	containerID := c.extractContainerID(bpfEvent.CgroupID)

	// Parse resolved IPs from DNS response data if available
	resolvedIPs := c.parseResolvedIPs(bpfEvent)

	// Extract namespace and service information from container context
	namespace, serviceName := c.extractKubernetesContext(bpfEvent.CgroupID, containerID)

	// DNS cache metrics removed for simplicity

	// Build comprehensive event data
	dnsData := &domain.DNSData{
		QueryName:    queryName,
		QueryType:    c.getQueryTypeName(bpfEvent.QType),
		ResponseCode: int(bpfEvent.Rcode),
		Answers:      resolvedIPs,
		Duration:     time.Duration(bpfEvent.LatencyNs),
		ClientIP:     srcIP,
		ServerIP:     dstIP,
		ClientPort:   bpfEvent.SrcPort,
		ServerPort:   bpfEvent.DstPort,
		Error:        bpfEvent.Rcode != 0,
	}

	// Enhanced metadata with container and Kubernetes context
	attributes := map[string]string{
		"query_name":    queryName,
		"query_type":    c.getQueryTypeName(bpfEvent.QType),
		"response_code": c.getRcodeName(bpfEvent.Rcode),
		"protocol":      c.getProtocolName(bpfEvent.Protocol),
		"client_ip":     srcIP,
		"server_ip":     dstIP,
		"cgroup_id":     fmt.Sprintf("%d", bpfEvent.CgroupID),
		"description": fmt.Sprintf("DNS %s query for %s (%s)",
			c.getQueryTypeName(bpfEvent.QType), queryName, c.getRcodeName(bpfEvent.Rcode)),
	}

	if containerID != "" {
		attributes["container_id"] = containerID
	}
	if namespace != "" {
		attributes["namespace"] = namespace
	}
	if serviceName != "" {
		attributes["service_name"] = serviceName
	}
	if len(resolvedIPs) > 0 {
		attributes["resolved_ips"] = fmt.Sprintf("[%s]", string(bytes.Join([][]byte{}, []byte(", "))))
		for i, ip := range resolvedIPs {
			if i < 3 { // Limit to first 3 IPs
				key := fmt.Sprintf("resolved_ip_%d", i+1)
				attributes[key] = ip
			}
		}
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("dns-%d-%s-%d", timestamp.UnixNano(), queryName, bpfEvent.PID),
		Type:      eventType,
		Timestamp: timestamp,
		Source:    c.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			DNS: dnsData,
			Process: &domain.ProcessData{
				PID:         int32(bpfEvent.PID),
				TID:         int32(bpfEvent.TID),
				UID:         int32(bpfEvent.UID),
				GID:         int32(bpfEvent.GID),
				CgroupPath:  c.getCgroupPath(bpfEvent.CgroupID),
				ContainerID: containerID,
			},
			Container: &domain.ContainerData{
				ContainerID: containerID,
				Labels: map[string]string{
					"io.kubernetes.pod.namespace":  namespace,
					"io.kubernetes.container.name": serviceName,
				},
			},
		},
		Metadata: domain.EventMetadata{
			Priority: c.calculateEventPriority(bpfEvent),
			CgroupID: bpfEvent.CgroupID,
			Tags: []string{
				fmt.Sprintf("protocol:%s", c.getProtocolName(bpfEvent.Protocol)),
				fmt.Sprintf("rcode:%s", c.getRcodeName(bpfEvent.Rcode)),
				fmt.Sprintf("qtype:%s", c.getQueryTypeName(bpfEvent.QType)),
			},
			Attributes: attributes,
			TraceID:    fmt.Sprintf("dns-%d", bpfEvent.DNSID),
		},
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

// extractIP extracts IP address from byte array
func (c *Collector) extractIP(addr []byte, ipVersion uint8) string {
	if ipVersion == 4 && len(addr) >= 4 {
		return fmt.Sprintf("%d.%d.%d.%d", addr[0], addr[1], addr[2], addr[3])
	} else if ipVersion == 6 && len(addr) >= 16 {
		return net.IP(addr[:16]).String()
	}
	return ""
}

// getProtocolName returns protocol name
func (c *Collector) getProtocolName(protocol uint8) string {
	switch protocol {
	case 17:
		return "UDP"
	case 6:
		return "TCP"
	default:
		return fmt.Sprintf("UNKNOWN_%d", protocol)
	}
}

// getQueryTypeName returns DNS query type name
func (c *Collector) getQueryTypeName(qtype uint16) string {
	switch qtype {
	case 1:
		return "A"
	case 28:
		return "AAAA"
	case 5:
		return "CNAME"
	case 15:
		return "MX"
	case 2:
		return "NS"
	case 12:
		return "PTR"
	case 6:
		return "SOA"
	case 16:
		return "TXT"
	case 33:
		return "SRV"
	default:
		return fmt.Sprintf("TYPE%d", qtype)
	}
}

// getRcodeName returns DNS response code name
func (c *Collector) getRcodeName(rcode uint8) string {
	switch rcode {
	case 0:
		return "NOERROR"
	case 1:
		return "FORMERR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 4:
		return "NOTIMP"
	case 5:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE%d", rcode)
	}
}

// parseResolvedIPs extracts resolved IP addresses from DNS response data
func (c *Collector) parseResolvedIPs(bpfEvent *BPFDNSEvent) []string {
	if bpfEvent.EventType != 2 || bpfEvent.Rcode != 0 { // Not a successful response
		return nil
	}

	// Check if the eBPF marked answers as available
	if len(bpfEvent.Data) > 9 && string(bpfEvent.Data[:9]) == "[ANSWERS]" {
		// For production, we would implement full DNS answer parsing here
		// This is a placeholder showing the structure
		return c.extractAnswersFromDNSData(bpfEvent.Data[9:], int(bpfEvent.DataLen-9))
	}

	return nil
}

// extractAnswersFromDNSData parses DNS answer section to extract IPs
func (c *Collector) extractAnswersFromDNSData(data []byte, length int) []string {
	// Simplified DNS answer parsing - production would be more comprehensive
	var resolvedIPs []string

	// Basic validation
	if length < 12 { // Minimum DNS header size
		return resolvedIPs
	}

	// This is a simplified extraction - real DNS parsing is complex
	// In production, this would properly parse DNS answer records

	// For now, return placeholder to show structure
	// Real implementation would parse DNS records properly
	return resolvedIPs
}

// extractTTLFromData extracts TTL from DNS response data
func (c *Collector) extractTTLFromData(bpfEvent *BPFDNSEvent) uint32 {
	if bpfEvent.EventType != 2 || bpfEvent.Rcode != 0 {
		return 0
	}

	// In production, this would parse the DNS answer section properly
	// For now, return a default value
	return 300 // 5 minutes default
}

// extractKubernetesContext extracts namespace and service from container context
func (c *Collector) extractKubernetesContext(cgroupID uint64, containerID string) (namespace, serviceName string) {
	if cgroupID == 0 || containerID == "" {
		return "", ""
	}

	// Try to extract from cgroup path patterns
	cgroupPath := c.getCgroupPath(cgroupID)
	if cgroupPath == "" {
		return "", ""
	}

	// Extract pod UID from cgroup path
	podUID := c.extractPodUID(cgroupPath)
	if podUID == "" {
		return "", ""
	}

	// In production, this would query Kubernetes API or maintain a cache
	// to map pod UID to namespace/service
	// For now, try to extract from cgroup patterns

	// Example Kubernetes cgroup patterns:
	// /kubepods/burstable/pod12345678-1234-1234-1234-123456789012/containerid
	parts := strings.Split(cgroupPath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "pod") && len(part) > 39 {
			// This is a pod UID, try to find namespace from path context
			// In real implementation, you'd have a pod->namespace cache
			if i > 0 {
				// Look for namespace hints in path
				for j := i - 1; j >= 0; j-- {
					if strings.Contains(parts[j], "namespace") {
						namespace = strings.TrimPrefix(parts[j], "namespace-")
						break
					}
				}
			}
			break
		}
	}

	// Placeholder - in production you'd maintain a service->pod mapping
	return namespace, serviceName
}
