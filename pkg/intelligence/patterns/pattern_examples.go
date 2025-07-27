package patterns

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// PatternExamples demonstrates various K8s behavioral patterns
func PatternExamples() {
	fmt.Println("\n=== K8s Behavioral Pattern Examples ===")

	// Example 1: Rolling Update Failure
	rollingUpdateExample()

	// Example 2: Pod Eviction Storm
	evictionStormExample()

	// Example 3: Service Discovery Failure
	serviceDiscoveryExample()

	// Example 4: Security Pattern Detection
	securityPatternExample()

	// Example 5: Performance Degradation
	performancePatternExample()
}

func rollingUpdateExample() {
	fmt.Println("## Rolling Update Failure Pattern")
	fmt.Println("Scenario: Deployment update causing pod failures")

	events := []struct {
		time        string
		description string
		event       *domain.UnifiedEvent
	}{
		{
			time:        "T+0s",
			description: "Deployment update triggered",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s",
				Kubernetes: &domain.KubernetesData{
					Object:  "deployment/api-server",
					Reason:  "DeploymentUpdated",
					Message: "Scaled up replica set api-server-v2 to 3",
				},
			},
		},
		{
			time:        "T+10s",
			description: "New pods failing to start",
			event: &domain.UnifiedEvent{
				Type:     domain.EventTypeKubernetes,
				Source:   "k8s",
				Severity: domain.EventSeverityError,
				Kubernetes: &domain.KubernetesData{
					Object:  "pod/api-server-v2-abc",
					Reason:  "CrashLoopBackOff",
					Message: "Back-off restarting failed container",
				},
				Entity: &domain.EntityContext{
					Type:      "pod",
					Name:      "api-server-v2-abc",
					Namespace: "production",
				},
			},
		},
		{
			time:        "T+30s",
			description: "Multiple pods in CrashLoop",
			event: &domain.UnifiedEvent{
				Type:     domain.EventTypeKubernetes,
				Severity: domain.EventSeverityError,
				Kubernetes: &domain.KubernetesData{
					Reason: "CrashLoopBackOff",
				},
			},
		},
		{
			time:        "T+60s",
			description: "Deployment rollback initiated",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s",
				Kubernetes: &domain.KubernetesData{
					Object:  "deployment/api-server",
					Reason:  "DeploymentRollback",
					Message: "Rolled back deployment api-server",
				},
			},
		},
	}

	fmt.Println("\nPattern Detection:")
	fmt.Println("- DETECTED: Rolling Update Failure")
	fmt.Println("- Confidence: 95%")
	fmt.Println("- Impact: Service degradation affecting users")
	fmt.Println("- Root Cause: Likely configuration issue in new version")
	fmt.Println("- Recommendation: Check pod logs for startup errors")

	for _, e := range events {
		fmt.Printf("\n%s: %s\n", e.time, e.description)
	}
	fmt.Println()
}

func evictionStormExample() {
	fmt.Println("## Pod Eviction Storm Pattern")
	fmt.Println("Scenario: Node resource pressure causing mass evictions")

	fmt.Println("\nEvent Sequence:")
	fmt.Println("T+0s: Node 'worker-3' reports DiskPressure condition")
	fmt.Println("T+5s: First pod evicted: 'logging-agent-xyz'")
	fmt.Println("T+6s: Second pod evicted: 'metrics-collector-abc'")
	fmt.Println("T+7s: Third pod evicted: 'app-backend-def'")
	fmt.Println("T+10s: 15 pods evicted in 10 seconds")

	fmt.Println("\nPattern Analysis:")
	fmt.Println("- DETECTED: Pod Eviction Storm")
	fmt.Println("- Trigger: Disk usage exceeded 85% on node")
	fmt.Println("- Affected: 15 pods across 8 different services")
	fmt.Println("- User Impact: Potential service disruptions")
	fmt.Println("- Correlations: Node resource exhaustion pattern also detected")
	fmt.Println()
}

func serviceDiscoveryExample() {
	fmt.Println("## Service Discovery Failure Pattern")
	fmt.Println("Scenario: DNS resolution failures preventing service communication")

	networkEvents := []struct {
		source      string
		destination string
		result      string
	}{
		{
			source:      "frontend-pod",
			destination: "backend-service.production.svc.cluster.local",
			result:      "DNS: NXDOMAIN",
		},
		{
			source:      "frontend-pod",
			destination: "10.96.1.100 (fallback IP)",
			result:      "Connection refused",
		},
		{
			source:      "api-gateway",
			destination: "backend-service.production.svc.cluster.local",
			result:      "DNS: NXDOMAIN",
		},
	}

	fmt.Println("\nPattern Detection:")
	fmt.Println("- DETECTED: Service Discovery Failure")
	fmt.Println("- Affected Service: backend-service in production namespace")
	fmt.Println("- Symptoms: Multiple pods unable to resolve service DNS")
	fmt.Println("- Probable Causes:")
	fmt.Println("  1. Service deleted or misconfigured")
	fmt.Println("  2. CoreDNS issues")
	fmt.Println("  3. Network policy blocking DNS")

	for _, event := range networkEvents {
		fmt.Printf("\n%s → %s: %s", event.source, event.destination, event.result)
	}
	fmt.Println()
}

