package ebpf

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// AvailabilityChecker provides detailed eBPF availability checks
type AvailabilityChecker struct {
	mu           sync.RWMutex
	cached       bool
	cachedResult *AvailabilityResult
}

// AvailabilityResult contains detailed information about eBPF availability
type AvailabilityResult struct {
	Available       bool
	Reason          string
	Details         []string
	Recommendations []string
	KernelVersion   string
	HasBTF          bool
	HasPermissions  bool
	IsContainer     bool
}

var availabilityChecker = &AvailabilityChecker{}

// CheckAvailability performs comprehensive eBPF availability checks
func CheckAvailability() *AvailabilityResult {
	availabilityChecker.mu.Lock()
	defer availabilityChecker.mu.Unlock()

	// Return cached result if available
	if availabilityChecker.cached {
		return availabilityChecker.cachedResult
	}

	result := &AvailabilityResult{
		Details:         make([]string, 0),
		Recommendations: make([]string, 0),
	}

	// Check OS
	if runtime.GOOS != "linux" {
		result.Available = false
		result.Reason = fmt.Sprintf("eBPF is only supported on Linux, current OS: %s", runtime.GOOS)
		result.Recommendations = append(result.Recommendations,
			"eBPF monitoring is not available on non-Linux systems",
			"The application will continue without eBPF-based insights")
		availabilityChecker.cachedResult = result
		availabilityChecker.cached = true
		return result
	}

	// Check kernel version
	kernelVersion, err := getKernelVersion()
	if err != nil {
		result.Details = append(result.Details, fmt.Sprintf("Failed to check kernel version: %v", err))
	} else {
		result.KernelVersion = kernelVersion
		result.Details = append(result.Details, fmt.Sprintf("Kernel version: %s", kernelVersion))

		// Check if kernel version is sufficient (4.15+)
		if !isKernelVersionSufficient(kernelVersion) {
			result.Available = false
			result.Reason = "Kernel version too old for eBPF"
			result.Details = append(result.Details, "eBPF requires Linux kernel 4.15 or newer")
			result.Recommendations = append(result.Recommendations,
				"Upgrade to Linux kernel 4.15 or newer for eBPF support",
				"The application will continue with reduced monitoring capabilities")
			availabilityChecker.cachedResult = result
			availabilityChecker.cached = true
			return result
		}
	}

	// Check if running in container
	result.IsContainer = isRunningInContainer()
	if result.IsContainer {
		result.Details = append(result.Details, "Running in container environment")
	}

	// Check permissions
	result.HasPermissions = checkPermissions()
	if !result.HasPermissions {
		result.Available = false
		result.Reason = "Insufficient permissions for eBPF"
		result.Details = append(result.Details, "eBPF requires root or CAP_BPF capability")

		if result.IsContainer {
			result.Recommendations = append(result.Recommendations,
				"Add --privileged flag when running the container",
				"Or add specific capabilities: --cap-add=CAP_BPF,CAP_PERFMON,CAP_NET_ADMIN,CAP_SYS_RESOURCE",
				"Example: docker run --cap-add=CAP_BPF,CAP_PERFMON your-image")
		} else {
			result.Recommendations = append(result.Recommendations,
				"Run as root: sudo tapio",
				"Or grant CAP_BPF capability: sudo setcap cap_bpf=eip /path/to/tapio",
				"The application will continue without eBPF-based monitoring")
		}
		availabilityChecker.cachedResult = result
		availabilityChecker.cached = true
		return result
	}

	// Check BTF availability
	result.HasBTF = checkBTFAvailability()
	if result.HasBTF {
		result.Details = append(result.Details, "BTF (BPF Type Format) is available")
	} else {
		result.Details = append(result.Details, "BTF not available, CO-RE features limited")
	}

	// Check if BPF syscall is available
	if !checkBPFSyscall() {
		result.Available = false
		result.Reason = "BPF syscall not available"
		result.Details = append(result.Details, "The BPF syscall is not available on this system")
		result.Recommendations = append(result.Recommendations,
			"Check if BPF is disabled in kernel config",
			"Check if running in a restricted environment (some container runtimes block BPF)")
		availabilityChecker.cachedResult = result
		availabilityChecker.cached = true
		return result
	}

	// All checks passed
	result.Available = true
	result.Reason = "All eBPF requirements met"
	result.Details = append(result.Details, "eBPF monitoring is fully available")

	availabilityChecker.cachedResult = result
	availabilityChecker.cached = true
	return result
}

// getKernelVersion retrieves the kernel version
func getKernelVersion() (string, error) {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// isKernelVersionSufficient checks if kernel version is 4.15+
func isKernelVersionSufficient(version string) bool {
	// Extract major and minor version
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}

	// Require at least 4.15
	if major > 4 {
		return true
	}
	if major == 4 && minor >= 15 {
		return true
	}
	return false
}

// checkPermissions checks if we have necessary permissions
func checkPermissions() bool {
	// Check if running as root
	if os.Geteuid() == 0 {
		return true
	}

	// TODO: Check for CAP_BPF capability
	// For now, only root is supported
	return false
}

// checkBTFAvailability checks if BTF is available
func checkBTFAvailability() bool {
	_, err := os.Stat("/sys/kernel/btf/vmlinux")
	return err == nil
}

// checkBPFSyscall checks if BPF syscall is available
func checkBPFSyscall() bool {
	// TODO: Actually test BPF syscall
	// For now, assume it's available on Linux
	return true
}

// isRunningInContainer detects if running in a container
func isRunningInContainer() bool {
	// Check for .dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check cgroup
	data, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		if strings.Contains(string(data), "docker") ||
			strings.Contains(string(data), "kubepods") ||
			strings.Contains(string(data), "containerd") {
			return true
		}
	}

	return false
}

// GetAvailabilityStatus returns a user-friendly status message
func GetAvailabilityStatus() string {
	result := CheckAvailability()
	if result.Available {
		return "eBPF monitoring is available and ready"
	}
	return fmt.Sprintf("eBPF not available: %s", result.Reason)
}

// GetDetailedStatus returns detailed availability information
func GetDetailedStatus() map[string]interface{} {
	result := CheckAvailability()
	return map[string]interface{}{
		"available":       result.Available,
		"reason":          result.Reason,
		"kernel_version":  result.KernelVersion,
		"has_btf":         result.HasBTF,
		"has_permissions": result.HasPermissions,
		"is_container":    result.IsContainer,
		"details":         result.Details,
		"recommendations": result.Recommendations,
	}
}
