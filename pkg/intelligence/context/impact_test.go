package context

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestImpactAnalyzer_AssessImpact(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name             string
		event            *domain.UnifiedEvent
		expectedSeverity string
		expectedCustomer bool
		expectedRevenue  bool
		expectedSLO      bool
		minImpact        float64
		maxImpact        float64
	}{
		{
			name:             "nil event",
			event:            nil,
			expectedSeverity: "",
			expectedCustomer: false,
			expectedRevenue:  false,
			expectedSLO:      false,
			minImpact:        0,
			maxImpact:        0,
		},
		{
			name: "critical payment service error",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeService,
				Source:    "k8s",
				Entity: &domain.EntityContext{
					Type:      "Service",
					Name:      "payment-service",
					Namespace: "production",
				},
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Payment processing failed",
				},
			},
			expectedSeverity: "critical",
			expectedCustomer: true,
			expectedRevenue:  true,
			expectedSLO:      true,
			minImpact:        0.8,
			maxImpact:        1.0,
		},
		{
			name: "high severity API gateway timeout",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Entity: &domain.EntityContext{
					Type:      "Service",
					Name:      "api-gateway",
					Namespace: "prod",
				},
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Connection timeout to backend service",
				},
			},
			expectedSeverity: "high",
			expectedCustomer: true,
			expectedRevenue:  false,
			expectedSLO:      true,
			minImpact:        0.6,
			maxImpact:        0.8,
		},
		{
			name: "medium severity database warning",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "k8s",
				Kubernetes: &domain.KubernetesData{
					EventType: "Warning",
					Object:    "StatefulSet/postgres-primary",
					Reason:    "HighMemoryUsage",
				},
			},
			expectedSeverity: "low",
			expectedCustomer: false,
			expectedRevenue:  false,
			expectedSLO:      false,
			minImpact:        0.1,
			maxImpact:        0.3,
		},
		{
			name: "low severity info log",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeLog,
				Source:    "app",
				Application: &domain.ApplicationData{
					Level:   "info",
					Message: "Cache refreshed successfully",
				},
			},
			expectedSeverity: "low",
			expectedCustomer: false,
			expectedRevenue:  false,
			expectedSLO:      false,
			minImpact:        0.0,
			maxImpact:        0.3,
		},
		{
			name: "critical system OOM event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "ebpf",
				Entity: &domain.EntityContext{
					Type:      "Pod",
					Name:      "checkout-service",
					Namespace: "production",
				},
				Kernel: &domain.KernelData{
					Comm:    "oom_reaper",
					Syscall: "kill",
					PID:     1234,
				},
			},
			expectedSeverity: "critical",
			expectedCustomer: true,
			expectedRevenue:  true,
			expectedSLO:      true,
			minImpact:        0.75,
			maxImpact:        1.0,
		},
		{
			name: "revenue impacting cart service error",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeService,
				Source:    "app",
				Entity: &domain.EntityContext{
					Type:      "Service",
					Name:      "cart-service",
					Namespace: "default",
				},
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Failed to update cart items",
				},
			},
			expectedSeverity: "high",
			expectedCustomer: true,
			expectedRevenue:  true,
			expectedSLO:      false,
			minImpact:        0.6,
			maxImpact:        0.8,
		},
		{
			name: "staging environment error",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeService,
				Source:    "k8s",
				Entity: &domain.EntityContext{
					Type:      "Service",
					Name:      "test-service",
					Namespace: "staging",
				},
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Service unavailable",
				},
			},
			expectedSeverity: "high",
			expectedCustomer: false,
			expectedRevenue:  false,
			expectedSLO:      false,
			minImpact:        0.25,
			maxImpact:        0.35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impact := analyzer.AssessImpact(tt.event)

			if tt.event == nil {
				if impact != nil {
					t.Errorf("Expected nil impact for nil event, got %v", impact)
				}
				return
			}

			if impact == nil {
				t.Fatal("Expected non-nil impact")
			}

			if impact.Severity != tt.expectedSeverity {
				t.Errorf("Severity = %v, want %v", impact.Severity, tt.expectedSeverity)
			}

			if impact.CustomerFacing != tt.expectedCustomer {
				t.Errorf("CustomerFacing = %v, want %v", impact.CustomerFacing, tt.expectedCustomer)
			}

			if impact.RevenueImpacting != tt.expectedRevenue {
				t.Errorf("RevenueImpacting = %v, want %v", impact.RevenueImpacting, tt.expectedRevenue)
			}

			if impact.SLOImpact != tt.expectedSLO {
				t.Errorf("SLOImpact = %v, want %v", impact.SLOImpact, tt.expectedSLO)
			}

			if impact.BusinessImpact < tt.minImpact || impact.BusinessImpact > tt.maxImpact {
				t.Errorf("BusinessImpact = %v, want between %v and %v",
					impact.BusinessImpact, tt.minImpact, tt.maxImpact)
			}

			if impact.BusinessImpact < 0.0 || impact.BusinessImpact > 1.0 {
				t.Errorf("BusinessImpact = %v, must be between 0.0 and 1.0", impact.BusinessImpact)
			}
		})
	}
}

