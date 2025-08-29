package resourcestarvation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventSchedWait, "scheduling_delay"},
		{EventCFSThrottle, "cfs_throttle"},
		{EventPriorityInvert, "priority_inversion"},
		{EventCoreMigrate, "core_migration"},
		{EventNoisyNeighbor, "noisy_neighbor"},
		{EventType(999), "unknown_999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.eventType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEventTypeIsCritical(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  bool
	}{
		{EventSchedWait, true},
		{EventCFSThrottle, true},
		{EventPriorityInvert, true},
		{EventCoreMigrate, false},
		{EventNoisyNeighbor, false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType.String(), func(t *testing.T) {
			result := tt.eventType.IsCritical()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSeverity(t *testing.T) {
	tests := []struct {
		name       string
		waitTimeNS uint64
		expected   string
	}{
		{"minor starvation", 50_000_000, SeverityMinor},          // 50ms
		{"moderate starvation", 200_000_000, SeverityModerate},   // 200ms
		{"severe starvation", 800_000_000, SeveritySevere},       // 800ms
		{"critical starvation", 3_000_000_000, SeverityCritical}, // 3s
		{"edge case minor", 99_999_999, SeverityMinor},           // 99.99ms
		{"edge case moderate", 100_000_000, SeverityModerate},    // 100ms exactly
		{"edge case severe", 500_000_000, SeveritySevere},        // 500ms exactly
		{"edge case critical", 2_000_000_000, SeverityCritical},  // 2s exactly
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSeverity(tt.waitTimeNS)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSchedulingPolicy(t *testing.T) {
	tests := []struct {
		policy   uint32
		expected string
	}{
		{0, "SCHED_NORMAL"},
		{1, "SCHED_FIFO"},
		{2, "SCHED_RR"},
		{3, "SCHED_BATCH"},
		{5, "SCHED_IDLE"},
		{6, "SCHED_DEADLINE"},
		{999, "UNKNOWN_999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GetSchedulingPolicy(tt.policy)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStarvationEventValidation(t *testing.T) {
	event := &StarvationEvent{
		Timestamp:       1234567890,
		EventType:       uint32(EventSchedWait),
		CPUCore:         2,
		VictimPID:       1001,
		VictimTGID:      1001,
		WaitTimeNS:      150_000_000,
		RunTimeNS:       50_000_000,
		CulpritPID:      2001,
		CulpritTGID:     2001,
		CulpritRuntime:  200_000_000,
		VictimCgroupID:  123456,
		CulpritCgroupID: 789012,
		VictimPrio:      0,
		CulpritPrio:     -10,
		VictimPolicy:    0,
	}

	assert.NotZero(t, event.Timestamp)
	assert.Equal(t, uint32(EventSchedWait), event.EventType)
	assert.Equal(t, uint32(2), event.CPUCore)
	assert.Equal(t, uint32(1001), event.VictimPID)
	assert.Equal(t, uint64(150_000_000), event.WaitTimeNS)
	assert.Equal(t, uint64(50_000_000), event.RunTimeNS)
}

func TestProcessedEventCreation(t *testing.T) {
	processedEvent := &ProcessedEvent{
		EventType: "scheduling_delay",
		CPUCore:   1,
		NodeName:  "test-node",
		Victim: VictimInfo{
			PID:        1234,
			TGID:       1234,
			Command:    "test-app",
			WaitTimeMS: 150.5,
			RunTimeMS:  50.2,
			Priority:   0,
			Policy:     "SCHED_NORMAL",
		},
		Impact: ImpactMetrics{
			WaitTimeMS:         150.5,
			WaitToRunRatio:     3.0,
			SeverityLevel:      SeverityModerate,
			EstimatedLatencyMS: 75.0,
		},
		Pattern: StarvationPattern{
			Type:        PatternBurst,
			Description: "Burst workload causing periodic starvation",
			Confidence:  0.8,
			Recurring:   true,
		},
	}

	assert.Equal(t, "scheduling_delay", processedEvent.EventType)
	assert.Equal(t, uint32(1), processedEvent.CPUCore)
	assert.Equal(t, "test-node", processedEvent.NodeName)
	assert.Equal(t, uint32(1234), processedEvent.Victim.PID)
	assert.Equal(t, 150.5, processedEvent.Victim.WaitTimeMS)
	assert.Equal(t, SeverityModerate, processedEvent.Impact.SeverityLevel)
	assert.Equal(t, PatternBurst, processedEvent.Pattern.Type)
	assert.True(t, processedEvent.Pattern.Recurring)
}

func TestPatternConstants(t *testing.T) {
	patterns := []string{
		PatternThrottle,
		PatternNoisyNeighbor,
		PatternBurst,
		PatternSustained,
		PatternPriorityInv,
		PatternCacheThrash,
	}

	for _, pattern := range patterns {
		assert.NotEmpty(t, pattern)
		assert.IsType(t, "", pattern)
	}
}

func TestSeverityConstants(t *testing.T) {
	severities := []string{
		SeverityMinor,
		SeverityModerate,
		SeveritySevere,
		SeverityCritical,
	}

	for _, severity := range severities {
		assert.NotEmpty(t, severity)
		assert.IsType(t, "", severity)
	}
}
