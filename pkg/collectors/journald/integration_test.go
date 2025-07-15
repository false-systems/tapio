package journald

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// TestJournaldCollector tests the complete journald collector
func TestJournaldCollector(t *testing.T) {
	config := collectors.CollectorConfig{
		Name:            "journald-test",
		Type:            "journald",
		Enabled:         true,
		EventBufferSize: 1000,
		Extra: map[string]interface{}{
			"priorities": []string{"0", "1", "2", "3", "4"},
		},
	}

	collector, err := NewCollector(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	// Test lifecycle
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Check health
	health := collector.Health()
	assert.NotNil(t, health)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestOOMDetection tests OOM kill detection
func TestOOMDetection(t *testing.T) {
	detector := NewOOMDetector()

	tests := []struct {
		name      string
		entries   []JournalEntry
		expectOOM bool
		victim    string
		pid       int
	}{
		{
			name: "kernel_oom_kill",
			entries: []JournalEntry{
				{
					Message:           "invoked oom-killer: gfp_mask=0x100cca(GFP_HIGHUSER_MOVABLE), order=0, oom_score_adj=0",
					Priority:          3,
					SyslogIdentifier:  "kernel",
					RealtimeTimestamp: 1000000000,
				},
				{
					Message:           "Out of memory: Killed process 1234 (java) total-vm:4000000kB, anon-rss:3500000kB",
					Priority:          3,
					SyslogIdentifier:  "kernel",
					RealtimeTimestamp: 1000000100,
				},
			},
			expectOOM: true,
			victim:    "java",
			pid:       1234,
		},
		{
			name: "cgroup_oom",
			entries: []JournalEntry{
				{
					Message:           "Memory cgroup out of memory: Killed process 5678 (python3) total-vm:2000000kB",
					Priority:          3,
					SyslogIdentifier:  "kernel",
					RealtimeTimestamp: 2000000000,
				},
			},
			expectOOM: true,
			victim:    "python3",
			pid:       5678,
		},
		{
			name: "not_oom",
			entries: []JournalEntry{
				{
					Message:           "Normal log message without OOM",
					Priority:          6,
					SyslogIdentifier:  "systemd",
					RealtimeTimestamp: 3000000000,
				},
			},
			expectOOM: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector.Reset()

			var event *unified.Event
			for _, entry := range tt.entries {
				e := entry // Create copy
				event = detector.Detect(&e)
			}

			if tt.expectOOM {
				require.NotNil(t, event, "Expected OOM event")
				assert.Equal(t, unified.EventTypeOOM, event.Type)
				assert.Equal(t, unified.SeverityCritical, event.Severity)
				assert.Equal(t, tt.victim, event.Data["victim_name"])
				assert.Equal(t, tt.pid, event.Data["victim_pid"])
			} else {
				assert.Nil(t, event, "Expected no OOM event")
			}
		})
	}
}

// TestContainerEventParsing tests container failure detection
func TestContainerEventParsing(t *testing.T) {
	parser := NewContainerEventParser()

	tests := []struct {
		name        string
		entry       JournalEntry
		expectEvent bool
		failureType string
	}{
		{
			name: "docker_pull_failure",
			entry: JournalEntry{
				Message:          "Error response from daemon: pull access denied for nginx:latest",
				SystemdUnit:      "docker.service",
				SyslogIdentifier: "dockerd",
				Priority:         3,
			},
			expectEvent: true,
			failureType: "pull_failure",
		},
		{
			name: "container_start_failure",
			entry: JournalEntry{
				Message:          "Error: failed to start container 1234abcd: OCI runtime create failed",
				SystemdUnit:      "containerd.service",
				SyslogIdentifier: "containerd",
				Priority:         3,
			},
			expectEvent: true,
			failureType: "start_failure",
		},
		{
			name: "normal_container_log",
			entry: JournalEntry{
				Message:          "Container 1234 started successfully",
				SystemdUnit:      "docker.service",
				SyslogIdentifier: "dockerd",
				Priority:         6,
			},
			expectEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := parser.Parse(&tt.entry)

			if tt.expectEvent {
				require.NotNil(t, event, "Expected container event")
				assert.Equal(t, "container_failure", event.Type)
				assert.Equal(t, tt.failureType, event.Data["failure_type"])
			} else {
				assert.Nil(t, event, "Expected no container event")
			}
		})
	}
}

