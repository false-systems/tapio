package formatters

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/falseyair/tapio/pkg/universal"
)

func TestPrometheusFormatter_FormatMetric(t *testing.T) {
	formatter := NewPrometheusFormatter("tapio", "test", nil)
	
	tests := []struct {
		name   string
		metric *universal.UniversalMetric
		check  func(t *testing.T, registry *prometheus.Registry)
	}{
		{
			name: "Gauge metric",
			metric: &universal.UniversalMetric{
				Name:      "memory_usage_bytes",
				Type:      universal.MetricTypeGauge,
				Value:     100.0,
				Unit:      "bytes",
				Timestamp: time.Now(),
				Target: universal.Target{
					Type:      universal.TargetTypePod,
					Name:      "test-pod",
					Namespace: "default",
				},
				Quality: universal.DataQuality{
					Level:      universal.QualityGood,
					Confidence: 1.0,
				},
			},
			check: func(t *testing.T, registry *prometheus.Registry) {
				metricFamilies, err := registry.Gather()
				if err != nil {
					t.Fatalf("Failed to gather metrics: %v", err)
				}
				
				found := false
				for _, mf := range metricFamilies {
					if *mf.Name == "tapio_test_memory_usage_bytes" {
						found = true
						if *mf.Type != dto.MetricType_GAUGE {
							t.Errorf("Expected gauge type, got %v", *mf.Type)
						}
						if len(mf.Metric) != 1 {
							t.Errorf("Expected 1 metric, got %d", len(mf.Metric))
						}
						if *mf.Metric[0].Gauge.Value != 100.0 {
							t.Errorf("Expected value 100.0, got %v", *mf.Metric[0].Gauge.Value)
						}
					}
				}
				if !found {
					t.Error("Metric not found in registry")
				}
			},
		},
		{
			name: "Counter metric",
			metric: &universal.UniversalMetric{
				Name:  "requests_total",
				Type:  universal.MetricTypeCounter,
				Value: 42.0,
				Target: universal.Target{
					Type: universal.TargetTypeContainer,
					Name: "test-pod",
					Container: "app",
				},
			},
			check: func(t *testing.T, registry *prometheus.Registry) {
				metricFamilies, err := registry.Gather()
				if err != nil {
					t.Fatalf("Failed to gather metrics: %v", err)
				}
				
				found := false
				for _, mf := range metricFamilies {
					if *mf.Name == "tapio_test_requests_total" {
						found = true
						if *mf.Type != dto.MetricType_COUNTER {
							t.Errorf("Expected counter type, got %v", *mf.Type)
						}
					}
				}
				if !found {
					t.Error("Counter metric not found")
				}
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatter.FormatMetric(tt.metric)
			if err != nil {
				t.Fatalf("FormatMetric failed: %v", err)
			}
			
			tt.check(t, formatter.GetRegistry())
		})
	}
}

func TestPrometheusFormatter_FormatEvent(t *testing.T) {
	formatter := NewPrometheusFormatter("tapio", "test", nil)
	
	event := &universal.UniversalEvent{
		ID:        "test-event",
		Type:      universal.EventTypeOOMKill,
		Level:     universal.EventLevelCritical,
		Category:  "memory",
		Timestamp: time.Now(),
		Target: universal.Target{
			Type: universal.TargetTypePod,
			Name: "test-pod",
		},
	}
	
	err := formatter.FormatEvent(event)
	if err != nil {
		t.Fatalf("FormatEvent failed: %v", err)
	}
	
	// Check that event counter was created
	metricFamilies, err := formatter.GetRegistry().Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	
	found := false
	for _, mf := range metricFamilies {
		if strings.Contains(*mf.Name, "events_oomkill_total") {
			found = true
			if *mf.Type != dto.MetricType_COUNTER {
				t.Errorf("Expected counter type for event, got %v", *mf.Type)
			}
		}
	}
	
	if !found {
		t.Error("Event counter not found")
	}
}

