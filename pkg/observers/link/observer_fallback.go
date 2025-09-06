//go:build !linux
// +build !linux

package link

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startEBPF is a no-op on non-Linux platforms
func (o *Observer) startEBPF() error {
	o.logger.Warn("Link observer requires Linux with eBPF support, running in mock mode")
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (o *Observer) stopEBPF() {
	// Nothing to clean up
}

// processEvents generates mock link failure events on non-Linux platforms
func (o *Observer) processEvents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventCount := 0
	failurePatterns := []string{
		"syn_timeout", "arp_timeout", "tcp_reset", "icmp_unreachable",
		"excessive_retransmit", "policy_block",
	}

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			eventCount++

			// Generate a mock link failure event
			pattern := failurePatterns[eventCount%len(failurePatterns)]
			o.generateMockFailure(pattern, eventCount)

			o.logger.Debug("Generated mock link failure",
				zap.String("pattern", pattern),
				zap.Int("count", eventCount))
		}
	}
}

// generateMockFailure creates a mock link failure event
func (o *Observer) generateMockFailure(pattern string, count int) {
	timestamp := time.Now()

	// Generate varying IPs
	srcIP := fmt.Sprintf("10.0.%d.%d", (count/10)%256, count%256)
	dstIP := fmt.Sprintf("10.0.%d.%d", ((count+50)/10)%256, (count+50)%256)
	srcPort := int32(30000 + (count*137)%10000)
	dstPort := int32(80 + (count%10)*443) // Vary between common ports

	switch pattern {
	case "syn_timeout":
		// Simulate SYN timeout
		syn := &SYNAttempt{
			Timestamp: timestamp.Add(-5 * time.Second),
			SrcIP:     srcIP,
			DstIP:     dstIP,
			SrcPort:   srcPort,
			DstPort:   dstPort,
			Retries:   3,
		}
		o.handleSYNTimeout(syn)

	case "arp_timeout":
		// Simulate ARP timeout
		arp := &ARPRequest{
			Timestamp: timestamp.Add(-1 * time.Second),
			SrcIP:     srcIP,
			TargetIP:  dstIP,
			Interface: fmt.Sprintf("eth%d", count%3),
			Retries:   2,
		}
		o.handleARPTimeout(arp)

	case "tcp_reset":
		// Simulate TCP reset
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("mock-link-rst-%d", count),
			Timestamp: timestamp,
			Type:      domain.CollectorEventType("link.tcp_reset"),
			Source:    o.name,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					EventType: "tcp_reset",
					Protocol:  "TCP",
					SrcIP:     srcIP,
					DstIP:     dstIP,
					SrcPort:   srcPort,
					DstPort:   dstPort,
				},
				Custom: map[string]string{
					"failure_type": "connection_reset",
					"layer":        "L4",
					"mock":         "true",
				},
			},
		}
		o.SendEvent(event)

	case "icmp_unreachable":
		// Simulate ICMP unreachable
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("mock-link-icmp-%d", count),
			Timestamp: timestamp,
			Type:      domain.CollectorEventType("link.icmp_unreachable"),
			Source:    o.name,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					EventType: "icmp_unreachable",
					Protocol:  "ICMP",
					SrcIP:     srcIP,
					DstIP:     dstIP,
				},
				Custom: map[string]string{
					"failure_type": "unreachable",
					"layer":        "L3",
					"icmp_type":    fmt.Sprintf("%d", 3+count%5),
					"mock":         "true",
				},
			},
		}
		o.SendEvent(event)

	case "excessive_retransmit":
		// Simulate excessive retransmissions
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("mock-link-retransmit-%d", count),
			Timestamp: timestamp,
			Type:      domain.CollectorEventType("link.excessive_retransmit"),
			Source:    o.name,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					EventType: "excessive_retransmit",
					Protocol:  "TCP",
					SrcIP:     srcIP,
					DstIP:     dstIP,
					SrcPort:   srcPort,
					DstPort:   dstPort,
				},
				Custom: map[string]string{
					"retransmit_count": fmt.Sprintf("%d", 3+rand.Intn(5)),
					"threshold":        fmt.Sprintf("%d", o.config.MaxRetransmits),
					"mock":             "true",
				},
			},
		}
		o.SendEvent(event)

	case "policy_block":
		// Simulate policy block with diagnosis
		failure := &LinkFailure{
			Type:      "syn_timeout",
			Layer:     4,
			Timestamp: timestamp,
			SrcIP:     srcIP,
			DstIP:     dstIP,
			SrcPort:   srcPort,
			DstPort:   dstPort,
		}

		// Send to correlator for mock diagnosis
		o.correlator.AnalyzeFailure(failure)

		// Generate mock diagnosis
		diagnosis := &LinkDiagnosis{
			Pattern:    "NetworkPolicyBlock",
			Confidence: 0.85,
			Severity:   domain.EventSeverityWarning,
			Layer:      4,
			Timestamp:  timestamp,
			Summary:    fmt.Sprintf("Mock: Connection blocked by NetworkPolicy"),
			Details:    fmt.Sprintf("Mock: TCP connection from %s to %s:%d blocked", srcIP, dstIP, dstPort),
			Evidence: []string{
				"Mock: SYN packet sent",
				"Mock: No SYN-ACK received",
				"Mock: NetworkPolicy denies traffic",
			},
			Impact:     fmt.Sprintf("Mock: Cannot connect to %s:%d", dstIP, dstPort),
			Resolution: "Mock: Update NetworkPolicy to allow traffic",
		}
		o.handleDiagnosis(diagnosis)
	}
}
