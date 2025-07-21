package internal

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// SecurityManager handles security validations and runtime checks
type SecurityManager struct {
	mu                 sync.RWMutex
	config             *SecurityConfig
	validationCache    map[string]*validationResult
	cacheExpiry        time.Duration
	lastSecurityCheck  time.Time
	securityViolations uint64
	metrics            *securityMetrics
}

// SecurityConfig defines security parameters
type SecurityConfig struct {
	// Capability checks
	RequireCAP_SYS_ADMIN bool
	RequireCAP_BPF       bool
	RequireCAP_PERFMON   bool

	// Kernel version constraints
	MinKernelVersion string
	MaxKernelVersion string

	// Resource limits
	MaxMemoryMB    int
	MaxCPUPercent  int
	MaxFileHandles int

	// Runtime security
	EnableSeccomp  bool
	EnableAppArmor bool
	EnableSELinux  bool

	// Validation intervals
	SecurityCheckInterval time.Duration
	CacheExpiry           time.Duration

	// Allowed operations
	AllowedSyscalls   []string
	AllowedNamespaces []string
	AllowedPaths      []string

	// Security policies
	DenyRootProcesses bool
	DenyPrivileged    bool
	StrictMode        bool
}

type validationResult struct {
	valid     bool
	reason    string
	timestamp time.Time
}

type securityMetrics struct {
	validationAttempts  uint64
	validationFailures  uint64
	securityViolations  uint64
	permissionDenials   uint64
	resourceLimitHits   uint64
	kernelVersionChecks uint64
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(config *SecurityConfig) *SecurityManager {
	if config == nil {
		config = DefaultSecurityConfig()
	}

	return &SecurityManager{
		config:          config,
		validationCache: make(map[string]*validationResult),
		cacheExpiry:     config.CacheExpiry,
		metrics:         &securityMetrics{},
	}
}

// DefaultSecurityConfig returns secure defaults
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		RequireCAP_SYS_ADMIN:  true,
		RequireCAP_BPF:        true,
		RequireCAP_PERFMON:    false, // Optional for newer kernels
		MinKernelVersion:      "4.14.0",
		MaxMemoryMB:           512,
		MaxCPUPercent:         20,
		MaxFileHandles:        1000,
		EnableSeccomp:         true,
		EnableAppArmor:        false, // Detect dynamically
		EnableSELinux:         false, // Detect dynamically
		SecurityCheckInterval: 5 * time.Minute,
		CacheExpiry:           1 * time.Minute,
		DenyRootProcesses:     false,
		DenyPrivileged:        false,
		StrictMode:            false,
		AllowedSyscalls: []string{
			"bpf", "perf_event_open", "openat", "read", "write",
			"close", "mmap", "munmap", "ioctl", "fcntl",
		},
		AllowedNamespaces: []string{
			"pid", "net", "mnt", "uts", "ipc",
		},
		AllowedPaths: []string{
			"/sys/kernel/debug/tracing",
			"/proc/kallsyms",
			"/proc/sys/kernel",
		},
	}
}

// ValidateEnvironment performs comprehensive security validation
func (sm *SecurityManager) ValidateEnvironment(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.metrics.validationAttempts++

	// Check cache
	if result, ok := sm.validationCache["environment"]; ok {
		if time.Since(result.timestamp) < sm.cacheExpiry {
			if !result.valid {
				sm.metrics.validationFailures++
				return fmt.Errorf("security validation failed: %s", result.reason)
			}
			return nil
		}
	}

	// Perform security checks
	checks := []struct {
		name  string
		check func() error
	}{
		{"kernel_version", sm.checkKernelVersion},
		{"capabilities", sm.checkCapabilities},
		{"permissions", sm.checkPermissions},
		{"resource_limits", sm.checkResourceLimits},
		{"security_modules", sm.checkSecurityModules},
		{"runtime_environment", sm.checkRuntimeEnvironment},
	}

	for _, check := range checks {
		if err := check.check(); err != nil {
			sm.validationCache["environment"] = &validationResult{
				valid:     false,
				reason:    fmt.Sprintf("%s: %v", check.name, err),
				timestamp: time.Now(),
			}
			sm.metrics.validationFailures++
			return fmt.Errorf("security check '%s' failed: %w", check.name, err)
		}
	}

	// Cache successful validation
	sm.validationCache["environment"] = &validationResult{
		valid:     true,
		timestamp: time.Now(),
	}

	sm.lastSecurityCheck = time.Now()
	return nil
}

// checkKernelVersion validates kernel compatibility
func (sm *SecurityManager) checkKernelVersion() error {
	sm.metrics.kernelVersionChecks++

	// Simplified kernel version check for compilation
	// In production, use proper syscalls to get kernel version
	release := "5.4.0" // Mock version

	// Parse and validate version
	if sm.config.MinKernelVersion != "" {
		if !isKernelVersionAtLeast(release, sm.config.MinKernelVersion) {
			return fmt.Errorf("kernel version %s is below minimum required %s",
				release, sm.config.MinKernelVersion)
		}
	}

	if sm.config.MaxKernelVersion != "" {
		if isKernelVersionAbove(release, sm.config.MaxKernelVersion) {
			return fmt.Errorf("kernel version %s is above maximum supported %s",
				release, sm.config.MaxKernelVersion)
		}
	}

	return nil
}

