// +build example

package collector

import (
    "context"
    "fmt"
    "log"
    "time"
)

// ExampleHumanOutput demonstrates the human-readable output functionality
func ExampleHumanOutput() {
    // Create semantic correlation engine
    engine := NewSemanticCorrelationEngine(100, 5*time.Second)
    
    // Start the engine
    ctx := context.Background()
    if err := engine.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Simulate different scenarios
    demonstrateMemoryLeakScenario(engine)
    demonstrateNetworkFailureScenario(engine)
    demonstrateComplexIncidentScenario(engine)
    
    // Stop the engine
    engine.Stop()
}

func demonstrateMemoryLeakScenario(engine *SemanticCorrelationEngine) {
    fmt.Println("\n=== MEMORY LEAK SCENARIO ===\n")
    
    // Create a memory leak insight
    insight := Insight{
        ID:          "mem-001",
        Type:        "pattern:memory_leak",
        Severity:    SeverityCritical,
        Title:       "Memory Leak Detected in Payment Service",
        Description: "Continuous memory growth detected over 2 hours",
        Timestamp:   time.Now(),
        Prediction: &Prediction{
            Type:        "oom_kill",
            Probability: 0.92,
            Confidence:  0.92,
            TimeToEvent: 45 * time.Minute,
        },
        Actions: []ActionableItem{
            {
                Title:       "Restart service immediately",
                Description: "Prevent OOM kill by restarting now",
                Commands:    []string{"kubectl rollout restart deployment/payment-service"},
                Risk:        "low",
            },
        },
    }
    
    // Show different explanation styles
    styles := []struct {
        style    ExplanationStyle
        audience Audience
        label    string
    }{
        {StyleSimple, AudienceDeveloper, "Simple Developer"},
        {StyleTechnical, AudienceDeveloper, "Technical Developer"},
        {StyleExecutive, AudienceBusiness, "Executive Business"},
    }
    
    for _, s := range styles {
        engine.SetHumanOutputStyle(s.style, s.audience)
        explanation := engine.GetHumanExplanation(insight)
        
        fmt.Printf("--- %s Explanation ---\n", s.label)
        fmt.Printf("What: %s\n", explanation.WhatHappened)
        fmt.Printf("Why: %s\n", explanation.WhyItHappened)
        fmt.Printf("Impact: %s\n", explanation.Impact)
        fmt.Printf("Action: %s\n", explanation.WhatToDo)
        fmt.Printf("Urgency: %s\n", explanation.Urgency)
        
        if len(explanation.Commands) > 0 {
            fmt.Printf("Commands:\n")
            for _, cmd := range explanation.Commands {
                fmt.Printf("  $ %s\n", cmd)
            }
        }
        fmt.Printf("Readability Score: %.2f\n\n", explanation.ReadableScore)
    }
}

func demonstrateNetworkFailureScenario(engine *SemanticCorrelationEngine) {
    fmt.Println("\n=== NETWORK FAILURE SCENARIO ===\n")
    
    // Set to technical style
    engine.SetHumanOutputStyle(StyleTechnical, AudienceDeveloper)
    
    insight := Insight{
        ID:          "net-001",
        Type:        "network_failure",
        Severity:    SeverityHigh,
        Title:       "Service Mesh Communication Failure",
        Description: "Multiple services unable to communicate",
        Timestamp:   time.Now(),
        Resources: []AffectedResource{
            {Type: "service", Name: "api-gateway"},
            {Type: "service", Name: "auth-service"},
            {Type: "service", Name: "user-service"},
        },
    }
    
    explanation := engine.GetHumanExplanation(insight)
    
    fmt.Println("Technical Explanation:")
    fmt.Printf("What: %s\n", explanation.WhatHappened)
    fmt.Printf("Why: %s\n", explanation.WhyItHappened)
    fmt.Printf("Impact: %s\n", explanation.Impact)
    fmt.Printf("Action: %s\n", explanation.WhatToDo)
    
    if len(explanation.Commands) > 0 {
        fmt.Printf("\nTroubleshooting Commands:\n")
        for _, cmd := range explanation.Commands {
            fmt.Printf("  $ %s\n", cmd)
        }
    }
}

func demonstrateComplexIncidentScenario(engine *SemanticCorrelationEngine) {
    fmt.Println("\n\n=== COMPLEX INCIDENT STORY ===\n")
    
    // Create a sequence of related insights
    baseTime := time.Now().Add(-30 * time.Minute)
    
    insights := []Insight{
        {
            ID:          "inc-001",
            Type:        "service_degradation",
            Severity:    SeverityMedium,
            Title:       "API Response Times Increasing",
            Description: "95th percentile latency increased from 200ms to 800ms",
            Timestamp:   baseTime,
        },
        {
            ID:          "inc-002",
            Type:        "memory_pressure",
            Severity:    SeverityHigh,
            Title:       "High Memory Usage in API Pods",
            Description: "Memory usage at 85% and climbing",
            Timestamp:   baseTime.Add(10 * time.Minute),
        },
        {
            ID:          "inc-003",
            Type:        "pattern:memory_leak",
            Severity:    SeverityCritical,
            Title:       "Memory Leak Confirmed",
            Description: "Pattern analysis confirms memory leak in API service",
            Timestamp:   baseTime.Add(20 * time.Minute),
            Prediction: &Prediction{
                Confidence:  0.88,
                TimeToEvent: 40 * time.Minute,
            },
        },
        {
            ID:          "inc-004",
            Type:        "pod_restart",
            Severity:    SeverityHigh,
            Title:       "API Pod Restarted Due to OOM",
            Description: "api-service-7d9f8b-x2k4p killed due to OOMKilled",
            Timestamp:   baseTime.Add(25 * time.Minute),
        },
    }
    
    // Generate incident story
    story := engine.GetInsightStory(insights)
    fmt.Println(story)
    
    // Also show the most critical insight with explanation
    fmt.Println("\n--- Critical Insight Details ---")
    engine.SetHumanOutputStyle(StyleSimple, AudienceDeveloper)
    criticalExplanation := engine.GetHumanExplanation(insights[2])
    
    fmt.Printf("What's happening: %s\n", criticalExplanation.WhatHappened)
    fmt.Printf("Root cause: %s\n", criticalExplanation.WhyItHappened)
    fmt.Printf("Business impact: %s\n", criticalExplanation.Impact)
    fmt.Printf("Immediate action: %s\n", criticalExplanation.WhatToDo)
    fmt.Printf("Confidence: %.0f%%\n", criticalExplanation.Confidence*100)
}

// This example can be run with: go run -tags=example human_output_example.go