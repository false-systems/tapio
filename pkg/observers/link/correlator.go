package link

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// LinkCorrelator analyzes link failures to find root causes
type LinkCorrelator struct {
	logger   *zap.Logger
	config   *Config
	context  *CorrelationContext
	patterns []FailurePattern

	// Channels
	failures chan *LinkFailure
	results  chan *LinkDiagnosis

	mu sync.RWMutex
}

// NewLinkCorrelator creates a new correlator
func NewLinkCorrelator(logger *zap.Logger, config *Config) *LinkCorrelator {
	lc := &LinkCorrelator{
		logger: logger,
		config: config,
		context: &CorrelationContext{
			RecentSYNs:      make(map[uint64]*SYNAttempt),
			RecentARPs:      make(map[uint32]*ARPRequest),
			RecentFailures:  make([]*LinkFailure, 0, 1000),
			LinkStates:      make(map[string]*LinkState),
			NetworkPolicies: make(map[string]*PolicyInfo),
			PodStates:       make(map[string]*PodContext),
			ServiceStates:   make(map[string]*ServiceContext),
		},
		failures: make(chan *LinkFailure, 1000),
		results:  make(chan *LinkDiagnosis, 100),
	}

	// Register failure patterns
	lc.patterns = []FailurePattern{
		{
			Name:        "NetworkPolicyBlock",
			Description: "Connection blocked by NetworkPolicy",
			Layer:       4,
			Detector:    lc.detectNetworkPolicyBlock,
		},
		{
			Name:        "ServiceDown",
			Description: "Service has no healthy endpoints",
			Layer:       4,
			Detector:    lc.detectServiceDown,
		},
		{
			Name:        "PodEvicted",
			Description: "Target pod was evicted or restarted",
			Layer:       3,
			Detector:    lc.detectPodEvicted,
		},
		{
			Name:        "NetworkPartition",
			Description: "Network partition between nodes",
			Layer:       3,
			Detector:    lc.detectNetworkPartition,
		},
		{
			Name:        "ARPPoisoning",
			Description: "Possible ARP poisoning or MAC conflict",
			Layer:       2,
			Detector:    lc.detectARPPoisoning,
		},
		{
			Name:        "MTUMismatch",
			Description: "MTU mismatch causing packet drops",
			Layer:       3,
			Detector:    lc.detectMTUMismatch,
		},
		{
			Name:        "ConnectionThrottling",
			Description: "Connection rate limiting or throttling",
			Layer:       4,
			Detector:    lc.detectThrottling,
		},
		{
			Name:        "DNSFailure",
			Description: "DNS resolution failure",
			Layer:       7, // Actually L7 but affects L3/L4
			Detector:    lc.detectDNSFailure,
		},
	}

	// Start processing
	go lc.process()

	return lc
}

// AnalyzeFailure submits a failure for analysis
func (lc *LinkCorrelator) AnalyzeFailure(failure *LinkFailure) {
	select {
	case lc.failures <- failure:
		// Store in recent failures
		lc.mu.Lock()
		lc.context.RecentFailures = append(lc.context.RecentFailures, failure)
		if len(lc.context.RecentFailures) > 1000 {
			lc.context.RecentFailures = lc.context.RecentFailures[1:]
		}
		lc.mu.Unlock()
	default:
		lc.logger.Warn("Correlator queue full, dropping failure event")
	}
}

// Results returns the diagnosis channel
func (lc *LinkCorrelator) Results() <-chan *LinkDiagnosis {
	return lc.results
}

// process runs the correlation engine
func (lc *LinkCorrelator) process() {
	for failure := range lc.failures {
		// Try each pattern
		for _, pattern := range lc.patterns {
			if diagnosis := pattern.Detector(failure, lc.context); diagnosis != nil {
				// Found a match
				diagnosis.Pattern = pattern.Name
				diagnosis.Layer = pattern.Layer
				diagnosis.PrimaryFailure = failure

				// Add related failures
				diagnosis.RelatedFailures = lc.findRelatedFailures(failure)

				select {
				case lc.results <- diagnosis:
				default:
					lc.logger.Warn("Results queue full, dropping diagnosis")
				}
				break // Stop after first match
			}
		}

		// Update link state
		lc.updateLinkState(failure)
	}
}

