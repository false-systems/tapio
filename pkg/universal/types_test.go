package universal

import (
	"fmt"
	"testing"
	"time"
)

func TestObjectPools(t *testing.T) {
	t.Run("MetricPool", func(t *testing.T) {
		// Get metric from pool
		m1 := GetMetric()
		if m1 == nil {
			t.Fatal("Expected non-nil metric from pool")
		}

		// Set some values
		m1.ID = "test-metric"
		m1.Name = "cpu_usage"
		m1.Value = 75.5
		m1.Labels["test"] = "value"

		// Return to pool
		PutMetric(m1)

		// Get another metric
		m2 := GetMetric()

		// Should be reset
		if m2.ID != "" || m2.Name != "" || m2.Value != 0 {
			t.Error("Metric not properly reset")
		}

		// Maps should be empty but not nil
		if m2.Labels == nil || len(m2.Labels) != 0 {
			t.Error("Labels map not properly reset")
		}
	})

	t.Run("EventPool", func(t *testing.T) {
		e1 := GetEvent()
		if e1 == nil {
			t.Fatal("Expected non-nil event from pool")
		}

		e1.ID = "test-event"
		e1.Type = EventTypeOOM
		e1.Details["key"] = "value"

		PutEvent(e1)

		e2 := GetEvent()
		if e2.ID != "" || e2.Type != "" {
			t.Error("Event not properly reset")
		}

		if e2.Details == nil || len(e2.Details) != 0 {
			t.Error("Details map not properly reset")
		}
	})

	t.Run("PredictionPool", func(t *testing.T) {
		p1 := GetPrediction()
		if p1 == nil {
			t.Fatal("Expected non-nil prediction from pool")
		}

		p1.ID = "test-prediction"
		p1.Evidence = append(p1.Evidence, Evidence{Type: "test"})
		p1.Factors = append(p1.Factors, "factor1")

		PutPrediction(p1)

		p2 := GetPrediction()
		if p2.ID != "" {
			t.Error("Prediction not properly reset")
		}

		if len(p2.Evidence) != 0 || len(p2.Factors) != 0 {
			t.Error("Slices not properly reset")
		}
	})
}

func TestMetricClone(t *testing.T) {
	original := GetMetric()
	original.ID = "original"
	original.Timestamp = time.Now()
	original.Name = "test_metric"
	original.Value = 42.0
	original.Labels["env"] = "test"
	original.Quality.Confidence = 0.95
	original.Quality.Tags["source"] = "test"

	clone := original.Clone()

	// Verify clone has same values
	if clone.ID != original.ID {
		t.Errorf("ID mismatch: got %s, want %s", clone.ID, original.ID)
	}

	if clone.Value != original.Value {
		t.Errorf("Value mismatch: got %f, want %f", clone.Value, original.Value)
	}

	if clone.Labels["env"] != "test" {
		t.Error("Labels not cloned properly")
	}

	if clone.Quality.Confidence != original.Quality.Confidence {
		t.Error("Quality not cloned properly")
	}

	// Verify deep copy - modify original
	original.Labels["env"] = "prod"
	original.Quality.Tags["source"] = "modified"

	if clone.Labels["env"] != "test" {
		t.Error("Clone was not deep copied - labels")
	}

	if clone.Quality.Tags["source"] != "test" {
		t.Error("Clone was not deep copied - quality tags")
	}

	// Clean up
	PutMetric(original)
	PutMetric(clone)
}

func TestTargetTypes(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		valid  bool
	}{
		{
			name: "Process target",
			target: Target{
				Type: TargetTypeProcess,
				Name: "nginx",
				PID:  1234,
			},
			valid: true,
		},
		{
			name: "Pod target",
			target: Target{
				Type:      TargetTypePod,
				Name:      "nginx-deployment-abc123",
				Namespace: "default",
				Pod:       "nginx-deployment-abc123",
			},
			valid: true,
		},
		{
			name: "Container target",
			target: Target{
				Type:      TargetTypeContainer,
				Name:      "nginx",
				Container: "nginx",
				Pod:       "nginx-deployment-abc123",
				Namespace: "default",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation
			if tt.target.Type == "" {
				t.Error("Target type should not be empty")
			}
			if tt.target.Name == "" {
				t.Error("Target name should not be empty")
			}
		})
	}
}

func TestDataQuality(t *testing.T) {
	dq := DataQuality{
		Confidence: 0.95,
		Source:     "ebpf",
		Version:    "1.0",
		Tags: map[string]string{
			"collector": "kernel",
		},
		Metadata: map[string]interface{}{
			"sample_rate": 1000,
		},
	}

	// Test confidence bounds
	if dq.Confidence < 0 || dq.Confidence > 1 {
		t.Errorf("Confidence out of bounds: %f", dq.Confidence)
	}

	// Test tags access
	if dq.Tags["collector"] != "kernel" {
		t.Error("Tags not accessible")
	}

	// Test metadata access
	if rate, ok := dq.Metadata["sample_rate"].(int); !ok || rate != 1000 {
		t.Error("Metadata not accessible")
	}
}

func TestUniversalDataset(t *testing.T) {
	dataset := &UniversalDataset{
		ID:        "test-dataset",
		Version:   "1.0",
		Timestamp: time.Now(),
		Source:    "test",
	}

	// Add metrics
	for i := 0; i < 5; i++ {
		m := GetMetric()
		m.ID = fmt.Sprintf("metric-%d", i)
		m.Name = "test_metric"
		m.Value = float64(i)
		dataset.Metrics = append(dataset.Metrics, *m)
		PutMetric(m)
	}

	// Add events
	for i := 0; i < 3; i++ {
		e := GetEvent()
		e.ID = fmt.Sprintf("event-%d", i)
		e.Type = EventTypeCustom
		dataset.Events = append(dataset.Events, *e)
		PutEvent(e)
	}

	// Add predictions
	for i := 0; i < 2; i++ {
		p := GetPrediction()
		p.ID = fmt.Sprintf("prediction-%d", i)
		p.Type = PredictionTypeOOM
		dataset.Predictions = append(dataset.Predictions, *p)
		PutPrediction(p)
	}

	// Verify counts
	if len(dataset.Metrics) != 5 {
		t.Errorf("Expected 5 metrics, got %d", len(dataset.Metrics))
	}

	if len(dataset.Events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(dataset.Events))
	}

	if len(dataset.Predictions) != 2 {
		t.Errorf("Expected 2 predictions, got %d", len(dataset.Predictions))
	}

	// Update sample count
	dataset.SampleCount = len(dataset.Metrics) + len(dataset.Events) + len(dataset.Predictions)

	if dataset.SampleCount != 10 {
		t.Errorf("Expected sample count 10, got %d", dataset.SampleCount)
	}
}

func BenchmarkMetricPool(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m := GetMetric()
			m.ID = "benchmark"
			m.Value = 42.0
			PutMetric(m)
		}
	})
}

func BenchmarkMetricClone(b *testing.B) {
	original := GetMetric()
	original.ID = "benchmark"
	original.Name = "test_metric"
	original.Value = 42.0
	original.Labels["env"] = "test"
	original.Quality.Tags["source"] = "benchmark"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clone := original.Clone()
		PutMetric(clone)
	}

	PutMetric(original)
}
