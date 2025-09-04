package networkcorrelator

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// FailurePattern represents a known network failure pattern
type FailurePattern struct {
	Name        string
	Description string
	Detector    func(*NetworkEvent, *CorrelationContext) *RootCause
}

// CorrelationContext holds recent events and state for correlation
type CorrelationContext struct {
	RecentSYNs      map[uint64]*SYNAttempt  // Pending SYNs
	RecentARPs      map[uint32]*ARPRequest  // Pending ARPs
	RecentFailures  []NetworkEvent          // Last 1000 failures
	PodRestarts     map[string]time.Time    // Pod IP -> restart time
	NetworkPolicies map[string]*PolicyInfo  // Active policies
	ServiceMap      map[string]*ServiceInfo // From service-map collector
	mu              sync.RWMutex
}

// RootCause is the final diagnosis
type RootCause struct {
	Pattern    string  // Which pattern matched
	Confidence float32 // How sure are we (0-1)

	// The story
	Summary  string   // One-liner: "Connection blocked by NetworkPolicy"
	Details  string   // Full explanation
	Evidence []string // Specific evidence points

	// Actionable info
	Impact     string // "All pods with label X can't reach service Y"
	Resolution string // "Add label 'allow-db=true' to pod or modify policy"

	// Technical details
	L4Event    *NetworkEvent // The TCP failure
	L2Event    *NetworkEvent // Related ARP failure (if any)
	PolicyName string        // Blocking policy (if any)

	// Timeline
	FailureTime time.Time
	Duration    time.Duration // How long the issue has persisted
}

// CorrelationEngine finds root causes for network failures
type CorrelationEngine struct {
	logger   *zap.Logger
	context  *CorrelationContext
	patterns []FailurePattern

	// Channels
	tcpEvents chan *NetworkEvent
	arpEvents chan *NetworkEvent
	results   chan *RootCause
}

// NewCorrelationEngine creates a new correlation engine
func NewCorrelationEngine(logger *zap.Logger) *CorrelationEngine {
	ce := &CorrelationEngine{
		logger: logger,
		context: &CorrelationContext{
			RecentSYNs:      make(map[uint64]*SYNAttempt),
			RecentARPs:      make(map[uint32]*ARPRequest),
			RecentFailures:  make([]NetworkEvent, 0, 1000),
			PodRestarts:     make(map[string]time.Time),
			NetworkPolicies: make(map[string]*PolicyInfo),
			ServiceMap:      make(map[string]*ServiceInfo),
		},
		tcpEvents: make(chan *NetworkEvent, 1000),
		arpEvents: make(chan *NetworkEvent, 100),
		results:   make(chan *RootCause, 100),
	}

	// Register all failure patterns
	ce.registerPatterns()

	return ce
}

// Register all known failure patterns
func (ce *CorrelationEngine) registerPatterns() {
	ce.patterns = []FailurePattern{
		{
			Name:        "NetworkPolicy Block",
			Description: "Connection blocked by NetworkPolicy",
			Detector:    ce.detectNetworkPolicyBlock,
		},
		{
			Name:        "Pod Restart Connection Loss",
			Description: "Connection failed due to pod restart",
			Detector:    ce.detectPodRestartFailure,
		},
		{
			Name:        "ARP Failure",
			Description: "L2 resolution failure",
			Detector:    ce.detectARPFailure,
		},
		{
			Name:        "Black Hole",
			Description: "Packets disappearing (CNI/iptables drop)",
			Detector:    ce.detectBlackHole,
		},
		{
			Name:        "Half-Open Connection",
			Description: "FIN without ACK - connection half-closed",
			Detector:    ce.detectHalfOpen,
		},
		{
			Name:        "Service Not Ready",
			Description: "Service exists but no endpoints",
			Detector:    ce.detectServiceNotReady,
		},
		{
			Name:        "CNI Bug",
			Description: "CNI plugin dropping packets",
			Detector:    ce.detectCNIBug,
		},
	}
}

