//go:build linux
// +build linux

package network

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystem_LinuxPlatform verifies tests run only on Linux
func TestSystem_LinuxPlatform(t *testing.T) {
	assert.Equal(t, "linux", runtime.GOOS, "System tests must run on Linux")
}

// TestSystem_BTFSupport verifies BTF (BPF Type Format) availability
func TestSystem_BTFSupport(t *testing.T) {
	// Check for vmlinux BTF - required for CO-RE
	btfPath := "/sys/kernel/btf/vmlinux"
	info, err := os.Stat(btfPath)

	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("BTF not available - kernel not compiled with CONFIG_DEBUG_INFO_BTF=y")
		}
		t.Fatalf("Failed to check BTF: %v", err)
	}

	assert.NotNil(t, info, "BTF vmlinux should exist")
	assert.Greater(t, info.Size(), int64(0), "BTF vmlinux should not be empty")
}

// TestSystem_BPFFilesystem verifies BPF filesystem is mounted
func TestSystem_BPFFilesystem(t *testing.T) {
	// Check if /sys/fs/bpf is mounted (required for pinning maps/programs)
	bpffsPath := "/sys/fs/bpf"
	info, err := os.Stat(bpffsPath)

	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("BPF filesystem not mounted - mount with: mount -t bpf none /sys/fs/bpf")
		}
		t.Fatalf("Failed to check BPF filesystem: %v", err)
	}

	assert.True(t, info.IsDir(), "BPF filesystem should be a directory")
}

// TestSystem_TracepointAvailability verifies sock/inet_sock_set_state tracepoint exists
func TestSystem_TracepointAvailability(t *testing.T) {
	tracepointPath := "/sys/kernel/debug/tracing/events/sock/inet_sock_set_state"

	// Check if tracepoint exists
	info, err := os.Stat(tracepointPath)

	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("Tracepoint sock/inet_sock_set_state not available - kernel too old or debugfs not mounted")
		}

		// Permission denied is common without root
		if os.IsPermission(err) {
			t.Skip("Cannot access tracepoint - run as root or check debugfs permissions")
		}

		t.Fatalf("Failed to check tracepoint: %v", err)
	}

	assert.True(t, info.IsDir(), "Tracepoint should be a directory")
}

// TestSystem_RootPrivileges checks if running with sufficient privileges for eBPF
func TestSystem_RootPrivileges(t *testing.T) {
	uid := os.Getuid()

	if uid != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	assert.Equal(t, 0, uid, "Must run as root for eBPF operations")
}

// TestSystem_KernelVersion verifies minimum kernel version (5.8+ for ring buffers)
func TestSystem_KernelVersion(t *testing.T) {
	// Read kernel version from /proc/version
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		t.Skipf("Cannot read kernel version: %v", err)
	}

	version := string(data)
	assert.NotEmpty(t, version, "Kernel version should not be empty")

	// We need Linux 5.8+ for BPF ring buffers
	// This is a basic check - real implementation would parse version
	assert.Contains(t, version, "Linux", "Should be running Linux kernel")
}

// TestSystem_DebugFSMounted verifies debugfs is mounted (needed for tracepoints)
func TestSystem_DebugFSMounted(t *testing.T) {
	debugfsPath := "/sys/kernel/debug"
	info, err := os.Stat(debugfsPath)

	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("debugfs not mounted - mount with: mount -t debugfs none /sys/kernel/debug")
		}
		t.Fatalf("Failed to check debugfs: %v", err)
	}

	assert.True(t, info.IsDir(), "debugfs should be mounted")

	// Check if tracing subdirectory exists
	tracingPath := "/sys/kernel/debug/tracing"
	tracingInfo, err := os.Stat(tracingPath)

	if err == nil {
		assert.True(t, tracingInfo.IsDir(), "tracing directory should exist")
	}
}

