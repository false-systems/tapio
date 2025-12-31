//go:build linux

package network

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// DNSProcessor detects DNS queries and responses (UDP port 53)
type DNSProcessor struct {
	// Future: Add OTEL metrics (dns_queries_total, dns_timeouts_total)
	// Future: Add query tracking for timeout detection
}

// NewDNSProcessor creates a new DNS query/response detector
func NewDNSProcessor() *DNSProcessor {
	return &DNSProcessor{}
}

// Process checks if event is DNS-related and extracts DNS data
func (p *DNSProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
	// Only process UDP traffic (DNS over TCP not implemented yet)
	if evt.Protocol != IPPROTO_UDP {
		return nil
	}

	// Check if either port is 53 (DNS)
	isDNSQuery := evt.DstPort == 53
	isDNSResponse := evt.SrcPort == 53

	if !isDNSQuery && !isDNSResponse {
		return nil // Not DNS
	}

	// Determine subtype
	subtype := "dns_query"
	if isDNSResponse {
		subtype = "dns_response"
	}

	return p.createDNSEvent(evt, subtype)
}

// createDNSEvent creates a domain event for DNS traffic
func (p *DNSProcessor) createDNSEvent(evt NetworkEventBPF, subtype string) *domain.ObserverEvent {
	// Convert IP addresses
	var srcIP, dstIP string
	if evt.Family == AF_INET {
		srcIP = convertIPv4(evt.SrcIP)
		dstIP = convertIPv4(evt.DstIP)
	} else {
		srcIP = convertIPv6(evt.SrcIPv6)
		dstIP = convertIPv6(evt.DstIPv6)
	}

	// Populate existing domain.NetworkEventData fields
	netData := &domain.NetworkEventData{
		Protocol: "DNS",
		SrcIP:    srcIP,
		DstIP:    dstIP,
		SrcPort:  evt.SrcPort,
		DstPort:  evt.DstPort,
		// Future: Parse DNS query from payload
		// DNSQuery:        parseDNSQuery(evt),
		// DNSResponseTime: calculateResponseTime(evt),
	}

	return &domain.ObserverEvent{
		ID:          uuid.New().String(),
		Type:        string(domain.EventTypeNetwork),
		Subtype:     subtype,
		Source:      "network",
		Timestamp:   time.Now(),
		NetworkData: netData,
	}
}