// detectNetworkPolicyBlock checks if failure is due to NetworkPolicy
func (lc *LinkCorrelator) detectNetworkPolicyBlock(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Only for L4 SYN timeouts
	if failure.Type != "syn_timeout" || failure.Layer != 4 {
		return nil
	}

	// Check if there's a policy that would block this
	for _, policy := range ctx.NetworkPolicies {
		if lc.policyWouldBlock(policy, failure.SrcIP, failure.DstIP, failure.DstPort) {
			return &LinkDiagnosis{
				Confidence: 0.95,
				Severity:   domain.EventSeverityWarning,
				Timestamp:  failure.Timestamp,
				Summary:    fmt.Sprintf("Connection blocked by NetworkPolicy %s", policy.Name),
				Details: fmt.Sprintf("TCP connection from %s to %s:%d blocked by NetworkPolicy %s/%s",
					failure.SrcIP, failure.DstIP, failure.DstPort, policy.Namespace, policy.Name),
				Evidence: []string{
					fmt.Sprintf("SYN sent to %s:%d", failure.DstIP, failure.DstPort),
					"No SYN-ACK received within timeout",
					fmt.Sprintf("NetworkPolicy %s blocks this traffic", policy.Name),
				},
				Impact:     fmt.Sprintf("Pod cannot connect to %s:%d", failure.DstIP, failure.DstPort),
				Resolution: fmt.Sprintf("Update NetworkPolicy %s to allow traffic to port %d", policy.Name, failure.DstPort),
			}
		}
	}

	return nil
}

// detectServiceDown checks if target service is down
func (lc *LinkCorrelator) detectServiceDown(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	if failure.Type != "syn_timeout" {
		return nil
	}

	// Check if destination is a service
	for _, svc := range ctx.ServiceStates {
		if svc.ClusterIP == failure.DstIP {
			if !svc.Healthy || len(svc.Endpoints) == 0 {
				return &LinkDiagnosis{
					Confidence: 0.90,
					Severity:   domain.EventSeverityError,
					Timestamp:  failure.Timestamp,
					Summary:    fmt.Sprintf("Service %s has no healthy endpoints", svc.Name),
					Details: fmt.Sprintf("Connection to service %s/%s (%s) failed - no healthy endpoints available",
						svc.Namespace, svc.Name, svc.ClusterIP),
					Evidence: []string{
						fmt.Sprintf("Service %s has %d endpoints", svc.Name, len(svc.Endpoints)),
						"All endpoints are unhealthy or missing",
						fmt.Sprintf("Connection to %s:%d timed out", failure.DstIP, failure.DstPort),
					},
					Impact:     fmt.Sprintf("All clients of service %s/%s will fail", svc.Namespace, svc.Name),
					Resolution: "Check pod health, ensure pods are running and ready",
				}
			}
		}
	}

	return nil
}

// detectPodEvicted checks if target pod was evicted
func (lc *LinkCorrelator) detectPodEvicted(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Check if destination pod was recently restarted
	for _, pod := range ctx.PodStates {
		if pod.IP == failure.DstIP && pod.Restarted {
			timeSinceRestart := failure.Timestamp.Sub(pod.RestartTime)
			if timeSinceRestart < 5*time.Minute {
				return &LinkDiagnosis{
					Confidence: 0.85,
					Severity:   domain.EventSeverityWarning,
					Timestamp:  failure.Timestamp,
					Summary:    fmt.Sprintf("Target pod %s was recently restarted", pod.Name),
					Details: fmt.Sprintf("Connection to %s failed - pod %s/%s restarted %v ago",
						failure.DstIP, pod.Namespace, pod.Name, timeSinceRestart),
					Evidence: []string{
						fmt.Sprintf("Pod %s has IP %s", pod.Name, pod.IP),
						fmt.Sprintf("Pod restarted at %s", pod.RestartTime.Format(time.RFC3339)),
						"Connection failed shortly after restart",
					},
					Impact:     "Temporary connection failures during pod restart",
					Resolution: "Wait for pod to fully initialize or implement retry logic",
				}
			}
		}
	}

	return nil
}

