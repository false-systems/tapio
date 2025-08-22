package systemdapi

import (
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name          string
		collectorName string
		config        Config
		expectError   bool
	}{
		{
			name:          "valid config",
			collectorName: "test-systemd-api",
			config:        TestConfig(),
			expectError:   false,
		},
		{
			name:          "default config",
			collectorName: "systemd-api",
			config:        DefaultConfig(),
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collectorName, tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, tt.collectorName, collector.Name())
				assert.NotNil(t, collector.events)
				assert.NotNil(t, collector.stats)
				assert.NotNil(t, collector.tracer)
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	config := TestConfig()
	collector, err := NewCollector("test-lifecycle", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test initial state
	assert.Equal(t, "test-lifecycle", collector.Name())
	assert.False(t, collector.IsHealthy()) // Not healthy until started

	// Test events channel
	eventsChan := collector.Events()
	assert.NotNil(t, eventsChan)

	// Test stopping before starting (should not error)
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "test config",
			config: TestConfig(),
		},
		{
			name:   "development config",
			config: DevelopmentConfig(),
		},
		{
			name:   "default config",
			config: DefaultConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-config", tt.config)
			require.NoError(t, err)
			assert.NotNil(t, collector)

			// Verify configuration is applied
			assert.Equal(t, tt.config.Name, collector.config.Name)
			assert.Equal(t, tt.config.BufferSize, collector.config.BufferSize)
			assert.Equal(t, tt.config.Priority, collector.config.Priority)
			assert.Equal(t, tt.config.Units, collector.config.Units)
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedValid  bool
		expectedName   string
		expectedBuffer int
	}{
		{
			name: "valid config",
			config: Config{
				Name:       "test",
				BufferSize: 100,
				MaxEntries: 50,
				Timeout:    time.Second * 5,
			},
			expectedValid:  true,
			expectedName:   "test",
			expectedBuffer: 100,
		},
		{
			name: "empty name gets default",
			config: Config{
				Name:       "",
				BufferSize: 100,
			},
			expectedValid:  true,
			expectedName:   "systemd-api",
			expectedBuffer: 100,
		},
		{
			name: "zero buffer gets default",
			config: Config{
				Name:       "test",
				BufferSize: 0,
			},
			expectedValid:  true,
			expectedName:   "test",
			expectedBuffer: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectedValid {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedName, tt.config.Name)
				assert.Equal(t, tt.expectedBuffer, tt.config.BufferSize)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestJournalMatches(t *testing.T) {
	config := Config{
		Units: []string{"kubelet.service", "containerd.service"},
		Matches: []sdjournal.Match{
			{Field: "PRIORITY", Value: "3"},
		},
	}

	matches := config.GetJournalMatches()
	assert.Len(t, matches, 3) // 2 units + 1 custom match

	// Check unit matches
	unitMatches := 0
	for _, match := range matches {
		if match.Field == "_SYSTEMD_UNIT" {
			unitMatches++
			assert.Contains(t, []string{"kubelet.service", "containerd.service"}, match.Value)
		}
	}
	assert.Equal(t, 2, unitMatches)

	// Check custom match
	customMatches := 0
	for _, match := range matches {
		if match.Field == "PRIORITY" && match.Value == "3" {
			customMatches++
		}
	}
	assert.Equal(t, 1, customMatches)
}

func TestPriorityFiltering(t *testing.T) {
	tests := []struct {
		name           string
		configPriority sdjournal.Priority
		testPriority   sdjournal.Priority
		shouldInclude  bool
	}{
		{
			name:           "error level includes error",
			configPriority: sdjournal.PriErr,
			testPriority:   sdjournal.PriErr,
			shouldInclude:  true,
		},
		{
			name:           "error level includes critical",
			configPriority: sdjournal.PriErr,
			testPriority:   sdjournal.PriCrit,
			shouldInclude:  true,
		},
		{
			name:           "error level excludes warning",
			configPriority: sdjournal.PriErr,
			testPriority:   sdjournal.PriWarning,
			shouldInclude:  false,
		},
		{
			name:           "warning level includes warning",
			configPriority: sdjournal.PriWarning,
			testPriority:   sdjournal.PriWarning,
			shouldInclude:  true,
		},
		{
			name:           "warning level includes error",
			configPriority: sdjournal.PriWarning,
			testPriority:   sdjournal.PriErr,
			shouldInclude:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Priority: tt.configPriority}
			result := config.ShouldIncludePriority(tt.testPriority)
			assert.Equal(t, tt.shouldInclude, result)
		})
	}
}

func TestJournalEntryEventType(t *testing.T) {
	tests := []struct {
		name         string
		entry        JournalEntry
		expectedType SystemdEventType
	}{
		{
			name: "service started",
			entry: JournalEntry{
				Unit:       "kubelet.service",
				UnitResult: "success",
				Message:    "Started Kubernetes kubelet",
			},
			expectedType: SystemdEventServiceStart,
		},
		{
			name: "service stopped",
			entry: JournalEntry{
				Unit:       "kubelet.service",
				UnitResult: "success",
				Message:    "Stopped Kubernetes kubelet",
			},
			expectedType: SystemdEventServiceStop,
		},
		{
			name: "service failed",
			entry: JournalEntry{
				Unit:       "kubelet.service",
				UnitResult: "failed",
				Message:    "Failed to start kubelet",
			},
			expectedType: SystemdEventServiceFailed,
		},
		{
			name: "service restart",
			entry: JournalEntry{
				Unit:    "kubelet.service",
				Message: "Restarting kubelet",
			},
			expectedType: SystemdEventServiceRestart,
		},
		{
			name: "system boot",
			entry: JournalEntry{
				Message: "System boot completed",
			},
			expectedType: SystemdEventSystemBoot,
		},
		{
			name: "generic journal entry",
			entry: JournalEntry{
				Message: "Some generic message",
			},
			expectedType: SystemdEventJournalEntry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventType := tt.entry.GetEventType()
			assert.Equal(t, tt.expectedType, eventType)
		})
	}
}

