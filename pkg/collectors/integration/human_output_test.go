package collector

import (
    "fmt"
    "testing"
    "time"
)

func TestHumanReadableFormatter_FormatInsight(t *testing.T) {
    tests := []struct {
        name     string
        style    ExplanationStyle
        audience Audience
        insight  Insight
        wantText string // Check if explanation contains this text
    }{
        {
            name:     "memory issue - simple style",
            style:    StyleSimple,
            audience: AudienceDeveloper,
            insight: Insight{
                Type:     "memory_pressure",
                Severity: SeverityHigh,
                Title:    "High Memory Usage Detected",
            },
            wantText: "running out of memory",
        },
        {
            name:     "memory issue - technical style",
            style:    StyleTechnical,
            audience: AudienceDeveloper,
            insight: Insight{
                Type:     "memory_pressure",
                Severity: SeverityHigh,
                Title:    "High Memory Usage Detected",
            },
            wantText: "Memory pressure detected",
        },
        {
            name:     "memory issue - executive style",
            style:    StyleExecutive,
            audience: AudienceBusiness,
            insight: Insight{
                Type:     "memory_pressure",
                Severity: SeverityHigh,
                Title:    "High Memory Usage Detected",
            },
            wantText: "Service reliability issue",
        },
        {
            name:     "memory leak pattern",
            style:    StyleTechnical,
            audience: AudienceDeveloper,
            insight: Insight{
                Type:     "pattern:memory_leak",
                Severity: SeverityCritical,
                Title:    "Memory Leak Detected",
                Prediction: &Prediction{
                    Confidence: 0.85,
                },
                Actions: []ActionableItem{
                    {
                        Title:    "Restart service",
                        Commands: []string{"kubectl rollout restart deployment/app"},
                    },
                },
            },
            wantText: "Memory leak detected (confidence: 85%)",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            formatter := NewHumanReadableFormatter(tt.style, tt.audience)
            explanation := formatter.FormatInsight(tt.insight)
            
            if explanation == nil {
                t.Fatal("Expected explanation, got nil")
            }
            
            // Check if the explanation contains expected text
            fullText := explanation.WhatHappened + explanation.WhyItHappened + 
                       explanation.Impact + explanation.WhatToDo
            
            if !contains(fullText, tt.wantText) {
                t.Errorf("Expected explanation to contain '%s', got:\n"+
                    "What: %s\nWhy: %s\nImpact: %s\nAction: %s",
                    tt.wantText,
                    explanation.WhatHappened,
                    explanation.WhyItHappened,
                    explanation.Impact,
                    explanation.WhatToDo)
            }
            
            // Verify urgency is set
            if explanation.Urgency == "" {
                t.Error("Expected urgency to be set")
            }
            
            // Verify readability score
            if explanation.ReadableScore <= 0 {
                t.Error("Expected positive readability score")
            }
        })
    }
}

func TestHumanReadableFormatter_FormatAsStory(t *testing.T) {
    formatter := NewHumanReadableFormatter(StyleSimple, AudienceDeveloper)
    
    // Create a sequence of insights
    baseTime := time.Now()
    insights := []Insight{
        {
            Title:       "Memory Usage Increasing",
            Description: "Service memory consumption growing",
            Severity:    SeverityMedium,
            Timestamp:   baseTime,
        },
        {
            Title:       "High Memory Alert",
            Description: "Memory usage exceeded 80%",
            Severity:    SeverityHigh,
            Timestamp:   baseTime.Add(5 * time.Minute),
        },
        {
            Title:       "Memory Leak Detected",
            Description: "Pattern analysis confirms memory leak",
            Severity:    SeverityCritical,
            Timestamp:   baseTime.Add(10 * time.Minute),
        },
    }
    
    story := formatter.FormatAsStory(insights)
    
    // Verify story contains key sections
    if !contains(story, "## Incident Story:") {
        t.Error("Expected story to have title")
    }
    
    if !contains(story, "### Timeline of Events") {
        t.Error("Expected story to have timeline")
    }
    
    if !contains(story, "### Impact") {
        t.Error("Expected story to have impact section")
    }
    
    if !contains(story, "### Recommended Actions") {
        t.Error("Expected story to have actions section")
    }
    
    // Verify it mentions escalation
    if !contains(story, "escalated") {
        t.Error("Expected story to mention escalation")
    }
}

func TestReadabilityScore(t *testing.T) {
    formatter := NewHumanReadableFormatter(StyleSimple, AudienceDeveloper)
    
    tests := []struct {
        name            string
        explanation     *HumanReadableExplanation
        minScore        float64
    }{
        {
            name: "simple sentences",
            explanation: &HumanReadableExplanation{
                WhatHappened:  "Service crashed.",
                WhyItHappened: "Out of memory.",
                WhatToDo:      "Restart it.",
            },
            minScore: 0.8, // Should be very readable
        },
        {
            name: "complex sentences",
            explanation: &HumanReadableExplanation{
                WhatHappened:  "The microservice architecture deployment experienced cascading failures due to improper circuit breaker configuration.",
                WhyItHappened: "The distributed system's resilience patterns were not correctly implemented, leading to downstream service degradation.",
                WhatToDo:      "Implement proper circuit breakers, configure retry policies, and establish service mesh observability.",
            },
            minScore: 0.4, // Should be less readable
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            score := formatter.calculateReadabilityScore(tt.explanation)
            if score < tt.minScore {
                t.Errorf("Expected readability score >= %f, got %f", tt.minScore, score)
            }
        })
    }
}

func ExampleHumanReadableFormatter() {
    // Create formatter for simple explanations
    formatter := NewHumanReadableFormatter(StyleSimple, AudienceDeveloper)
    
    // Format a memory leak insight
    insight := Insight{
        Type:     "pattern:memory_leak",
        Severity: SeverityCritical,
        Title:    "Memory Leak Detected in API Service",
        Prediction: &Prediction{
            Confidence: 0.92,
        },
    }
    
    explanation := formatter.FormatInsight(insight)
    
    fmt.Println("=== Simple Explanation ===")
    fmt.Printf("What: %s\n", explanation.WhatHappened)
    fmt.Printf("Why: %s\n", explanation.WhyItHappened)
    fmt.Printf("Impact: %s\n", explanation.Impact)
    fmt.Printf("Action: %s\n", explanation.WhatToDo)
    fmt.Printf("Urgency: %s\n", explanation.Urgency)
    
    // Now technical style
    techFormatter := NewHumanReadableFormatter(StyleTechnical, AudienceDeveloper)
    techExplanation := techFormatter.FormatInsight(insight)
    
    fmt.Println("\n=== Technical Explanation ===")
    fmt.Printf("What: %s\n", techExplanation.WhatHappened)
    fmt.Printf("Commands: %v\n", techExplanation.Commands)
}

// Helper function
func contains(s, substr string) bool {
    return len(substr) > 0 && len(s) >= len(substr) && 
           (s == substr || len(s) > len(substr) && 
            (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
             len(s) > len(substr) && findSubstring(s[1:len(s)-1], substr)))
}

func findSubstring(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}