package runtimesignals

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeSignalDecoding tests the signal and exit code decoding functions
func TestRuntimeSignalDecoding(t *testing.T) {
	t.Run("SignalNameMapping", func(t *testing.T) {
		testCases := []struct {
			signum int
			name   string
		}{
			{SIGTERM, "SIGTERM"},
			{SIGKILL, "SIGKILL"},
			{SIGINT, "SIGINT"},
			{SIGQUIT, "SIGQUIT"},
			{SIGABRT, "SIGABRT"},
			{SIGSEGV, "SIGSEGV"},
			{SIGBUS, "SIGBUS"},
			{999, "UNKNOWN"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.name, GetSignalName(tc.signum))
			})
		}
	})

	t.Run("SignalDescription", func(t *testing.T) {
		testCases := []struct {
			signum int
			desc   string
		}{
			{SIGTERM, "Termination request"},
			{SIGKILL, "Kill (cannot be caught or ignored)"},
			{SIGINT, "Interrupt from keyboard (Ctrl+C)"},
			{SIGSEGV, "Segmentation fault (invalid memory reference)"},
		}

		for _, tc := range testCases {
			desc := GetSignalDescription(tc.signum)
			assert.Equal(t, tc.desc, desc)
		}
	})

	t.Run("SignalFatality", func(t *testing.T) {
		assert.True(t, IsSignalFatal(SIGKILL))
		assert.True(t, IsSignalFatal(SIGTERM))
		assert.True(t, IsSignalFatal(SIGSEGV))
		assert.False(t, IsSignalFatal(999)) // Unknown signal
	})

	t.Run("ExitCodeDecoding", func(t *testing.T) {
		testCases := []struct {
			name     string
			exitCode uint32
			expected ExitInfo
		}{
			{
				name:     "SuccessfulExit",
				exitCode: 0,
				expected: ExitInfo{
					Code:        0,
					Signal:      0,
					CoreDumped:  false,
					Description: "Successful exit",
				},
			},
			{
				name:     "ErrorExitCode1",
				exitCode: 1 << 8,
				expected: ExitInfo{
					Code:        1,
					Signal:      0,
					CoreDumped:  false,
					Description: "Exited with code 1",
				},
			},
			{
				name:     "ExitCode137_SIGKILL",
				exitCode: 9, // Killed by signal 9 (SIGKILL)
				expected: ExitInfo{
					Code:        9,
					Signal:      9,
					CoreDumped:  false,
					Description: "Terminated by SIGKILL",
				},
			},
			{
				name:     "SegfaultWithCore",
				exitCode: SIGSEGV | 0x80,
				expected: ExitInfo{
					Code:        SIGSEGV | 0x80,
					Signal:      SIGSEGV,
					CoreDumped:  true,
					Description: "Terminated by SIGSEGV (core dumped)",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				info := DecodeExitCode(tc.exitCode)
				assert.Equal(t, tc.expected.Code, info.Code)
				assert.Equal(t, tc.expected.Signal, info.Signal)
				assert.Equal(t, tc.expected.CoreDumped, info.CoreDumped)
				assert.Equal(t, tc.expected.Description, info.Description)
			})
		}
	})
}

// TestRuntimeEventProcessing tests runtime event processing
func TestRuntimeEventProcessing(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-runtime")
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test processRuntimeEvent (through createEvent)
	t.Run("ProcessExecEvent", func(t *testing.T) {
		data := map[string]string{
			"event_type": "process_exec",
			"pid":        "1234",
			"tgid":       "1234",
			"ppid":       "1",
			"command":    "test-process",
			"uid":        "1000",
			"gid":        "1000",
		}

		event := collector.createEvent("process_exec", data)
		assert.NotNil(t, event)
		assert.Equal(t, "test-runtime", event.Source)
		// Process events use EventTypeKernelProcess
		assert.NotNil(t, event.EventData.Process)
		assert.Equal(t, int32(1234), event.EventData.Process.PID)
	})

	t.Run("ProcessExitEvent", func(t *testing.T) {
		data := map[string]string{
			"event_type":       "process_exit",
			"pid":              "5678",
			"tgid":             "5678",
			"exit_code":        "137",
			"exit_signal":      "9",
			"exit_description": "Terminated by SIGKILL",
		}

		event := collector.createEvent("process_exit", data)
		assert.NotNil(t, event)
		assert.Equal(t, "test-runtime", event.Source)
		assert.NotNil(t, event.EventData.Process)
		assert.Equal(t, int32(5678), event.EventData.Process.PID)
	})

	t.Run("SignalEvent", func(t *testing.T) {
		data := map[string]string{
			"event_type":         "signal_sent",
			"pid":                "9999",
			"signal_number":      "15",
			"signal_name":        "SIGTERM",
			"signal_description": "Termination request",
			"sender_pid":         "1",
		}

		event := collector.createEvent("signal_sent", data)
		assert.NotNil(t, event)
		// Signal events use EventTypeKernelProcess
		assert.Equal(t, "runtime", event.Metadata.Tags[0])
	})

	t.Run("OOMKillEvent", func(t *testing.T) {
		data := map[string]string{
			"event_type": "oom_kill",
			"pid":        "7777",
			"command":    "memory-hog",
		}

		event := collector.createEvent("oom_kill", data)
		assert.NotNil(t, event)
		// OOM events use EventTypeContainerOOM
	})
}