func TestCorrelationHints(t *testing.T) {
	entry := JournalEntry{
		PID:          1234,
		Hostname:     "test-node",
		Unit:         "kubelet.service",
		InvocationID: "abc123",
		MachineID:    "machine123",
		BootID:       "boot456",
		CgroupPath:   "/kubepods/burstable/pod123/container456",
	}

	hints := entry.GetCorrelationHints()

	assert.Equal(t, int32(1234), hints.ProcessID)
	assert.Equal(t, "test-node", hints.NodeName)
	assert.Equal(t, "/kubepods/burstable/pod123/container456", hints.CgroupPath)

	// Check correlation tags
	assert.NotNil(t, hints.CorrelationTags)
	assert.Equal(t, "kubelet.service", hints.CorrelationTags["systemd_unit"])
	assert.Equal(t, "abc123", hints.CorrelationTags["systemd_invocation"])
	assert.Equal(t, "machine123", hints.CorrelationTags["machine_id"])
	assert.Equal(t, "boot456", hints.CorrelationTags["boot_id"])
}

func TestSystemdEventDataToCollectorEvent(t *testing.T) {
	now := time.Now()
	entry := &JournalEntry{
		Timestamp: now,
		Message:   "Test message",
		Priority:  int(sdjournal.PriErr),
		Unit:      "test.service",
		PID:       1234,
		Hostname:  "test-host",
	}

	eventData := &SystemdEventData{
		EventType:    SystemdEventServiceStart,
		Source:       "journal",
		JournalEntry: entry,
		UnitName:     "test.service",
		UnitType:     "service",
	}

	collectorEvent := eventData.ToCollectorEvent("test-collector")

	// Verify basic fields
	assert.NotEmpty(t, collectorEvent.EventID)
	assert.Equal(t, "test-collector", collectorEvent.Source)
	assert.NotZero(t, collectorEvent.Timestamp)

	// Verify event data
	assert.NotNil(t, collectorEvent.EventData.RawData)
	assert.Equal(t, "systemd-journal", collectorEvent.EventData.RawData.Format)
	assert.Equal(t, "application/json", collectorEvent.EventData.RawData.ContentType)

	// Verify metadata
	assert.Equal(t, domain.PriorityHigh, collectorEvent.Metadata.Priority)
	assert.Contains(t, collectorEvent.Metadata.Tags, "systemd")
	assert.Contains(t, collectorEvent.Metadata.Tags, "journal")

	// Verify labels
	assert.Equal(t, "test.service", collectorEvent.Metadata.Labels["systemd_unit"])
	assert.Equal(t, "service", collectorEvent.Metadata.Labels["systemd_unit_type"])
	assert.Equal(t, "journal", collectorEvent.Metadata.Labels["systemd_source"])

	// Verify correlation hints
	assert.Equal(t, int32(1234), collectorEvent.CorrelationHints.ProcessID)
	assert.Equal(t, "test-host", collectorEvent.CorrelationHints.NodeName)
}

func TestPriorityMapping(t *testing.T) {
	tests := []struct {
		systemdPriority sdjournal.Priority
		expectedDomain  domain.EventPriority
	}{
		{sdjournal.PriEmerg, domain.PriorityCritical},
		{sdjournal.PriAlert, domain.PriorityCritical},
		{sdjournal.PriCrit, domain.PriorityCritical},
		{sdjournal.PriErr, domain.PriorityHigh},
		{sdjournal.PriWarning, domain.PriorityNormal},
		{sdjournal.PriNotice, domain.PriorityNormal},
		{sdjournal.PriInfo, domain.PriorityLow},
		{sdjournal.PriDebug, domain.PriorityLow},
	}

	for _, tt := range tests {
		t.Run(string(tt.expectedDomain), func(t *testing.T) {
			domainPriority := GetDomainPriority(tt.systemdPriority)
			assert.Equal(t, tt.expectedDomain, domainPriority)
		})
	}
}

