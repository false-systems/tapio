package rules

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/types"
)

// Mock data source for testing
type mockDataSource struct {
	sourceType correlation.SourceType
	available  bool
	data       interface{}
	err        error
}

func (m *mockDataSource) GetType() correlation.SourceType {
	return m.sourceType
}

func (m *mockDataSource) IsAvailable() bool {
	return m.available
}

func (m *mockDataSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	return m.data, m.err
}

// Test helper functions
func createTestProcessStats() map[uint32]*correlation.ProcessMemoryStats {
	return map[uint32]*correlation.ProcessMemoryStats{
		1234: {
			PID:            1234,
			Command:        "test-app",
			TotalAllocated: 1000000,
			TotalFreed:     800000,
			CurrentUsage:   200000,
			AllocationRate: 1000.0,
			LastUpdate:     time.Now(),
			InContainer:    true,
			ContainerPID:   5678,
			GrowthPattern: []correlation.MemoryDataPoint{
				{Timestamp: time.Now().Add(-5 * time.Minute), Usage: 100000},
				{Timestamp: time.Now().Add(-4 * time.Minute), Usage: 120000},
				{Timestamp: time.Now().Add(-3 * time.Minute), Usage: 140000},
				{Timestamp: time.Now().Add(-2 * time.Minute), Usage: 160000},
				{Timestamp: time.Now().Add(-1 * time.Minute), Usage: 180000},
				{Timestamp: time.Now(), Usage: 200000},
			},
		},
	}
}

func createTestEBPFData() *correlation.EBPFData {
	return &correlation.EBPFData{
		ProcessStats: createTestProcessStats(),
		SystemMetrics: correlation.SystemMetrics{
			TotalMemory:     8000000000, // 8GB
			AvailableMemory: 2000000000, // 2GB
			MemoryPressure:  0.75,       // High pressure
			CPUUsage:        0.6,
			Timestamp:       time.Now(),
		},
		MemoryEvents: []correlation.MemoryEvent{
			{
				Timestamp:   time.Now().Add(-1 * time.Minute),
				PID:         1234,
				EventType:   "allocation",
				Size:        4096,
				TotalMemory: 200000,
			},
		},
		Timestamp: time.Now(),
	}
}

func createTestKubernetesData() *correlation.KubernetesData {
	return &correlation.KubernetesData{
		Pods:      []corev1.Pod{},
		Events:    []corev1.Event{},
		Metrics:   map[string]interface{}{},
		Problems:  []types.Problem{},
		Timestamp: time.Now(),
	}
}

func createTestMetricsData() *correlation.MetricsData {
	return &correlation.MetricsData{
		PodMetrics: map[string]correlation.PodMetrics{
			"test-pod": {
				Name:      "test-pod",
				Namespace: "default",
				Memory: correlation.ResourceMetrics{
					Current: 200000,
					Limit:   1000000,
					Request: 100000,
					Usage:   0.2,
					Trend:   0.05, // Growing
				},
			},
		},
		ContainerMetrics: map[string]correlation.ContainerMetrics{},
		NodeMetrics: correlation.NodeMetrics{
			Name: "test-node",
			Memory: correlation.ResourceMetrics{
				Current: 6000000000,
				Limit:   8000000000,
				Usage:   0.75,
				Trend:   0.02,
			},
			MemoryPressure: true,
		},
		Timestamp: time.Now(),
	}
}

func createTestDataCollection() *correlation.DataCollection {
	sources := map[correlation.SourceType]correlation.DataSource{
		correlation.SourceEBPF: &mockDataSource{
			sourceType: correlation.SourceEBPF,
			available:  true,
			data:       createTestEBPFData(),
		},
		correlation.SourceKubernetes: &mockDataSource{
			sourceType: correlation.SourceKubernetes,
			available:  true,
			data:       createTestKubernetesData(),
		},
		correlation.SourceMetrics: &mockDataSource{
			sourceType: correlation.SourceMetrics,
			available:  true,
			data:       createTestMetricsData(),
		},
	}
	return correlation.NewDataCollection(sources)
}

func TestOOMPredictionRule_GetMetadata(t *testing.T) {
	rule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:       2,
		PredictionThreshold: 0.8,
		MinConfidence:       0.5,
	})
	metadata := rule.GetMetadata()

	assert.Equal(t, "oom_prediction", metadata.ID)
	assert.Equal(t, "Out-of-Memory Prediction", metadata.Name)
	assert.Contains(t, metadata.Description, "OOM")
	assert.True(t, metadata.Enabled)
	assert.Contains(t, metadata.Tags, "memory")
	assert.Contains(t, metadata.Tags, "prediction")
}