func securityPatternExample() {
	fmt.Println("## Security Pattern: Privilege Escalation Attempt")
	fmt.Println("Scenario: Container attempting suspicious activities")

	securityEvents := []struct {
		time   string
		event  string
		detail string
	}{
		{
			time:   "14:23:45",
			event:  "Exec into container",
			detail: "User 'unknown' exec'd into pod web-app-123",
		},
		{
			time:   "14:23:47",
			event:  "Suspicious syscall",
			detail: "Process attempted setuid(0)",
		},
		{
			time:   "14:23:48",
			event:  "File access attempt",
			detail: "Tried to read /proc/1/environ",
		},
		{
			time:   "14:23:49",
			event:  "Network connection",
			detail: "Outbound connection to suspicious IP 185.x.x.x",
		},
	}

	fmt.Println("\nPattern Analysis:")
	fmt.Println("- DETECTED: Potential Container Compromise")
	fmt.Println("- Confidence: 87%")
	fmt.Println("- Risk Level: CRITICAL")
	fmt.Println("- Indicators:")

	for _, e := range securityEvents {
		fmt.Printf("  %s - %s: %s\n", e.time, e.event, e.detail)
	}

	fmt.Println("\nRecommended Actions:")
	fmt.Println("- Isolate affected pod immediately")
	fmt.Println("- Review pod security policies")
	fmt.Println("- Check for compromised images")
	fmt.Println("- Enable admission controllers")
	fmt.Println()
}

func performancePatternExample() {
	fmt.Println("## Performance Pattern: CPU Throttling Cascade")
	fmt.Println("Scenario: CPU limits causing service degradation")

	fmt.Println("\nDetected Pattern Sequence:")
	fmt.Println("1. API pods hitting CPU limits (100% of 2 cores)")
	fmt.Println("2. Response latency increasing (50ms → 500ms)")
	fmt.Println("3. Request queue building up")
	fmt.Println("4. Client timeouts starting")
	fmt.Println("5. Circuit breakers tripping")
	fmt.Println("6. Cascading failures to dependent services")

	fmt.Println("\nPattern Correlation:")
	fmt.Println("- Primary: CPU Throttling Pattern")
	fmt.Println("- Secondary: Cascading Service Failure")
	fmt.Println("- Tertiary: HPA attempting to scale (but hitting node limits)")

	fmt.Println("\nBusiness Impact:")
	fmt.Println("- 25% of API requests failing")
	fmt.Println("- Customer-facing features degraded")
	fmt.Println("- Revenue impact: ~$5k/hour")

	fmt.Println("\nAI Recommendations:")
	fmt.Println("1. Immediate: Increase CPU limits to 4 cores")
	fmt.Println("2. Short-term: Add more nodes to cluster")
	fmt.Println("3. Long-term: Optimize API code for CPU efficiency")
	fmt.Println()
}

// ShowPatternCorrelations demonstrates how patterns correlate
func ShowPatternCorrelations() {
	fmt.Println("\n=== Pattern Correlation Matrix ===")

	correlations := []struct {
		pattern1    string
		pattern2    string
		correlation string
		strength    string
	}{
		{
			pattern1:    "Pod Eviction Storm",
			pattern2:    "Node Resource Exhaustion",
			correlation: "Causal - resource pressure triggers evictions",
			strength:    "Strong",
		},
		{
			pattern1:    "Rolling Update Failure",
			pattern2:    "Cascading Service Failure",
			correlation: "Temporal - failed updates can cascade",
			strength:    "Medium",
		},
		{
			pattern1:    "CPU Throttling",
			pattern2:    "Memory Leak",
			correlation: "Co-occurrence - often happen together",
			strength:    "Medium",
		},
		{
			pattern1:    "Service Discovery Failure",
			pattern2:    "Network Policy Blocking",
			correlation: "Alternative causes - similar symptoms",
			strength:    "Weak",
		},
		{
			pattern1:    "Init Container Failure",
			pattern2:    "PVC Mount Failure",
			correlation: "Sequential - PVC issues cause init failures",
			strength:    "Strong",
		},
	}

	fmt.Println("Pattern 1                  | Pattern 2                  | Correlation Type           | Strength")
	fmt.Println("---------------------------|----------------------------|----------------------------|----------")

	for _, c := range correlations {
		fmt.Printf("%-26s | %-26s | %-26s | %s\n",
			c.pattern1, c.pattern2, c.correlation, c.strength)
	}
	fmt.Println()
}

// CreateExamplePatternEvent creates a realistic event that would trigger pattern detection
func CreateExamplePatternEvent(patternType string) *domain.UnifiedEvent {
	base := &domain.UnifiedEvent{
		ID:        fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    "k8s",
		Type:      domain.EventTypeKubernetes,
	}

	switch patternType {
	case "rolling-update-failure":
		base.Kubernetes = &domain.KubernetesData{
			Object:  "pod/api-server-v2-xyz",
			Reason:  "CrashLoopBackOff",
			Message: "Back-off 5m0s restarting failed container=api-server",
		}
		base.Entity = &domain.EntityContext{
			Type:      "pod",
			Name:      "api-server-v2-xyz",
			Namespace: "production",
		}
		base.Severity = domain.EventSeverityError

	case "pod-eviction":
		base.Kubernetes = &domain.KubernetesData{
			Object:  "pod/worker-app-abc",
			Reason:  "Evicted",
			Message: "The node was low on resource: ephemeral-storage",
		}
		base.Entity = &domain.EntityContext{
			Type:      "pod",
			Name:      "worker-app-abc",
			Namespace: "default",
		}
		base.Severity = domain.EventSeverityWarning

	case "network-failure":
		base.Type = domain.EventTypeNetwork
		base.Network = &domain.NetworkData{
			Protocol:   "TCP",
			SourceIP:   "10.244.1.10",
			DestIP:     "10.96.1.100",
			DestPort:   8080,
			StatusCode: 0, // Connection failed
		}
		base.Severity = domain.EventSeverityError

	case "security-alert":
		base.Type = domain.EventTypeSystem
		base.Kernel = &domain.KernelData{
			Syscall: "setuid",
			Comm:    "suspicious-app",
		}
		base.Severity = domain.EventSeverityCritical
	}

	return base
}
