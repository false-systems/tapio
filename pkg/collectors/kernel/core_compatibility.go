package kernel

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// KernelVersion represents kernel version information
type KernelVersion struct {
	Major    int
	Minor    int
	Patch    int
	Revision int
	Flavor   string
}

// CoreCompatibility handles CO-RE (Compile Once, Run Everywhere) compatibility
type CoreCompatibility struct {
	kernelVersion KernelVersion
	features      KernelFeatures
	btfPath       string
}

// KernelFeatures represents available kernel features
type KernelFeatures struct {
	HasBTF              bool
	HasRingBuffer       bool
	HasBPFLSM           bool
	HasCgroupV2         bool
	HasTracepoints      bool
	HasKprobes          bool
	HasUprobes          bool
	HasFentry           bool
	HasFexit            bool
	HasCORERelocations  bool
	KernelStructOffsets map[string]int
}

// NewCoreCompatibility creates a new CO-RE compatibility checker
func NewCoreCompatibility() (*CoreCompatibility, error) {
	version, err := getKernelVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get kernel version: %w", err)
	}

	cc := &CoreCompatibility{
		kernelVersion: version,
		btfPath:       findBTFPath(),
	}

	if err := cc.detectFeatures(); err != nil {
		return nil, fmt.Errorf("failed to detect kernel features: %w", err)
	}

	return cc, nil
}

// getKernelVersion parses the kernel version from uname
func getKernelVersion() (KernelVersion, error) {
	var utsname unix.Utsname
	err := unix.Uname(&utsname)
	if err != nil {
		return KernelVersion{}, fmt.Errorf("uname failed: %w", err)
	}

	release := unix.ByteSliceToString(utsname.Release[:])
	return parseKernelVersion(release)
}

// parseKernelVersion parses kernel version string like "5.15.0-72-generic"
func parseKernelVersion(release string) (KernelVersion, error) {
	var version KernelVersion

	// Split on '-' to separate version from flavor
	parts := strings.Split(release, "-")
	versionPart := parts[0]

	if len(parts) > 1 {
		version.Flavor = strings.Join(parts[1:], "-")
	}

	// Parse version numbers
	versionNumbers := strings.Split(versionPart, ".")
	if len(versionNumbers) < 2 {
		return version, fmt.Errorf("invalid version format: %s", release)
	}

	// Parse major.minor.patch
	if _, err := fmt.Sscanf(versionNumbers[0], "%d", &version.Major); err != nil {
		return version, fmt.Errorf("invalid major version: %s", versionNumbers[0])
	}

	if _, err := fmt.Sscanf(versionNumbers[1], "%d", &version.Minor); err != nil {
		return version, fmt.Errorf("invalid minor version: %s", versionNumbers[1])
	}

	if len(versionNumbers) >= 3 {
		if _, err := fmt.Sscanf(versionNumbers[2], "%d", &version.Patch); err != nil {
			// Ignore patch parsing errors for complex version strings
			version.Patch = 0
		}
	}

	return version, nil
}

// findBTFPath finds the BTF (BPF Type Format) information path
func findBTFPath() string {
	// Common BTF locations
	btfPaths := []string{
		"/sys/kernel/btf/vmlinux",
		"/boot/vmlinux-" + getKernelVersionString(),
		"/usr/lib/debug/boot/vmlinux-" + getKernelVersionString(),
		"/lib/modules/" + getKernelVersionString() + "/vmlinux",
	}

	for _, path := range btfPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "" // BTF not found
}

// getKernelVersionString returns kernel version as string
func getKernelVersionString() string {
	var utsname unix.Utsname
	if err := unix.Uname(&utsname); err != nil {
		return "unknown"
	}
	return unix.ByteSliceToString(utsname.Release[:])
}

// detectFeatures detects available kernel features
func (cc *CoreCompatibility) detectFeatures() error {
	features := &cc.features

	// Check BTF availability
	features.HasBTF = cc.btfPath != "" || cc.kernelVersion.isAtLeast(5, 4, 0)

	// Check ring buffer support (introduced in kernel 5.8)
	features.HasRingBuffer = cc.kernelVersion.isAtLeast(5, 8, 0)

	// Check BPF LSM support (introduced in kernel 5.7)
	features.HasBPFLSM = cc.kernelVersion.isAtLeast(5, 7, 0) && cc.hasLSMBPF()

	// Check cgroup v2 support
	features.HasCgroupV2 = cc.hasCgroupV2()

	// Check tracepoint support (available in most kernels)
	features.HasTracepoints = true

	// Check kprobe support (available in most kernels)
	features.HasKprobes = true

	// Check uprobe support (introduced around 3.5)
	features.HasUprobes = cc.kernelVersion.isAtLeast(3, 5, 0)

	// Check fentry/fexit support (introduced in kernel 5.5)
	features.HasFentry = cc.kernelVersion.isAtLeast(5, 5, 0)
	features.HasFexit = cc.kernelVersion.isAtLeast(5, 5, 0)

	// Check CO-RE relocations (requires BTF)
	features.HasCORERelocations = features.HasBTF && cc.kernelVersion.isAtLeast(5, 4, 0)

	// Initialize kernel struct offsets map
	features.KernelStructOffsets = make(map[string]int)

	return nil
}

// isAtLeast checks if kernel version is at least the specified version
func (kv KernelVersion) isAtLeast(major, minor, patch int) bool {
	if kv.Major > major {
		return true
	}
	if kv.Major < major {
		return false
	}
	if kv.Minor > minor {
		return true
	}
	if kv.Minor < minor {
		return false
	}
	return kv.Patch >= patch
}

