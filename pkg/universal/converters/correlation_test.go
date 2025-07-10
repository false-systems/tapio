package converters

import (
	"testing"
	"time"

	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/universal"
)

func TestCorrelationConverter_ConvertFinding(t *testing.T) {
	converter := NewCorrelationConverter("test-correlation", "1.0")

	tests := []struct {
		name    string
		finding *correlation.Finding
		check   func(t *testing.T, pred *universal.UniversalPrediction, err error)
	}{
		{
			name: "OOM Finding with prediction",
			finding: &correlation.Finding{
				RuleID:      "oom-001",
				Title:       "Imminent OOM Risk",
				Description: "Process likely to run out of memory",
				Severity:    correlation.SeverityCritical,
				Confidence:  0.95,
				CreatedAt:   time.Now(),
				Resource: &correlation.ResourceReference{
					Kind:      "Pod",
					Name:      "webapp-xyz",
					Namespace: "production",
				},
				Prediction: &correlation.Prediction{
					Event:       "Out of Memory Kill",
					TimeToEvent: 10 * time.Minute,
					Confidence:  0.9,
					Factors: []string{
						"High memory growth rate",
						"Limited container memory",
					},
					Mitigation: []string{
						"Increase memory limit",
						"Scale horizontally",
					},
				},
				Evidence: []correlation.Evidence{
					{
						Type:        "metric",
						Description: "Memory usage trending up",
						Confidence:  0.95,
						Source:      correlation.SourceMetrics,
						Data: map[string]interface{}{
							"current_usage": 450 * 1024 * 1024,
							"limit":         512 * 1024 * 1024,
						},
					},
				},
			},
			check: func(t *testing.T, pred *universal.UniversalPrediction, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if pred.Type != universal.PredictionTypeOOM {
					t.Errorf("Expected prediction type %s, got %s", universal.PredictionTypeOOM, pred.Type)
				}

				if pred.TimeToEvent != 10*time.Minute {
					t.Errorf("Expected time to event 10m, got %v", pred.TimeToEvent)
				}

				if pred.Probability != 0.9 {
					t.Errorf("Expected probability 0.9, got %f", pred.Probability)
				}

				if pred.Impact != universal.ImpactLevelCritical {
					t.Errorf("Expected impact level critical, got %s", pred.Impact)
				}

				if len(pred.Factors) != 2 {
					t.Errorf("Expected 2 factors, got %d", len(pred.Factors))
				}

				if len(pred.Mitigations) != 2 {
					t.Errorf("Expected 2 mitigations, got %d", len(pred.Mitigations))
				}

				if len(pred.Evidence) != 1 {
					t.Errorf("Expected 1 evidence, got %d", len(pred.Evidence))
				}

				if pred.Target.Type != universal.TargetTypePod {
					t.Errorf("Expected target type pod, got %s", pred.Target.Type)
				}
			},
		},
		{
			name: "Immediate issue without prediction",
			finding: &correlation.Finding{
				RuleID:      "crash-001",
				Title:       "Application Crash Pattern",
				Description: "Multiple crashes detected",
				Severity:    correlation.SeverityError,
				Confidence:  0.85,
				CreatedAt:   time.Now(),
				Resource: &correlation.ResourceReference{
					Kind: "Deployment",
					Name: "api-server",
				},
			},
			check: func(t *testing.T, pred *universal.UniversalPrediction, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if pred.Type != universal.PredictionTypeCrash {
					t.Errorf("Expected prediction type %s, got %s", universal.PredictionTypeCrash, pred.Type)
				}

				if pred.TimeToEvent != 0 {
					t.Errorf("Expected immediate issue (0 time to event), got %v", pred.TimeToEvent)
				}

				if pred.Probability != 0.85 {
					t.Errorf("Expected probability 0.85, got %f", pred.Probability)
				}

				if pred.Impact != universal.ImpactLevelHigh {
					t.Errorf("Expected impact level high, got %s", pred.Impact)
				}
			},
		},
		{
			name:    "Nil finding",
			finding: nil,
			check: func(t *testing.T, pred *universal.UniversalPrediction, err error) {
				if err == nil {
					t.Error("Expected error for nil finding")
				}
				if pred != nil {
					t.Error("Expected nil prediction for nil finding")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pred, err := converter.ConvertFinding(tt.finding)
			tt.check(t, pred, err)

			// Clean up
			if pred != nil {
				universal.PutPrediction(pred)
			}
		})
	}
}

func TestCorrelationConverter_ConvertOOMPrediction(t *testing.T) {
	converter := NewCorrelationConverter("test-correlation", "1.0")

	target := &universal.Target{
		Type:      universal.TargetTypePod,
		Name:      "webapp-123",
		Pod:       "webapp-123",
		Namespace: "default",
	}

	pred, err := converter.ConvertOOMPrediction(
		target,
		15*time.Minute,
		0.92,
		400*1024*1024, // 400MB usage
		512*1024*1024, // 512MB limit
		2*1024*1024,   // 2MB/min growth
	)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify prediction
	if pred.Type != universal.PredictionTypeOOM {
		t.Errorf("Expected prediction type %s, got %s", universal.PredictionTypeOOM, pred.Type)
	}

	if pred.TimeToEvent != 15*time.Minute {
		t.Errorf("Expected time to event 15m, got %v", pred.TimeToEvent)
	}

	if pred.Impact != universal.ImpactLevelMedium {
		t.Errorf("Expected impact level medium for 15m prediction, got %s", pred.Impact)
	}

	// Verify evidence
	if len(pred.Evidence) != 2 {
		t.Errorf("Expected 2 evidence items, got %d", len(pred.Evidence))
	}

	// Verify factors
	if len(pred.Factors) != 3 {
		t.Errorf("Expected 3 factors, got %d", len(pred.Factors))
	}

	// Verify mitigations
	if len(pred.Mitigations) == 0 {
		t.Error("Expected at least one mitigation")
	}

	// Clean up
	universal.PutPrediction(pred)
}

func TestCorrelationConverter_mapFindingToPredictionType(t *testing.T) {
	converter := NewCorrelationConverter("test", "1.0")

	tests := []struct {
		title    string
		expected universal.PredictionType
	}{
		{"OOM Risk Detected", universal.PredictionTypeOOM},
		{"Memory Exhaustion Warning", universal.PredictionTypeOOM},
		{"Application Crash Pattern", universal.PredictionTypeCrash},
		{"Frequent Restarts Detected", universal.PredictionTypeCrash},
		{"Disk Space Running Low", universal.PredictionTypeDiskFull},
		{"Storage Capacity Warning", universal.PredictionTypeDiskFull},
		{"CPU Performance Degradation", universal.PredictionTypePerformance},
		{"High CPU Usage", universal.PredictionTypePerformance},
		{"Unknown Issue", universal.PredictionTypeCustom},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			result := converter.mapFindingToPredictionType(tt.title)
			if result != tt.expected {
				t.Errorf("Expected %s for title '%s', got %s", tt.expected, tt.title, result)
			}
		})
	}
}

