//go:build linux
// +build linux

package network

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// LinkProcessor detects network link failures (SYN timeouts, high retransmit rates)
type LinkProcessor struct {
	// Future: Add OTEL metrics here
}

// NewLinkProcessor creates a new link failure detector
func NewLinkProcessor() *LinkProcessor {
	return &LinkProcessor{}
}

// Process checks if event indicates a link failure and returns domain event
func (p *LinkProcessor) Process(ctx context.Context, evt NetworkEventBPF) *domain.ObserverEvent {
	// Detect SYN timeout: TCP_SYN_SENT → TCP_CLOSE
	if p.isSYNTimeout(evt) {
		return p.createLinkFailureEvent(evt, "syn_timeout")
	}

	return nil // Not a link failure
}

// isSYNTimeout detects connection attempt failures
func (p *LinkProcessor) isSYNTimeout(evt NetworkEventBPF) bool {
	return evt.OldState == TCP_SYN_SENT && evt.NewState == TCP_CLOSE
}

// createLinkFailureEvent creates a domain event for link failures
func (p *LinkProcessor) createLinkFailureEvent(evt NetworkEventBPF, failureType string) *domain.ObserverEvent {
	// Convert IP addresses (handle both IPv4 and IPv6)
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
		Protocol: "TCP",
		SrcIP:    srcIP,
		DstIP:    dstIP,
		SrcPort:  evt.SrcPort,
		DstPort:  evt.DstPort,
		TCPState: tcpStateName(evt.NewState),
	}

	return &domain.ObserverEvent{
		Type:        string(domain.EventTypeNetwork),
		Subtype:     "link_failure",
		NetworkData: netData,
	}
}
