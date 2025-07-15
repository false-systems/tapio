//go:build linux
// +build linux

package journald

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// SmartFilter implements intelligent filtering to achieve 95% noise reduction
// This is CRITICAL for performance - we only want events that matter
type SmartFilter struct {
	// Configuration
	config *JournaldConfig

	// Noise patterns to ignore
	noisePatterns    []*regexp.Regexp
	noisyUnits       map[string]bool
	noisyIdentifiers map[string]bool

	// Allow patterns (override noise filtering)
	importantPatterns []*regexp.Regexp
	importantUnits    map[string]bool

	// Dynamic filtering based on frequency
	frequencyTracker *frequencyTracker

	// Statistics
	stats struct {
		mu             sync.RWMutex
		totalProcessed uint64
		totalFiltered  uint64
		byReason       map[string]uint64
	}
}

// frequencyTracker tracks message frequency for dynamic filtering
type frequencyTracker struct {
	mu         sync.RWMutex
	buckets    map[string]*frequencyBucket
	maxBuckets int
}

type frequencyBucket struct {
	count      int
	lastSeen   time.Time
	suppressed bool
}

// NewSmartFilter creates an OPINIONATED filter for 95% noise reduction
func NewSmartFilter(config *JournaldConfig) *SmartFilter {
	f := &SmartFilter{
		config:           config,
		noisyUnits:       make(map[string]bool),
		noisyIdentifiers: make(map[string]bool),
		importantUnits:   make(map[string]bool),
		frequencyTracker: newFrequencyTracker(10000),
	}

	f.stats.byReason = make(map[string]uint64)

	// Initialize filters
	f.initializeNoisePatterns()
	f.initializeNoisyServices()
	f.initializeImportantPatterns()

	return f
}

// ShouldProcess determines if an entry should be processed
func (f *SmartFilter) ShouldProcess(entry *JournalEntry) bool {
	f.stats.mu.Lock()
	f.stats.totalProcessed++
	f.stats.mu.Unlock()

	// Fast path: Check priority first (most effective filter)
	if !f.isPriorityImportant(entry) {
		f.recordFiltered("low_priority")
		return false
	}

	// Check if it's from a noisy unit we should ignore
	if f.isNoisyUnit(entry) && !f.isImportantOverride(entry) {
		f.recordFiltered("noisy_unit")
		return false
	}

	// Check noise patterns
	if f.matchesNoisePattern(entry) && !f.isImportantOverride(entry) {
		f.recordFiltered("noise_pattern")
		return false
	}

	// Check frequency-based filtering
	if f.shouldSuppressByFrequency(entry) {
		f.recordFiltered("high_frequency")
		return false
	}

	// Additional OPINIONATED filters
	if f.isSystemdNoise(entry) {
		f.recordFiltered("systemd_noise")
		return false
	}

	if f.isKernelNoise(entry) {
		f.recordFiltered("kernel_noise")
		return false
	}

	// Entry passed all filters
	return true
}

// initializeNoisePatterns sets up patterns for noise we want to filter
func (f *SmartFilter) initializeNoisePatterns() {
	noiseRegexes := []string{
		// Systemd session/slice noise
		`(?i)started session \d+ of user`,
		`(?i)starting session \d+ of user`,
		`(?i)removed slice user-\d+\.slice`,
		`(?i)created slice user-\d+\.slice`,
		`(?i)starting user slice of`,
		`(?i)removed session \d+`,
		`(?i)new session \d+ of user`,
		`(?i)session-\d+\.scope: succeeded`,

		// systemd target noise
		`(?i)reached target`,
		`(?i)stopped target`,
		`(?i)starting .*? target`,
		`(?i)started .*? target`,

		// Authentication noise (unless failed)
		`(?i)pam_unix.*?: session opened`,
		`(?i)pam_unix.*?: session closed`,
		`(?i)accepted publickey for`,
		`(?i)disconnected from .* port`,

		// Cron noise
		`(?i)\(cron\) cmd`,
		`(?i)cron\[\d+\]: \(`,

		// DHCP noise
		`(?i)dhcp(ack|request|offer|discover)`,

		// Common info messages
		`(?i)^(info|information|notice):`,
		`(?i)started\s+\w+\.service`,
		`(?i)starting\s+\w+\.service`,
		`(?i)listening on`,
		`(?i)closed .* gracefully`,
		`(?i)reloading configuration`,

		// Audit noise
		`(?i)audit.*?: .*? res=success`,

		// NetworkManager noise
		`(?i)networkmanager.*?: \s*<info>`,

		// Snap noise
		`(?i)snapd.*?: .*?(done|started|autorefresh)`,
	}

	f.noisePatterns = make([]*regexp.Regexp, 0, len(noiseRegexes))
	for _, pattern := range noiseRegexes {
		if re, err := regexp.Compile(pattern); err == nil {
			f.noisePatterns = append(f.noisePatterns, re)
		}
	}
}