func TestImpactAnalyzer_isCustomerFacing(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected bool
	}{
		{
			name: "frontend service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "frontend",
				},
			},
			expected: true,
		},
		{
			name: "api-gateway service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "api-gateway",
				},
			},
			expected: true,
		},
		{
			name: "service in frontend namespace",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name:      "some-service",
					Namespace: "frontend-prod",
				},
			},
			expected: true,
		},
		{
			name: "kubernetes object with customer service",
			event: &domain.UnifiedEvent{
				Kubernetes: &domain.KubernetesData{
					Object: "Deployment/web-api",
				},
			},
			expected: true,
		},
		{
			name: "log message mentioning customer",
			event: &domain.UnifiedEvent{
				Application: &domain.ApplicationData{
					Message: "Customer login failed",
				},
			},
			expected: true,
		},
		{
			name: "internal service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name:      "cache-service",
					Namespace: "internal",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.isCustomerFacing(tt.event)
			if got != tt.expected {
				t.Errorf("isCustomerFacing() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImpactAnalyzer_isRevenueImpacting(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected bool
	}{
		{
			name: "payment namespace",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Namespace: "payments",
				},
			},
			expected: true,
		},
		{
			name: "billing service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "billing-processor",
				},
			},
			expected: true,
		},
		{
			name: "payment error log",
			event: &domain.UnifiedEvent{
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Payment transaction failed",
				},
			},
			expected: true,
		},
		{
			name: "regular service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name:      "logging-service",
					Namespace: "monitoring",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.isRevenueImpacting(tt.event)
			if got != tt.expected {
				t.Errorf("isRevenueImpacting() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImpactAnalyzer_hasSLOImpact(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected bool
	}{
		{
			name: "SLO monitored service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "payment-service",
				},
			},
			expected: true,
		},
		{
			name: "network timeout error",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeNetwork,
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Connection timeout",
				},
			},
			expected: true,
		},
		{
			name: "service unavailable",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeService,
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Service unavailable",
				},
			},
			expected: true,
		},
		{
			name: "regular service info log",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "cache-service",
				},
				Application: &domain.ApplicationData{
					Level:   "info",
					Message: "Cache refreshed",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.hasSLOImpact(tt.event)
			if got != tt.expected {
				t.Errorf("hasSLOImpact() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImpactAnalyzer_identifyAffectedServices(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected []string
	}{
		{
			name: "service from entity",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "user-service",
				},
			},
			expected: []string{"user-service"},
		},
		{
			name: "service from kubernetes pod",
			event: &domain.UnifiedEvent{
				Kubernetes: &domain.KubernetesData{
					Object: "Pod/payment-service-7d4b8c6f5-x2p4n",
				},
			},
			expected: []string{"payment-service"},
		},
		{
			name: "service from network port",
			event: &domain.UnifiedEvent{
				Network: &domain.NetworkData{
					DestPort: 5432,
				},
			},
			expected: []string{"postgresql"},
		},
		{
			name: "multiple services",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Name: "api-gateway",
				},
				Network: &domain.NetworkData{
					DestPort: 6379,
				},
			},
			expected: []string{"api-gateway", "redis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.identifyAffectedServices(tt.event)

			// Check length
			if len(got) != len(tt.expected) {
				t.Errorf("identifyAffectedServices() returned %d services, want %d", len(got), len(tt.expected))
				return
			}

			// Check each expected service is present
			for _, expectedService := range tt.expected {
				found := false
				for _, service := range got {
					if service == expectedService {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected service %s not found in result", expectedService)
				}
			}
		})
	}
}