// TestRuntimeCollectorLifecycle tests the collector lifecycle
func TestRuntimeCollectorLifecycle(t *testing.T) {
	setupOTELForTesting(t)

	t.Run("StartStop", func(t *testing.T) {
		collector, err := NewCollector("test-lifecycle")
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)
		assert.True(t, collector.IsHealthy())

		err = collector.Stop()
		require.NoError(t, err)
		assert.False(t, collector.IsHealthy())
	})

	t.Run("EventChannel", func(t *testing.T) {
		collector, err := NewCollector("test-channel")
		require.NoError(t, err)

		events := collector.Events()
		assert.NotNil(t, events)

		// Channel should be buffered
		assert.Equal(t, 10000, cap(events))
	})

	t.Run("Name", func(t *testing.T) {
		collector, err := NewCollector("test-name")
		require.NoError(t, err)
		assert.Equal(t, "test-name", collector.Name())
	})
}

// TestRuntimeMetrics tests runtime-specific metrics
func TestRuntimeMetrics(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-runtime-metrics")
	require.NoError(t, err)

	// Verify all runtime metrics are initialized
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.errorsTotal)
	assert.NotNil(t, collector.processingTime)
	assert.NotNil(t, collector.droppedEvents)
	assert.NotNil(t, collector.bufferUsage)

	// Runtime-specific metrics (though still named namespace for backward compatibility)
	assert.NotNil(t, collector.ebpfLoadsTotal)
	assert.NotNil(t, collector.ebpfLoadErrors)
	assert.NotNil(t, collector.ebpfAttachTotal)
	assert.NotNil(t, collector.ebpfAttachErrors)
	assert.NotNil(t, collector.collectorHealth)
}

// TestRuntimeEventTypes tests the runtime event type constants
func TestRuntimeEventTypes(t *testing.T) {
	// Verify event type constants are defined
	assert.Equal(t, uint32(1), EventTypeProcessExec)
	assert.Equal(t, uint32(2), EventTypeProcessExit)
	assert.Equal(t, uint32(3), EventTypeSignalGenerate)
	assert.Equal(t, uint32(4), EventTypeSignalDeliver)
	assert.Equal(t, uint32(5), EventTypeOOMKill)
	assert.Equal(t, uint32(6), EventTypeCoreDump)
}

// TestRuntimeSignalConstants tests signal constants
func TestRuntimeSignalConstants(t *testing.T) {
	// Verify signal constants match Linux values
	assert.Equal(t, 15, SIGTERM)
	assert.Equal(t, 9, SIGKILL)
	assert.Equal(t, 2, SIGINT)
	assert.Equal(t, 3, SIGQUIT)
	assert.Equal(t, 6, SIGABRT)
	assert.Equal(t, 11, SIGSEGV)
	assert.Equal(t, 7, SIGBUS)
}

// TestCollectorStats tests the CollectorStats structure
func TestCollectorStats(t *testing.T) {
	stats := &CollectorStats{
		EventsGenerated:  100,
		EventsDropped:    5,
		LastEventTime:    1234567890,
		ProcessExecs:     20,
		ProcessExits:     18,
		SignalsDelivered: 50,
		SignalsGenerated: 45,
		OOMKills:         2,
	}

	assert.Equal(t, uint64(100), stats.EventsGenerated)
	assert.Equal(t, uint64(5), stats.EventsDropped)
	assert.Equal(t, uint64(1234567890), stats.LastEventTime)
	assert.Equal(t, uint64(20), stats.ProcessExecs)
	assert.Equal(t, uint64(18), stats.ProcessExits)
	assert.Equal(t, uint64(50), stats.SignalsDelivered)
	assert.Equal(t, uint64(45), stats.SignalsGenerated)
	assert.Equal(t, uint64(2), stats.OOMKills)
}

// TestPodInfoExtraction tests Kubernetes pod info extraction
func TestPodInfoExtraction(t *testing.T) {
	collector, err := NewCollector("test-k8s")
	require.NoError(t, err)

	t.Run("CNIFormat", func(t *testing.T) {
		podInfo := collector.parseK8sFromNetns("/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000")
		require.NotNil(t, podInfo)
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", podInfo.PodUID)
	})

	t.Run("KubepodsFormat", func(t *testing.T) {
		// Kubepods format with underscores
		podInfo := collector.parseK8sFromNetns("/sys/fs/cgroup/kubepods/pod550e8400_e29b_41d4_a716_446655440000/container")
		require.NotNil(t, podInfo)
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", podInfo.PodUID)
	})

	t.Run("InvalidPath", func(t *testing.T) {
		podInfo := collector.parseK8sFromNetns("/some/random/path")
		assert.Nil(t, podInfo)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		podInfo := collector.parseK8sFromNetns("")
		assert.Nil(t, podInfo)
	})
}

// TestRuntimeSignalEventStructure tests the RuntimeSignalEvent structure
func TestRuntimeSignalEventStructure(t *testing.T) {
	event := &RuntimeSignalEvent{
		Timestamp: 1234567890,
		EventType: "process_exit",
		PID:       1234,
		TGID:      1234,
		PPID:      1,
		Command:   "test-process",
		UID:       1000,
		GID:       1000,
		ExitInfo: &ExitInfo{
			Code:        137,
			Signal:      9,
			CoreDumped:  false,
			Description: "Terminated by SIGKILL",
		},
		SignalInfo: &SignalInfo{
			Number:      9,
			Name:        "SIGKILL",
			Description: "Kill (cannot be caught or ignored)",
			IsFatal:     true,
		},
		SenderPID: 1,
		IsOOMKill: true,
	}

	assert.Equal(t, uint64(1234567890), event.Timestamp)
	assert.Equal(t, "process_exit", event.EventType)
	assert.Equal(t, uint32(1234), event.PID)
	assert.NotNil(t, event.ExitInfo)
	assert.NotNil(t, event.SignalInfo)
	assert.True(t, event.IsOOMKill)
}
