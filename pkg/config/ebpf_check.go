package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// EBPFCapabilityCheck provides comprehensive eBPF capability detection
type EBPFCapabilityCheck struct {
	Available       bool           `json:"available"`
	Reason          string         `json:"reason"`
	Kernel          KernelInfo     `json:"kernel"`
	Permissions     PermissionInfo `json:"permissions"`
	Features        FeatureSupport `json:"features"`
	Recommendations []string       `json:"recommendations"`
	FixCommands     []string       `json:"fix_commands"`
}

// KernelInfo contains kernel-related information
type KernelInfo struct {
	Version     string `json:"version"`
	VersionCode int    `json:"version_code"`
	MinRequired int    `json:"min_required"`
	Supported   bool   `json:"supported"`
	ConfigPath  string `json:"config_path"`
	HasBPFFS    bool   `json:"has_bpf_fs"`
}

// PermissionInfo contains permission-related information
type PermissionInfo struct {
	RunningAsRoot  bool     `json:"running_as_root"`
	UID            int      `json:"uid"`
	GID            int      `json:"gid"`
	Capabilities   []string `json:"capabilities"`
	HasCapBPF      bool     `json:"has_cap_bpf"`
	HasCapPerfmon  bool     `json:"has_cap_perfmon"`
	HasCapSysAdmin bool     `json:"has_cap_sys_admin"`
}

// FeatureSupport contains eBPF feature support information
type FeatureSupport struct {
	Maps       bool `json:"maps"`
	Programs   bool `json:"programs"`
	Tracing    bool `json:"tracing"`
	Networking bool `json:"networking"`
	LSM        bool `json:"lsm"`
	BTF        bool `json:"btf"`
}

// CheckEBPFCapabilities performs a comprehensive eBPF capability check
func CheckEBPFCapabilities() *EBPFCapabilityCheck {
	check := &EBPFCapabilityCheck{
		Available:       false,
		Recommendations: make([]string, 0),
		FixCommands:     make([]string, 0),
	}

	// Check operating system
	if runtime.GOOS != "linux" {
		check.Reason = fmt.Sprintf("eBPF is only supported on Linux (running on %s)", runtime.GOOS)
		check.Recommendations = append(check.Recommendations, "Use a Linux system to enable eBPF features")
		return check
	}

	// Check kernel version and configuration
	check.Kernel = checkKernelSupport()
	if !check.Kernel.Supported {
		check.Reason = fmt.Sprintf("Kernel version %s is too old (minimum required: 4.15)", check.Kernel.Version)
		check.Recommendations = append(check.Recommendations, "Upgrade to Linux kernel 4.15 or newer")
		check.FixCommands = append(check.FixCommands, "sudo apt update && sudo apt upgrade linux-generic")
		return check
	}

	// Check permissions
	check.Permissions = checkPermissions()
	if !check.Permissions.RunningAsRoot && !check.Permissions.HasCapBPF {
		check.Reason = "Insufficient permissions for eBPF operations"
		check.Recommendations = append(check.Recommendations,
			"Run as root or grant CAP_BPF capability",
			"Alternative: Use 'sudo' for eBPF features")
		check.FixCommands = append(check.FixCommands,
			"sudo setcap cap_bpf,cap_perfmon+ep $(which tapio)",
			"# Or run with sudo: sudo tapio sniff")
		return check
	}

	// Check eBPF filesystem
	if !check.Kernel.HasBPFFS {
		check.Reason = "BPF filesystem not mounted"
		check.Recommendations = append(check.Recommendations, "Mount BPF filesystem")
		check.FixCommands = append(check.FixCommands,
			"sudo mount -t bpf bpf /sys/fs/bpf",
			"# To make permanent: echo 'bpf /sys/fs/bpf bpf defaults 0 0' | sudo tee -a /etc/fstab")
	}

	// Check feature support
	check.Features = checkFeatureSupport()

	// Determine overall availability
	if check.Kernel.Supported && (check.Permissions.RunningAsRoot || check.Permissions.HasCapBPF) {
		check.Available = true
		check.Reason = "eBPF is available and supported"
		if !check.Kernel.HasBPFFS {
			check.Recommendations = append(check.Recommendations, "Mount BPF filesystem for optimal performance")
		}
	}

	// Add general recommendations
	if check.Available {
		check.Recommendations = append(check.Recommendations,
			"eBPF features are available",
			"Consider enabling sampling to reduce overhead",
			"Monitor memory usage when using eBPF programs")
	}

	return check
}

