package oom

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestOOMEventTypeValidation(t *testing.T) {
	tests := []struct {
		eventType    OOMEventType
		isCritical   bool
		isPredictive bool
		stringRep    string
	}{
		{OOMKillVictim, true, false, "oom_kill_victim"},
		{OOMKillTriggered, true, false, "oom_kill_triggered"},
		{MemoryPressureHigh, false, true, "memory_pressure_high"},
		{MemoryPressureCrit, true, true, "memory_pressure_critical"},
		{ContainerMemoryLimit, false, false, "container_memory_limit"},
		{CgroupOOMNotify, false, false, "cgroup_oom_notification"},
		{OOMEventType(999), false, false, "unknown"}, // Unknown type
	}

	for _, tt := range tests {
		t.Run(tt.stringRep, func(t *testing.T) {
			assert.Equal(t, tt.isCritical, tt.eventType.IsCritical())
			assert.Equal(t, tt.isPredictive, tt.eventType.IsPredictive())
			assert.Equal(t, tt.stringRep, tt.eventType.String())
		})
	}
}

func TestOOMConfigDefaults(t *testing.T) {
	config := DefaultOOMConfig()
	require.NotNil(t, config)

	// Test default values
	assert.True(t, config.EnablePrediction)
	assert.Equal(t, uint32(95), config.PredictionThresholdPct)
	assert.Equal(t, uint32(80), config.HighPressureThresholdPct)
	assert.Equal(t, uint32(1048576), config.RingBufferSize) // 1MB
	assert.Equal(t, uint32(100), config.EventBatchSize)
	assert.True(t, config.CollectCmdline)
	assert.False(t, config.CollectEnvironment) // Should be false by default (expensive)
	assert.True(t, config.CollectMemoryDetails)
	assert.True(t, config.ExcludeSystemProcesses)
	assert.True(t, config.EnableK8sCorrelation)
}

func TestOOMConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *OOMConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid default config",
			config:  DefaultOOMConfig(),
			wantErr: false,
		},
		{
			name: "prediction threshold too high",
			config: &OOMConfig{
				PredictionThresholdPct:   150,
				HighPressureThresholdPct: 80,
				RingBufferSize:           4096,
				EventBatchSize:           1,
			},
			wantErr:     true,
			errContains: "prediction_threshold_percent",
		},
		{
			name: "high pressure threshold too high",
			config: &OOMConfig{
				PredictionThresholdPct:   95,
				HighPressureThresholdPct: 101,
				RingBufferSize:           4096,
				EventBatchSize:           1,
			},
			wantErr:     true,
			errContains: "high_pressure_threshold_percent",
		},
		{
			name: "ring buffer too small",
			config: &OOMConfig{
				PredictionThresholdPct:   95,
				HighPressureThresholdPct: 80,
				RingBufferSize:           1024, // Less than 4096
				EventBatchSize:           1,
			},
			wantErr:     true,
			errContains: "ring_buffer_size",
		},
		{
			name: "zero batch size",
			config: &OOMConfig{
				PredictionThresholdPct:   95,
				HighPressureThresholdPct: 80,
				RingBufferSize:           4096,
				EventBatchSize:           0,
			},
			wantErr:     true,
			errContains: "event_batch_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEventConversion(t *testing.T) {
	// Create a raw OOM event
	now := time.Now()
	rawEvent := &OOMEvent{
		Timestamp:          uint64(now.UnixNano()),
		PID:                1234,
		TGID:               1234,
		PPID:               5678,
		KillerPID:          91011,
		MemoryUsage:        800 * 1024 * 1024,  // 800MB
		MemoryLimit:        1024 * 1024 * 1024, // 1GB
		MemoryMaxUsage:     850 * 1024 * 1024,  // 850MB peak
		SwapUsage:          100 * 1024 * 1024,  // 100MB swap
		CacheUsage:         200 * 1024 * 1024,  // 200MB cache
		UID:                1000,
		GID:                1000,
		CgroupID:           12345,
		EventType:          uint32(OOMKillVictim),
		OOMScore:           100,
		PagesScanned:       1000,
		PagesReclaimed:     200,
		GFPFlags:           0x400, // Example GFP flags
		Order:              0,
		TriggerPID:         9999,
		AllocationSize:     50 * 1024 * 1024, // 50MB allocation
		TimeToKillMS:       5000,             // 5 seconds
		PressureDurationMS: 30000,            // 30 seconds
		AllocationRateMBS:  15,               // 15 MB/s
		ReclaimEfficiency:  20,               // 20%
	}

	// Copy strings (simulate kernel data)
	copy(rawEvent.Comm[:], "test-process")
	copy(rawEvent.CgroupPath[:], "/sys/fs/cgroup/memory/kubepods/burstable/pod123/container456")
	copy(rawEvent.ContainerID[:], "container456")
	copy(rawEvent.Cmdline[:], "test-process --config=/etc/app.conf")

	// Convert to processed event
	processed := rawEvent.ToProcessedEvent()

	// Verify basic fields
	assert.Equal(t, OOMKillVictim, processed.EventType)
	assert.Equal(t, uint32(1234), processed.PID)
	assert.Equal(t, uint32(1234), processed.TGID)
	assert.Equal(t, uint32(5678), processed.PPID)
	assert.Equal(t, uint32(91011), processed.KillerPID)
	assert.Equal(t, "test-process", processed.Command)
	assert.Equal(t, "test-process --config=/etc/app.conf", processed.Commandline)
	assert.Equal(t, uint32(1000), processed.UID)
	assert.Equal(t, uint32(1000), processed.GID)
	assert.Equal(t, uint32(100), processed.OOMScore)

	// Verify memory statistics
	assert.Equal(t, uint64(800*1024*1024), processed.MemoryStats.UsageBytes)
	assert.Equal(t, uint64(1024*1024*1024), processed.MemoryStats.LimitBytes)
	assert.Equal(t, uint64(850*1024*1024), processed.MemoryStats.MaxUsageBytes)
	assert.Equal(t, uint64(100*1024*1024), processed.MemoryStats.SwapUsageBytes)
	assert.Equal(t, uint64(200*1024*1024), processed.MemoryStats.CacheUsageBytes)
	assert.InDelta(t, 78.125, processed.MemoryStats.UsagePercent, 0.001) // 800/1024 * 100
	assert.Equal(t, "medium", processed.MemoryStats.PressureLevel)       // 78.125% is medium (50-80%)

	// Verify Kubernetes context
	assert.Equal(t, "container456", processed.KubernetesContext.ContainerID)
	assert.Equal(t, uint64(12345), processed.KubernetesContext.CgroupID)
	assert.Equal(t, "/sys/fs/cgroup/memory/kubepods/burstable/pod123/container456", processed.KubernetesContext.CgroupPath)

	// Verify performance data
	assert.Equal(t, uint64(1000), processed.PerformanceData.PagesScanned)
	assert.Equal(t, uint64(200), processed.PerformanceData.PagesReclaimed)
	assert.InDelta(t, 0.2, processed.PerformanceData.ReclaimRatio, 0.001) // 200/1000
	assert.Equal(t, uint32(0), processed.PerformanceData.AllocationOrder)
	assert.Equal(t, uint32(0x400), processed.PerformanceData.GFPFlags)
	assert.Equal(t, uint64(50*1024*1024), processed.PerformanceData.AllocationSize)
	assert.Equal(t, uint64(5000), processed.PerformanceData.TimeToKillMS)

	// Verify system context
	assert.Equal(t, "1.0.0", processed.SystemContext.CollectorVersion)
	assert.Equal(t, "confirmed", processed.SystemContext.EventReliability)

	// Since this is not a predictive event, PredictionData should be nil
	assert.Nil(t, processed.PredictionData)
}

