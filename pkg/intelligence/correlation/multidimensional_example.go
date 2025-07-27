//go:build experimental
// +build experimental

package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// MultiDimensionalCorrelationDemo demonstrates the correlation engine in action
func MultiDimensionalCorrelationDemo() {
	fmt.Println("\n=== Multi-Dimensional K8s Correlation Demo ===\n")

	// Create engine with all dimensions enabled
	logger, _ := zap.NewProduction()
	config := EngineConfig{
		TemporalWindow:   5 * time.Minute,
		CausalWindow:     2 * time.Minute,
		MinConfidence:    0.7,
		MinCorrelation:   0.6,
		MaxGraphSize:     10000,
		EnableOwnership:  true,
		EnableSpatial:    true,
		EnableTemporal:   true,
		EnableCausal:     true,
		EnableSemantic:   true,
		EnableDependency: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	// Scenario: E-commerce platform experiencing cascading failure
	fmt.Println("## Scenario: E-commerce Platform Cascading Failure\n")

	baseTime := time.Now()

	// 1. Database starts experiencing high load
	dbHighLoad := &domain.UnifiedEvent{
		ID:        "db-high-load",
		Timestamp: baseTime,
		Type:      domain.EventTypeMetrics,
		Source:    "prometheus",
		Message:   "MySQL CPU usage at 95%",
		Severity:  domain.EventSeverityWarning,
		K8sContext: &domain.K8sContext{
			Name:         "mysql-primary",
			Namespace:    "production",
			WorkloadKind: "StatefulSet",
			WorkloadName: "mysql",
			NodeName:     "node-db-1",
			Zone:         "us-east-1a",
			Labels: map[string]string{
				"app":  "mysql",
				"role": "primary",
			},
		},
		Metrics: &domain.MetricsData{
			MetricName: "container_cpu_usage_seconds_total",
			Value:      0.95,
			Unit:       "ratio",
		},
		Semantic: &domain.SemanticContext{
			Intent:   "resource-pressure",
			Category: "performance",
			Domain:   "database",
		},
	}

	results, _ := engine.Process(ctx, dbHighLoad)
	printResults("DB High Load", results)

	// 2. Database connection pool exhaustion
	time.Sleep(100 * time.Millisecond)
	dbConnExhausted := &domain.UnifiedEvent{
		ID:        "db-conn-exhausted",
		Timestamp: baseTime.Add(30 * time.Second),
		Type:      domain.EventTypeApplication,
		Source:    "mysql",
		Message:   "Too many connections",
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:         "mysql-primary",
			Namespace:    "production",
			WorkloadKind: "StatefulSet",
			WorkloadName: "mysql",
			NodeName:     "node-db-1",
			Zone:         "us-east-1a",
			Labels: map[string]string{
				"app":  "mysql",
				"role": "primary",
			},
		},
		Application: &domain.ApplicationData{
			Level:     "error",
			ErrorType: "TooManyConnectionsError",
		},
		Semantic: &domain.SemanticContext{
			Intent:   "connection-exhaustion",
			Category: "availability",
			Domain:   "database",
		},
	}

	results, _ = engine.Process(ctx, dbConnExhausted)
	printResults("DB Connection Exhaustion", results)

	// 3. Order service starts failing
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		orderFailure := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("order-failure-%d", i),
			Timestamp: baseTime.Add(35*time.Second + time.Duration(i)*time.Second),
			Type:      domain.EventTypeApplication,
			Source:    "order-service",
			Message:   "Database connection timeout",
			Severity:  domain.EventSeverityError,
			K8sContext: &domain.K8sContext{
				Name:         fmt.Sprintf("order-service-%d", i),
				Namespace:    "production",
				WorkloadKind: "Deployment",
				WorkloadName: "order-service",
				NodeName:     "node-app-2",
				Zone:         "us-east-1b",
				Labels: map[string]string{
					"app":     "order-service",
					"tier":    "backend",
					"version": "v2.1",
				},
				Dependencies: []domain.ResourceDependency{
					{
						Kind:      "Service",
						Name:      "mysql",
						Namespace: "production",
						Type:      "database",
						Required:  true,
					},
				},
			},
			Application: &domain.ApplicationData{
				Level:      "error",
				ErrorType:  "DatabaseConnectionTimeout",
				StackTrace: "at OrderRepository.save()\nat OrderService.createOrder()",
			},
			Semantic: &domain.SemanticContext{
				Intent:   "database-connection-failure",
				Category: "availability",
				Domain:   "orders",
			},
			Impact: &domain.ImpactContext{
				Severity:         "high",
				BusinessImpact:   0.8,
				CustomerFacing:   true,
				RevenueImpacting: true,
				AffectedServices: []string{"order-service"},
			},
		}

		results, _ = engine.Process(ctx, orderFailure)
		if i == 2 { // Print results for last one
			printResults("Order Service Failures", results)
		}
	}

	// 4. Payment service affected
	time.Sleep(100 * time.Millisecond)
	paymentTimeout := &domain.UnifiedEvent{
		ID:        "payment-timeout",
		Timestamp: baseTime.Add(40 * time.Second),
		Type:      domain.EventTypeNetwork,
		Source:    "envoy",
		Message:   "Upstream request timeout",
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:         "payment-service-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "payment-service",
			NodeName:     "node-app-3",
			Zone:         "us-east-1b",
			Labels: map[string]string{
				"app":  "payment-service",
				"tier": "backend",
			},
			Consumers: []domain.K8sResourceRef{
				{
					Kind:      "Service",
					Name:      "order-service",
					Namespace: "production",
				},
			},
		},
		Network: &domain.NetworkData{
			Protocol:   "HTTP",
			SourceIP:   "10.244.2.15",
			DestIP:     "10.96.1.200", // order-service ClusterIP
			DestPort:   8080,
			StatusCode: 504,
			Latency:    30000000000, // 30 seconds
		},
		Semantic: &domain.SemanticContext{
			Intent:   "service-timeout",
			Category: "availability",
			Domain:   "payments",
		},
		CorrelationHints: []string{
			"service:production/order-service:8080",
			"high_latency",
			"server_error",
		},
	}

	results, _ = engine.Process(ctx, paymentTimeout)
	printResults("Payment Service Timeout", results)

	// 5. Frontend getting 503s
	time.Sleep(100 * time.Millisecond)
	frontendErrors := &domain.UnifiedEvent{
		ID:        "frontend-503",
		Timestamp: baseTime.Add(45 * time.Second),
		Type:      domain.EventTypeNetwork,
		Source:    "nginx",
		Message:   "Service unavailable",
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:         "frontend-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "frontend",
			NodeName:     "node-web-1",
			Zone:         "us-east-1c",
			Labels: map[string]string{
				"app":  "frontend",
				"tier": "web",
			},
		},
		Network: &domain.NetworkData{
			Protocol:   "HTTP",
			Method:     "POST",
			Path:       "/api/v1/orders",
			StatusCode: 503,
		},
		Semantic: &domain.SemanticContext{
			Intent:   "service-unavailable",
			Category: "availability",
			Domain:   "customer-facing",
		},
		Impact: &domain.ImpactContext{
			Severity:         "critical",
			BusinessImpact:   0.95,
			CustomerFacing:   true,
			RevenueImpacting: true,
			AffectedUsers:    1250,
			AffectedServices: []string{"frontend", "order-service", "payment-service"},
		},
	}

	results, _ = engine.Process(ctx, frontendErrors)
	printResults("Frontend Service Unavailable", results)

	// 6. SRE gets paged - high-level event
	time.Sleep(100 * time.Millisecond)
	incidentEvent := &domain.UnifiedEvent{
		ID:        "incident-page",
		Timestamp: baseTime.Add(50 * time.Second),
		Type:      domain.EventTypeLog,
		Source:    "pagerduty",
		Message:   "Critical: Multiple services down in production",
		Severity:  domain.EventSeverityCritical,
		K8sContext: &domain.K8sContext{
			Namespace: "production",
		},
		Semantic: &domain.SemanticContext{
			Intent:   "incident-triggered",
			Category: "operations",
			Domain:   "platform",
		},
	}

	results, _ = engine.Process(ctx, incidentEvent)
	printResults("Incident Triggered - Full Correlation", results)

	// Print final statistics
	fmt.Println("\n## Correlation Engine Statistics")
	stats := engine.graph.Stats()
	fmt.Printf("Total Events Processed: %d\n", stats.TotalEvents)
	fmt.Printf("Events in Graph: %d\n", stats.CurrentEvents)
	fmt.Printf("Unique Workloads: %d\n", stats.Indexes.Workloads)
	fmt.Printf("Nodes Affected: %d\n", stats.Indexes.Nodes)
	fmt.Printf("Zones Affected: %d\n", stats.Indexes.Zones)

	fmt.Println("\n## Key Insights:")
	fmt.Println("1. Root Cause: Database CPU exhaustion led to connection pool exhaustion")
	fmt.Println("2. Cascade Path: Database → Order Service → Payment Service → Frontend")
	fmt.Println("3. Cross-Zone Impact: Started in us-east-1a, spread to 1b and 1c")
	fmt.Println("4. Business Impact: 1250 users affected, revenue-impacting")
	fmt.Println("5. Multi-Dimensional: Ownership (same app), Spatial (cross-zone), Temporal (burst), Causal (dependency failure)")
}