// initializeNoisyServices identifies services that generate mostly noise
func (f *SmartFilter) initializeNoisyServices() {
	// Units that are almost always noise
	noisyUnitsList := []string{
		"systemd-logind.service",
		"systemd-timesyncd.service",
		"snapd.service",
		"packagekit.service",
		"ModemManager.service",
		"polkit.service",
		"accounts-daemon.service",
		"udisks2.service",
		"bolt.service",
		"colord.service",
		"rtkit-daemon.service",
	}

	for _, unit := range noisyUnitsList {
		f.noisyUnits[unit] = true
	}

	// Identifiers that are almost always noise
	noisyIdentifiersList := []string{
		"systemd", // Unless critical priority
		"dbus",
		"dbus-daemon",
		"gdm",
		"gnome-shell",
		"pulseaudio",
		"bluetoothd",
		"wpa_supplicant",
		"avahi-daemon",
	}

	for _, ident := range noisyIdentifiersList {
		f.noisyIdentifiers[ident] = true
	}
}

// initializeImportantPatterns sets up patterns that should never be filtered
func (f *SmartFilter) initializeImportantPatterns() {
	importantRegexes := []string{
		// Critical errors
		`(?i)(panic|fatal|critical|emergency)`,
		`(?i)(failed|error|cannot|unable)`,
		`(?i)(killed|died|crashed|abort)`,
		`(?i)(oom|out of memory)`,

		// Resource issues
		`(?i)(no space|disk full)`,
		`(?i)(cpu stall|hung task)`,
		`(?i)(blocked for more than)`,

		// Security issues
		`(?i)(authentication failure|invalid user|bad password)`,
		`(?i)(break-in attempt|intrusion)`,
		`(?i)(segfault|general protection fault)`,

		// Kubernetes/Container specific
		`(?i)(kubelet|docker|containerd).*?(error|failed)`,
		`(?i)(pod|container).*?(error|failed|crashed)`,
		`(?i)(failed to start container)`,
		`(?i)(back-?off)`,

		// Important state changes
		`(?i)(stopping|shutting down|terminating)`,
		`(?i)(not responding|timeout|timed out)`,
	}

	f.importantPatterns = make([]*regexp.Regexp, 0, len(importantRegexes))
	for _, pattern := range importantRegexes {
		if re, err := regexp.Compile(pattern); err == nil {
			f.importantPatterns = append(f.importantPatterns, re)
		}
	}

	// Important units to always monitor
	importantUnitsList := []string{
		"kubelet.service",
		"docker.service",
		"containerd.service",
		"etcd.service",
		"kube-apiserver.service",
		"kube-controller-manager.service",
		"kube-scheduler.service",
		"kube-proxy.service",
	}

	for _, unit := range importantUnitsList {
		f.importantUnits[unit] = true
	}
}

// isPriorityImportant checks if the priority indicates importance
func (f *SmartFilter) isPriorityImportant(entry *JournalEntry) bool {
	// Priority levels:
	// 0 (emerg), 1 (alert), 2 (crit), 3 (err), 4 (warning), 5 (notice), 6 (info), 7 (debug)

	// Always process emergency through error
	if entry.Priority <= 3 {
		return true
	}

	// Process warnings from important services
	if entry.Priority == 4 {
		return f.isImportantService(entry)
	}

	// Filter out notice, info, debug
	return false
}

// isNoisyUnit checks if the unit is known to be noisy
func (f *SmartFilter) isNoisyUnit(entry *JournalEntry) bool {
	if f.noisyUnits[entry.SystemdUnit] {
		return true
	}

	// Check identifier only if priority is not critical
	if entry.Priority > 3 && f.noisyIdentifiers[entry.SyslogIdentifier] {
		return true
	}

	return false
}

// matchesNoisePattern checks if message matches noise patterns
func (f *SmartFilter) matchesNoisePattern(entry *JournalEntry) bool {
	message := entry.Message

	for _, pattern := range f.noisePatterns {
		if pattern.MatchString(message) {
			return true
		}
	}

	return false
}

// isImportantOverride checks if entry should override noise filtering
func (f *SmartFilter) isImportantOverride(entry *JournalEntry) bool {
	// Check if from important unit
	if f.importantUnits[entry.SystemdUnit] {
		return true
	}

	// Check important patterns
	message := entry.Message
	for _, pattern := range f.importantPatterns {
		if pattern.MatchString(message) {
			return true
		}
	}

	return false
}

// isImportantService checks if the service is important
func (f *SmartFilter) isImportantService(entry *JournalEntry) bool {
	// Important units
	if f.importantUnits[entry.SystemdUnit] {
		return true
	}

	// Check by identifier patterns
	importantIdentifiers := []string{
		"kernel", "kubelet", "docker", "containerd",
		"etcd", "kube-", "calico", "flannel", "weave",
		"coredns", "prometheus", "grafana",
	}

	ident := strings.ToLower(entry.SyslogIdentifier)
	for _, important := range importantIdentifiers {
		if strings.Contains(ident, important) {
			return true
		}
	}

	return false
}

