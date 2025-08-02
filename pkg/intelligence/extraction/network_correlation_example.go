package extraction

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NetworkCorrelationScenarios demonstrates various network correlation patterns
func NetworkCorrelationScenarios() {
	fmt.Println("\n=== K8s Network Correlation Scenarios ===\n")

	// Scenario 1: Service Call Chain
	serviceCallChainScenario()

	// Scenario 2: Cross-Namespace Communication
	crossNamespaceScenario()

	// Scenario 3: Load Balancer to Pod
	loadBalancerScenario()

	// Scenario 4: Network Policy Violation
	networkPolicyScenario()

	// Scenario 5: Service Mesh Correlation
	serviceMeshScenario()
}

func serviceCallChainScenario() {
	fmt.Println("## Scenario 1: Service Call Chain")
	fmt.Println("Frontend → API Service → Database Service")

	events := []struct {
		id          string
		description string
		network     domain.NetworkData
		k8sContext  domain.K8sContext
	}{
		{
			id:          "net-1",
			description: "Frontend pod calls API service",
			network: domain.NetworkData{
				Protocol:   "HTTP",
				SourceIP:   "10.244.1.10", // frontend-pod IP
				SourcePort: 45678,
				DestIP:     "10.96.1.100", // api-service ClusterIP
				DestPort:   8080,
				Direction:  "egress",
			},
			k8sContext: domain.K8sContext{
				Name:      "frontend-pod-abc",
				Namespace: "production",
				Labels:    map[string]string{"app": "frontend"},
			},
		},
		{
			id:          "net-2",
			description: "API pod receives request from service",
			network: domain.NetworkData{
				Protocol:   "HTTP",
				SourceIP:   "10.244.1.10", // Same as above (SNAT)
				SourcePort: 45678,
				DestIP:     "10.244.2.20", // api-pod IP
				DestPort:   8080,
				Direction:  "ingress",
			},
			k8sContext: domain.K8sContext{
				Name:      "api-pod-xyz",
				Namespace: "production",
				Labels:    map[string]string{"app": "api"},
			},
		},
		{
			id:          "net-3",
			description: "API pod calls database service",
			network: domain.NetworkData{
				Protocol:   "TCP",
				SourceIP:   "10.244.2.20", // api-pod IP
				SourcePort: 34567,
				DestIP:     "10.96.2.200", // db-service ClusterIP
				DestPort:   3306,
				Direction:  "egress",
			},
			k8sContext: domain.K8sContext{
				Name:      "api-pod-xyz",
				Namespace: "production",
			},
		},
		{
			id:          "net-4",
			description: "Database receives connection",
			network: domain.NetworkData{
				Protocol:   "TCP",
				SourceIP:   "10.244.2.20",
				SourcePort: 34567,
				DestIP:     "10.244.3.30", // db-pod IP
				DestPort:   3306,
				Direction:  "ingress",
				Latency:    150000000, // 150ms - high latency!
			},
			k8sContext: domain.K8sContext{
				Name:      "mysql-pod-123",
				Namespace: "production",
				Labels:    map[string]string{"app": "mysql"},
			},
		},
	}

	fmt.Println("\nCorrelation Insights:")
	fmt.Println("1. Service Topology: Frontend → API Service (10.96.1.100) → API Pod → DB Service (10.96.2.200) → MySQL Pod")
	fmt.Println("2. Latency Issue: High latency (150ms) on database connection indicates potential issue")
	fmt.Println("3. Same Transaction: All events share similar timestamp and can be correlated by:")
	fmt.Println("   - Source IP continuity (10.244.1.10 → 10.244.2.20)")
	fmt.Println("   - Service ClusterIP matching")
	fmt.Println("   - Port continuity")
	fmt.Println()

	_ = events
}