// TestSmartFilter tests noise filtering
func TestSmartFilter(t *testing.T) {
	config := &JournaldConfig{
		FilterNoisyUnits: true,
	}
	filter := NewSmartFilter(config)

	tests := []struct {
		name   string
		entry  JournalEntry
		expect bool // Should process
	}{
		{
			name: "critical_kernel_message",
			entry: JournalEntry{
				Message:          "kernel panic - not syncing: Out of memory",
				Priority:         0,
				SyslogIdentifier: "kernel",
			},
			expect: true,
		},
		{
			name: "systemd_session_noise",
			entry: JournalEntry{
				Message:          "Started Session 123 of user ubuntu",
				Priority:         6,
				SyslogIdentifier: "systemd",
				SystemdUnit:      "systemd-logind.service",
			},
			expect: false,
		},
		{
			name: "important_kubelet_error",
			entry: JournalEntry{
				Message:          "Failed to start container: image not found",
				Priority:         3,
				SyslogIdentifier: "kubelet",
				SystemdUnit:      "kubelet.service",
			},
			expect: true,
		},
		{
			name: "cron_noise",
			entry: JournalEntry{
				Message:          "CRON[1234]: (root) CMD (/usr/bin/update)",
				Priority:         6,
				SyslogIdentifier: "CRON",
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.ShouldProcess(&tt.entry)
			assert.Equal(t, tt.expect, result)
		})
	}

	// Check filter efficiency
	stats := filter.GetStatistics()
	t.Logf("Filter statistics: %+v", stats)
}

// TestSemanticEnrichment tests event enrichment
func TestSemanticEnrichment(t *testing.T) {
	enricher := NewSemanticEnricher()

	entry := &JournalEntry{
		Message:          "Container docker-1234abcd failed to start: OCI runtime error",
		SystemdUnit:      "docker-1234abcd.scope",
		SyslogIdentifier: "dockerd",
		Priority:         3,
		PID:              5678,
		UID:              0,
		Hostname:         "node-1",
		MachineID:        "machine123",
		BootID:           "boot456",
	}

	event := &unified.Event{
		Type:     "container_failure",
		Category: unified.CategoryReliability,
		Severity: unified.SeverityError,
		Data: map[string]interface{}{
			"message": entry.Message,
		},
		Attributes: make(map[string]interface{}),
		Labels:     make(map[string]string),
		Context:    &unified.EventContext{},
	}

	enricher.Enrich(event, entry)

	// Check enrichments
	assert.Equal(t, "docker-1234abcd.scope", event.Attributes["systemd_unit"])
	assert.Equal(t, "container_runtime", event.Attributes["service_type"])
	assert.Equal(t, "root", event.Attributes["user_type"])
	assert.NotEmpty(t, event.Attributes["fingerprint"])
	assert.Contains(t, event.Labels, "container")
}

// TestParserPatterns tests critical event patterns
func TestParserPatterns(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		message     string
		expectParse bool
		severity    unified.Severity
		category    unified.Category
	}{
		{
			name:        "service_crash",
			message:     "Main process exited, code=killed, status=9/KILL",
			expectParse: true,
			severity:    unified.SeverityError,
			category:    unified.CategoryReliability,
		},
		{
			name:        "disk_full",
			message:     "No space left on device",
			expectParse: true,
			severity:    unified.SeverityCritical,
			category:    unified.CategoryMemory,
		},
		{
			name:        "network_timeout",
			message:     "Connection timeout after 30s: context deadline exceeded",
			expectParse: true,
			severity:    unified.SeverityWarning,
			category:    unified.CategorySystem,
		},
		{
			name:        "normal_info",
			message:     "Service started successfully",
			expectParse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &JournalEntry{
				Message:  tt.message,
				Priority: 3,
			}

			event := parser.ParseCritical(entry)

			if tt.expectParse {
				require.NotNil(t, event, "Expected event to be parsed")
				assert.Equal(t, tt.severity, event.Severity)
				assert.Equal(t, tt.category, event.Category)
			} else {
				assert.Nil(t, event, "Expected no event")
			}
		})
	}
}

// BenchmarkOOMDetection benchmarks OOM detection performance
func BenchmarkOOMDetection(b *testing.B) {
	detector := NewOOMDetector()
	entry := &JournalEntry{
		Message:           "Out of memory: Killed process 1234 (java) total-vm:4000000kB",
		Priority:          3,
		SyslogIdentifier:  "kernel",
		RealtimeTimestamp: 1000000000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.Detect(entry)
		detector.Reset()
	}
}

// BenchmarkSmartFilter benchmarks filtering performance
func BenchmarkSmartFilter(b *testing.B) {
	filter := NewSmartFilter(&JournaldConfig{})
	entry := &JournalEntry{
		Message:          "Normal system message",
		Priority:         6,
		SyslogIdentifier: "systemd",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter.ShouldProcess(entry)
	}
}