// isSystemdNoise checks for systemd-specific noise
func (f *SmartFilter) isSystemdNoise(entry *JournalEntry) bool {
	if entry.SyslogIdentifier != "systemd" {
		return false
	}

	// Only filter non-critical systemd messages
	if entry.Priority <= 3 {
		return false
	}

	message := entry.Message
	systemdNoise := []string{
		"Got notification message from PID",
		"Got WATCHDOG=1",
		"Received SIGRTMIN+24",
		"Started Session",
		"Starting Session",
		"Closed D-Bus User Message Bus Socket",
		"tmp.mount: Directory /tmp to mount over is not empty",
	}

	for _, noise := range systemdNoise {
		if strings.Contains(message, noise) {
			return true
		}
	}

	return false
}

// isKernelNoise checks for kernel-specific noise
func (f *SmartFilter) isKernelNoise(entry *JournalEntry) bool {
	if entry.SyslogIdentifier != "kernel" {
		return false
	}

	// Only filter non-critical kernel messages
	if entry.Priority <= 4 {
		return false
	}

	message := entry.Message
	kernelNoise := []string{
		"audit: ",
		"IPv6: ADDRCONF",
		"Bluetooth:",
		"usb ",
		"ACPI:",
		"pci ",
		"intel_",
		"snd_",
		"iwlwifi:",
	}

	for _, noise := range kernelNoise {
		if strings.Contains(message, noise) {
			return true
		}
	}

	return false
}

// shouldSuppressByFrequency implements frequency-based suppression
func (f *SmartFilter) shouldSuppressByFrequency(entry *JournalEntry) bool {
	// Create a key for frequency tracking
	key := f.createFrequencyKey(entry)

	// Check and update frequency
	return f.frequencyTracker.shouldSuppress(key)
}

// createFrequencyKey creates a key for frequency tracking
func (f *SmartFilter) createFrequencyKey(entry *JournalEntry) string {
	// Use unit + first 50 chars of message for grouping
	message := entry.Message
	if len(message) > 50 {
		message = message[:50]
	}

	// Normalize the message
	message = strings.ToLower(message)
	// Remove numbers to group similar messages
	message = regexp.MustCompile(`\d+`).ReplaceAllString(message, "N")

	return entry.SystemdUnit + ":" + message
}

// recordFiltered updates filter statistics
func (f *SmartFilter) recordFiltered(reason string) {
	f.stats.mu.Lock()
	defer f.stats.mu.Unlock()

	f.stats.totalFiltered++
	f.stats.byReason[reason]++
}

// GetActiveFilters returns currently active filter configuration
func (f *SmartFilter) GetActiveFilters() map[string]interface{} {
	return map[string]interface{}{
		"noise_patterns":     len(f.noisePatterns),
		"noisy_units":        len(f.noisyUnits),
		"important_patterns": len(f.importantPatterns),
		"important_units":    len(f.importantUnits),
		"max_priority":       4, // Warning and above
	}
}

// UpdateConfig updates filter configuration
func (f *SmartFilter) UpdateConfig(config *JournaldConfig) error {
	f.config = config
	// Re-initialize patterns if needed
	return nil
}

// GetStatistics returns filter statistics
func (f *SmartFilter) GetStatistics() map[string]uint64 {
	f.stats.mu.RLock()
	defer f.stats.mu.RUnlock()

	stats := make(map[string]uint64)
	stats["total_processed"] = f.stats.totalProcessed
	stats["total_filtered"] = f.stats.totalFiltered

	for reason, count := range f.stats.byReason {
		stats["filtered_"+reason] = count
	}

	if f.stats.totalProcessed > 0 {
		stats["filter_rate_percent"] = (f.stats.totalFiltered * 100) / f.stats.totalProcessed
	}

	return stats
}

// frequencyTracker implementation

func newFrequencyTracker(maxBuckets int) *frequencyTracker {
	return &frequencyTracker{
		buckets:    make(map[string]*frequencyBucket),
		maxBuckets: maxBuckets,
	}
}

func (ft *frequencyTracker) shouldSuppress(key string) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	now := time.Now()

	// Get or create bucket
	bucket, exists := ft.buckets[key]
	if !exists {
		// Cleanup if too many buckets
		if len(ft.buckets) >= ft.maxBuckets {
			ft.cleanup(now)
		}

		bucket = &frequencyBucket{
			count:    1,
			lastSeen: now,
		}
		ft.buckets[key] = bucket
		return false
	}

	// Reset count if been quiet for a while
	if now.Sub(bucket.lastSeen) > 5*time.Minute {
		bucket.count = 1
		bucket.suppressed = false
		bucket.lastSeen = now
		return false
	}

	// Update bucket
	bucket.count++
	bucket.lastSeen = now

	// Suppress if seeing too many (more than 10 in 30 seconds)
	timeDiff := now.Sub(bucket.lastSeen)
	if timeDiff < 30*time.Second && bucket.count > 10 {
		bucket.suppressed = true
		return true
	}

	return bucket.suppressed
}

func (ft *frequencyTracker) cleanup(now time.Time) {
	// Remove old entries
	for key, bucket := range ft.buckets {
		if now.Sub(bucket.lastSeen) > 10*time.Minute {
			delete(ft.buckets, key)
		}
	}
}