func crossNamespaceScenario() {
	fmt.Println("## Scenario 2: Cross-Namespace Communication")
	fmt.Println("Payment service (payment ns) → Inventory service (inventory ns)")

	fmt.Println("\nCorrelation Insights:")
	fmt.Println("1. Cross-namespace calls can be identified by different namespaces in source/dest pods")
	fmt.Println("2. Network policies may block cross-namespace traffic")
	fmt.Println("3. Service DNS would be: inventory-service.inventory.svc.cluster.local")
	fmt.Println("4. Higher security scrutiny for cross-namespace calls")
	fmt.Println()
}

func loadBalancerScenario() {
	fmt.Println("## Scenario 3: External Load Balancer to Internal Service")
	fmt.Println("External Client → LoadBalancer → NodePort → Service → Pods")

	fmt.Println("\nNetwork Flow:")
	fmt.Println("1. External IP (1.2.3.4) → LoadBalancer IP (34.5.6.7)")
	fmt.Println("2. LoadBalancer → Node IP (192.168.1.10) on NodePort (30080)")
	fmt.Println("3. Node → Service ClusterIP (10.96.1.100:80)")
	fmt.Println("4. Service → Backend Pod IPs (10.244.x.x)")

	fmt.Println("\nCorrelation Challenges:")
	fmt.Println("- Multiple NAT translations")
	fmt.Println("- Need to correlate by timing and port mappings")
	fmt.Println("- LoadBalancer logs may be needed for full trace")
	fmt.Println()
}

func networkPolicyScenario() {
	fmt.Println("## Scenario 4: Network Policy Blocking Traffic")

	events := []struct {
		description string
		result      string
	}{
		{
			description: "Pod A (namespace: default) → Pod B (namespace: secure)",
			result:      "DROPPED by NetworkPolicy 'deny-from-default'",
		},
		{
			description: "Pod A creates TCP SYN packet",
			result:      "No response - connection timeout",
		},
	}

	fmt.Println("\nCorrelation Insights:")
	fmt.Println("1. Missing response packets indicate policy drops")
	fmt.Println("2. eBPF can capture dropped packets at kernel level")
	fmt.Println("3. Correlate with NetworkPolicy objects to identify which policy blocked")
	fmt.Println("4. Common pattern: SYN sent but no SYN-ACK received")

	_ = events
	fmt.Println()
}

func serviceMeshScenario() {
	fmt.Println("## Scenario 5: Service Mesh (Istio/Linkerd) Correlation")

	fmt.Println("\nEnhanced Observability:")
	fmt.Println("1. Sidecar Proxy Interception:")
	fmt.Println("   App Container → Envoy (localhost:15001) → Actual Destination")

	fmt.Println("\n2. Additional Correlation Data:")
	fmt.Println("   - X-Request-ID header for tracing")
	fmt.Println("   - X-B3-TraceId for distributed tracing")
	fmt.Println("   - Envoy access logs with detailed metrics")

	fmt.Println("\n3. mTLS Between Services:")
	fmt.Println("   - Certificate-based pod identity")
	fmt.Println("   - Automatic encryption between services")

	fmt.Println("\n4. Traffic Management:")
	fmt.Println("   - Circuit breaker activations")
	fmt.Println("   - Retry attempts")
	fmt.Println("   - Load balancing decisions")
	fmt.Println()
}

// NetworkCorrelationMatrix shows how different network events correlate
func NetworkCorrelationMatrix() {
	fmt.Println("\n=== Network Correlation Matrix ===\n")

	matrix := [][]string{
		{"Event Type", "Correlates With", "Correlation Method", "Confidence"},
		{"----------", "--------------", "-----------------", "----------"},
		{"TCP SYN", "TCP SYN-ACK", "5-tuple + timing", "High"},
		{"HTTP Request", "HTTP Response", "5-tuple + headers", "High"},
		{"Service Call", "Pod Selection", "ClusterIP → EndpointIP", "High"},
		{"Ingress", "Backend Pods", "Host header + path", "Medium"},
		{"NetworkPolicy Drop", "Connection Timeout", "Missing response", "Medium"},
		{"DNS Query", "Service Discovery", "Query name matching", "High"},
		{"Load Balancer", "NodePort", "Port mapping + time", "Medium"},
		{"Pod-to-Pod", "Node-to-Node", "Overlay network", "High"},
		{"Egress NAT", "External Response", "Connection tracking", "Medium"},
		{"Service Mesh", "App Traffic", "Sidecar interception", "High"},
	}

	for _, row := range matrix {
		fmt.Printf("%-20s %-20s %-25s %-10s\n", row[0], row[1], row[2], row[3])
	}
	fmt.Println()
}