func TestPredictiveEventConversion(t *testing.T) {
	// Create a predictive OOM event
	rawEvent := &OOMEvent{
		Timestamp:          uint64(time.Now().UnixNano()),
		PID:                2345,
		TGID:               2345,
		EventType:          uint32(MemoryPressureCrit),
		MemoryUsage:        950 * 1024 * 1024,  // 950MB
		MemoryLimit:        1024 * 1024 * 1024, // 1GB
		CgroupID:           54321,
		PressureDurationMS: 45000, // 45 seconds under pressure
		AllocationRateMBS:  25,    // 25 MB/s allocation rate
		ReclaimEfficiency:  15,    // 15% reclaim efficiency
	}

	copy(rawEvent.Comm[:], "memory-hog")
	copy(rawEvent.ContainerID[:], "hog-container")

	// Convert to processed event
	processed := rawEvent.ToProcessedEvent()

	// Verify it's recognized as predictive
	assert.True(t, processed.EventType.IsPredictive())
	assert.True(t, processed.EventType.IsCritical()) // Critical prediction

	// Verify memory pressure is high (not critical - that's 95%+)
	assert.InDelta(t, 92.77, processed.MemoryStats.UsagePercent, 0.1) // ~950/1024 * 100
	assert.Equal(t, "high", processed.MemoryStats.PressureLevel)      // 92.77% is high (80-95%)

	// Verify prediction data is populated
	require.NotNil(t, processed.PredictionData)
	assert.Equal(t, uint32(25), processed.PredictionData.AllocationRateMBS)
	assert.Equal(t, uint32(45000), processed.PredictionData.PressureDurationMS)
	assert.Equal(t, uint32(15), processed.PredictionData.ReclaimEfficiency)
	assert.Equal(t, uint32(85), processed.PredictionData.ConfidencePercent) // Default confidence

	// Check calculated fields
	assert.Equal(t, "increasing", processed.PredictionData.AllocationTrend) // > 10 MB/s
	assert.Equal(t, "severe", processed.PredictionData.PressureSeverity)    // > 30 seconds
	assert.Equal(t, "immediate_scale_up", processed.PredictionData.RecommendedAction)
	assert.Equal(t, "critical", processed.PredictionData.EstimatedImpactLevel)

	// Should have predicted OOM time
	assert.NotNil(t, processed.PredictionData.PredictedOOMTimeS)
	assert.NotNil(t, processed.MemoryStats.TimeToExhaustion)
}

