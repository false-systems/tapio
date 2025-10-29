//go:build linux
// +build linux

package network

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// StatusProcessor monitors HTTP/HTTPS connections (TCP port 80/443)
// Note: HTTP status code parsing requires payload inspection (future: kprobe on tcp_recvmsg)
type StatusProcessor struct {
	// Future: Add OTEL metrics (http_connections_total, http_5xx_total, http_4xx_total)
	// Future: Add payload parsing for status codes
}

// NewStatusProcessor creates a new HTTP/HTTPS connection monitor
func NewStatusProcessor() *StatusProcessor {
	return &StatusProcessor{}
}

// Process checks if event is HTTP/HTTPS-related and extracts connection data
func (p *StatusProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
	// Only process TCP traffic
	if evt.Protocol != IPPROTO_TCP {
		return nil
	}

	// Only track ESTABLISHED connections (successful HTTP handshake)
	if evt.NewState != TCP_ESTABLISHED {
		return nil
	}

	// Check if either port is 80 (HTTP) or 443 (HTTPS)
	isHTTP := evt.DstPort == 80 || evt.SrcPort == 80
	isHTTPS := evt.DstPort == 443 || evt.SrcPort == 443

	if !isHTTP && !isHTTPS {
		return nil // Not HTTP/HTTPS
	}

	// Determine protocol and subtype
	protocol := "HTTP"
	subtype := "http_connection"
	if isHTTPS {
		protocol = "HTTPS"
		subtype = "https_connection"
	}

	return p.createHTTPEvent(evt, protocol, subtype)
}

// createHTTPEvent creates a domain event for HTTP/HTTPS connections
func (p *StatusProcessor) createHTTPEvent(evt NetworkEventBPF, protocol, subtype string) *domain.ObserverEvent {
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
		Protocol: protocol,
		SrcIP:    srcIP,
		DstIP:    dstIP,
		SrcPort:  evt.SrcPort,
		DstPort:  evt.DstPort,
		TCPState: tcpStateName(evt.NewState),
		// Future: Parse HTTP from payload (requires kprobe on tcp_recvmsg)
		// HTTPMethod:     parseHTTPMethod(payload),
		// HTTPPath:       parseHTTPPath(payload),
		// HTTPStatusCode: parseHTTPStatus(payload),
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