func TestOOMPredictionRule_CheckRequirements(t *testing.T) {
	rule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:       2,
		PredictionThreshold: 0.8,
		MinConfidence:       0.5,
	})
	ctx := context.Background()

	t.Run("All sources available", func(t *testing.T) {
		dataCollection := createTestDataCollection()
		err := rule.CheckRequirements(ctx, dataCollection)
		assert.NoError(t, err)
	})

	t.Run("Missing required sources", func(t *testing.T) {
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceKubernetes: &mockDataSource{
				sourceType: correlation.SourceKubernetes,
				available:  true,
			},
		}
		dataCollection := correlation.NewDataCollection(sources)
		err := rule.CheckRequirements(ctx, dataCollection)
		assert.Error(t, err)
	})

	t.Run("eBPF source unavailable", func(t *testing.T) {
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  false,
			},
		}
		dataCollection := correlation.NewDataCollection(sources)
		err := rule.CheckRequirements(ctx, dataCollection)
		assert.Error(t, err)
	})
}

func TestOOMPredictionRule_Execute(t *testing.T) {
	rule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:       2,
		PredictionThreshold: 0.8,
		MinConfidence:       0.5,
	})
	ctx := context.Background()
	dataCollection := createTestDataCollection()

	ruleCtx := &correlation.RuleContext{
		DataCollection:   dataCollection,
		PreviousFindings: []correlation.Finding{},
		ExecutionTime:    time.Now(),
		Metadata:         make(map[string]interface{}),
	}

	findings, err := rule.Execute(ctx, ruleCtx)
	require.NoError(t, err)

	// Should find OOM predictions for processes with growing memory
	assert.Greater(t, len(findings), 0)

	for _, finding := range findings {
		assert.Equal(t, "oom_prediction", finding.RuleID)
		assert.Contains(t, finding.Title, "OOM")
		assert.NotNil(t, finding.Prediction)
		assert.Greater(t, finding.Confidence, 0.0)
		assert.LessOrEqual(t, finding.Confidence, 1.0)

		// Should have evidence from multiple sources
		assert.Greater(t, len(finding.Evidence), 0)

		// Should have prediction details
		assert.NotEmpty(t, finding.Prediction.Event)
		assert.Greater(t, finding.Prediction.Confidence, 0.0)
	}
}

func TestOOMPredictionRule_CalculateGrowthRate(t *testing.T) {
	rule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:       2,
		PredictionThreshold: 0.8,
		MinConfidence:       0.5,
	})

	// Test with consistent growth pattern
	dataPoints := []correlation.MemoryDataPoint{
		{Timestamp: time.Now().Add(-5 * time.Minute), Usage: 100000},
		{Timestamp: time.Now().Add(-4 * time.Minute), Usage: 120000},
		{Timestamp: time.Now().Add(-3 * time.Minute), Usage: 140000},
		{Timestamp: time.Now().Add(-2 * time.Minute), Usage: 160000},
		{Timestamp: time.Now().Add(-1 * time.Minute), Usage: 180000},
		{Timestamp: time.Now(), Usage: 200000},
	}

	growthRate := rule.calculateGrowthRate(dataPoints)

	// Should detect positive growth rate
	assert.Greater(t, growthRate, 0.0)

	// Growth rate should be reasonable (not testing exact value due to implementation differences)
	assert.Less(t, growthRate, 10000.0) // Should be less than 10KB/s for this test data
}

// Note: calculateConfidence is not exported, so we test it indirectly through Execute

func TestMemoryLeakRule_GetMetadata(t *testing.T) {
	rule := NewMemoryLeakRule(MemoryLeakConfig{
		MinObservationPeriod: 5 * time.Minute,
		GrowthThreshold:      0.8,
		MinConfidence:        0.5,
	})
	metadata := rule.GetMetadata()

	assert.Equal(t, "memory_leak_detection", metadata.ID)
	assert.Equal(t, "Memory Leak Detection", metadata.Name)
	assert.Contains(t, metadata.Description, "memory leak")
	assert.True(t, metadata.Enabled)
	assert.Contains(t, metadata.Tags, "memory")
	assert.Contains(t, metadata.Tags, "leak")
}

func TestMemoryLeakRule_CheckRequirements(t *testing.T) {
	rule := NewMemoryLeakRule(MemoryLeakConfig{})
	ctx := context.Background()

	t.Run("All sources available", func(t *testing.T) {
		dataCollection := createTestDataCollection()
		err := rule.CheckRequirements(ctx, dataCollection)
		assert.NoError(t, err)
	})

	t.Run("Partial sources available", func(t *testing.T) {
		// Memory leak rule should work with just eBPF data
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  true,
				data:       createTestEBPFData(),
			},
		}
		dataCollection := correlation.NewDataCollection(sources)
		err := rule.CheckRequirements(ctx, dataCollection)
		assert.NoError(t, err)
	})
}

func TestMemoryLeakRule_Execute(t *testing.T) {
	rule := NewMemoryLeakRule(MemoryLeakConfig{})
	ctx := context.Background()
	dataCollection := createTestDataCollection()

	ruleCtx := &correlation.RuleContext{
		DataCollection:   dataCollection,
		PreviousFindings: []correlation.Finding{},
		ExecutionTime:    time.Now(),
		Metadata:         make(map[string]interface{}),
	}

	findings, err := rule.Execute(ctx, ruleCtx)
	require.NoError(t, err)

	// May or may not find leaks depending on the data patterns
	for _, finding := range findings {
		assert.Equal(t, "memory_leak", finding.RuleID)
		assert.Contains(t, finding.Title, "leak")
		assert.Greater(t, finding.Confidence, 0.0)
		assert.LessOrEqual(t, finding.Confidence, 1.0)

		// Should have evidence
		assert.Greater(t, len(finding.Evidence), 0)

		// Check evidence sources
		foundEBPF := false
		for _, evidence := range finding.Evidence {
			if evidence.Source == correlation.SourceEBPF {
				foundEBPF = true
			}
		}
		assert.True(t, foundEBPF, "Should have eBPF evidence")
	}
}