func TestCollectorEventConversion(t *testing.T) {
	// Create a processed OOM event
	processed := &ProcessedOOMEvent{
		EventType: OOMKillVictim,
		Timestamp: time.Now(),
		PID:       1234,
		PPID:      5678,
		Command:   "test-app",
		UID:       1000,
		GID:       1000,
		OOMScore:  150,

		MemoryStats: MemoryStatistics{
			UsageBytes:    900 * 1024 * 1024,
			LimitBytes:    1024 * 1024 * 1024,
			UsagePercent:  87.89,
			PressureLevel: "high",
		},

		KubernetesContext: KubernetesContext{
			ContainerID:   "test-container-789",
			ContainerName: "app-container",
			Runtime:       "containerd",
			PodName:       "test-app-xyz",
			PodNamespace:  "production",
			PodUID:        "pod-uuid-123",
			NodeName:      "worker-node-01",
			CgroupPath:    "/kubepods/burstable/pod123/container789",
			CgroupID:      98765,
		},

		PerformanceData: PerformanceData{
			PagesScanned:   2000,
			PagesReclaimed: 500,
			ReclaimRatio:   0.25,
		},

		SystemContext: SystemContext{
			CollectorVersion: "2.0.0",
			EventReliability: "confirmed",
		},
	}

	// Convert to collector event
	collectorEvent := processed.ToCollectorEvent()

	// Verify core event fields
	assert.Contains(t, collectorEvent.EventID, "oom-")
	assert.Equal(t, processed.Timestamp, collectorEvent.Timestamp)
	assert.Equal(t, domain.EventTypeContainerOOM, collectorEvent.Type)
	assert.Equal(t, "oom-collector", collectorEvent.Source)
	assert.Equal(t, domain.EventSeverityCritical, collectorEvent.Severity) // OOM kill is critical

	// Verify event data
	require.NotNil(t, collectorEvent.EventData.Container)
	assert.Equal(t, "test-container-789", collectorEvent.EventData.Container.ContainerID)
	assert.Equal(t, "containerd", collectorEvent.EventData.Container.Runtime)
	assert.Equal(t, "killed", collectorEvent.EventData.Container.State)
	assert.Equal(t, "oom_kill", collectorEvent.EventData.Container.Action)
	assert.Equal(t, int32(1234), collectorEvent.EventData.Container.PID)

	require.NotNil(t, collectorEvent.EventData.Process)
	assert.Equal(t, int32(1234), collectorEvent.EventData.Process.PID)
	assert.Equal(t, int32(5678), collectorEvent.EventData.Process.PPID)
	assert.Equal(t, "test-app", collectorEvent.EventData.Process.Command)
	assert.Equal(t, int32(1000), collectorEvent.EventData.Process.UID)
	assert.Equal(t, int32(1000), collectorEvent.EventData.Process.GID)
	assert.Equal(t, "test-container-789", collectorEvent.EventData.Process.ContainerID)

	// Verify metadata
	assert.Equal(t, "test-app-xyz", collectorEvent.Metadata.PodName)
	assert.Equal(t, "production", collectorEvent.Metadata.PodNamespace)
	assert.Equal(t, "pod-uuid-123", collectorEvent.Metadata.PodUID)
	assert.Equal(t, "test-container-789", collectorEvent.Metadata.ContainerID)
	assert.Equal(t, "app-container", collectorEvent.Metadata.ContainerName)
	assert.Equal(t, "worker-node-01", collectorEvent.Metadata.NodeName)
	assert.Equal(t, int32(1234), collectorEvent.Metadata.PID)
	assert.Equal(t, int32(5678), collectorEvent.Metadata.PPID)
	assert.Equal(t, uint64(98765), collectorEvent.Metadata.CgroupID)
	assert.Equal(t, "test-app", collectorEvent.Metadata.Command)
	assert.Equal(t, domain.PriorityCritical, collectorEvent.Metadata.Priority)

	// Verify correlation hints
	require.NotNil(t, collectorEvent.CorrelationHints)
	assert.Equal(t, "pod-uuid-123", collectorEvent.CorrelationHints.PodUID)
	assert.Equal(t, "test-container-789", collectorEvent.CorrelationHints.ContainerID)
	assert.Equal(t, int32(1234), collectorEvent.CorrelationHints.ProcessID)
	assert.Equal(t, "/kubepods/burstable/pod123/container789", collectorEvent.CorrelationHints.CgroupPath)
	assert.Equal(t, "worker-node-01", collectorEvent.CorrelationHints.NodeName)

	require.NotNil(t, collectorEvent.CorrelationHints.CorrelationTags)
	assert.Equal(t, "oom_kill_victim", collectorEvent.CorrelationHints.CorrelationTags["oom_event_type"])
	assert.Equal(t, "high", collectorEvent.CorrelationHints.CorrelationTags["memory_pressure"])
	assert.Equal(t, "containerd", collectorEvent.CorrelationHints.CorrelationTags["container_runtime"])

	// Verify K8s context
	require.NotNil(t, collectorEvent.K8sContext)
	assert.Equal(t, "Pod", collectorEvent.K8sContext.Kind)
	assert.Equal(t, "test-app-xyz", collectorEvent.K8sContext.Name)
	assert.Equal(t, "production", collectorEvent.K8sContext.Namespace)
	assert.Equal(t, "pod-uuid-123", collectorEvent.K8sContext.UID)
	assert.Equal(t, "worker-node-01", collectorEvent.K8sContext.NodeName)
}