func TestImpactAnalyzer_estimateAffectedUsers(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		impact   *domain.ImpactContext
		minUsers int
		maxUsers int
	}{
		{
			name: "critical customer-facing production",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Namespace: "production",
				},
			},
			impact: &domain.ImpactContext{
				CustomerFacing: true,
				Severity:       "critical",
			},
			minUsers: 10000,
			maxUsers: 20000,
		},
		{
			name: "high severity internal service",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Namespace: "internal",
				},
			},
			impact: &domain.ImpactContext{
				CustomerFacing: false,
				Severity:       "high",
			},
			minUsers: 100,
			maxUsers: 1000,
		},
		{
			name: "staging environment",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Namespace: "staging",
				},
			},
			impact: &domain.ImpactContext{
				CustomerFacing: true,
				Severity:       "critical",
			},
			minUsers: 100,
			maxUsers: 2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := analyzer.estimateAffectedUsers(tt.event, tt.impact)
			if users < tt.minUsers || users > tt.maxUsers {
				t.Errorf("estimateAffectedUsers() = %d, want between %d and %d",
					users, tt.minUsers, tt.maxUsers)
			}
		})
	}
}

func TestImpactAnalyzer_CustomConfiguration(t *testing.T) {
	customCritical := map[string]bool{"custom-critical": true}
	customRevenue := map[string]bool{"custom-revenue": true}
	customCustomer := map[string]bool{"custom-frontend": true}
	customSLO := map[string]bool{"custom-slo": true}

	analyzer := NewImpactAnalyzerWithConfig(
		customCritical,
		customRevenue,
		customCustomer,
		customSLO,
	)

	// Test custom critical namespace
	event := &domain.UnifiedEvent{
		ID:        "test",
		Timestamp: time.Now(),
		Type:      domain.EventTypeService,
		Source:    "test",
		Entity: &domain.EntityContext{
			Namespace: "custom-critical",
		},
	}
	impact := analyzer.AssessImpact(event)
	if impact.BusinessImpact < 0.45 {
		t.Errorf("Custom critical namespace should have high impact, got %v", impact.BusinessImpact)
	}

	// Test custom revenue namespace
	event.Entity.Namespace = "custom-revenue"
	impact = analyzer.AssessImpact(event)
	if !impact.RevenueImpacting {
		t.Error("Custom revenue namespace should be revenue impacting")
	}

	// Test custom customer service
	event.Entity.Name = "custom-frontend"
	event.Entity.Namespace = "default"
	impact = analyzer.AssessImpact(event)
	if !impact.CustomerFacing {
		t.Error("Custom customer service should be customer facing")
	}

	// Test custom SLO service
	event.Entity.Name = "custom-slo"
	impact = analyzer.AssessImpact(event)
	if !impact.SLOImpact {
		t.Error("Custom SLO service should have SLO impact")
	}
}