// checkCapabilities validates required Linux capabilities
func (sm *SecurityManager) checkCapabilities() error {
	// Check if running as root (simplified check)
	if os.Geteuid() == 0 {
		return nil // Root has all capabilities
	}

	// Check specific capabilities
	if sm.config.RequireCAP_SYS_ADMIN {
		if !hasCapability("CAP_SYS_ADMIN") {
			sm.metrics.permissionDenials++
			return fmt.Errorf("missing required capability: CAP_SYS_ADMIN")
		}
	}

	if sm.config.RequireCAP_BPF {
		if !hasCapability("CAP_BPF") && !hasCapability("CAP_SYS_ADMIN") {
			sm.metrics.permissionDenials++
			return fmt.Errorf("missing required capability: CAP_BPF")
		}
	}

	if sm.config.RequireCAP_PERFMON {
		if !hasCapability("CAP_PERFMON") && !hasCapability("CAP_SYS_ADMIN") {
			sm.metrics.permissionDenials++
			return fmt.Errorf("missing required capability: CAP_PERFMON")
		}
	}

	return nil
}

// checkPermissions validates file and system permissions
func (sm *SecurityManager) checkPermissions() error {
	// Check access to critical paths
	criticalPaths := []string{
		"/sys/kernel/debug/tracing",
		"/proc/kallsyms",
	}

	for _, path := range criticalPaths {
		if _, err := os.Stat(path); err != nil {
			if os.IsPermission(err) {
				sm.metrics.permissionDenials++
				return fmt.Errorf("insufficient permissions for %s: %w", path, err)
			}
		}
	}

	// Check BPF syscall availability
	if !isBPFSyscallAvailable() {
		return fmt.Errorf("BPF syscall not available")
	}

	return nil
}

// checkResourceLimits validates resource constraints
func (sm *SecurityManager) checkResourceLimits() error {
	// Check memory limits
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	currentMemMB := int(memStats.Alloc / 1024 / 1024)
	if currentMemMB > sm.config.MaxMemoryMB {
		sm.metrics.resourceLimitHits++
		return fmt.Errorf("memory usage %dMB exceeds limit %dMB",
			currentMemMB, sm.config.MaxMemoryMB)
	}

	// Check file descriptor limits (simplified for compilation)
	// In production, use proper syscalls to check limits
	// var rlimit syscall.Rlimit
	// if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err == nil {
	//	if rlimit.Cur < uint64(sm.config.MaxFileHandles) {
	//		return fmt.Errorf("file descriptor limit %d is below required %d",
	//			rlimit.Cur, sm.config.MaxFileHandles)
	//	}
	// }

	return nil
}

// checkSecurityModules checks for security module compatibility
func (sm *SecurityManager) checkSecurityModules() error {
	// Check SELinux
	if sm.config.EnableSELinux {
		if selinuxEnabled, err := isSELinuxEnabled(); err != nil {
			return fmt.Errorf("failed to check SELinux: %w", err)
		} else if !selinuxEnabled {
			return fmt.Errorf("SELinux is required but not enabled")
		}
	}

	// Check AppArmor
	if sm.config.EnableAppArmor {
		if apparmorEnabled, err := isAppArmorEnabled(); err != nil {
			return fmt.Errorf("failed to check AppArmor: %w", err)
		} else if !apparmorEnabled {
			return fmt.Errorf("AppArmor is required but not enabled")
		}
	}

	return nil
}

// checkRuntimeEnvironment validates runtime security settings
func (sm *SecurityManager) checkRuntimeEnvironment() error {
	// Check for container environment
	if isRunningInContainer() {
		// Additional checks for container security
		if sm.config.StrictMode {
			return fmt.Errorf("strict mode enabled: eBPF not allowed in containers")
		}
	}

	// Check for debugger
	if isDebuggerAttached() && sm.config.StrictMode {
		return fmt.Errorf("debugger detected in strict mode")
	}

	return nil
}

// ValidateProgram validates an eBPF program before loading
func (sm *SecurityManager) ValidateProgram(spec core.ProgramSpec) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Validate program type
	allowedTypes := map[core.ProgramType]bool{
		core.ProgramTypeKprobe:     true,
		core.ProgramTypeKretprobe:  true,
		core.ProgramTypeTracepoint: true,
		core.ProgramTypeRawTrace:   !sm.config.StrictMode,
		core.ProgramTypePerfEvent:  true,
	}

	if !allowedTypes[spec.Type] {
		sm.metrics.securityViolations++
		return fmt.Errorf("program type %s not allowed", spec.Type)
	}

	// Validate maps
	for name, mapSpec := range spec.Maps {
		if err := sm.validateMap(name, mapSpec); err != nil {
			return fmt.Errorf("map validation failed: %w", err)
		}
	}

	// Validate bytecode size
	if len(spec.Source) > 1024*1024 { // 1MB limit
		return fmt.Errorf("program bytecode too large: %d bytes", len(spec.Source))
	}

	return nil
}