func printResults(event string, results []*MultiDimCorrelationResult) {
	fmt.Printf("\n### Event: %s\n", event)

	if len(results) == 0 {
		fmt.Println("No correlations found yet (building context)")
		return
	}

	for _, result := range results {
		fmt.Printf("\nCorrelation Found: %s\n", result.Type)
		fmt.Printf("Confidence: %.2f\n", result.Confidence)
		fmt.Printf("Events Correlated: %d\n", len(result.Events))

		// Print dimensions involved
		fmt.Println("Dimensions:")
		for _, dim := range result.Dimensions {
			fmt.Printf("  - %s: %s (confidence: %.2f)\n",
				dim.Dimension, dim.Type, dim.Confidence)
		}

		// Print root cause if identified
		if result.RootCause != nil {
			fmt.Printf("\nRoot Cause Analysis:\n")
			fmt.Printf("  Root Event: %s\n", result.RootCause.EventID)
			fmt.Printf("  Confidence: %.2f\n", result.RootCause.Confidence)
			fmt.Printf("  Reasoning: %s\n", result.RootCause.Reasoning)

			if len(result.RootCause.CausalChain) > 0 {
				fmt.Println("  Causal Chain:")
				for i, step := range result.RootCause.CausalChain {
					fmt.Printf("    %d. %s - %s\n", i+1,
						step.Timestamp.Format("15:04:05"),
						step.Description)
				}
			}
		}

		// Print impact
		if result.Impact != nil {
			fmt.Printf("\nImpact Analysis:\n")
			fmt.Printf("  Severity: %s\n", result.Impact.Severity)
			fmt.Printf("  Business Impact: %.2f\n", result.Impact.BusinessImpact)
			fmt.Printf("  Affected Services: %v\n", result.Impact.ServiceImpact)
			if result.Impact.UserImpact > 0 {
				fmt.Printf("  Users Affected: %d\n", result.Impact.UserImpact)
			}
		}

		// Print recommendation
		if result.Recommendation != "" {
			fmt.Printf("\nRecommendation: %s\n", result.Recommendation)
		}
	}
}