func TestContainerIDExtraction(t *testing.T) {
	tests := []struct {
		name       string
		cgroupPath string
		expectedID string
	}{
		{
			name:       "empty path",
			cgroupPath: "",
			expectedID: "",
		},
		{
			name:       "short path",
			cgroupPath: "/short",
			expectedID: "",
		},
		{
			name:       "kubernetes pod path with container ID",
			cgroupPath: "/kubepods/burstable/pod123/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expectedID: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		{
			name:       "path without container ID",
			cgroupPath: "/kubepods/burstable/pod123/short",
			expectedID: "",
		},
		{
			name:       "path with non-hex string",
			cgroupPath: "/kubepods/burstable/pod123/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdeg",
			expectedID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerID := extractContainerIDFromCgroup(tt.cgroupPath)
			assert.Equal(t, tt.expectedID, containerID)
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("contains function", func(t *testing.T) {
		assert.True(t, contains("hello world", "hello"))
		assert.True(t, contains("hello world", "world"))
		assert.True(t, contains("hello world", "lo wo"))
		assert.False(t, contains("hello world", "xyz"))
		assert.True(t, contains("test", "test"))
		assert.False(t, contains("test", "testing"))
	})

	t.Run("isHexString function", func(t *testing.T) {
		assert.True(t, isHexString("123456789abcdef"))
		assert.True(t, isHexString("ABCDEF"))
		assert.True(t, isHexString("0123456789abcdefABCDEF"))
		assert.False(t, isHexString(""))
		assert.False(t, isHexString("xyz"))
		assert.False(t, isHexString("123g"))
	})

	t.Run("splitString function", func(t *testing.T) {
		parts := splitString("/a/b/c", "/")
		expected := []string{"", "a", "b", "c"}
		assert.Equal(t, expected, parts)

		parts = splitString("", "/")
		assert.Equal(t, []string{}, parts)

		parts = splitString("abc", "/")
		assert.Equal(t, []string{"abc"}, parts)
	})

	t.Run("timeToString function", func(t *testing.T) {
		result := timeToString(0)
		assert.Equal(t, "0", result)

		result = timeToString(15) // 15 = F in hex
		assert.Equal(t, "f", result)

		result = timeToString(255) // 255 = FF in hex
		assert.Equal(t, "ff", result)
	})
}

func TestStatistics(t *testing.T) {
	config := TestConfig()
	collector, err := NewCollector("test-stats", config)
	require.NoError(t, err)

	// Test initial statistics
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.EntriesProcessed)
	assert.Equal(t, int64(0), stats.EntriesDropped)
	assert.Equal(t, int64(0), stats.ErrorsTotal)
	assert.False(t, stats.JournalConnected)

	// Modify stats and verify
	collector.stats.EntriesProcessed = 100
	collector.stats.EntriesDropped = 5
	collector.stats.ErrorsTotal = 2
	collector.stats.JournalConnected = true

	stats = collector.Statistics()
	assert.Equal(t, int64(100), stats.EntriesProcessed)
	assert.Equal(t, int64(5), stats.EntriesDropped)
	assert.Equal(t, int64(2), stats.ErrorsTotal)
	assert.True(t, stats.JournalConnected)
}

// Benchmark tests for performance validation

func BenchmarkJournalEntryEventType(b *testing.B) {
	entry := JournalEntry{
		Unit:       "kubelet.service",
		UnitResult: "success",
		Message:    "Started Kubernetes kubelet",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = entry.GetEventType()
	}
}

func BenchmarkCorrelationHints(b *testing.B) {
	entry := JournalEntry{
		PID:          1234,
		Hostname:     "test-node",
		Unit:         "kubelet.service",
		InvocationID: "abc123",
		MachineID:    "machine123",
		BootID:       "boot456",
		CgroupPath:   "/kubepods/burstable/pod123/container456",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = entry.GetCorrelationHints()
	}
}

func BenchmarkContainerIDExtraction(b *testing.B) {
	cgroupPath := "/kubepods/burstable/pod123/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractContainerIDFromCgroup(cgroupPath)
	}
}

func BenchmarkSystemdEventDataToCollectorEvent(b *testing.B) {
	entry := &JournalEntry{
		Timestamp: time.Now(),
		Message:   "Test message",
		Priority:  int(sdjournal.PriErr),
		Unit:      "test.service",
		PID:       1234,
		Hostname:  "test-host",
	}

	eventData := &SystemdEventData{
		EventType:    SystemdEventServiceStart,
		Source:       "journal",
		JournalEntry: entry,
		UnitName:     "test.service",
		UnitType:     "service",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eventData.ToCollectorEvent("test-collector")
	}
}