// NetworkAnomalyPatterns shows patterns that indicate network issues
func NetworkAnomalyPatterns() {
	fmt.Println("\n=== Network Anomaly Patterns ===\n")

	patterns := []struct {
		pattern     string
		indicators  []string
		correlation string
	}{
		{
			pattern: "Service Discovery Failure",
			indicators: []string{
				"DNS queries for service.namespace.svc.cluster.local failing",
				"Connection attempts to 0.0.0.0",
				"Pods using hardcoded IPs instead of service names",
			},
			correlation: "Correlate DNS failures with connection failures in same namespace",
		},
		{
			pattern: "Network Policy Misconfiguration",
			indicators: []string{
				"Asymmetric traffic (request seen but no response)",
				"Connections timing out after 3-way handshake",
				"Working in default namespace but not others",
			},
			correlation: "Match source/dest labels with NetworkPolicy selectors",
		},
		{
			pattern: "Service Mesh Circuit Breaker",
			indicators: []string{
				"503 Service Unavailable from Envoy",
				"Multiple connection attempts in short time",
				"X-Envoy-Overloaded header present",
			},
			correlation: "Group by X-Request-ID and destination service",
		},
		{
			pattern: "Pod Network Initialization Race",
			indicators: []string{
				"Connections failing in first 5-10 seconds",
				"CNI plugin errors in kubelet logs",
				"Pod IP not yet assigned",
			},
			correlation: "Match pod creation time with first network attempts",
		},
		{
			pattern: "Cross-Zone Traffic Surge",
			indicators: []string{
				"Increased latency for specific pod pairs",
				"Source and destination in different zones",
				"Correlated with zone failure or rebalancing",
			},
			correlation: "Group by zone labels and measure latency percentiles",
		},
	}

	for _, p := range patterns {
		fmt.Printf("Pattern: %s\n", p.pattern)
		fmt.Println("Indicators:")
		for _, ind := range p.indicators {
			fmt.Printf("  - %s\n", ind)
		}
		fmt.Printf("Correlation: %s\n\n", p.correlation)
	}
}

// Example of creating rich network events with full K8s context
func ExampleNetworkEventWithContext() *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        "net-event-123",
		Timestamp: time.Now(),
		Type:      domain.EventTypeNetwork,
		Source:    "cni",

		Network: &domain.NetworkData{
			Protocol:   "HTTP",
			SourceIP:   "10.244.1.10",
			SourcePort: 45678,
			DestIP:     "10.96.1.100", // Service ClusterIP
			DestPort:   8080,
			Direction:  "egress",
			Method:     "POST",
			Path:       "/api/v1/orders",
			StatusCode: 500,
			Latency:    250000000, // 250ms
		},

		K8sContext: &domain.K8sContext{
			Name:         "frontend-pod-abc",
			Namespace:    "production",
			NodeName:     "node-1",
			Zone:         "us-east-1a",
			WorkloadKind: "Deployment",
			WorkloadName: "frontend",
			Labels: map[string]string{
				"app":     "frontend",
				"version": "v2",
			},
			Consumers: []domain.K8sResourceRef{
				{
					Kind:      "Service",
					Name:      "api-service",
					Namespace: "production",
				},
			},
		},

		CorrelationHints: []string{
			"service:production/api-service:8080",
			"high_latency",
			"server_error",
			"http",
			"cross_service",
		},

		Impact: &domain.ImpactContext{
			Severity:             "high",
			InfrastructureImpact: 0.8,
			SystemCritical:       true,
			AffectedServices:     []string{"frontend", "api"},
		},
	}
}