func TestObservationEventConversion(t *testing.T) {
	// Create a processed OOM event
	processed := &ProcessedOOMEvent{
		EventType: MemoryPressureHigh,
		Timestamp: time.Now(),
		PID:       3456,
		Command:   "web-server",

		MemoryStats: MemoryStatistics{
			UsageBytes:    800 * 1024 * 1024,
			LimitBytes:    1024 * 1024 * 1024,
			PressureLevel: "high",
		},

		KubernetesContext: KubernetesContext{
			ContainerID:  "web-container-456",
			PodName:      "web-app-abc",
			PodNamespace: "staging",
			NodeName:     "worker-node-02",
		},

		PredictionData: &PredictionData{
			PredictedOOMTimeS: func(i int64) *int64 { return &i }(300), // 5 minutes
		},
	}

	// Convert to observation event
	observationEvent := processed.ToObservationEvent()

	// Verify core fields
	assert.Contains(t, observationEvent.ID, "oom-")
	assert.Equal(t, processed.Timestamp, observationEvent.Timestamp)
	assert.Equal(t, "oom", observationEvent.Source)
	assert.Equal(t, "memory_pressure_high", observationEvent.Type)

	// Verify correlation keys
	require.NotNil(t, observationEvent.PID)
	assert.Equal(t, int32(3456), *observationEvent.PID)
	require.NotNil(t, observationEvent.ContainerID)
	assert.Equal(t, "web-container-456", *observationEvent.ContainerID)
	require.NotNil(t, observationEvent.PodName)
	assert.Equal(t, "web-app-abc", *observationEvent.PodName)
	require.NotNil(t, observationEvent.Namespace)
	assert.Equal(t, "staging", *observationEvent.Namespace)
	require.NotNil(t, observationEvent.NodeName)
	assert.Equal(t, "worker-node-02", *observationEvent.NodeName)

	// Verify event data fields
	require.NotNil(t, observationEvent.Action)
	assert.Equal(t, "warn", *observationEvent.Action) // High pressure = warn
	require.NotNil(t, observationEvent.Target)
	assert.Equal(t, "web-server", *observationEvent.Target)
	require.NotNil(t, observationEvent.Result)
	assert.Equal(t, "killed", *observationEvent.Result)
	require.NotNil(t, observationEvent.Reason)
	assert.Equal(t, "out_of_memory", *observationEvent.Reason)

	// Verify metrics
	require.NotNil(t, observationEvent.Duration)
	assert.Equal(t, int64(300), *observationEvent.Duration) // From prediction
	require.NotNil(t, observationEvent.Size)
	assert.Equal(t, int64(800), *observationEvent.Size) // Memory usage in MB

	// Verify data map
	assert.NotNil(t, observationEvent.Data)
	assert.Equal(t, "memory_pressure_high", observationEvent.Data["event_type"])
	assert.Equal(t, "800", observationEvent.Data["memory_usage_mb"])
	assert.Equal(t, "1024", observationEvent.Data["memory_limit_mb"])
	assert.Equal(t, "high", observationEvent.Data["memory_pressure"])
	assert.Equal(t, "web-container-456", observationEvent.Data["container_id"])
}

func TestNullTerminatedString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "normal string with null terminator",
			input:    []byte("hello\x00world"),
			expected: "hello",
		},
		{
			name:     "string without null terminator",
			input:    []byte("hello"),
			expected: "hello",
		},
		{
			name:     "empty bytes",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "only null byte",
			input:    []byte{0},
			expected: "",
		},
		{
			name:     "string with embedded nulls",
			input:    []byte("a\x00b\x00c"),
			expected: "a",
		},
		{
			name:     "long string with null",
			input:    append([]byte("very-long-process-name"), 0, 0, 0, 0),
			expected: "very-long-process-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullTerminatedString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	// Test stringPtr helper
	s := "test"
	ptr := stringPtr(s)
	require.NotNil(t, ptr)
	assert.Equal(t, s, *ptr)

	// Test generateEventID
	now := time.Now()
	eventID := generateEventID(1234, now)
	assert.Contains(t, eventID, "oom-")

	// Test struct size validation
	size := GetOOMEventSize()
	actualSize := unsafe.Sizeof(OOMEvent{})
	assert.Equal(t, actualSize, size)
}

func TestMemoryStatsCalculation(t *testing.T) {
	processed := &ProcessedOOMEvent{
		MemoryStats: MemoryStatistics{
			UsageBytes: 512 * 1024 * 1024,  // 512MB
			LimitBytes: 1024 * 1024 * 1024, // 1GB
		},
		PerformanceData: PerformanceData{
			PagesScanned:   1000,
			PagesReclaimed: 300,
		},
	}

	// Calculate derived fields
	processed.calculateDerivedFields()

	// Check usage percentage
	assert.InDelta(t, 50.0, processed.MemoryStats.UsagePercent, 0.1)

	// Check pressure level
	assert.Equal(t, "medium", processed.MemoryStats.PressureLevel) // 50% is medium

	// Check reclaim ratio
	assert.InDelta(t, 0.3, processed.PerformanceData.ReclaimRatio, 0.001) // 300/1000
}