// DimensionExamples shows how each dimension works
func DimensionExamples() {
	fmt.Println("\n=== K8s Correlation Dimensions Explained ===\n")

	fmt.Println("## 1. Ownership Dimension")
	fmt.Println("Correlates events by K8s ownership hierarchy:")
	fmt.Println("- Same Deployment/StatefulSet/DaemonSet")
	fmt.Println("- Same ReplicaSet (for rolling updates)")
	fmt.Println("- Same application label")
	fmt.Println("Example: All pods from 'api' deployment crashing")

	fmt.Println("\n## 2. Spatial Dimension")
	fmt.Println("Correlates events by K8s topology:")
	fmt.Println("- Same node (node-level issues)")
	fmt.Println("- Same zone (zone failures)")
	fmt.Println("- Same namespace (tenant issues)")
	fmt.Println("- Cross-namespace (security/network policies)")
	fmt.Println("Example: All pods on node-1 experiencing network issues")

	fmt.Println("\n## 3. Temporal Dimension")
	fmt.Println("Correlates events by time patterns:")
	fmt.Println("- Event bursts (many events quickly)")
	fmt.Println("- Periodic patterns (recurring issues)")
	fmt.Println("- Cascades (rapid succession)")
	fmt.Println("Example: 50 error events within 10 seconds")

	fmt.Println("\n## 4. Causal Dimension")
	fmt.Println("Correlates events by cause-effect:")
	fmt.Println("- Resource exhaustion → Pod failures")
	fmt.Println("- Config changes → Restarts")
	fmt.Println("- Service failures → Consumer errors")
	fmt.Println("- Error propagation chains")
	fmt.Println("Example: OOM kill → Pod restart → Service disruption")

	fmt.Println("\n## 5. Semantic Dimension")
	fmt.Println("Correlates events by meaning:")
	fmt.Println("- Same intent (e.g., 'connection-failure')")
	fmt.Println("- Same category (e.g., 'security')")
	fmt.Println("- Same domain (e.g., 'payments')")
	fmt.Println("- Domain-specific patterns")
	fmt.Println("Example: All 'authentication-failed' events")

	fmt.Println("\n## 6. Dependency Dimension")
	fmt.Println("Correlates events by K8s dependencies:")
	fmt.Println("- Shared ConfigMaps/Secrets")
	fmt.Println("- Shared PVCs")
	fmt.Println("- Service dependencies")
	fmt.Println("- Consumer relationships")
	fmt.Println("Example: All pods using corrupted ConfigMap failing")
}