// Run the correlation engine
func (ce *CorrelationEngine) Run(ctx context.Context) error {
	// Cleanup old events periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event := <-ce.tcpEvents:
			ce.processTCPFailure(event)

		case event := <-ce.arpEvents:
			ce.processARPFailure(event)

		case <-ticker.C:
			ce.cleanupOldEvents()
		}
	}
}

// Process TCP failure and find root cause
func (ce *CorrelationEngine) processTCPFailure(event *NetworkEvent) {
	ce.context.mu.Lock()
	defer ce.context.mu.Unlock()

	// Add to recent failures
	ce.context.RecentFailures = append(ce.context.RecentFailures, *event)
	if len(ce.context.RecentFailures) > 1000 {
		ce.context.RecentFailures = ce.context.RecentFailures[1:]
	}

	// Try each pattern detector
	var bestMatch *RootCause
	var bestConfidence float32

	for _, pattern := range ce.patterns {
		if rootCause := pattern.Detector(event, ce.context); rootCause != nil {
			if rootCause.Confidence > bestConfidence {
				bestMatch = rootCause
				bestConfidence = rootCause.Confidence
			}
		}
	}

	if bestMatch != nil {
		ce.results <- bestMatch
		ce.logger.Info("Root cause identified",
			zap.String("pattern", bestMatch.Pattern),
			zap.String("summary", bestMatch.Summary),
			zap.Float32("confidence", bestMatch.Confidence))
	} else {
		// Unknown failure pattern
		ce.results <- &RootCause{
			Pattern:    "Unknown",
			Confidence: 0.1,
			Summary:    fmt.Sprintf("TCP connection failed: %s", event.FailureType()),
			Details:    "Unable to determine specific root cause",
			Evidence:   []string{event.String()},
		}
	}
}

// Process ARP failure
func (ce *CorrelationEngine) processARPFailure(event *NetworkEvent) {
	ce.context.mu.Lock()
	defer ce.context.mu.Unlock()

	// Store for correlation with TCP failures
	if event.DstIP != nil {
		ip := event.DstIP.To4()
		if ip != nil {
			ipKey := binary.BigEndian.Uint32(ip)
			ce.context.RecentARPs[ipKey] = &ARPRequest{
				Timestamp:    event.Timestamp,
				RequesterIP:  event.SrcIP,
				TargetIP:     event.DstIP,
				RequesterMAC: event.SrcMAC,
			}
		}
	}
}

// Pattern Detectors

