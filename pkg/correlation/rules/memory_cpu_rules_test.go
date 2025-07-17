package rules

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// TestMemoryPressureCascadeRule tests the memory pressure cascade detection rule
func TestMemoryPressureCascadeRule(t *testing.T) {
	tests := []struct {
		name           string
		events         []correlation.Event
		expectedResult bool
		expectedConf   float64
		expectedSev    correlation.Severity
	}{
		{
			name: "no OOM events",
			events: []correlation.Event{
				{
					ID:        "restart1",
					Timestamp: time.Now(),
					Source:    correlation.SourceKubernetes,
					Type:      "pod.restart",
					Entity: correlation.Entity{
						Node: "node1",
						Pod:  "pod1",
					},
				},
			},
			expectedResult: false,
		},
		{
			name: "OOM with insufficient restarts",
			events: []correlation.Event{
				{
					ID:        "oom1",
					Timestamp: time.Now(),
					Source:    correlation.SourceEBPF,
					Type:      "oom_kill",
					Entity: correlation.Entity{
						Node: "node1",
						Pod:  "pod1",
					},
				},
				{
					ID:        "restart1",
					Timestamp: time.Now().Add(30 * time.Second),
					Source:    correlation.SourceKubernetes,
					Type:      "pod.restart",
					Entity: correlation.Entity{
						Node: "node1",
						Pod:  "pod1",
					},
				},
			},
			expectedResult: false,
		},
		{
			name: "valid memory pressure cascade",
			events: func() []correlation.Event {
				baseTime := time.Now()
				return []correlation.Event{
					{
						ID:        "oom1",
						Timestamp: baseTime,
						Source:    correlation.SourceEBPF,
						Type:      "oom_kill",
						Entity: correlation.Entity{
							Node: "node1",
							Pod:  "pod1",
						},
					},
					{
						ID:        "restart1",
						Timestamp: baseTime.Add(30 * time.Second),
						Source:    correlation.SourceKubernetes,
						Type:      "pod.restart",
						Entity: correlation.Entity{
							Node: "node1",
							Pod:  "pod1",
						},
					},
					{
						ID:        "restart2",
						Timestamp: baseTime.Add(45 * time.Second),
						Source:    correlation.SourceKubernetes,
						Type:      "pod.restart",
						Entity: correlation.Entity{
							Node: "node1",
							Pod:  "pod2",
						},
					},
					{
						ID:        "restart3",
						Timestamp: baseTime.Add(60 * time.Second),
						Source:    correlation.SourceKubernetes,
						Type:      "pod.restart",
						Entity: correlation.Entity{
							Node: "node1",
							Pod:  "pod3",
						},
					},
				}
			}(),
			expectedResult: true,
			expectedConf:   0.65, // Base 0.5 + restarts 0.15
			expectedSev:    correlation.SeverityLevelWarning,
		},
		{
			name: "high severity cascade with many affected pods",
			events: func() []correlation.Event {
				baseTime := time.Now()
				events := []correlation.Event{
					{
						ID:        "oom1",
						Timestamp: baseTime,
						Source:    correlation.SourceEBPF,
						Type:      "oom_kill",
						Entity: correlation.Entity{
							Node: "node1",
							Pod:  "pod1",
						},
					},
				}
				// Add many pod restarts
				for i := 1; i <= 6; i++ {
					events = append(events, correlation.Event{
						ID:        "restart" + string(rune('0'+i)),
						Timestamp: baseTime.Add(time.Duration(i*15) * time.Second),
						Source:    correlation.SourceKubernetes,
						Type:      "pod.restart",
						Entity: correlation.Entity{
							Node:      "node1",
							Pod:       "pod" + string(rune('0'+i)),
							Namespace: "default",
						},
					})
				}
				return events
			}(),
			expectedResult: true,
			expectedSev:    correlation.SeverityLevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := MemoryPressureCascade()

			// Create test context
			ctx := createTestContext(tt.events)

			result := rule.Evaluate(ctx)

			if tt.expectedResult {
				if result == nil {
					t.Errorf("expected result but got nil")
					return
				}

				if tt.expectedConf > 0 {
					if result.Confidence < tt.expectedConf-0.1 || result.Confidence > tt.expectedConf+0.1 {
						t.Errorf("expected confidence ~%.2f, got %.2f", tt.expectedConf, result.Confidence)
					}
				}

				if tt.expectedSev != "" {
					if result.Severity != tt.expectedSev {
						t.Errorf("expected severity %v, got %v", tt.expectedSev, result.Severity)
					}
				}

				if result.Title == "" {
					t.Error("expected non-empty title")
				}

				if len(result.Evidence.Events) == 0 {
					t.Error("expected evidence events")
				}
			} else {
				if result != nil {
					t.Errorf("expected no result but got %+v", result)
				}
			}
		})
	}
}