func TestImpactAnalyzer_ConfigurationMethods(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	// Add custom namespace
	analyzer.AddCriticalNamespace("CUSTOM-NS")
	event := &domain.UnifiedEvent{
		ID:        "test",
		Timestamp: time.Now(),
		Type:      domain.EventTypeService,
		Source:    "test",
		Entity: &domain.EntityContext{
			Namespace: "custom-ns", // Should match case-insensitive
		},
	}
	if !analyzer.isInCriticalNamespace(event) {
		t.Error("Added critical namespace should be recognized")
	}

	// Add revenue namespace
	analyzer.AddRevenueNamespace("Revenue-NS")
	event.Entity.Namespace = "revenue-ns"
	if !analyzer.isRevenueImpacting(event) {
		t.Error("Added revenue namespace should be recognized")
	}

	// Add customer service
	analyzer.AddCustomerService("Custom-Service")
	event.Entity.Name = "custom-service"
	if !analyzer.isCustomerFacing(event) {
		t.Error("Added customer service should be recognized")
	}

	// Add SLO service
	analyzer.AddSLOService("SLO-Service")
	event.Entity.Name = "slo-service"
	if !analyzer.hasSLOImpact(event) {
		t.Error("Added SLO service should be recognized")
	}

	// Set severity thresholds
	analyzer.SetSeverityThresholds(0.9, 0.7, 0.4)
	event.Application = &domain.ApplicationData{
		Level:   "error",
		Message: "Test error",
	}
	impact := analyzer.AssessImpact(event)
	// With higher thresholds, same impact score should result in lower severity
	if impact.Severity == "critical" && impact.BusinessImpact < 0.9 {
		t.Error("Severity should respect custom thresholds")
	}
}

func TestExtractServiceName(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		expected string
	}{
		{
			name:     "deployment with replica set and pod hash",
			podName:  "payment-service-7d4b8c6f5-x2p4n",
			expected: "payment-service",
		},
		{
			name:     "statefulset pod",
			podName:  "postgres-primary-0",
			expected: "postgres-primary-0", // No hash pattern
		},
		{
			name:     "simple pod name",
			podName:  "nginx",
			expected: "nginx",
		},
		{
			name:     "deployment with single hash",
			podName:  "web-app-x2p4n",
			expected: "web-app",
		},
		{
			name:     "complex service name",
			podName:  "api-gateway-service-abc123-def456",
			expected: "api-gateway-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractServiceName(tt.podName)
			if got != tt.expected {
				t.Errorf("extractServiceName(%s) = %s, want %s", tt.podName, got, tt.expected)
			}
		})
	}
}

func TestIsHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid hash", "7d4b8c6f5", true},
		{"valid short hash", "x2p4n", true},
		{"too short", "abc", false},
		{"too long", "abcdefghijk123", false},
		{"only letters", "abcdef", false},
		{"only numbers", "123456", false},
		{"with special chars", "abc-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHash(tt.input)
			if got != tt.expected {
				t.Errorf("isHash(%s) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestImpactAnalyzer_EdgeCases(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	t.Run("fatal application log", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      domain.EventTypeLog,
			Source:    "app",
			Application: &domain.ApplicationData{
				Level:   "fatal",
				Message: "Application crash",
			},
		}
		impact := analyzer.AssessImpact(event)
		if impact.Severity != "critical" {
			t.Errorf("Fatal log should have critical severity, got %s", impact.Severity)
		}
	})

	t.Run("kernel panic event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      domain.EventTypeSystem,
			Source:    "ebpf",
			Kernel: &domain.KernelData{
				Syscall: "panic",
			},
		}
		impact := analyzer.AssessImpact(event)
		if impact.Severity != "critical" {
			t.Errorf("Kernel panic should have critical severity, got %s", impact.Severity)
		}
	})

	t.Run("service crash with panic message", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      domain.EventTypeService,
			Source:    "app",
			Application: &domain.ApplicationData{
				Level:   "error",
				Message: "Service panic: runtime error",
			},
		}
		impact := analyzer.AssessImpact(event)
		if impact.Severity != "critical" {
			t.Errorf("Service panic should have critical severity, got %s", impact.Severity)
		}
	})

	t.Run("warning level log", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      domain.EventTypeLog,
			Source:    "app",
			Application: &domain.ApplicationData{
				Level:   "warn",
				Message: "High memory usage",
			},
		}
		impact := analyzer.AssessImpact(event)
		if impact.Severity == "critical" || impact.Severity == "high" {
			t.Errorf("Warning log should not have %s severity", impact.Severity)
		}
	})

	t.Run("network event on common ports", func(t *testing.T) {
		ports := map[uint16]string{
			80:   "web-service",
			443:  "https-service",
			3306: "mysql",
			5432: "postgresql",
			6379: "redis",
			9200: "elasticsearch",
			5672: "rabbitmq",
			9092: "kafka",
		}

		for port, expectedService := range ports {
			event := &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					DestPort: port,
				},
			}
			impact := analyzer.AssessImpact(event)
			found := false
			for _, service := range impact.AffectedServices {
				if service == expectedService {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Port %d should identify service %s", port, expectedService)
			}
		}
	})

	t.Run("unknown event type", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      "custom-type",
			Source:    "custom",
		}
		impact := analyzer.AssessImpact(event)
		if impact == nil {
			t.Error("Should handle unknown event types gracefully")
		}
		if impact.BusinessImpact < 0 || impact.BusinessImpact > 1 {
			t.Errorf("Business impact should be valid even for unknown types: %v", impact.BusinessImpact)
		}
	})

	t.Run("event with all impact factors", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test",
			Timestamp: time.Now(),
			Type:      domain.EventTypeService,
			Source:    "k8s",
			Entity: &domain.EntityContext{
				Type:      "Service",
				Name:      "payment-service",
				Namespace: "production",
			},
			Application: &domain.ApplicationData{
				Level:   "fatal",
				Message: "Payment service crashed",
			},
		}
		impact := analyzer.AssessImpact(event)
		if impact.BusinessImpact < 0.95 {
			t.Errorf("Event with all critical factors should have near-maximum impact, got %v", impact.BusinessImpact)
		}
		if impact.Severity != "critical" {
			t.Errorf("Event with all critical factors should have critical severity, got %s", impact.Severity)
		}
	})
}