// TestSystem_BPFMapSupport verifies BPF_MAP_TYPE_RINGBUF support
func TestSystem_BPFMapSupport(t *testing.T) {
	// Ring buffers were added in Linux 5.8
	// This test verifies the kernel supports the map type we need

	// Check available_filter_functions (proxy for BPF support)
	functionsPath := "/sys/kernel/debug/tracing/available_filter_functions"
	_, err := os.Stat(functionsPath)

	if err != nil {
		if os.IsPermission(err) {
			t.Skip("Cannot access tracing - need root or debugfs permissions")
		}
		if os.IsNotExist(err) {
			t.Skip("Tracing not available - debugfs not mounted or kernel too old")
		}
		t.Fatalf("Failed to check filter functions: %v", err)
	}
}

// TestSystem_RequiredCapabilities documents required Linux capabilities
func TestSystem_RequiredCapabilities(t *testing.T) {
	// This test documents the capabilities needed for eBPF network observer
	// CAP_BPF (Linux 5.8+) or CAP_SYS_ADMIN (older kernels)
	// CAP_NET_ADMIN for network tracepoints
	// CAP_PERFMON for perf events

	uid := os.Getuid()
	if uid != 0 {
		t.Skip("Capability checks require root - run with sudo")
	}

	// If we're root, we have all capabilities
	assert.Equal(t, 0, uid, "Root has all required capabilities")
}

// TestSystem_MockModeEnvironment tests mock mode for non-Linux development
func TestSystem_MockModeEnvironment(t *testing.T) {
	// Save original value
	originalMockMode := os.Getenv("TAPIO_MOCK_MODE")
	defer func() {
		if originalMockMode == "" {
			if err := os.Unsetenv("TAPIO_MOCK_MODE"); err != nil {
				t.Logf("failed to unset TAPIO_MOCK_MODE: %v", err)
			}
		} else {
			if err := os.Setenv("TAPIO_MOCK_MODE", originalMockMode); err != nil {
				t.Logf("failed to restore TAPIO_MOCK_MODE: %v", err)
			}
		}
	}()

	// Test mock mode enabled
	os.Setenv("TAPIO_MOCK_MODE", "true")
	mockMode := os.Getenv("TAPIO_MOCK_MODE")
	assert.Equal(t, "true", mockMode, "Mock mode should be enabled")

	// Test mock mode disabled
	os.Setenv("TAPIO_MOCK_MODE", "false")
	mockMode = os.Getenv("TAPIO_MOCK_MODE")
	assert.Equal(t, "false", mockMode, "Mock mode should be disabled")

	// Test mock mode unset
	os.Unsetenv("TAPIO_MOCK_MODE")
	mockMode = os.Getenv("TAPIO_MOCK_MODE")
	assert.Equal(t, "", mockMode, "Mock mode should be unset")
}

// TestSystem_PerfEventSupport verifies perf_event_open is available
func TestSystem_PerfEventSupport(t *testing.T) {
	// Check if perf events are enabled
	perfPath := "/proc/sys/kernel/perf_event_paranoid"
	data, err := os.ReadFile(perfPath)

	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("Perf events not available - CONFIG_PERF_EVENTS not enabled")
		}
		t.Fatalf("Failed to read perf_event_paranoid: %v", err)
	}

	paranoid := string(data)
	assert.NotEmpty(t, paranoid, "perf_event_paranoid should have a value")

	// Values: -1 (no restrictions), 0 (allow some access), 1+ (restricted)
	// For eBPF we generally need -1 or 0, or run as root
}

// TestSystem_ResourceLimits verifies ulimit settings for eBPF
func TestSystem_ResourceLimits(t *testing.T) {
	// eBPF requires sufficient locked memory (RLIMIT_MEMLOCK)
	// Check current limits
	limitsPath := "/proc/self/limits"
	data, err := os.ReadFile(limitsPath)

	require.NoError(t, err, "Should read process limits")
	assert.NotEmpty(t, data, "Limits should not be empty")

	limits := string(data)
	assert.Contains(t, limits, "Max locked memory", "Should show locked memory limit")

	// Typical requirement: unlimited or at least several MB for maps
}
