package rules

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/domain"
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

// Test basic rule creation and metadata
func TestRulesBasic(t *testing.T) {
	// Test OOM prediction rule
	oomRule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:     3,
		PredictionWindow:  15 * time.Minute,
		CriticalThreshold: 0.9,
		WarningThreshold:  0.8,
		MinConfidence:     0.5,
		UseEBPFData:       true,
		UseMetricsData:    true,
		UseKubernetesData: true,
	})

	oomMeta := oomRule.GetMetadata()
	assert.Equal(t, "oom_prediction", oomMeta.ID)
	assert.Equal(t, "Out-of-Memory Prediction", oomMeta.Name)
	assert.True(t, oomMeta.Enabled)

	// Test memory leak rule
	leakRule := NewMemoryLeakRule(MemoryLeakConfig{
		MinObservationPeriod: 10 * time.Minute,
		MinDataPoints:        5,
		LeakThreshold:        1000.0,
		ConsistencyThreshold: 0.8,
		MinConfidence:        0.5,
		UseEBPFData:          true,
		UseMetricsData:       true,
		UseKubernetesData:    true,
	})

	leakMeta := leakRule.GetMetadata()
	assert.Equal(t, "memory_leak_detection", leakMeta.ID)
	assert.Equal(t, "Memory Leak Detection", leakMeta.Name)
	assert.True(t, leakMeta.Enabled)

	// Test CPU throttling rule
	cpuRule := NewCPUThrottlingRule(CPUThrottlingConfig{
		MinObservationPeriod: 5 * time.Minute,
		MinDataPoints:        5,
		ThrottlingThreshold:  0.2,
		ImpactThreshold:      0.1,
		MinConfidence:        0.7,
		UseEBPFData:          true,
		UseMetricsData:       true,
		UseKubernetesData:    true,
	})

	cpuMeta := cpuRule.GetMetadata()
	assert.Equal(t, "cpu_throttling", cpuMeta.ID)
	assert.Equal(t, "CPU Throttling Detection", cpuMeta.Name)
	assert.True(t, cpuMeta.Enabled)

	// Test crash loop rule
	crashRule := NewCrashLoopRule(CrashLoopConfig{
		MinRestartCount:      3,
		ObservationWindow:    30 * time.Minute,
		RestartRateThreshold: 5.0,
		BackoffThreshold:     5 * time.Minute,
		MinConfidence:        0.8,
		UseKubernetesData:    true,
		UseLogsData:          true,
		UseMetricsData:       true,
	})

	crashMeta := crashRule.GetMetadata()
	assert.Equal(t, "crash_loop_detection", crashMeta.ID)
	assert.Equal(t, "Pod Crash Loop Detection", crashMeta.Name)
	assert.True(t, crashMeta.Enabled)

	// Test disk pressure rule
	diskRule := NewDiskPressureRule(DiskPressureConfig{
		UsageThreshold:        0.85,
		InodeThreshold:        0.90,
		IOWaitThreshold:       0.20,
		WriteLatencyThreshold: 100 * time.Millisecond,
		ReadLatencyThreshold:  50 * time.Millisecond,
		MinConfidence:         0.7,
		PredictionWindow:      24 * time.Hour,
		UseEBPFData:           true,
		UseMetricsData:        true,
		UseKubernetesData:     true,
	})

	diskMeta := diskRule.GetMetadata()
	assert.Equal(t, "disk_pressure_detection", diskMeta.ID)
	assert.Equal(t, "Disk Pressure Detection", diskMeta.Name)
	assert.True(t, diskMeta.Enabled)
}

