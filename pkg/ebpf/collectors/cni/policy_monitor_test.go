package cni

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

func TestPolicyViolation_Validation(t *testing.T) {
	violation := &PolicyViolation{
		ID:              "violation-001",
		Timestamp:       time.Now(),
		PolicyName:      "deny-all-ingress",
		PolicyNamespace: "production",
		ViolationType:   "ingress",
		SourceIP:        net.ParseIP("10.244.1.100"),
		SourcePort:      44321,
		DestinationIP:   net.ParseIP("10.244.2.50"),
		DestinationPort: 8080,
		Protocol:        6, // TCP
		Action:          "deny",
		BytesBlocked:    1024,
		PacketsBlocked:  5,
		Severity:        "high",
		RiskScore:       0.8,
		Labels: map[string]string{
			"policy":      "security",
			"environment": "prod",
		},
	}

	assert.Equal(t, "violation-001", violation.ID)
	assert.Equal(t, "deny-all-ingress", violation.PolicyName)
	assert.Equal(t, "production", violation.PolicyNamespace)
	assert.Equal(t, "ingress", violation.ViolationType)
	assert.Equal(t, "deny", violation.Action)
	assert.Equal(t, uint64(1024), violation.BytesBlocked)
	assert.Equal(t, uint64(5), violation.PacketsBlocked)
	assert.Equal(t, 0.8, violation.RiskScore)
}