// TestCPUThrottleDetectionRule tests the CPU throttling detection rule
func TestCPUThrottleDetectionRule(t *testing.T) {
	tests := []struct {
		name           string
		events         []correlation.Event
		metrics        map[string]float64
		expectedResult bool
		expectedConf   float64
	}{
		{
			name:           "no throttle events",
			events:         []correlation.Event{},
			expectedResult: false,
		},
		{
			name: "low throttling ratio",
			events: []correlation.Event{
				{
					ID:        "throttle1",
					Timestamp: time.Now(),
					Source:    correlation.SourceEBPF,
					Type:      "cpu_throttle",
					Attributes: map[string]interface{}{
						"throttle_ratio": 0.05, // 5% throttling
					},
				},
			},
			expectedResult: false,
		},
		{
			name: "significant throttling",
			events: []correlation.Event{
				{
					ID:        "throttle1",
					Timestamp: time.Now(),
					Source:    correlation.SourceEBPF,
					Type:      "cpu_throttle",
					Attributes: map[string]interface{}{
						"throttle_ratio": 0.3, // 30% throttling
					},
					AttributeHas: "throttle_ratio",
				},
				{
					ID:        "throttle2",
					Timestamp: time.Now().Add(10 * time.Second),
					Source:    correlation.SourceEBPF,
					Type:      "cpu_throttle",
					Attributes: map[string]interface{}{
						"throttle_ratio": 0.4, // 40% throttling
					},
					AttributeHas: "throttle_ratio",
				},
			},
			metrics: map[string]float64{
				"container.cpu.usage_percent": 0.95, // 95% CPU usage
			},
			expectedResult: true,
			expectedConf:   0.8, // Base 0.4 + throttle ratio 0.2 + event count 0.1 + high CPU 0.1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := CPUThrottleDetection()

			// Create test context with metrics
			ctx := createTestContextWithMetrics(tt.events, tt.metrics)

			result := rule.Evaluate(ctx)

			if tt.expectedResult {
				if result == nil {
					t.Errorf("expected result but got nil")
					return
				}

				if tt.expectedConf > 0 {
					if result.Confidence < tt.expectedConf-0.2 || result.Confidence > tt.expectedConf+0.2 {
						t.Errorf("expected confidence ~%.2f, got %.2f", tt.expectedConf, result.Confidence)
					}
				}

				if result.Title == "" {
					t.Error("expected non-empty title")
				}
			} else {
				if result != nil {
					t.Errorf("expected no result but got %+v", result)
				}
			}
		})
	}
}

// TestMemoryLeakDetectionRule tests the memory leak detection rule
func TestMemoryLeakDetectionRule(t *testing.T) {
	tests := []struct {
		name           string
		metricSeries   correlation.MetricSeries
		metricValues   map[string]float64
		expectedResult bool
		expectedConf   float64
	}{
		{
			name: "insufficient data points",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: []correlation.MetricPoint{
					{Timestamp: time.Now(), Value: 100.0},
					{Timestamp: time.Now().Add(time.Minute), Value: 105.0},
				},
			},
			expectedResult: false,
		},
		{
			name: "stable memory usage",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: func() []correlation.MetricPoint {
					points := make([]correlation.MetricPoint, 15)
					baseTime := time.Now()
					for i := range points {
						points[i] = correlation.MetricPoint{
							Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
							Value:     100.0 + float64(i%3), // Stable with minor fluctuation
						}
					}
					return points
				}(),
			},
			expectedResult: false,
		},
		{
			name: "strong increasing trend (memory leak)",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: func() []correlation.MetricPoint {
					points := make([]correlation.MetricPoint, 20)
					baseTime := time.Now()
					for i := range points {
						points[i] = correlation.MetricPoint{
							Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
							Value:     100.0 + float64(i)*10.0, // Strong increase
						}
					}
					return points
				}(),
			},
			metricValues: map[string]float64{
				"container.memory.limit": 500.0,
				"container.memory.usage": 290.0, // 58% usage
			},
			expectedResult: true,
			expectedConf:   0.6, // Base 0.4 + strong trend 0.1 + data points 0.1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := MemoryLeakDetection()

			// Create test context with metric series
			ctx := createTestContextWithMetricSeries([]correlation.Event{}, map[string]correlation.MetricSeries{
				"container.memory.usage": tt.metricSeries,
			}, tt.metricValues)

			result := rule.Evaluate(ctx)

			if tt.expectedResult {
				if result == nil {
					t.Errorf("expected result but got nil")
					return
				}

				if tt.expectedConf > 0 {
					if result.Confidence < tt.expectedConf-0.2 || result.Confidence > tt.expectedConf+0.2 {
						t.Errorf("expected confidence ~%.2f, got %.2f", tt.expectedConf, result.Confidence)
					}
				}

				if result.Title != "Potential memory leak detected" {
					t.Errorf("expected title 'Potential memory leak detected', got '%s'", result.Title)
				}
			} else {
				if result != nil {
					t.Errorf("expected no result but got %+v", result)
				}
			}
		})
	}
}

