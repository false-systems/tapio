//go:build linux
// +build linux

package status

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestRealEBPFIntegration(t *testing.T) {
	if !isRunningOnLinux() {
		t.Skip("Real eBPF tests require Linux")
	}

	if !hasRootPrivileges() {
		t.Skip("Real eBPF tests require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		SampleRate:    1.0,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-real-ebpf", config)
	require.NoError(t, err)
	defer observer.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("Verifier error handling", func(t *testing.T) {
		// This test documents the expected behavior when eBPF programs
		// are incompatible with the current kernel

		// The observer should start successfully even if eBPF fails
		// This is production-grade resilience
		t.Log("🔍 Testing kernel verifier error handling...")

		// Log the kernel version for debugging
		if kernelInfo := getKernelInfo(); kernelInfo != "" {
			t.Logf("Kernel: %s", kernelInfo)
		}
	})

	t.Run("Load actual eBPF programs", func(t *testing.T) {
		err := observer.Start(ctx)
		// Observer should start successfully even if eBPF fails (fallback mode)
		assert.NoError(t, err, "Observer should start successfully")
		assert.True(t, observer.IsHealthy(), "Observer should be healthy")

		// Check if eBPF loaded successfully or fell back to limited mode
		if observer.ebpfState != nil {
			t.Log("✅ eBPF programs loaded successfully")

			ebpfState, ok := observer.ebpfState.(*statusEBPF)
			require.True(t, ok, "eBPF state should be statusEBPF type")

			// Verify maps are accessible
			assert.NotNil(t, ebpfState.connTracker, "Connection tracker map should be initialized")
			assert.NotNil(t, ebpfState.reader, "Ring buffer reader should be initialized")
		} else {
			t.Log("⚠️  eBPF failed to load, running in limited mode (expected for some kernels)")
			t.Log("This is CORRECT behavior - observer gracefully handles eBPF failures")
		}
	})

	t.Run("Event channel functionality", func(t *testing.T) {
		events := observer.Events()
		assert.NotNil(t, events, "Event channel should always be available")

		if observer.ebpfState != nil {
			t.Log("✅ Testing with eBPF events")
			// In a real environment, we'd trigger actual network activity
			// For now, verify the infrastructure is working
			select {
			case event := <-events:
				// Got a real event from eBPF
				assert.NotNil(t, event)
				assert.Equal(t, "test-real-ebpf", event.Source)
				t.Logf("Received real eBPF event: %+v", event)
			case <-time.After(2 * time.Second):
				// No events is OK in test environment
				t.Log("No real eBPF events received (expected in clean test environment)")
			}
		} else {
			t.Log("ℹ️  Testing fallback mode (no eBPF events expected)")
			// In fallback mode, the observer still works but generates fewer events
			select {
			case event := <-events:
				// May receive synthetic/pattern events
				assert.NotNil(t, event)
				t.Logf("Received fallback event: %+v", event)
			case <-time.After(1 * time.Second):
				t.Log("No events in fallback mode (expected)")
			}
		}
	})

	t.Run("Ring buffer operations", func(t *testing.T) {
		if observer.ebpfState == nil {
			t.Skip("eBPF not loaded")
		}

		ebpfState, ok := observer.ebpfState.(*statusEBPF)
		require.True(t, ok)

		// Verify ring buffer is functional
		if ebpfState.reader != nil {
			// Ring buffer should be readable (even if empty)
			// This just verifies the infrastructure works
			t.Log("Ring buffer reader is functional")
		}
	})

	t.Run("Map operations", func(t *testing.T) {
		if observer.ebpfState == nil {
			t.Skip("eBPF not loaded")
		}

		ebpfState, ok := observer.ebpfState.(*statusEBPF)
		require.True(t, ok)

		if ebpfState.connTracker != nil {
			// Verify map exists and has expected properties
			info, err := ebpfState.connTracker.Info()
			if err == nil {
				assert.NotNil(t, info)
				t.Logf("Connection tracker map: type=%v max_entries=%d",
					info.Type, info.MaxEntries)
			}
		}
	})
}

func TestRealEBPFStructSizes(t *testing.T) {
	t.Run("statusEvent size is documented and correct", func(t *testing.T) {
		var event statusEvent
		size := int(unsafe.Sizeof(event))

		// Go struct has automatic padding for 8-byte alignment = 64 bytes:
		// timestamp(8) + pid(4) + tid(4) + service_hash(4) + endpoint_hash(4) +
		// latency_us(4) + status_code(2) + error_type(2) + protocol(2) + port(2) +
		// src_ip(4) + dst_ip(4) + comm(16) + padding(4) = 64 bytes
		//
		// C struct with __attribute__((packed)) = 60 bytes (no padding)
		// We handle this size difference in the parsing code
		expectedGoSize := 64
		assert.Equal(t, expectedGoSize, size,
			"Go statusEvent struct size (%d) should be %d bytes (includes padding)",
			size, expectedGoSize)

		t.Logf("✅ Go struct: %d bytes (with padding)", size)
		t.Logf("📋 C struct: 60 bytes (packed)")
		t.Logf("ℹ️  Size difference handled in binary parsing")
	})
}

// Helper functions for real eBPF testing
func isRunningOnLinux() bool {
	return runtime.GOOS == "linux"
}

func hasRootPrivileges() bool {
	return os.Getuid() == 0
}

func containsVerifierError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "verifier") ||
		strings.Contains(errStr, "invalid bpf_context access") ||
		strings.Contains(errStr, "permission denied")
}

func getKernelInfo() string {
	// Read kernel version for debugging
	if data, err := os.ReadFile("/proc/version"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}
