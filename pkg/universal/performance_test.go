package universal

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool()

	// Test get and put
	buf1 := pool.Get()
	if buf1 == nil {
		t.Fatal("Expected non-nil buffer")
	}

	// Write some data
	buf1.WriteString("test data")

	// Return to pool
	pool.Put(buf1)

	// Get another buffer - should be reset
	buf2 := pool.Get()
	if buf2.Len() != 0 {
		t.Error("Buffer not reset properly")
	}

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := pool.Get()
			buf.WriteString("concurrent test")
			pool.Put(buf)
		}()
	}
	wg.Wait()
}

func TestSerializer(t *testing.T) {
	serializer := NewSerializer()

	t.Run("SerializeMetric", func(t *testing.T) {
		metric := GetMetric()
		metric.ID = "test-metric"
		metric.Name = "cpu_usage"
		metric.Value = 75.5
		metric.Labels["host"] = "server1"

		data, err := serializer.SerializeMetric(metric)
		if err != nil {
			t.Fatalf("Failed to serialize metric: %v", err)
		}

		// Verify we can deserialize
		var decoded UniversalMetric
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if decoded.ID != metric.ID {
			t.Errorf("Expected ID %s, got %s", metric.ID, decoded.ID)
		}

		PutMetric(metric)
	})

	t.Run("SerializeEvent", func(t *testing.T) {
		event := GetEvent()
		event.ID = "test-event"
		event.Type = EventTypeOOM
		event.Level = EventLevelCritical
		event.Message = "OOM detected"

		data, err := serializer.SerializeEvent(event)
		if err != nil {
			t.Fatalf("Failed to serialize event: %v", err)
		}

		var decoded UniversalEvent
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if decoded.Type != event.Type {
			t.Errorf("Expected type %s, got %s", event.Type, decoded.Type)
		}

		PutEvent(event)
	})

	t.Run("SerializePrediction", func(t *testing.T) {
		prediction := GetPrediction()
		prediction.ID = "test-prediction"
		prediction.Type = PredictionTypeOOM
		prediction.Probability = 0.95

		data, err := serializer.SerializePrediction(prediction)
		if err != nil {
			t.Fatalf("Failed to serialize prediction: %v", err)
		}

		var decoded UniversalPrediction
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if decoded.Probability != prediction.Probability {
			t.Errorf("Expected probability %f, got %f", prediction.Probability, decoded.Probability)
		}

		PutPrediction(prediction)
	})
}

// TestDataProcessor implements DataProcessor for testing
type TestDataProcessor struct {
	metrics     []*UniversalMetric
	events      []*UniversalEvent
	predictions []*UniversalPrediction
	mu          sync.Mutex
}

func (p *TestDataProcessor) ProcessMetrics(metrics []*UniversalMetric) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metrics = append(p.metrics, metrics...)
	return nil
}

func (p *TestDataProcessor) ProcessEvents(events []*UniversalEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, events...)
	return nil
}

func (p *TestDataProcessor) ProcessPredictions(predictions []*UniversalPrediction) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.predictions = append(p.predictions, predictions...)
	return nil
}