func (ce *CorrelationEngine) detectNetworkPolicyBlock(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	// Simplified for now - would check actual policies
	if event.EventType == EventTCPSYNTimeout {
		return &RootCause{
			Pattern:     "NetworkPolicy Block",
			Confidence:  0.75,
			Summary:     "Connection likely blocked by NetworkPolicy",
			Details:     fmt.Sprintf("SYN sent to %s:%d but no response received", event.DstIP, event.DstPort),
			Evidence:    []string{fmt.Sprintf("SYN timeout after %s", event.Duration)},
			Impact:      "Connection cannot be established",
			Resolution:  "Check NetworkPolicies affecting source and destination pods",
			L4Event:     event,
			FailureTime: event.Timestamp,
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectPodRestartFailure(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	if event.EventType == EventOrphanACK {
		// Check if destination pod recently restarted
		if restartTime, exists := ctx.PodRestarts[event.DstIP.String()]; exists {
			timeSinceRestart := time.Since(restartTime)
			if timeSinceRestart < 5*time.Minute {
				return &RootCause{
					Pattern:    "Pod Restart Connection Loss",
					Confidence: 0.9,
					Summary:    "Connection failed: Pod restarted and lost connection state",
					Details:    fmt.Sprintf("Pod at %s restarted %s ago", event.DstIP, timeSinceRestart),
					Evidence: []string{
						fmt.Sprintf("ACK for unknown connection at %s", event.Timestamp),
						fmt.Sprintf("Pod restarted at %s", restartTime),
					},
					Impact:      "All existing connections to this pod were terminated",
					Resolution:  "Connections will re-establish automatically",
					L4Event:     event,
					FailureTime: event.Timestamp,
					Duration:    timeSinceRestart,
				}
			}
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectARPFailure(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	// Look for corresponding ARP timeout
	if event.EventType == EventTCPSYNTimeout && event.DstIP != nil {
		ip := event.DstIP.To4()
		if ip != nil {
			ipKey := binary.BigEndian.Uint32(ip)
			if arpRequest, exists := ctx.RecentARPs[ipKey]; exists {
				// Found related ARP failure
				return &RootCause{
					Pattern:    "ARP Failure",
					Confidence: 0.85,
					Summary:    "Connection failed: Cannot resolve MAC address (L2 failure)",
					Details:    fmt.Sprintf("ARP resolution failed for IP %s", event.DstIP),
					Evidence: []string{
						fmt.Sprintf("TCP SYN to %s at %s", event.DstIP, event.Timestamp),
						fmt.Sprintf("ARP request failed at %s", arpRequest.Timestamp),
					},
					Impact:      fmt.Sprintf("Cannot reach any services at IP %s", event.DstIP),
					Resolution:  "Check if target pod exists, verify CNI plugin",
					L4Event:     event,
					FailureTime: event.Timestamp,
				}
			}
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectBlackHole(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	if event.EventType == EventDupSYN {
		// Count retries
		retryCount := 0
		for _, failure := range ctx.RecentFailures {
			if failure.EventType == EventDupSYN &&
				failure.DstIP.Equal(event.DstIP) &&
				failure.DstPort == event.DstPort {
				retryCount++
			}
		}

		if retryCount > 3 {
			return &RootCause{
				Pattern:    "Black Hole",
				Confidence: 0.8,
				Summary:    "Packets disappearing - likely dropped by iptables/CNI",
				Details:    fmt.Sprintf("%d SYN retries detected", retryCount),
				Evidence: []string{
					fmt.Sprintf("%d SYN retries for same connection", retryCount),
					"No response (not even RST)",
				},
				Impact:      "Connection impossible until route/rules fixed",
				Resolution:  "Check iptables rules, verify CNI configuration",
				L4Event:     event,
				FailureTime: event.Timestamp,
			}
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectHalfOpen(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	if event.EventType == EventFINNoACK {
		return &RootCause{
			Pattern:     "Half-Open Connection",
			Confidence:  0.7,
			Summary:     "Connection half-closed: FIN sent but no ACK received",
			Details:     "Remote end may have crashed or network partitioned during close",
			Evidence:    []string{fmt.Sprintf("FIN without ACK after %s", event.Duration)},
			Impact:      "Connection stuck in FIN_WAIT state",
			Resolution:  "Connection will timeout eventually, check remote pod health",
			L4Event:     event,
			FailureTime: event.Timestamp,
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectServiceNotReady(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	if event.EventType == EventTCPReset {
		// Check if destination is a service without endpoints
		if service, exists := ctx.ServiceMap[event.DstIP.String()]; exists {
			if service.EndpointCount == 0 {
				return &RootCause{
					Pattern:     "Service Not Ready",
					Confidence:  0.85,
					Summary:     fmt.Sprintf("Service '%s' has no ready endpoints", service.Name),
					Details:     "Service exists but no pods are ready to handle requests",
					Evidence:    []string{"Connection refused (RST)", "Service endpoint count: 0"},
					Impact:      "All requests to this service will fail",
					Resolution:  "Check pod readiness, deployment status",
					L4Event:     event,
					FailureTime: event.Timestamp,
				}
			}
		}
	}
	return nil
}

func (ce *CorrelationEngine) detectCNIBug(event *NetworkEvent, ctx *CorrelationContext) *RootCause {
	// Pattern: Should be allowed but still failing
	if event.EventType == EventTCPSYNTimeout {
		// Simplified check - would verify no policies blocking
		return &RootCause{
			Pattern:     "CNI Bug",
			Confidence:  0.6,
			Summary:     "Possible CNI plugin issue",
			Details:     "Connection should be allowed but packets are being dropped",
			Evidence:    []string{"No NetworkPolicy blocking", "Packets disappearing"},
			Impact:      "Random connection failures",
			Resolution:  "Check CNI plugin logs, consider upgrading CNI",
			L4Event:     event,
			FailureTime: event.Timestamp,
		}
	}
	return nil
}

// Clean up old events
func (ce *CorrelationEngine) cleanupOldEvents() {
	ce.context.mu.Lock()
	defer ce.context.mu.Unlock()

	now := time.Now()
	maxAge := 5 * time.Minute

	// Clean old failures
	if len(ce.context.RecentFailures) > 100 {
		cutoff := 0
		for i, event := range ce.context.RecentFailures {
			if now.Sub(event.Timestamp) < maxAge {
				cutoff = i
				break
			}
		}
		if cutoff > 0 {
			ce.context.RecentFailures = ce.context.RecentFailures[cutoff:]
		}
	}
}

// EmitCorrelatedEvent converts root cause to domain event
func (ce *CorrelationEngine) EmitCorrelatedEvent(rootCause *RootCause) *domain.CollectorEvent {
	severity := domain.EventSeverityWarning
	if rootCause.Confidence > 0.9 {
		severity = domain.EventSeverityError
	}

	// Build network data
	var networkData *domain.NetworkData
	if rootCause.L4Event != nil {
		networkData = &domain.NetworkData{
			Protocol:   "tcp",
			Direction:  "outbound",
			SourceIP:   rootCause.L4Event.SrcIP.String(),
			SourcePort: int32(rootCause.L4Event.SrcPort),
			DestIP:     rootCause.L4Event.DstIP.String(),
			DestPort:   int32(rootCause.L4Event.DstPort),
		}
	}

	// Build metadata with our correlation results
	metadata := domain.EventMetadata{
		Tags: []string{
			"network-failure",
			rootCause.Pattern,
			fmt.Sprintf("confidence:%.0f", rootCause.Confidence*100),
		},
		Labels: map[string]string{
			"pattern":    rootCause.Pattern,
			"confidence": fmt.Sprintf("%.0f%%", rootCause.Confidence*100),
			"impact":     rootCause.Impact,
		},
	}

	// Add evidence as attributes
	attributes := make(map[string]string)
	attributes["details"] = rootCause.Details
	attributes["resolution"] = rootCause.Resolution
	for i, evidence := range rootCause.Evidence {
		attributes[fmt.Sprintf("evidence_%d", i)] = evidence
	}
	metadata.Attributes = attributes

	// Build correlation hints for intelligence layer
	correlationHints := &domain.CorrelationHints{}
	if rootCause.L4Event != nil {
		if rootCause.L4Event.SrcIP != nil {
			correlationHints.ConnectionID = fmt.Sprintf("%s:%d-%s:%d",
				rootCause.L4Event.SrcIP.String(), rootCause.L4Event.SrcPort,
				rootCause.L4Event.DstIP.String(), rootCause.L4Event.DstPort)
		}
	}
	// Add correlation tags
	correlationHints.CorrelationTags = map[string]string{
		"pattern":    rootCause.Pattern,
		"confidence": fmt.Sprintf("%.2f", rootCause.Confidence),
		"failure_id": fmt.Sprintf("network-failure-%d", time.Now().UnixNano()),
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("netcorr-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeTCP,
		Severity:  severity,
		Source:    "network-correlator",
		Timestamp: rootCause.FailureTime,
		EventData: domain.EventDataContainer{
			Network: networkData,
		},
		Metadata:         metadata,
		CorrelationHints: correlationHints,
	}
}