// TestContainerOOMPredictionRule tests the OOM prediction rule
func TestContainerOOMPredictionRule(t *testing.T) {
	tests := []struct {
		name           string
		metricSeries   correlation.MetricSeries
		metricValues   map[string]float64
		expectedResult bool
		expectCritical bool
	}{
		{
			name: "low memory usage",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: []correlation.MetricPoint{
					{Timestamp: time.Now().Add(-5 * time.Minute), Value: 100.0},
					{Timestamp: time.Now().Add(-4 * time.Minute), Value: 105.0},
					{Timestamp: time.Now().Add(-3 * time.Minute), Value: 110.0},
					{Timestamp: time.Now().Add(-2 * time.Minute), Value: 115.0},
					{Timestamp: time.Now().Add(-1 * time.Minute), Value: 120.0},
				},
			},
			metricValues: map[string]float64{
				"container.memory.limit": 1000.0,
			},
			expectedResult: false, // Only 12% usage
		},
		{
			name: "high usage but stable",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: []correlation.MetricPoint{
					{Timestamp: time.Now().Add(-5 * time.Minute), Value: 800.0},
					{Timestamp: time.Now().Add(-4 * time.Minute), Value: 802.0},
					{Timestamp: time.Now().Add(-3 * time.Minute), Value: 798.0},
					{Timestamp: time.Now().Add(-2 * time.Minute), Value: 801.0},
					{Timestamp: time.Now().Add(-1 * time.Minute), Value: 799.0},
				},
			},
			metricValues: map[string]float64{
				"container.memory.limit": 1000.0,
			},
			expectedResult: false, // High usage but not increasing
		},
		{
			name: "rapid memory increase - OOM imminent",
			metricSeries: correlation.MetricSeries{
				Name: "container.memory.usage",
				Points: []correlation.MetricPoint{
					{Timestamp: time.Now().Add(-5 * time.Minute), Value: 800.0},
					{Timestamp: time.Now().Add(-4 * time.Minute), Value: 820.0},
					{Timestamp: time.Now().Add(-3 * time.Minute), Value: 840.0},
					{Timestamp: time.Now().Add(-2 * time.Minute), Value: 860.0},
					{Timestamp: time.Now().Add(-1 * time.Minute), Value: 880.0},
				},
			},
			metricValues: map[string]float64{
				"container.memory.limit": 1000.0,
			},
			expectedResult: true,
			expectCritical: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := ContainerOOMPrediction()

			// Create test context
			ctx := createTestContextWithMetricSeries([]correlation.Event{}, map[string]correlation.MetricSeries{
				"container.memory.usage": tt.metricSeries,
			}, tt.metricValues)

			result := rule.Evaluate(ctx)

			if tt.expectedResult {
				if result == nil {
					t.Errorf("expected result but got nil")
					return
				}

				if tt.expectCritical && result.Severity != correlation.SeverityLevelCritical {
					t.Errorf("expected critical severity, got %v", result.Severity)
				}

				if result.Title == "" {
					t.Error("expected non-empty title")
				}

				if len(result.Actions) == 0 {
					t.Error("expected actions for OOM prediction")
				}
			} else {
				if result != nil {
					t.Errorf("expected no result but got %+v", result)
				}
			}
		})
	}
}