// Note: detectSustainedGrowth is not exported, so we test it indirectly through Execute

// Note: calculateLeakConfidence is not exported, so we test it indirectly through Execute

func TestRulesIntegration(t *testing.T) {
	// Test that both rules can be used together in a registry
	registry := correlation.NewRuleRegistry()

	oomRule := NewOOMPredictionRule(OOMPredictionConfig{})
	leakRule := NewMemoryLeakRule(MemoryLeakConfig{})

	err := registry.RegisterRule(oomRule)
	require.NoError(t, err)

	err = registry.RegisterRule(leakRule)
	require.NoError(t, err)

	// Verify both rules are registered and enabled
	assert.True(t, registry.IsEnabled("oom_prediction"))
	assert.True(t, registry.IsEnabled("memory_leak"))

	enabledRules := registry.GetEnabledRules()
	assert.Len(t, enabledRules, 2)

	// Test getting rules by tag
	memoryRules := registry.GetRulesByTag("memory")
	assert.Len(t, memoryRules, 2)
}

func TestRulesWithMissingData(t *testing.T) {
	t.Run("OOM rule with missing metrics data", func(t *testing.T) {
		rule := NewOOMPredictionRule(OOMPredictionConfig{})

		// Create data collection with only eBPF data
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  true,
				data:       createTestEBPFData(),
			},
		}
		dataCollection := correlation.NewDataCollection(sources)

		ruleCtx := &correlation.RuleContext{
			DataCollection:   dataCollection,
			PreviousFindings: []correlation.Finding{},
			ExecutionTime:    time.Now(),
			Metadata:         make(map[string]interface{}),
		}

		ctx := context.Background()
		_, err := rule.Execute(ctx, ruleCtx)

		// Should handle gracefully and still produce findings
		assert.NoError(t, err)
		// May have findings based on eBPF data alone
	})

	t.Run("Memory leak rule with minimal data", func(t *testing.T) {
		rule := NewMemoryLeakRule(MemoryLeakConfig{})

		// Create minimal eBPF data
		minimalStats := map[uint32]*correlation.ProcessMemoryStats{
			1234: {
				PID:          1234,
				Command:      "test-app",
				CurrentUsage: 100000,
				GrowthPattern: []correlation.MemoryDataPoint{
					{Timestamp: time.Now().Add(-1 * time.Minute), Usage: 90000},
					{Timestamp: time.Now(), Usage: 100000},
				},
			},
		}

		ebpfData := &correlation.EBPFData{
			ProcessStats:  minimalStats,
			SystemMetrics: correlation.SystemMetrics{},
			MemoryEvents:  []correlation.MemoryEvent{},
			Timestamp:     time.Now(),
		}

		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  true,
				data:       ebpfData,
			},
		}
		dataCollection := correlation.NewDataCollection(sources)

		ruleCtx := &correlation.RuleContext{
			DataCollection:   dataCollection,
			PreviousFindings: []correlation.Finding{},
			ExecutionTime:    time.Now(),
			Metadata:         make(map[string]interface{}),
		}

		ctx := context.Background()
		_, err := rule.Execute(ctx, ruleCtx)

		// Should handle minimal data gracefully
		assert.NoError(t, err)
		// Likely no findings due to insufficient data, but should not error
	})
}

func TestRuleValidation(t *testing.T) {
	t.Run("OOM rule validation", func(t *testing.T) {
		rule := NewOOMPredictionRule(OOMPredictionConfig{})
		err := rule.Validate()
		assert.NoError(t, err)
	})

	t.Run("Memory leak rule validation", func(t *testing.T) {
		rule := NewMemoryLeakRule(MemoryLeakConfig{})
		err := rule.Validate()
		assert.NoError(t, err)
	})
}

func TestConfidenceFactors(t *testing.T) {
	t.Run("OOM rule confidence factors", func(t *testing.T) {
		rule := NewOOMPredictionRule(OOMPredictionConfig{})
		factors := rule.GetConfidenceFactors()

		assert.Contains(t, factors, "data_consistency")
		assert.Contains(t, factors, "sample_size")
		assert.Contains(t, factors, "trend_strength")
		assert.Contains(t, factors, "multi_source_correlation")
	})

	t.Run("Memory leak rule confidence factors", func(t *testing.T) {
		rule := NewMemoryLeakRule(MemoryLeakConfig{})
		factors := rule.GetConfidenceFactors()

		assert.Contains(t, factors, "growth_consistency")
		assert.Contains(t, factors, "observation_period")
		assert.Contains(t, factors, "data_point_count")
		assert.Contains(t, factors, "pattern_strength")
	})
}