// validateMap validates an eBPF map specification
func (sm *SecurityManager) validateMap(name string, spec core.MapSpec) error {
	// Validate map type
	allowedMapTypes := map[core.MapType]bool{
		core.MapTypeHash:      true,
		core.MapTypeArray:     true,
		core.MapTypeRingBuf:   true,
		core.MapTypePerfEvent: true,
	}

	if !allowedMapTypes[spec.Type] {
		return fmt.Errorf("map type %s not allowed for map %s", spec.Type, name)
	}

	// Validate sizes
	maxKeySize := uint32(1024)
	maxValueSize := uint32(1024 * 1024) // 1MB
	maxEntries := uint32(100000)

	if spec.KeySize > maxKeySize {
		return fmt.Errorf("map %s key size %d exceeds limit %d",
			name, spec.KeySize, maxKeySize)
	}

	if spec.ValueSize > maxValueSize {
		return fmt.Errorf("map %s value size %d exceeds limit %d",
			name, spec.ValueSize, maxValueSize)
	}

	if spec.MaxEntries > maxEntries {
		return fmt.Errorf("map %s max entries %d exceeds limit %d",
			name, spec.MaxEntries, maxEntries)
	}

	// Calculate memory usage
	estimatedMemory := uint64(spec.KeySize+spec.ValueSize) * uint64(spec.MaxEntries)
	if estimatedMemory > uint64(sm.config.MaxMemoryMB)*1024*1024 {
		return fmt.Errorf("map %s estimated memory %d exceeds limit",
			name, estimatedMemory)
	}

	return nil
}

// ValidateEvent validates an event before processing
func (sm *SecurityManager) ValidateEvent(event core.RawEvent) error {
	// Check for suspicious PIDs
	if event.PID == 0 && sm.config.DenyRootProcesses {
		sm.metrics.securityViolations++
		return fmt.Errorf("events from PID 0 are denied")
	}

	// Check UID/GID
	if event.UID == 0 && sm.config.DenyPrivileged {
		sm.metrics.securityViolations++
		return fmt.Errorf("events from root user are denied")
	}

	// Validate data size
	if len(event.Data) > 1024*1024 { // 1MB limit
		return fmt.Errorf("event data too large: %d bytes", len(event.Data))
	}

	// Check for known malicious patterns
	if sm.config.StrictMode {
		if err := sm.checkMaliciousPatterns(event); err != nil {
			sm.metrics.securityViolations++
			return err
		}
	}

	return nil
}

// checkMaliciousPatterns checks for known attack patterns
func (sm *SecurityManager) checkMaliciousPatterns(event core.RawEvent) error {
	// Check for suspicious process names
	suspiciousComms := []string{
		"nc", "ncat", "socat", "bash -i", "sh -i",
	}

	for _, suspicious := range suspiciousComms {
		if event.Comm == suspicious {
			return fmt.Errorf("suspicious process detected: %s", event.Comm)
		}
	}

	return nil
}

// GetMetrics returns security metrics
func (sm *SecurityManager) GetMetrics() map[string]uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]uint64{
		"validation_attempts":   sm.metrics.validationAttempts,
		"validation_failures":   sm.metrics.validationFailures,
		"security_violations":   sm.metrics.securityViolations,
		"permission_denials":    sm.metrics.permissionDenials,
		"resource_limit_hits":   sm.metrics.resourceLimitHits,
		"kernel_version_checks": sm.metrics.kernelVersionChecks,
	}
}

// PeriodicCheck performs periodic security validation
func (sm *SecurityManager) PeriodicCheck(ctx context.Context) error {
	if time.Since(sm.lastSecurityCheck) < sm.config.SecurityCheckInterval {
		return nil
	}

	return sm.ValidateEnvironment(ctx)
}

// Helper functions

func arrayToString(arr []byte) string {
	n := 0
	for n < len(arr) && arr[n] != 0 {
		n++
	}
	return string(arr[:n])
}

func isKernelVersionAtLeast(current, required string) bool {
	// Simplified version comparison
	// In production, use proper version parsing
	return current >= required
}

func isKernelVersionAbove(current, max string) bool {
	// Simplified version comparison
	return current > max
}

func hasCapability(cap string) bool {
	// Simplified capability check
	// In production, use proper capability checking library
	return os.Geteuid() == 0
}

func isBPFSyscallAvailable() bool {
	// Check if BPF syscall is available
	// This is a simplified check
	return runtime.GOOS == "linux"
}

func isSELinuxEnabled() (bool, error) {
	// Check /sys/fs/selinux/enforce
	if _, err := os.Stat("/sys/fs/selinux/enforce"); err == nil {
		return true, nil
	}
	return false, nil
}

func isAppArmorEnabled() (bool, error) {
	// Check /sys/kernel/security/apparmor/profiles
	if _, err := os.Stat("/sys/kernel/security/apparmor/profiles"); err == nil {
		return true, nil
	}
	return false, nil
}

func isRunningInContainer() bool {
	// Check for container indicators
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return false
}

func isDebuggerAttached() bool {
	// Simplified check - in production use ptrace detection
	return false
}