// TestRuleRegistryFunctions tests the rule registry functions
func TestRuleRegistryFunctions(t *testing.T) {
	t.Run("GetRuleByID", func(t *testing.T) {
		rule := GetRuleByID("memory-pressure-cascade")
		if rule == nil {
			t.Error("expected to find memory-pressure-cascade rule")
		}

		rule = GetRuleByID("nonexistent-rule")
		if rule != nil {
			t.Error("expected nil for nonexistent rule")
		}
	})

	t.Run("GetRulesByCategory", func(t *testing.T) {
		resourceRules := GetRulesByCategory(correlation.CategoryResource)
		if len(resourceRules) == 0 {
			t.Error("expected to find resource category rules")
		}

		performanceRules := GetRulesByCategory(correlation.CategoryPerformance)
		if len(performanceRules) == 0 {
			t.Error("expected to find performance category rules")
		}
	})

	t.Run("GetRulesByTag", func(t *testing.T) {
		memoryRules := GetRulesByTag("memory")
		if len(memoryRules) == 0 {
			t.Error("expected to find memory tagged rules")
		}

		cpuRules := GetRulesByTag("cpu")
		if len(cpuRules) == 0 {
			t.Error("expected to find cpu tagged rules")
		}
	})

	t.Run("ListAllRules", func(t *testing.T) {
		allRules := ListAllRules()
		if len(allRules) != 6 {
			t.Errorf("expected 6 rules, got %d", len(allRules))
		}
	})

	t.Run("GetRuleSummaries", func(t *testing.T) {
		summaries := GetRuleSummaries()
		if len(summaries) != 6 {
			t.Errorf("expected 6 rule summaries, got %d", len(summaries))
		}

		for _, summary := range summaries {
			if summary.ID == "" {
				t.Error("expected non-empty rule ID in summary")
			}
			if summary.Name == "" {
				t.Error("expected non-empty rule name in summary")
			}
		}
	})
}

// TestValidateRuleFunction tests rule validation
func TestValidateRuleFunction(t *testing.T) {
	tests := []struct {
		name        string
		rule        *correlation.Rule
		expectError bool
	}{
		{
			name:        "nil rule",
			rule:        nil,
			expectError: true,
		},
		{
			name: "missing ID",
			rule: &correlation.Rule{
				Name: "Test Rule",
				Evaluate: func(ctx *correlation.Context) *correlation.Result {
					return nil
				},
			},
			expectError: true,
		},
		{
			name: "missing name",
			rule: &correlation.Rule{
				ID: "test-rule",
				Evaluate: func(ctx *correlation.Context) *correlation.Result {
					return nil
				},
			},
			expectError: true,
		},
		{
			name: "missing evaluate function",
			rule: &correlation.Rule{
				ID:   "test-rule",
				Name: "Test Rule",
			},
			expectError: true,
		},
		{
			name: "invalid confidence",
			rule: &correlation.Rule{
				ID:            "test-rule",
				Name:          "Test Rule",
				MinConfidence: 1.5, // Invalid
				Evaluate: func(ctx *correlation.Context) *correlation.Result {
					return nil
				},
			},
			expectError: true,
		},
		{
			name: "no sources specified",
			rule: &correlation.Rule{
				ID:   "test-rule",
				Name: "Test Rule",
				Evaluate: func(ctx *correlation.Context) *correlation.Result {
					return nil
				},
			},
			expectError: true,
		},
		{
			name: "valid rule",
			rule: &correlation.Rule{
				ID:            "test-rule",
				Name:          "Test Rule",
				MinConfidence: 0.7,
				RequiredSources: []correlation.EventSource{
					correlation.SourceEBPF,
				},
				Evaluate: func(ctx *correlation.Context) *correlation.Result {
					return nil
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRule(tt.rule)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}

// Helper functions for creating test contexts

func createTestContext(events []correlation.Event) *correlation.Context {
	return createTestContextWithMetrics(events, nil)
}

func createTestContextWithMetrics(events []correlation.Event, metrics map[string]float64) *correlation.Context {
	// Use the existing NewTestContext function
	ctx := correlation.NewTestContext(events)

	// Add metric values if provided
	if metrics != nil {
		for name, value := range metrics {
			// Create a simple metric series with one point
			series := correlation.MetricSeries{
				Name: name,
				Points: []correlation.MetricPoint{
					{
						Timestamp: time.Now(),
						Value:     value,
					},
				},
			}
			ctx.SetMetric(name, series)
		}
	}

	// Set test metadata
	ctx.CorrelationID = "test-correlation"
	ctx.RuleID = "test-rule"

	return ctx
}

func createTestContextWithMetricSeries(events []correlation.Event, series map[string]correlation.MetricSeries, values map[string]float64) *correlation.Context {
	ctx := createTestContextWithMetrics(events, values)

	// Add metric series data
	if series != nil {
		for name, metricSeries := range series {
			ctx.SetMetric(name, metricSeries)
		}
	}

	return ctx
}
