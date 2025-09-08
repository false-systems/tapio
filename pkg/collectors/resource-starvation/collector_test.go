package resourcestarvation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := NewDefaultConfig()

	collector, err := NewCollector(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	assert.Equal(t, config.Name, collector.config.Name)
	assert.NotNil(t, collector.logger)
	assert.NotNil(t, collector.tracer)
	assert.NotNil(t, collector.meter)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid config",
			config:      NewDefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid starvation threshold",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: -1,
			},
			expectError: true,
			errorMsg:    "starvation_threshold_ms must be positive",
		},
		{
			name: "invalid threshold ordering",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     50,
				CriticalThresholdMS:   200,
			},
			expectError: true,
			errorMsg:    "severe_threshold_ms",
		},
		{
			name: "invalid sample rate",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   1000,
				SampleRate:            1.5,
			},
			expectError: true,
			errorMsg:    "sample_rate must be between 0.0 and 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEventTypeMapping(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventSchedWait, "Scheduling Delay"},
		{EventCFSThrottle, "CFS Throttling"},
		{EventPriorityInvert, "Priority Inversion"},
		{EventCoreMigrate, "Core Migration"},
		{EventNoisyNeighbor, "Noisy Neighbor"},
		{EventType(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.eventType.String())
		})
	}
}

func TestSeverityMapping(t *testing.T) {
	tests := []struct {
		waitTimeNS uint64
		expected   string
	}{
		{50_000_000, "low"},         // 50ms
		{150_000_000, "medium"},     // 150ms
		{600_000_000, "high"},       // 600ms
		{2_500_000_000, "critical"}, // 2.5s
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetSeverity(tt.waitTimeNS))
		})
	}
}

func TestProcessEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := NewDefaultConfig()
	collector, err := NewCollector(config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	event := &StarvationEvent{
		Timestamp:      uint64(time.Now().UnixNano()),
		EventType:      uint32(EventSchedWait),
		VictimPID:      1234,
		VictimTGID:     1234,
		WaitTimeNS:     150_000_000, // 150ms
		CPUCore:        0,
		VictimComm:     [16]byte{'t', 'e', 's', 't'},
		VictimCgroupID: 12345,
	}

	err = collector.ProcessEvent(ctx, event)
	assert.NoError(t, err)
}

func TestPatternDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := NewDefaultConfig()
	config.EnablePatternDetection = true

	collector, err := NewCollector(config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Simulate periodic throttling
	event := &StarvationEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		EventType:   uint32(EventCFSThrottle),
		VictimPID:   1234,
		WaitTimeNS:  100_000_000, // 100ms
		ThrottledNS: 100_000_000,
	}

	err = collector.ProcessEvent(ctx, event)
	require.NoError(t, err)

	// Check if pattern was detected
	pattern := collector.detectStarvationPattern(event)
	assert.Equal(t, PatternThrottle, pattern)
}

func TestSchedulingPolicyString(t *testing.T) {
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
		{99, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetSchedulingPolicy(tt.policy))
		})
	}
}

func TestBytesToString(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := NewDefaultConfig()
	collector, err := NewCollector(config, logger)
	require.NoError(t, err)

	tests := []struct {
		input    []byte
		expected string
	}{
		{[]byte("test\x00\x00\x00"), "test"},
		{[]byte("full16bytescomm\x00"), "full16bytescomm"},
		{[]byte("\x00\x00\x00"), ""},
		{[]byte("no-null-term"), "no-null-term"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.bytesToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPatternDescription(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := NewDefaultConfig()
	collector, err := NewCollector(config, logger)
	require.NoError(t, err)

	tests := []struct {
		pattern  string
		expected string
	}{
		{PatternBurst, "Short burst of high CPU usage causing temporary starvation"},
		{PatternSustained, "Long-running CPU intensive task causing persistent starvation"},
		{PatternThrottle, "CFS bandwidth throttling limiting CPU access"},
		{PatternCacheThrash, "Frequent core migrations causing cache misses"},
		{PatternNoisyNeighbor, "Co-located workload consuming excessive CPU"},
		{"unknown", "Unknown starvation pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := collector.getPatternDescription(tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}