// PhilosophicalContext explains the Kantian approach
func PhilosophicalContext() {
	fmt.Println("\n=== Kantian Epistemology Applied to K8s ===\n")

	fmt.Println("## The K8s Noumenon")
	fmt.Println("K8s cluster state is unknowable in its totality (noumenon)")
	fmt.Println("We only observe phenomena (events) through our collectors")

	fmt.Println("\n## Categories of Understanding")
	fmt.Println("We apply mental categories to make sense of phenomena:")
	fmt.Println("1. Ownership - Identity and belonging")
	fmt.Println("2. Spatial - Location and topology")
	fmt.Println("3. Temporal - Time and sequence")
	fmt.Println("4. Causal - Cause and effect")
	fmt.Println("5. Semantic - Meaning and intent")
	fmt.Println("6. Dependency - Relationships and requirements")

	fmt.Println("\n## Synthetic A Priori Judgments")
	fmt.Println("The correlation engine makes judgments that are:")
	fmt.Println("- Synthetic: Add new knowledge (correlations)")
	fmt.Println("- A Priori: Based on K8s structure, not just observation")
	fmt.Println("Example: 'Pods in same Deployment correlate' - true by K8s design")

	fmt.Println("\n## Transcendental Deduction")
	fmt.Println("We deduce hidden relationships from observable events:")
	fmt.Println("- From pod failures → infer node issues")
	fmt.Println("- From timing patterns → infer causality")
	fmt.Println("- From error types → infer system state")

	fmt.Println("\n## The Limits of Observability")
	fmt.Println("Like Kant's limits of reason, we acknowledge:")
	fmt.Println("- Not all correlations are causal")
	fmt.Println("- Some system states remain hidden")
	fmt.Println("- Context enrichment has boundaries")
	fmt.Println("- Perfect knowledge is impossible")
}