func TestBatchProcessor(t *testing.T) {
	testProcessor := &TestDataProcessor{}
	batchSize := 5
	flushTimeout := 100 * time.Millisecond

	bp := NewBatchProcessor(batchSize, flushTimeout, testProcessor)
	defer bp.Stop()

	t.Run("Batch size trigger", func(t *testing.T) {
		// Add metrics up to batch size
		for i := 0; i < batchSize; i++ {
			metric := GetMetric()
			metric.ID = fmt.Sprintf("metric-%d", i)
			if err := bp.AddMetric(metric); err != nil {
				t.Fatalf("Failed to add metric: %v", err)
			}
		}

		// Should have processed immediately
		time.Sleep(10 * time.Millisecond)

		testProcessor.mu.Lock()
		if len(testProcessor.metrics) != batchSize {
			t.Errorf("Expected %d metrics processed, got %d", batchSize, len(testProcessor.metrics))
		}
		testProcessor.mu.Unlock()
	})

	t.Run("Timeout trigger", func(t *testing.T) {
		// Clear previous results
		testProcessor.mu.Lock()
		testProcessor.events = nil
		testProcessor.mu.Unlock()

		// Add fewer events than batch size
		for i := 0; i < 3; i++ {
			event := GetEvent()
			event.ID = fmt.Sprintf("event-%d", i)
			if err := bp.AddEvent(event); err != nil {
				t.Fatalf("Failed to add event: %v", err)
			}
		}

		// Wait for timeout
		time.Sleep(flushTimeout + 50*time.Millisecond)

		testProcessor.mu.Lock()
		if len(testProcessor.events) != 3 {
			t.Errorf("Expected 3 events processed after timeout, got %d", len(testProcessor.events))
		}
		testProcessor.mu.Unlock()
	})

	t.Run("Manual flush", func(t *testing.T) {
		// Clear previous results
		testProcessor.mu.Lock()
		testProcessor.predictions = nil
		testProcessor.mu.Unlock()

		// Add one prediction
		prediction := GetPrediction()
		prediction.ID = "test-prediction"
		if err := bp.AddPrediction(prediction); err != nil {
			t.Fatalf("Failed to add prediction: %v", err)
		}

		// Manual flush
		bp.Flush()

		testProcessor.mu.Lock()
		if len(testProcessor.predictions) != 1 {
			t.Errorf("Expected 1 prediction after manual flush, got %d", len(testProcessor.predictions))
		}
		testProcessor.mu.Unlock()
	})
}

func TestMetricAggregator(t *testing.T) {
	agg := NewMetricAggregator()

	target := Target{
		Type: TargetTypeProcess,
		Name: "test-process",
		PID:  1234,
	}

	// Add multiple metrics
	for i := 0; i < 10; i++ {
		metric := GetMetric()
		metric.Name = "cpu_usage"
		metric.Target = target
		metric.Value = float64(i * 10)
		metric.Timestamp = time.Now().Add(time.Duration(i) * time.Second)

		agg.Aggregate(metric)
		PutMetric(metric)
	}

	// Get aggregated results
	results := agg.GetAggregated()

	key := fmt.Sprintf("%s_%s_%s", target.Type, target.Name, "cpu_usage")
	aggMetric, exists := results[key]
	if !exists {
		t.Fatal("Expected aggregated metric not found")
	}

	// Verify aggregation
	if aggMetric.Count != 10 {
		t.Errorf("Expected count 10, got %d", aggMetric.Count)
	}

	if aggMetric.Sum != 450 { // 0+10+20+...+90
		t.Errorf("Expected sum 450, got %f", aggMetric.Sum)
	}

	if aggMetric.Min != 0 {
		t.Errorf("Expected min 0, got %f", aggMetric.Min)
	}

	if aggMetric.Max != 90 {
		t.Errorf("Expected max 90, got %f", aggMetric.Max)
	}

	if aggMetric.LastValue != 90 {
		t.Errorf("Expected last value 90, got %f", aggMetric.LastValue)
	}

	// Test reset
	agg.Reset()
	results = agg.GetAggregated()
	if len(results) != 0 {
		t.Error("Expected empty results after reset")
	}
}

func BenchmarkSerializer_SerializeMetric(b *testing.B) {
	serializer := NewSerializer()
	metric := GetMetric()
	metric.ID = "bench-metric"
	metric.Name = "test_metric"
	metric.Value = 42.0
	metric.Labels["env"] = "test"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		data, err := serializer.SerializeMetric(metric)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}

	PutMetric(metric)
}

func BenchmarkBatchProcessor_AddMetric(b *testing.B) {
	testProcessor := &TestDataProcessor{}
	bp := NewBatchProcessor(100, time.Second, testProcessor)
	defer bp.Stop()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metric := GetMetric()
			metric.ID = "bench-metric"
			metric.Value = 42.0

			if err := bp.AddMetric(metric); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMetricAggregator_Aggregate(b *testing.B) {
	agg := NewMetricAggregator()

	metric := GetMetric()
	metric.Name = "benchmark_metric"
	metric.Target.Type = TargetTypeProcess
	metric.Target.Name = "bench-process"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		metric.Value = float64(i)
		metric.Timestamp = time.Now()
		agg.Aggregate(metric)
	}

	PutMetric(metric)
}