// checkKernelSupport checks if the kernel supports eBPF
func checkKernelSupport() KernelInfo {
	info := KernelInfo{
		MinRequired: 415, // Kernel 4.15
	}

	// Get kernel version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		versionStr := string(data)
		info.Version = extractKernelVersion(versionStr)
		info.VersionCode = parseKernelVersion(info.Version)
		info.Supported = info.VersionCode >= info.MinRequired
	}

	// Check for BPF filesystem
	if _, err := os.Stat("/sys/fs/bpf"); err == nil {
		info.HasBPFFS = true
	}

	// Check kernel config if available
	configPaths := []string{
		"/proc/config.gz",
		"/boot/config-" + info.Version,
		"/boot/config",
	}

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			info.ConfigPath = path
			break
		}
	}

	return info
}

// checkPermissions checks current user permissions for eBPF
func checkPermissions() PermissionInfo {
	info := PermissionInfo{
		UID:           os.Getuid(),
		GID:           os.Getgid(),
		RunningAsRoot: os.Getuid() == 0,
	}

	// Check capabilities if not root
	if !info.RunningAsRoot {
		caps := getCurrentCapabilities()
		info.Capabilities = caps
		info.HasCapBPF = contains(caps, "cap_bpf")
		info.HasCapPerfmon = contains(caps, "cap_perfmon")
		info.HasCapSysAdmin = contains(caps, "cap_sys_admin")
	} else {
		// Root has all capabilities
		info.HasCapBPF = true
		info.HasCapPerfmon = true
		info.HasCapSysAdmin = true
	}

	return info
}

// checkFeatureSupport checks what eBPF features are supported
func checkFeatureSupport() FeatureSupport {
	support := FeatureSupport{}

	// Basic checks - these would need actual eBPF program loading to be accurate
	// For now, we make educated guesses based on kernel version and permissions

	// Check if bpftool is available
	if _, err := exec.LookPath("bpftool"); err == nil {
		// Use bpftool to check feature support
		if output, err := exec.Command("bpftool", "feature").Output(); err == nil {
			features := string(output)
			support.Maps = strings.Contains(features, "eBPF map_type")
			support.Programs = strings.Contains(features, "eBPF program_type")
			support.Tracing = strings.Contains(features, "kprobe")
			support.Networking = strings.Contains(features, "socket")
		}
	} else {
		// Fallback to basic checks
		support.Maps = true       // Basic map support is widely available
		support.Programs = true   // Basic program support is widely available
		support.Tracing = true    // Tracing is usually available
		support.Networking = true // Networking eBPF is common
	}

	// Check for BTF support
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err == nil {
		support.BTF = true
	}

	return support
}

// extractKernelVersion extracts kernel version from /proc/version
func extractKernelVersion(versionStr string) string {
	// Example: "Linux version 5.4.0-74-generic (buildd@lgw01-amd64-038) ..."
	parts := strings.Fields(versionStr)
	if len(parts) >= 3 {
		return parts[2]
	}
	return "unknown"
}

// parseKernelVersion converts a version string like "5.4.0" to numeric code
func parseKernelVersion(version string) int {
	// Remove any suffixes like "-generic"
	if idx := strings.Index(version, "-"); idx > 0 {
		version = version[:idx]
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0
	}

	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])

	if err1 != nil || err2 != nil {
		return 0
	}

	// Convert to comparable integer (e.g., 5.4 -> 504)
	return major*100 + minor
}

// getCurrentCapabilities gets the current process capabilities
func getCurrentCapabilities() []string {
	var caps []string

	// Read capabilities from /proc/self/status
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return caps
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "CapEff:") {
			// Parse capability mask
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				capMask := parts[1]
				caps = parseCapabilityMask(capMask)
			}
			break
		}
	}

	return caps
}

// parseCapabilityMask converts a capability mask to capability names
func parseCapabilityMask(mask string) []string {
	var caps []string

	// This is a simplified version - real implementation would need
	// to parse the hex mask and map to capability names
	// For now, we'll use a basic check

	if mask != "0000000000000000" {
		// Has some capabilities
		caps = append(caps, "some_capabilities")
	}

	return caps
}

// GetEBPFRecommendations returns recommendations for enabling eBPF
func GetEBPFRecommendations() []string {
	check := CheckEBPFCapabilities()

	if check.Available {
		return []string{
			"eBPF is available and ready to use",
			"Enable eBPF features in configuration: features.enable_ebpf = true",
			"Consider adjusting sampling rate based on your needs",
		}
	}

	return check.Recommendations
}

// GetEBPFFixCommands returns commands to fix eBPF issues
func GetEBPFFixCommands() []string {
	check := CheckEBPFCapabilities()
	return check.FixCommands
}

// IsEBPFAvailable returns a simple boolean check
func IsEBPFAvailable() bool {
	check := CheckEBPFCapabilities()
	return check.Available
}

// GetEBPFStatus returns a human-readable status string
func GetEBPFStatus() string {
	check := CheckEBPFCapabilities()

	if check.Available {
		return "eBPF is available and supported"
	}

	return fmt.Sprintf("eBPF not available: %s", check.Reason)
}