// detectNetworkPartition checks for network partition
func (lc *LinkCorrelator) detectNetworkPartition(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Look for multiple failures between same network segments
	srcNetwork := getNetworkSegment(failure.SrcIP)
	dstNetwork := getNetworkSegment(failure.DstIP)

	failureCount := 0
	for _, f := range ctx.RecentFailures {
		if getNetworkSegment(f.SrcIP) == srcNetwork &&
			getNetworkSegment(f.DstIP) == dstNetwork {
			failureCount++
		}
	}

	if failureCount >= 5 {
		return &LinkDiagnosis{
			Confidence: 0.75,
			Severity:   domain.EventSeverityError,
			Timestamp:  failure.Timestamp,
			Summary:    fmt.Sprintf("Possible network partition between %s and %s", srcNetwork, dstNetwork),
			Details: fmt.Sprintf("Multiple connection failures detected between network segments %s and %s",
				srcNetwork, dstNetwork),
			Evidence: []string{
				fmt.Sprintf("%d failures in last %v", failureCount, lc.config.CorrelationWindow),
				fmt.Sprintf("Latest failure: %s -> %s", failure.SrcIP, failure.DstIP),
				"Consistent pattern suggests network-level issue",
			},
			Impact:     fmt.Sprintf("All traffic between %s and %s affected", srcNetwork, dstNetwork),
			Resolution: "Check network connectivity, routing tables, and firewall rules between segments",
		}
	}

	return nil
}

// detectARPPoisoning checks for ARP-related issues
func (lc *LinkCorrelator) detectARPPoisoning(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	if failure.Type != "arp_timeout" {
		return nil
	}

	// Check for multiple ARP failures to same target
	arpFailures := 0
	for _, f := range ctx.RecentFailures {
		if f.Type == "arp_timeout" && f.DstIP == failure.DstIP {
			arpFailures++
		}
	}

	if arpFailures >= 3 {
		return &LinkDiagnosis{
			Confidence: 0.70,
			Severity:   domain.EventSeverityError,
			Timestamp:  failure.Timestamp,
			Summary:    fmt.Sprintf("ARP resolution failing for %s", failure.DstIP),
			Details: fmt.Sprintf("Multiple ARP resolution failures for IP %s - possible ARP poisoning or L2 issue",
				failure.DstIP),
			Evidence: []string{
				fmt.Sprintf("%d ARP failures for %s", arpFailures, failure.DstIP),
				fmt.Sprintf("Interface: %s", failure.Interface),
				"No ARP replies received",
			},
			Impact:     fmt.Sprintf("Cannot reach %s at L2 layer", failure.DstIP),
			Resolution: "Check ARP table, verify MAC addresses, check for duplicate IPs",
		}
	}

	return nil
}

// detectMTUMismatch checks for MTU issues
func (lc *LinkCorrelator) detectMTUMismatch(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Look for pattern of large packet failures
	if failure.Type == "packet_too_large" ||
		(failure.Type == "syn_timeout" && failure.RetryCount > 2) {
		return &LinkDiagnosis{
			Confidence: 0.65,
			Severity:   domain.EventSeverityWarning,
			Timestamp:  failure.Timestamp,
			Summary:    "Possible MTU mismatch causing packet drops",
			Details: fmt.Sprintf("Connection failures suggest MTU issues between %s and %s",
				failure.SrcIP, failure.DstIP),
			Evidence: []string{
				"Multiple retransmissions detected",
				"Large packets may be getting dropped",
				"Path MTU discovery might be blocked",
			},
			Impact:     "Connections with large payloads will fail or be slow",
			Resolution: "Check MTU settings on interfaces and along network path, ensure ICMP is not blocked",
		}
	}

	return nil
}