func TestCorrelationConverter_mapSeverityToImpact(t *testing.T) {
	converter := NewCorrelationConverter("test", "1.0")

	tests := []struct {
		severity correlation.Severity
		expected universal.ImpactLevel
	}{
		{correlation.SeverityCritical, universal.ImpactLevelCritical},
		{correlation.SeverityError, universal.ImpactLevelHigh},
		{correlation.SeverityWarning, universal.ImpactLevelMedium},
		{correlation.SeverityInfo, universal.ImpactLevelLow},
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			result := converter.mapSeverityToImpact(tt.severity)
			if result != tt.expected {
				t.Errorf("Expected %s for severity %s, got %s", tt.expected, tt.severity, result)
			}
		})
	}
}

func TestCorrelationConverter_calculateOOMImpact(t *testing.T) {
	converter := NewCorrelationConverter("test", "1.0")

	tests := []struct {
		timeToOOM time.Duration
		expected  universal.ImpactLevel
	}{
		{3 * time.Minute, universal.ImpactLevelCritical},
		{10 * time.Minute, universal.ImpactLevelHigh},
		{25 * time.Minute, universal.ImpactLevelMedium},
		{45 * time.Minute, universal.ImpactLevelLow},
	}

	for _, tt := range tests {
		t.Run(tt.timeToOOM.String(), func(t *testing.T) {
			result := converter.calculateOOMImpact(tt.timeToOOM)
			if result != tt.expected {
				t.Errorf("Expected impact %s for time %v, got %s", tt.expected, tt.timeToOOM, result)
			}
		})
	}
}