// Test rule validation
func TestRulesValidation(t *testing.T) {
	// Valid OOM rule
	oomRule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:     3,
		PredictionWindow:  15 * time.Minute,
		CriticalThreshold: 0.9,
		WarningThreshold:  0.8,
		MinConfidence:     0.5,
		UseEBPFData:       true,
	})
	assert.NoError(t, oomRule.Validate())

	// Valid memory leak rule
	leakRule := NewMemoryLeakRule(MemoryLeakConfig{
		MinObservationPeriod: 10 * time.Minute,
		MinDataPoints:        5,
		LeakThreshold:        1000.0,
		ConsistencyThreshold: 0.8,
		GrowthAcceleration:   1.5,
		MinConfidence:        0.5,
		UseEBPFData:          true,
	})
	assert.NoError(t, leakRule.Validate())

	// Valid CPU throttling rule
	cpuRule := NewCPUThrottlingRule(CPUThrottlingConfig{
		MinObservationPeriod: 5 * time.Minute,
		MinDataPoints:        5,
		ThrottlingThreshold:  0.2,
		ImpactThreshold:      0.1,
		MinConfidence:        0.7,
	})
	assert.NoError(t, cpuRule.Validate())

	// Valid crash loop rule
	crashRule := NewCrashLoopRule(CrashLoopConfig{
		MinRestartCount:      3,
		ObservationWindow:    30 * time.Minute,
		RestartRateThreshold: 5.0,
		BackoffThreshold:     5 * time.Minute,
		MinConfidence:        0.8,
	})
	assert.NoError(t, crashRule.Validate())

	// Valid disk pressure rule
	diskRule := NewDiskPressureRule(DiskPressureConfig{
		UsageThreshold:        0.85,
		InodeThreshold:        0.90,
		IOWaitThreshold:       0.20,
		WriteLatencyThreshold: 100 * time.Millisecond,
		ReadLatencyThreshold:  50 * time.Millisecond,
		MinConfidence:         0.7,
		PredictionWindow:      24 * time.Hour,
	})
	assert.NoError(t, diskRule.Validate())
}

// Test rule registry
func TestRulesRegistry(t *testing.T) {
	registry := correlation.NewRuleRegistry()

	// Register OOM rule
	oomRule := NewOOMPredictionRule(OOMPredictionConfig{
		MinDataPoints:     3,
		PredictionWindow:  15 * time.Minute,
		CriticalThreshold: 0.9,
		WarningThreshold:  0.8,
		MinConfidence:     0.5,
		UseEBPFData:       true,
	})

	err := registry.RegisterRule(oomRule)
	require.NoError(t, err)
	assert.True(t, registry.IsEnabled("oom_prediction"))

	// Register memory leak rule
	leakRule := NewMemoryLeakRule(MemoryLeakConfig{
		MinObservationPeriod: 10 * time.Minute,
		MinDataPoints:        5,
		LeakThreshold:        1000.0,
		ConsistencyThreshold: 0.8,
		GrowthAcceleration:   1.5,
		MinConfidence:        0.5,
		UseEBPFData:          true,
	})

	err = registry.RegisterRule(leakRule)
	require.NoError(t, err)
	assert.True(t, registry.IsEnabled("memory_leak_detection"))

	// Register CPU throttling rule
	cpuRule := NewCPUThrottlingRule(DefaultCPUThrottlingConfig())
	err = registry.RegisterRule(cpuRule)
	require.NoError(t, err)
	assert.True(t, registry.IsEnabled("cpu_throttling"))

	// Register crash loop rule
	crashRule := NewCrashLoopRule(DefaultCrashLoopConfig())
	err = registry.RegisterRule(crashRule)
	require.NoError(t, err)
	assert.True(t, registry.IsEnabled("crash_loop_detection"))

	// Register disk pressure rule
	diskRule := NewDiskPressureRule(DefaultDiskPressureConfig())
	err = registry.RegisterRule(diskRule)
	require.NoError(t, err)
	assert.True(t, registry.IsEnabled("disk_pressure_detection"))

	// Check enabled rules
	enabledRules := registry.GetEnabledRules()
	assert.Len(t, enabledRules, 5)

	// Check tags
	memoryRules := registry.GetRulesByTag("memory")
	assert.Len(t, memoryRules, 2)

	performanceRules := registry.GetRulesByTag("performance")
	assert.Len(t, performanceRules, 4) // OOM, memory leak, CPU throttling, disk pressure

	stabilityRules := registry.GetRulesByTag("stability")
	assert.Len(t, stabilityRules, 2) // Memory leak, crash loop
}