// hasLSMBPF checks if BPF LSM is enabled
func (cc *CoreCompatibility) hasLSMBPF() bool {
	// Check /sys/kernel/security/lsm for "bpf"
	data, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil {
		return false
	}

	lsms := strings.TrimSpace(string(data))
	return strings.Contains(lsms, "bpf")
}

// hasCgroupV2 checks if cgroup v2 is available
func (cc *CoreCompatibility) hasCgroupV2() bool {
	// Check if /sys/fs/cgroup has cgroup v2 magic number
	var statfs unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &statfs); err != nil {
		return false
	}

	// CGROUP2_SUPER_MAGIC = 0x63677270
	return statfs.Type == 0x63677270
}

// GetCompatiblePrograms returns which eBPF programs are compatible
func (cc *CoreCompatibility) GetCompatiblePrograms() []string {
	var programs []string

	// Always available basic programs
	if cc.features.HasKprobes {
		programs = append(programs, "kprobe_tcp_connect", "kprobe_tcp_close")
	}

	if cc.features.HasTracepoints {
		programs = append(programs, "tracepoint_kmem", "tracepoint_sched")
	}

	// Advanced features
	if cc.features.HasFentry && cc.features.HasCORERelocations {
		programs = append(programs, "fentry_tcp_connect", "fentry_memory_alloc")
	}

	if cc.features.HasBPFLSM {
		programs = append(programs, "lsm_file_open", "lsm_task_setuid")
	}

	return programs
}

// GenerateVMLinuxHeader generates a minimal vmlinux.h for CO-RE
func (cc *CoreCompatibility) GenerateVMLinuxHeader() (string, error) {
	if !cc.features.HasBTF || cc.btfPath == "" {
		return cc.generateFallbackHeader(), nil
	}

	// For production, we would use BTF information to generate
	// the exact structs for this kernel version
	// For now, return a basic header
	return cc.generateFallbackHeader(), nil
}

// generateFallbackHeader generates a fallback header without BTF
func (cc *CoreCompatibility) generateFallbackHeader() string {
	return `// Generated minimal vmlinux.h for kernel ` + cc.kernelVersion.String() + `
#ifndef __VMLINUX_H__
#define __VMLINUX_H__

// Basic types
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;

typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

// Task struct minimal definition
struct task_struct {
    volatile long int state;
    void *stack;
    int pid;
    int tgid;
    char comm[16];
} __attribute__((preserve_access_index));

// Socket structures
struct sock_common {
    unsigned short skc_family;
    unsigned short skc_num;
    unsigned short skc_dport;
    unsigned int skc_rcv_saddr;
    unsigned int skc_daddr;
} __attribute__((preserve_access_index));

struct sock {
    struct sock_common __sk_common;
} __attribute__((preserve_access_index));

#endif /* __VMLINUX_H__ */`
}

// String returns kernel version as string
func (kv KernelVersion) String() string {
	base := fmt.Sprintf("%d.%d.%d", kv.Major, kv.Minor, kv.Patch)
	if kv.Revision > 0 {
		base += fmt.Sprintf(".%d", kv.Revision)
	}
	if kv.Flavor != "" {
		base += "-" + kv.Flavor
	}
	return base
}

// IsCompatible checks if the given feature is compatible with current kernel
func (cc *CoreCompatibility) IsCompatible(feature string) bool {
	switch feature {
	case "ring_buffer":
		return cc.features.HasRingBuffer
	case "bpf_lsm":
		return cc.features.HasBPFLSM
	case "cgroup_v2":
		return cc.features.HasCgroupV2
	case "fentry":
		return cc.features.HasFentry
	case "fexit":
		return cc.features.HasFexit
	case "core_relocations":
		return cc.features.HasCORERelocations
	case "btf":
		return cc.features.HasBTF
	default:
		return false
	}
}

// GetFallbackStrategy returns fallback strategy for unsupported features
func (cc *CoreCompatibility) GetFallbackStrategy(feature string) string {
	switch feature {
	case "ring_buffer":
		if !cc.features.HasRingBuffer {
			return "use_perf_buffer"
		}
	case "fentry":
		if !cc.features.HasFentry {
			return "use_kprobe"
		}
	case "bpf_lsm":
		if !cc.features.HasBPFLSM {
			return "use_syscall_tracing"
		}
	case "core_relocations":
		if !cc.features.HasCORERelocations {
			return "use_hardcoded_offsets"
		}
	}
	return "not_supported"
}

// GetKernelVersion returns the detected kernel version
func (cc *CoreCompatibility) GetKernelVersion() KernelVersion {
	return cc.kernelVersion
}

// GetFeatures returns detected kernel features
func (cc *CoreCompatibility) GetFeatures() KernelFeatures {
	return cc.features
}

// ValidateEBPFProgram validates if an eBPF program can run on this kernel
func (cc *CoreCompatibility) ValidateEBPFProgram(programType string) (bool, string) {
	switch programType {
	case "kprobe":
		if !cc.features.HasKprobes {
			return false, "kprobes not supported"
		}
	case "tracepoint":
		if !cc.features.HasTracepoints {
			return false, "tracepoints not supported"
		}
	case "fentry", "fexit":
		if !cc.features.HasFentry {
			return false, "fentry/fexit requires kernel 5.5+"
		}
	case "lsm":
		if !cc.features.HasBPFLSM {
			return false, "BPF LSM not enabled or requires kernel 5.7+"
		}
	case "ring_buffer":
		if !cc.features.HasRingBuffer {
			return false, "ring buffer requires kernel 5.8+, falling back to perf buffer"
		}
	}

	return true, "compatible"
}