// detectThrottling checks for rate limiting
func (lc *LinkCorrelator) detectThrottling(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Count recent connections to same destination
	connCount := 0
	var window = 10 * time.Second
	cutoff := failure.Timestamp.Add(-window)

	for _, f := range ctx.RecentFailures {
		if f.DstIP == failure.DstIP && f.DstPort == failure.DstPort && f.Timestamp.After(cutoff) {
			connCount++
		}
	}

	if connCount >= 10 {
		return &LinkDiagnosis{
			Confidence: 0.70,
			Severity:   domain.EventSeverityWarning,
			Timestamp:  failure.Timestamp,
			Summary:    fmt.Sprintf("Connection throttling detected to %s:%d", failure.DstIP, failure.DstPort),
			Details:    fmt.Sprintf("%d connection attempts in %v - likely rate limiting", connCount, window),
			Evidence: []string{
				fmt.Sprintf("%d failures to %s:%d in %v", connCount, failure.DstIP, failure.DstPort, window),
				"Rapid connection attempts detected",
				"Target may be rate limiting connections",
			},
			Impact:     "New connections being rejected due to rate limits",
			Resolution: "Implement connection pooling, reduce connection rate, or increase rate limits",
		}
	}

	return nil
}

// detectDNSFailure checks for DNS-related issues
func (lc *LinkCorrelator) detectDNSFailure(failure *LinkFailure, ctx *CorrelationContext) *LinkDiagnosis {
	// Check if destination IP is a DNS server
	if failure.DstPort == 53 || failure.DstIP == "10.96.0.10" { // Common kube-dns IP
		return &LinkDiagnosis{
			Confidence: 0.80,
			Severity:   domain.EventSeverityError,
			Timestamp:  failure.Timestamp,
			Summary:    "DNS resolution failure detected",
			Details:    fmt.Sprintf("Cannot reach DNS server at %s", failure.DstIP),
			Evidence: []string{
				fmt.Sprintf("Connection to DNS server %s:53 failed", failure.DstIP),
				"DNS queries will fail",
				"Service discovery affected",
			},
			Impact:     "Pods cannot resolve service names, external domains unreachable",
			Resolution: "Check DNS pod health, verify kube-dns/CoreDNS is running",
		}
	}

	return nil
}

// Helper functions

func (lc *LinkCorrelator) policyWouldBlock(policy *PolicyInfo, srcIP, dstIP string, dstPort int32) bool {
	// Simplified policy check - in reality would need full evaluation
	for _, rule := range policy.Egress {
		for _, port := range rule.Ports {
			if port == dstPort {
				// Check if destination is in allowed list
				for _, allowed := range rule.To {
					if strings.Contains(allowed, dstIP) {
						return false // Allowed
					}
				}
				return true // Not in allowed list, blocked
			}
		}
	}
	return false
}

func (lc *LinkCorrelator) findRelatedFailures(primary *LinkFailure) []*LinkFailure {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	related := make([]*LinkFailure, 0)
	window := 5 * time.Second

	for _, f := range lc.context.RecentFailures {
		if f == primary {
			continue
		}

		// Same source or destination
		if (f.SrcIP == primary.SrcIP || f.DstIP == primary.DstIP) &&
			primary.Timestamp.Sub(f.Timestamp).Abs() < window {
			related = append(related, f)
		}
	}

	return related
}

func (lc *LinkCorrelator) updateLinkState(failure *LinkFailure) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	key := fmt.Sprintf("%s->%s:%d", failure.SrcIP, failure.DstIP, failure.DstPort)
	state, exists := lc.context.LinkStates[key]

	if !exists {
		state = &LinkState{
			Endpoint: key,
			State:    "healthy",
		}
		lc.context.LinkStates[key] = state
	}

	state.FailureCount++
	state.LastFailure = failure
	state.LastSeen = failure.Timestamp

	// Update state based on failure rate
	if state.FailureCount > 5 {
		state.State = "failed"
	} else if state.FailureCount > 2 {
		state.State = "degraded"
	}
}

func getNetworkSegment(ip string) string {
	// Simple network segment extraction (first 3 octets for IPv4)
	parts := strings.Split(ip, ".")
	if len(parts) >= 3 {
		return strings.Join(parts[:3], ".")
	}
	return ip
}