// Test basic execution flow
func TestRulesExecution(t *testing.T) {
	t.Run("OOM Rule with no data", func(t *testing.T) {
		// Create rule
		rule := NewOOMPredictionRule(OOMPredictionConfig{
			MinDataPoints:     3,
			PredictionWindow:  15 * time.Minute,
			CriticalThreshold: 0.9,
			WarningThreshold:  0.8,
			MinConfidence:     0.5,
			UseEBPFData:       true,
		})

		// Create empty data collection
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceKubernetes: &mockDataSource{
				sourceType: correlation.SourceKubernetes,
				available:  true,
				data: &correlation.KubernetesData{
					Pods:      []corev1.Pod{},
					Events:    []corev1.Event{},
					Metrics:   map[string]interface{}{},
					Problems:  []types.Problem{},
					Timestamp: time.Now(),
				},
			},
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  true,
				data: &correlation.EBPFData{
					ProcessStats:  map[uint32]*correlation.ProcessMemoryStats{},
					SystemMetrics: correlation.SystemMetrics{},
					MemoryEvents:  []correlation.MemoryEvent{},
					CPUEvents:     []correlation.CPUEvent{},
					IOEvents:      []correlation.IOEvent{},
					Timestamp:     time.Now(),
				},
			},
		}
		dataCollection := correlation.NewDataCollection(sources)

		// Execute
		ruleCtx := &correlation.RuleContext{
			DataCollection:   dataCollection,
			PreviousFindings: []correlation.Finding{},
			ExecutionTime:    time.Now(),
			Metadata:         make(map[string]interface{}),
		}

		ctx := context.Background()
		findings, err := rule.Execute(ctx, ruleCtx)

		// Should handle empty data gracefully
		assert.NoError(t, err)
		assert.Empty(t, findings) // No findings with no data
	})

	t.Run("Memory Leak Rule with no data", func(t *testing.T) {
		// Create rule
		rule := NewMemoryLeakRule(MemoryLeakConfig{
			MinObservationPeriod: 10 * time.Minute,
			MinDataPoints:        5,
			LeakThreshold:        1000.0,
			ConsistencyThreshold: 0.8,
			MinConfidence:        0.5,
			UseEBPFData:          true,
		})

		// Create empty data collection
		sources := map[correlation.SourceType]correlation.DataSource{
			correlation.SourceKubernetes: &mockDataSource{
				sourceType: correlation.SourceKubernetes,
				available:  true,
				data: &correlation.KubernetesData{
					Pods:      []corev1.Pod{},
					Events:    []corev1.Event{},
					Metrics:   map[string]interface{}{},
					Problems:  []types.Problem{},
					Timestamp: time.Now(),
				},
			},
			correlation.SourceEBPF: &mockDataSource{
				sourceType: correlation.SourceEBPF,
				available:  true,
				data: &correlation.EBPFData{
					ProcessStats:  map[uint32]*correlation.ProcessMemoryStats{},
					SystemMetrics: correlation.SystemMetrics{},
					MemoryEvents:  []correlation.MemoryEvent{},
					CPUEvents:     []correlation.CPUEvent{},
					IOEvents:      []correlation.IOEvent{},
					Timestamp:     time.Now(),
				},
			},
		}
		dataCollection := correlation.NewDataCollection(sources)

		// Execute
		ruleCtx := &correlation.RuleContext{
			DataCollection:   dataCollection,
			PreviousFindings: []correlation.Finding{},
			ExecutionTime:    time.Now(),
			Metadata:         make(map[string]interface{}),
		}

		ctx := context.Background()
		findings, err := rule.Execute(ctx, ruleCtx)

		// Should handle empty data gracefully
		assert.NoError(t, err)
		assert.Empty(t, findings) // No findings with no data
	})
}