func TestPolicyCollector_DetermineSeverity(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  opinionated.EventSeverity
	}{
		{
			name: "critical severity",
			violation: &PolicyViolation{
				Severity: "critical",
			},
			expected: opinionated.SeverityHigh,
		},
		{
			name: "high severity",
			violation: &PolicyViolation{
				Severity: "high",
			},
			expected: opinionated.SeverityHigh,
		},
		{
			name: "medium severity",
			violation: &PolicyViolation{
				Severity: "medium",
			},
			expected: opinionated.SeverityMedium,
		},
		{
			name: "low severity",
			violation: &PolicyViolation{
				Severity: "low",
			},
			expected: opinionated.SeverityInfo,
		},
		{
			name: "high risk score",
			violation: &PolicyViolation{
				Severity:  "unknown",
				RiskScore: 0.9,
			},
			expected: opinionated.SeverityHigh,
		},
		{
			name: "medium risk score",
			violation: &PolicyViolation{
				Severity:  "unknown",
				RiskScore: 0.6,
			},
			expected: opinionated.SeverityMedium,
		},
		{
			name: "low risk score",
			violation: &PolicyViolation{
				Severity:  "unknown",
				RiskScore: 0.3,
			},
			expected: opinionated.SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := collector.determinePolicyViolationSeverity(tt.violation)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestPolicyCollector_LateralMovementDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  bool
	}{
		{
			name: "lateral movement to kube-system",
			violation: &PolicyViolation{
				Action: "deny",
				SourcePod: &PodInfo{
					Namespace: "application",
				},
				DestinationPod: &PodInfo{
					Namespace: "kube-system",
				},
			},
			expected: true,
		},
		{
			name: "lateral movement to istio-system",
			violation: &PolicyViolation{
				Action: "deny",
				SourcePod: &PodInfo{
					Namespace: "frontend",
				},
				DestinationPod: &PodInfo{
					Namespace: "istio-system",
				},
			},
			expected: true,
		},
		{
			name: "allowed lateral movement",
			violation: &PolicyViolation{
				Action: "allow",
				SourcePod: &PodInfo{
					Namespace: "app",
				},
				DestinationPod: &PodInfo{
					Namespace: "kube-system",
				},
			},
			expected: false,
		},
		{
			name: "same namespace communication",
			violation: &PolicyViolation{
				Action: "deny",
				SourcePod: &PodInfo{
					Namespace: "application",
				},
				DestinationPod: &PodInfo{
					Namespace: "application",
				},
			},
			expected: false,
		},
		{
			name: "non-sensitive namespace",
			violation: &PolicyViolation{
				Action: "deny",
				SourcePod: &PodInfo{
					Namespace: "app1",
				},
				DestinationPod: &PodInfo{
					Namespace: "app2",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isLateralMovementAttempt(tt.violation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_PrivilegeEscalationDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  bool
	}{
		{
			name: "SSH access attempt",
			violation: &PolicyViolation{
				DestinationPort: 22,
				Action:          "deny",
			},
			expected: true,
		},
		{
			name: "Kubernetes API access",
			violation: &PolicyViolation{
				DestinationPort: 6443,
				Action:          "deny",
			},
			expected: true,
		},
		{
			name: "HTTPS access to API server",
			violation: &PolicyViolation{
				DestinationPort: 443,
				Action:          "deny",
			},
			expected: true,
		},
		{
			name: "kubelet port access",
			violation: &PolicyViolation{
				DestinationPort: 10250,
				Action:          "deny",
			},
			expected: true,
		},
		{
			name: "normal HTTP access",
			violation: &PolicyViolation{
				DestinationPort: 80,
				Action:          "deny",
			},
			expected: false,
		},
		{
			name: "allowed SSH access",
			violation: &PolicyViolation{
				DestinationPort: 22,
				Action:          "allow",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isPrivilegeEscalationAttempt(tt.violation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_DataExfiltrationDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  bool
	}{
		{
			name: "large external transfer",
			violation: &PolicyViolation{
				DestinationIP: net.ParseIP("8.8.8.8"),
				BytesBlocked:  2 * 1024 * 1024, // 2MB
			},
			expected: true,
		},
		{
			name: "ICMP tunneling",
			violation: &PolicyViolation{
				Protocol: 1, // ICMP
			},
			expected: true,
		},
		{
			name: "external SQL Server access",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 1433,
			},
			expected: true,
		},
		{
			name: "external RDP access",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 3389,
			},
			expected: true,
		},
		{
			name: "small internal transfer",
			violation: &PolicyViolation{
				DestinationIP: net.ParseIP("10.244.1.10"),
				BytesBlocked:  1024,
			},
			expected: false,
		},
		{
			name: "normal TCP traffic",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("8.8.8.8"),
				DestinationPort: 80,
				Protocol:        6, // TCP
				BytesBlocked:    1024,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isDataExfiltrationAttempt(tt.violation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_ReconnaissanceDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  bool
	}{
		{
			name: "port scanning attempt",
			violation: &PolicyViolation{
				Action:         "deny",
				PacketsBlocked: 1,
			},
			expected: true,
		},
		{
			name: "DNS discovery attempt",
			violation: &PolicyViolation{
				DestinationPort: 53,
			},
			expected: true,
		},
		{
			name: "mDNS discovery",
			violation: &PolicyViolation{
				DestinationPort: 5353,
			},
			expected: true,
		},
		{
			name: "NetBIOS discovery",
			violation: &PolicyViolation{
				DestinationPort: 137,
			},
			expected: true,
		},
		{
			name: "normal traffic",
			violation: &PolicyViolation{
				Action:          "deny",
				PacketsBlocked:  10,
				DestinationPort: 80,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isReconnaissanceActivity(tt.violation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_C2CommunicationDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		violation *PolicyViolation
		expected  bool
	}{
		{
			name: "suspicious external port 8080",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 8080,
			},
			expected: true,
		},
		{
			name: "suspicious external port 4444",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 4444,
			},
			expected: true,
		},
		{
			name: "IRC communication",
			violation: &PolicyViolation{
				DestinationPort: 6667,
			},
			expected: true,
		},
		{
			name: "IRC SSL communication",
			violation: &PolicyViolation{
				DestinationPort: 6697,
			},
			expected: true,
		},
		{
			name: "internal suspicious port",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("10.244.1.10"),
				DestinationPort: 8080,
			},
			expected: false,
		},
		{
			name: "normal HTTP traffic",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 80,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isC2Communication(tt.violation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_ExternalIPDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		ip       net.IP
		expected bool
	}{
		{
			name:     "external IP",
			ip:       net.ParseIP("8.8.8.8"),
			expected: true,
		},
		{
			name:     "private class A",
			ip:       net.ParseIP("10.244.1.10"),
			expected: false,
		},
		{
			name:     "private class B",
			ip:       net.ParseIP("172.16.0.1"),
			expected: false,
		},
		{
			name:     "private class C",
			ip:       net.ParseIP("192.168.1.1"),
			expected: false,
		},
		{
			name:     "loopback",
			ip:       net.ParseIP("127.0.0.1"),
			expected: false,
		},
		{
			name:     "public IP",
			ip:       net.ParseIP("1.1.1.1"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isExternalIP(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPolicyCollector_ProcessPolicyViolation(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval:     5 * time.Second,
		EnablePolicyMonitoring: true,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	// Mock event channel
	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	// Add mock pods for enrichment
	collector.podIndex = map[string]*PodInfo{
		"10.244.1.100": {
			Name:      "source-pod",
			Namespace: "application",
			IP:        net.ParseIP("10.244.1.100"),
		},
		"10.244.2.50": {
			Name:      "dest-pod",
			Namespace: "backend",
			IP:        net.ParseIP("10.244.2.50"),
		},
	}

	violation := &PolicyViolation{
		ID:              "violation-test",
		Timestamp:       time.Now(),
		PolicyName:      "deny-cross-namespace",
		PolicyNamespace: "security",
		ViolationType:   "egress",
		SourceIP:        net.ParseIP("10.244.1.100"),
		SourcePort:      44321,
		DestinationIP:   net.ParseIP("10.244.2.50"),
		DestinationPort: 8080,
		Protocol:        6, // TCP
		Action:          "deny",
		BytesBlocked:    2048,
		PacketsBlocked:  3,
		Severity:        "high",
		RiskScore:       0.8,
		Labels: map[string]string{
			"policy": "security",
			"type":   "cross-namespace",
		},
		Annotations: map[string]string{
			"reason": "policy-violation",
		},
	}

	collector.processPolicyViolation(violation)

	// Verify event was generated
	select {
	case event := <-eventCh:
		opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
		require.True(t, ok)

		assert.Contains(t, opinionatedEvent.Id, "policy-violation-")
		assert.Equal(t, "network.policy_violation", opinionatedEvent.EventType)
		assert.Equal(t, "application", opinionatedEvent.Namespace)
		assert.Equal(t, "source-pod", opinionatedEvent.PodName)
		assert.Equal(t, opinionated.SeverityError, opinionatedEvent.Severity)

		// Check policy-specific attributes
		assert.Equal(t, "deny-cross-namespace", opinionatedEvent.Attributes["policy.name"])
		assert.Equal(t, "security", opinionatedEvent.Attributes["policy.namespace"])
		assert.Equal(t, "egress", opinionatedEvent.Attributes["policy.violation_type"])
		assert.Equal(t, "deny", opinionatedEvent.Attributes["policy.action"])
		assert.Equal(t, "high", opinionatedEvent.Attributes["policy.severity"])
		assert.Equal(t, 0.8, opinionatedEvent.Attributes["policy.risk_score"])

		// Check traffic attributes
		assert.Equal(t, "10.244.1.100", opinionatedEvent.Attributes["source.ip"])
		assert.Equal(t, uint16(44321), opinionatedEvent.Attributes["source.port"])
		assert.Equal(t, "10.244.2.50", opinionatedEvent.Attributes["destination.ip"])
		assert.Equal(t, uint16(8080), opinionatedEvent.Attributes["destination.port"])
		assert.Equal(t, "tcp", opinionatedEvent.Attributes["protocol"])
		assert.Equal(t, uint64(2048), opinionatedEvent.Attributes["traffic.bytes_blocked"])
		assert.Equal(t, uint64(3), opinionatedEvent.Attributes["traffic.packets_blocked"])

		// Check pod context
		assert.Equal(t, "source-pod", opinionatedEvent.Attributes["source.pod.name"])
		assert.Equal(t, "application", opinionatedEvent.Attributes["source.pod.namespace"])
		assert.Equal(t, "dest-pod", opinionatedEvent.Attributes["destination.pod.name"])
		assert.Equal(t, "backend", opinionatedEvent.Attributes["destination.pod.namespace"])

		// Check labels and annotations
		assert.Equal(t, "security", opinionatedEvent.Attributes["label.policy"])
		assert.Equal(t, "cross-namespace", opinionatedEvent.Attributes["label.type"])
		assert.Equal(t, "policy-violation", opinionatedEvent.Attributes["annotation.reason"])

	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected policy violation event but none was generated")
	}

	// Verify metrics were updated
	collector.metrics.mutex.RLock()
	assert.Greater(t, collector.metrics.PolicyViolations, uint64(0))
	assert.Greater(t, collector.metrics.BlockedConnections, uint64(0))
	collector.metrics.mutex.RUnlock()
}

func TestPolicyCollector_SecurityImplicationDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	tests := []struct {
		name             string
		violation        *PolicyViolation
		expectedTags     []string
		expectedSeverity opinionated.Severity
	}{
		{
			name: "lateral movement attempt",
			violation: &PolicyViolation{
				Action:         "deny",
				SourcePod:      &PodInfo{Namespace: "app"},
				DestinationPod: &PodInfo{Namespace: "kube-system"},
			},
			expectedTags:     []string{"LATERAL_MOVEMENT"},
			expectedSeverity: opinionated.SeverityError,
		},
		{
			name: "privilege escalation attempt",
			violation: &PolicyViolation{
				DestinationPort: 22,
				Action:          "deny",
			},
			expectedTags:     []string{"PRIVILEGE_ESCALATION"},
			expectedSeverity: opinionated.SeverityCritical,
		},
		{
			name: "data exfiltration attempt",
			violation: &PolicyViolation{
				DestinationIP: net.ParseIP("8.8.8.8"),
				BytesBlocked:  2 * 1024 * 1024,
			},
			expectedTags:     []string{"DATA_EXFILTRATION"},
			expectedSeverity: opinionated.SeverityError,
		},
		{
			name: "reconnaissance activity",
			violation: &PolicyViolation{
				Action:         "deny",
				PacketsBlocked: 1,
			},
			expectedTags:     []string{"RECONNAISSANCE"},
			expectedSeverity: opinionated.SeverityWarning,
		},
		{
			name: "C2 communication",
			violation: &PolicyViolation{
				DestinationIP:   net.ParseIP("1.2.3.4"),
				DestinationPort: 4444,
			},
			expectedTags:     []string{"C2_COMMUNICATION"},
			expectedSeverity: opinionated.SeverityCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set defaults
			if tt.violation.ID == "" {
				tt.violation.ID = "test-violation"
			}
			if tt.violation.Timestamp.IsZero() {
				tt.violation.Timestamp = time.Now()
			}
			if tt.violation.PolicyName == "" {
				tt.violation.PolicyName = "test-policy"
			}
			if tt.violation.PolicyNamespace == "" {
				tt.violation.PolicyNamespace = "default"
			}
			if tt.violation.ViolationType == "" {
				tt.violation.ViolationType = "egress"
			}
			if tt.violation.SourceIP == nil {
				tt.violation.SourceIP = net.ParseIP("10.244.1.10")
			}
			if tt.violation.DestinationIP == nil {
				tt.violation.DestinationIP = net.ParseIP("10.244.2.20")
			}
			if tt.violation.Protocol == 0 {
				tt.violation.Protocol = 6 // TCP
			}
			if tt.violation.Action == "" {
				tt.violation.Action = "deny"
			}

			collector.processPolicyViolation(tt.violation)

			// Verify event contains expected security implications
			select {
			case event := <-eventCh:
				opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
				require.True(t, ok)

				// Check severity
				assert.Equal(t, tt.expectedSeverity, opinionatedEvent.Severity)

				// Check tags in message
				for _, tag := range tt.expectedTags {
					assert.Contains(t, opinionatedEvent.Message, fmt.Sprintf("[%s]", tag))
				}

				// Check security attributes
				for _, tag := range tt.expectedTags {
					securityKey := fmt.Sprintf("security.%s", strings.ToLower(tag))
					assert.Equal(t, true, opinionatedEvent.Attributes[securityKey])
				}

			case <-time.After(100 * time.Millisecond):
				t.Fatalf("Expected security implication event for %s but none was generated", tt.name)
			}
		})
	}
}

func TestPolicyCollector_UpdateMetrics(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	// Test denied violation
	deniedViolation := &PolicyViolation{
		Action: "deny",
	}
	collector.updatePolicyMetrics(deniedViolation)

	// Test allowed violation
	allowedViolation := &PolicyViolation{
		Action: "allow",
	}
	collector.updatePolicyMetrics(allowedViolation)

	collector.metrics.mutex.RLock()
	assert.Equal(t, uint64(2), collector.metrics.PolicyViolations)
	assert.Equal(t, uint64(1), collector.metrics.BlockedConnections)
	assert.Equal(t, uint64(1), collector.metrics.AllowedConnections)
	collector.metrics.mutex.RUnlock()
}

func TestNetworkPolicy_Validation(t *testing.T) {
	policy := &NetworkPolicy{
		Name:      "test-policy",
		Namespace: "default",
		Spec: &NetworkPolicySpec{
			PodSelector: &LabelSelector{
				MatchLabels: map[string]string{
					"app": "frontend",
				},
			},
			PolicyTypes: []PolicyType{PolicyTypeIngress, PolicyTypeEgress},
			Ingress: []NetworkPolicyIngressRule{
				{
					Ports: []NetworkPolicyPort{
						{
							Protocol: &Protocol("TCP"),
							Port: &IntOrString{
								Type:   TypeInt,
								IntVal: 80,
							},
						},
					},
				},
			},
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, "test-policy", policy.Name)
	assert.Equal(t, "default", policy.Namespace)
	assert.NotNil(t, policy.Spec)
	assert.Equal(t, "frontend", policy.Spec.PodSelector.MatchLabels["app"])
	assert.Len(t, policy.Spec.PolicyTypes, 2)
	assert.Len(t, policy.Spec.Ingress, 1)
}

// Benchmark tests for policy violation processing
func BenchmarkPolicyCollector_ProcessPolicyViolation(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval:     5 * time.Second,
		EnablePolicyMonitoring: true,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	eventCh := make(chan interface{}, 1000)
	collector.eventChan = eventCh

	violation := &PolicyViolation{
		ID:              "benchmark-violation",
		Timestamp:       time.Now(),
		PolicyName:      "test-policy",
		PolicyNamespace: "default",
		ViolationType:   "egress",
		SourceIP:        net.ParseIP("10.244.1.10"),
		DestinationIP:   net.ParseIP("10.244.2.20"),
		DestinationPort: 80,
		Protocol:        6,
		Action:          "deny",
		Severity:        "medium",
		RiskScore:       0.5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		violation.ID = fmt.Sprintf("benchmark-violation-%d", i)
		collector.processPolicyViolation(violation)
	}
}

func BenchmarkPolicyCollector_SecurityImplicationDetection(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	violations := []*PolicyViolation{
		{
			Action:         "deny",
			SourcePod:      &PodInfo{Namespace: "app"},
			DestinationPod: &PodInfo{Namespace: "kube-system"},
		},
		{
			DestinationPort: 22,
			Action:          "deny",
		},
		{
			DestinationIP: net.ParseIP("8.8.8.8"),
			BytesBlocked:  2 * 1024 * 1024,
		},
		{
			Action:         "deny",
			PacketsBlocked: 1,
		},
		{
			DestinationIP:   net.ParseIP("1.2.3.4"),
			DestinationPort: 4444,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		violation := violations[i%len(violations)]
		collector.isLateralMovementAttempt(violation)
		collector.isPrivilegeEscalationAttempt(violation)
		collector.isDataExfiltrationAttempt(violation)
		collector.isReconnaissanceActivity(violation)
		collector.isC2Communication(violation)
	}
}