func TestPrometheusFormatter_FormatPrediction(t *testing.T) {
	formatter := NewPrometheusFormatter("tapio", "", nil)
	
	prediction := &universal.UniversalPrediction{
		ID:          "test-pred",
		Type:        "oom",
		Severity:    universal.SeverityHigh,
		Confidence:  0.85,
		TimeToEvent: 5 * time.Minute,
		Target: universal.Target{
			Type: universal.TargetTypeProcess,
			Name: "test-process",
			PID:  1234,
		},
	}
	
	err := formatter.FormatPrediction(prediction)
	if err != nil {
		t.Fatalf("FormatPrediction failed: %v", err)
	}
	
	// Check that prediction metrics were created
	metricFamilies, err := formatter.GetRegistry().Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	
	foundConfidence := false
	foundTimeToEvent := false
	
	for _, mf := range metricFamilies {
		if strings.Contains(*mf.Name, "prediction_oom_confidence") {
			foundConfidence = true
			if *mf.Metric[0].Gauge.Value != 0.85 {
				t.Errorf("Expected confidence 0.85, got %v", *mf.Metric[0].Gauge.Value)
			}
		}
		if strings.Contains(*mf.Name, "prediction_oom_time_to_event_seconds") {
			foundTimeToEvent = true
			if *mf.Metric[0].Gauge.Value != 300.0 { // 5 minutes
				t.Errorf("Expected 300 seconds, got %v", *mf.Metric[0].Gauge.Value)
			}
		}
	}
	
	if !foundConfidence {
		t.Error("Prediction confidence metric not found")
	}
	if !foundTimeToEvent {
		t.Error("Prediction time_to_event metric not found")
	}
}

func TestPrometheusFormatter_Labels(t *testing.T) {
	formatter := NewPrometheusFormatter("", "", nil)
	
	metric := &universal.UniversalMetric{
		Name:  "test_metric",
		Type:  universal.MetricTypeGauge,
		Value: 100.0,
		Target: universal.Target{
			Type:      universal.TargetTypePod,
			Name:      "my-pod",
			Namespace: "production",
		},
		Labels: map[string]string{
			"custom_label": "custom_value",
			"environment":  "prod",
		},
		Quality: universal.DataQuality{
			Level: universal.QualityDegraded,
		},
	}
	
	err := formatter.FormatMetric(metric)
	if err != nil {
		t.Fatalf("FormatMetric failed: %v", err)
	}
	
	// Check labels
	metricFamilies, err := formatter.GetRegistry().Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	
	for _, mf := range metricFamilies {
		if *mf.Name == "test_metric" {
			metric := mf.Metric[0]
			labelMap := make(map[string]string)
			for _, label := range metric.Label {
				labelMap[*label.Name] = *label.Value
			}
			
			// Check expected labels
			expectedLabels := map[string]string{
				"pod":          "my-pod",
				"namespace":    "production",
				"custom_label": "custom_value",
				"environment":  "prod",
				"quality":      "degraded",
			}
			
			for k, v := range expectedLabels {
				if labelMap[k] != v {
					t.Errorf("Expected label %s=%s, got %s", k, v, labelMap[k])
				}
			}
		}
	}
}

func TestPrometheusFormatter_BatchFormat(t *testing.T) {
	formatter := NewPrometheusFormatter("tapio", "batch", nil)
	
	items := []interface{}{
		&universal.UniversalMetric{
			Name:  "metric1",
			Type:  universal.MetricTypeGauge,
			Value: 10.0,
			Target: universal.Target{
				Type: universal.TargetTypeNode,
				Name: "node1",
			},
		},
		&universal.UniversalEvent{
			Type:  universal.EventTypeMemoryPressure,
			Level: universal.EventLevelWarning,
			Target: universal.Target{
				Type: universal.TargetTypeNode,
				Name: "node1",
			},
		},
		&universal.UniversalPrediction{
			Type:        "disk_full",
			Confidence:  0.75,
			TimeToEvent: 1 * time.Hour,
			Severity:    universal.SeverityMedium,
			Target: universal.Target{
				Type: universal.TargetTypeNode,
				Name: "node1",
			},
		},
	}
	
	err := formatter.BatchFormat(items)
	if err != nil {
		t.Fatalf("BatchFormat failed: %v", err)
	}
	
	// Check that all metrics were created
	metricFamilies, err := formatter.GetRegistry().Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	
	if len(metricFamilies) < 3 {
		t.Errorf("Expected at least 3 metric families, got %d", len(metricFamilies))
	}
}