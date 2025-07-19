// OTEL Connection Test - Verifies Tapio's native OTEL output actually sends traces
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yairfalse/tapio/pkg/health"
	"github.com/yairfalse/tapio/pkg/output"
)

func main() {
	fmt.Println("=== Tapio OTEL Connection Test ===")
	fmt.Println("This test verifies that OTEL traces are actually sent to collectors")
	fmt.Println()

	// Get endpoint from environment or use default
	endpoint := os.Getenv("OTEL_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	fmt.Printf("Testing connection to OTEL collector at: %s\n", endpoint)
	fmt.Println("Make sure your OTEL collector (Jaeger/Tempo/etc) is running!")
	fmt.Println()

	// Create OTEL output configuration
	config := &output.OTELOutputConfig{
		Endpoint:                 endpoint,
		ServiceName:              "tapio-otel-test",
		ServiceVersion:           "1.0.0",
		ServiceInstance:          "test-instance",
		Insecure:                 true,
		Timeout:                  10 * time.Second,
		IncludeHumanExplanations: true,
		IncludePredictions:       true,
		IncludeRecommendations:   true,
		IncludeBusinessImpact:    true,
	}

	// Create OTEL native output
	otelOutput, err := output.NewOTELNativeOutput(config)
	if err != nil {
		log.Fatalf("Failed to create OTEL output: %v", err)
	}
	defer func() {
		fmt.Println("\nClosing OTEL connection and flushing traces...")
		if err := otelOutput.Close(); err != nil {
			log.Printf("Error closing OTEL output: %v", err)
		} else {
			fmt.Println("✓ OTEL connection closed successfully")
		}
	}()

	// Create a test health analysis with rich data
	analysis := createTestAnalysis()

	// Send the analysis as OTEL traces
	ctx := context.Background()
	fmt.Println("Sending test traces to OTEL collector...")

	if err := otelOutput.OutputHealthCheck(ctx, analysis); err != nil {
		log.Fatalf("Failed to output health check: %v", err)
	}

	fmt.Println("✓ Traces sent successfully!")
	fmt.Println()

	// Give time for traces to be processed
	fmt.Println("Waiting 2 seconds for traces to be processed...")
	time.Sleep(2 * time.Second)

	// Print instructions for viewing traces
	fmt.Println("\n=== How to Verify Traces Were Received ===")
	fmt.Println()
	fmt.Println("1. Jaeger UI:")
	fmt.Printf("   Open http://localhost:16686 and search for service: tapio-otel-test\n")
	fmt.Println()
	fmt.Println("2. Grafana Tempo:")
	fmt.Printf("   Query for service.name=\"tapio-otel-test\" in Explore view\n")
	fmt.Println()
	fmt.Println("3. OTEL Collector logs:")
	fmt.Println("   Check collector logs for received spans")
	fmt.Println()
	fmt.Println("You should see:")
	fmt.Println("- Root span: tapio.check")
	fmt.Println("- Child spans for each issue")
	fmt.Println("- Prediction spans with future failure scenarios")
	fmt.Println("- Rich attributes including human explanations")
}

func createTestAnalysis() *health.Analysis {
	return &health.Analysis{
		Target:      "test-deployment",
		Namespace:   "default",
		Status:      health.StatusDegraded,
		HealthScore: 0.65,
		Issues: []health.Issue{
			{
				ID:         "issue-001",
				Type:       "OOMKiller",
				Severity:   health.SeverityHigh,
				Confidence: 0.95,
				Pattern:    "Repeated OOM kills every 5 minutes",
				RiskScore:  0.85,
				Entity: health.Entity{
					Type:      "Pod",
					Name:      "api-service-abc123",
					Namespace: "default",
				},
				HumanExplanation: &health.HumanExplanation{
					WhatHappened:  "Your API service is running out of memory and being killed",
					WhyItHappened: "Memory limit (256Mi) is too low for current traffic load",
					WhatToDo:      "Increase memory limit to at least 512Mi",
					HowToPrevent:  "Set up memory alerts at 80% usage",
					IsUrgent:      true,
				},
				BusinessImpact: &health.BusinessImpact{
					Score:            0.9,
					AffectedServices: []string{"API Gateway", "User Service"},
					AffectedUsers:    1500,
					RevenueRisk:      25000.0,
				},
				CorrelationGroup: &health.CorrelationGroup{
					ID:            "corr-memory-pressure",
					RootCause:     "Traffic spike causing memory pressure",
					RelatedEvents: 47,
					Confidence:    0.88,
				},
				Evidence: []health.Evidence{
					{
						Type:        "metric",
						Description: "Memory usage spiked to 256Mi before OOM",
						Confidence:  0.99,
						Timestamp:   time.Now().Add(-5 * time.Minute),
					},
					{
						Type:        "log",
						Description: "Kernel: Out of memory: Kill process 1234",
						Confidence:  1.0,
						Timestamp:   time.Now().Add(-3 * time.Minute),
					},
				},
			},
			{
				ID:         "issue-002",
				Type:       "HighCPU",
				Severity:   health.SeverityMedium,
				Confidence: 0.82,
				Pattern:    "CPU throttling detected",
				RiskScore:  0.65,
				Entity: health.Entity{
					Type:      "Pod",
					Name:      "worker-service-xyz789",
					Namespace: "default",
				},
				HumanExplanation: &health.HumanExplanation{
					WhatHappened:  "Worker service is being CPU throttled",
					WhyItHappened: "CPU limit is causing performance degradation",
					WhatToDo:      "Consider removing CPU limits or increasing to 2 cores",
					HowToPrevent:  "Monitor CPU throttling metrics",
					IsUrgent:      false,
				},
			},
		},
		Predictions: []health.Prediction{
			{
				ID:          "pred-001",
				Type:        "ServiceOutage",
				Scenario:    "API service will fail completely if traffic increases 20%",
				Probability: 0.78,
				Confidence:  0.85,
				TimeToEvent: 45 * time.Minute,
				Severity:    "critical",
				PreventionActions: []string{
					"kubectl patch deployment api-service -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'",
					"kubectl scale deployment api-service --replicas=3",
				},
			},
		},
		Recommendations: []health.Recommendation{
			{
				Type:                "ResourceAdjustment",
				Action:              "Increase memory limit for api-service",
				Command:             "kubectl set resources deployment api-service -c=api --limits=memory=512Mi",
				Priority:            0.95,
				ExpectedImprovement: 0.85,
			},
			{
				Type:                "Scaling",
				Action:              "Add horizontal pod autoscaler",
				Command:             "kubectl autoscale deployment api-service --min=2 --max=5 --cpu-percent=70",
				Priority:            0.80,
				ExpectedImprovement: 0.70,
			},
		},
	}
}