func TestCorrelationConverter_generateOOMMitigations(t *testing.T) {
	converter := NewCorrelationConverter("test", "1.0")

	target := &universal.Target{
		Type:      universal.TargetTypePod,
		Name:      "webapp",
		Pod:       "webapp-123",
		Namespace: "default",
	}

	// Test critical situation (< 10 minutes)
	mitigations := converter.generateOOMMitigations(target, 5*time.Minute)

	// Should have immediate actions
	if len(mitigations) < 4 {
		t.Errorf("Expected at least 4 mitigations for critical situation, got %d", len(mitigations))
	}

	// Check for critical urgency mitigation
	hasCritical := false
	for _, m := range mitigations {
		if m.Urgency == "critical" {
			hasCritical = true
			break
		}
	}

	if !hasCritical {
		t.Error("Expected at least one critical urgency mitigation")
	}

	// Test non-critical situation
	mitigations = converter.generateOOMMitigations(target, 30*time.Minute)

	// Should have general mitigations
	if len(mitigations) < 2 {
		t.Errorf("Expected at least 2 mitigations, got %d", len(mitigations))
	}
}

func TestCorrelationConverter_ConvertCorrelationResult(t *testing.T) {
	converter := NewCorrelationConverter("test-correlation", "1.0")

	result := map[string]interface{}{
		"patterns": map[string]int{
			"memory_spike": 5,
			"cpu_throttle": 3,
		},
		"insights": []string{
			"Memory usage correlates with request volume",
			"CPU throttling occurs during peak hours",
		},
		"confidence": 0.85,
	}

	dataset, err := converter.ConvertCorrelationResult(result)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify dataset
	if dataset.Source != "test-correlation" {
		t.Errorf("Expected source 'test-correlation', got %s", dataset.Source)
	}

	if dataset.OverallQuality.Confidence != 0.85 {
		t.Errorf("Expected confidence 0.85, got %f", dataset.OverallQuality.Confidence)
	}

	// Check patterns in tags
	if dataset.Tags["pattern_memory_spike"] != "5" {
		t.Error("Expected pattern_memory_spike tag")
	}

	if dataset.Tags["pattern_cpu_throttle"] != "3" {
		t.Error("Expected pattern_cpu_throttle tag")
	}

	// Check insights in metadata
	insights, ok := dataset.Metadata["insights"].([]string)
	if !ok || len(insights) != 2 {
		t.Error("Expected 2 insights in metadata")
	}
}

func TestCorrelationConverter_ConvertFindings(t *testing.T) {
	converter := NewCorrelationConverter("test-correlation", "1.0")

	findings := []correlation.Finding{
		{
			RuleID:     "test-001",
			Title:      "Test Finding 1",
			Severity:   correlation.SeverityWarning,
			Confidence: 0.8,
			CreatedAt:  time.Now(),
		},
		{
			RuleID:     "test-002",
			Title:      "Test Finding 2",
			Severity:   correlation.SeverityError,
			Confidence: 0.9,
			CreatedAt:  time.Now(),
		},
	}

	predictions, err := converter.ConvertFindings(findings)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(predictions) != 2 {
		t.Errorf("Expected 2 predictions, got %d", len(predictions))
	}

	// Clean up
	for _, pred := range predictions {
		universal.PutPrediction(pred)
	}
}