func TestPressureLevelCalculation(t *testing.T) {
	tests := []struct {
		usagePercent  float64
		expectedLevel string
	}{
		{30.0, "low"},
		{65.0, "medium"},
		{85.0, "high"},
		{97.0, "critical"},
		{100.0, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.expectedLevel, func(t *testing.T) {
			processed := &ProcessedOOMEvent{
				MemoryStats: MemoryStatistics{
					UsagePercent: tt.usagePercent,
				},
			}

			processed.calculateDerivedFields()
			assert.Equal(t, tt.expectedLevel, processed.MemoryStats.PressureLevel)
		})
	}
}

func TestActionDetermination(t *testing.T) {
	tests := []struct {
		eventType      OOMEventType
		expectedAction string
	}{
		{OOMKillVictim, "kill"},
		{MemoryPressureHigh, "warn"},
		{MemoryPressureCrit, "alert"},
		{ContainerMemoryLimit, "monitor"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType.String(), func(t *testing.T) {
			processed := &ProcessedOOMEvent{
				EventType: tt.eventType,
			}

			action := processed.determineAction()
			assert.Equal(t, tt.expectedAction, action)
		})
	}
}

func TestSeverityAndPriorityDetermination(t *testing.T) {
	tests := []struct {
		eventType        OOMEventType
		usagePercent     float64
		expectedSeverity domain.EventSeverity
		expectedPriority domain.EventPriority
	}{
		{OOMKillVictim, 100.0, domain.EventSeverityCritical, domain.PriorityCritical},
		{MemoryPressureCrit, 96.0, domain.EventSeverityCritical, domain.PriorityHigh},
		{MemoryPressureHigh, 92.0, domain.EventSeverityHigh, domain.PriorityNormal},
		{MemoryPressureHigh, 82.0, domain.EventSeverityMedium, domain.PriorityNormal},
		{ContainerMemoryLimit, 70.0, domain.EventSeverityLow, domain.PriorityNormal},
	}

	for _, tt := range tests {
		t.Run(tt.eventType.String(), func(t *testing.T) {
			processed := &ProcessedOOMEvent{
				EventType: tt.eventType,
				MemoryStats: MemoryStatistics{
					UsagePercent: tt.usagePercent,
				},
			}

			severity := processed.determineSeverity()
			priority := processed.determinePriority()

			assert.Equal(t, tt.expectedSeverity, severity)
			assert.Equal(t, tt.expectedPriority, priority)
		})
	}
}

// Benchmark tests for performance validation

func BenchmarkEventConversion(b *testing.B) {
	rawEvent := &OOMEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		PID:         1234,
		TGID:        1234,
		EventType:   uint32(OOMKillVictim),
		MemoryUsage: 1024 * 1024 * 1024,
		MemoryLimit: 2048 * 1024 * 1024,
	}

	copy(rawEvent.Comm[:], "test-process")
	copy(rawEvent.ContainerID[:], "container123")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		processed := rawEvent.ToProcessedEvent()
		_ = processed.ToCollectorEvent()
		_ = processed.ToObservationEvent()
	}
}

func BenchmarkNullTerminatedString(b *testing.B) {
	testBytes := []byte("long-process-name-with-args\x00extra-data")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = nullTerminatedString(testBytes)
	}
}

func BenchmarkMemoryCalculations(b *testing.B) {
	processed := &ProcessedOOMEvent{
		MemoryStats: MemoryStatistics{
			UsageBytes: 800 * 1024 * 1024,
			LimitBytes: 1024 * 1024 * 1024,
		},
		PerformanceData: PerformanceData{
			PagesScanned:   2000,
			PagesReclaimed: 400,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		processed.calculateDerivedFields()
	}
}