func TestImpactAnalyzer_AllEventTypes(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	eventTypes := []domain.EventType{
		domain.EventTypeSystem,
		domain.EventTypeCPU,
		domain.EventTypeMemory,
		domain.EventTypeDisk,
		domain.EventTypeNetwork,
		domain.EventTypeProcess,
		domain.EventTypeLog,
		domain.EventTypeKubernetes,
		domain.EventTypeService,
		"unknown-type",
	}

	for _, eventType := range eventTypes {
		t.Run(string(eventType), func(t *testing.T) {
			event := &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      eventType,
				Source:    "test",
			}

			// Should not panic
			impact := analyzer.AssessImpact(event)
			if impact == nil {
				t.Error("AssessImpact should not return nil")
			}
			if impact.BusinessImpact < 0 || impact.BusinessImpact > 1 {
				t.Errorf("Business impact for %s should be between 0 and 1, got %v",
					eventType, impact.BusinessImpact)
			}

			validSeverities := map[string]bool{
				"critical": true,
				"high":     true,
				"medium":   true,
				"low":      true,
			}
			if !validSeverities[impact.Severity] {
				t.Errorf("Invalid severity %s for event type %s", impact.Severity, eventType)
			}
		})
	}
}

func TestImpactAnalyzer_CustomerFacingPatterns(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	patterns := []string{
		"user authentication failed",
		"customer order processed",
		"client connection timeout",
		"frontend rendering error",
		"UI component crashed",
		"mobile app sync failed",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				Application: &domain.ApplicationData{
					Message: pattern,
				},
			}
			if !analyzer.isCustomerFacing(event) {
				t.Errorf("Pattern '%s' should be identified as customer-facing", pattern)
			}
		})
	}
}

func TestImpactAnalyzer_RevenuePatterns(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	patterns := []string{
		"payment processing failed",
		"transaction timeout",
		"billing cycle error",
		"invoice generation failed",
		"charge declined",
		"refund processing error",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: pattern,
				},
			}
			if !analyzer.isRevenueImpacting(event) {
				t.Errorf("Pattern '%s' should be identified as revenue-impacting", pattern)
			}
		})
	}
}

func TestImpactAnalyzer_SLOPatterns(t *testing.T) {
	analyzer := NewImpactAnalyzer()

	patterns := []string{
		"request timeout",
		"service unavailable",
		"connection refused",
		"high latency detected",
		"response too slow",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				Type: domain.EventTypeNetwork,
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: pattern,
				},
			}
			if !analyzer.hasSLOImpact(event) {
				t.Errorf("Pattern '%s' should be identified as SLO-impacting", pattern)
			}
		})
	}
}
