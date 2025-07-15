//go:build linux
// +build linux

package journald

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// OOMDetector specializes in detecting and parsing OOM kill events
// This is CRITICAL for Kubernetes debugging - we must catch every OOM
type OOMDetector struct {
	// Patterns for different OOM formats
	kernelOOMPattern   *regexp.Regexp
	cgroupOOMPattern   *regexp.Regexp
	scorePattern       *regexp.Regexp
	victimPattern      *regexp.Regexp
	memoryStatsPattern *regexp.Regexp
	containerPattern   *regexp.Regexp
	podPattern         *regexp.Regexp

	// State tracking for multi-line OOM events
	activeOOM *oomState
}

type oomState struct {
	startTime   int64
	victimPID   int
	victimName  string
	victimScore int
	containerID string
	podName     string
	namespace   string
	memoryLimit int64
	memoryUsage int64
	messages    []string
}

// NewOOMDetector creates a specialized OOM detector
func NewOOMDetector() *OOMDetector {
	return &OOMDetector{
		// Kernel OOM killer invocation
		kernelOOMPattern: regexp.MustCompile(`(?i)(invoked oom-killer:|out of memory:|oom-kill:|memory cgroup out of memory)`),

		// Cgroup-specific OOM
		cgroupOOMPattern: regexp.MustCompile(`(?i)memory cgroup out of memory.*?under oom`),

		// OOM score and victim selection
		scorePattern:  regexp.MustCompile(`(?i)\[\s*(\d+)\]\s+(\d+)\s+\d+\s+(\d+)\s+\d+\s+(\d+)\s+.*?(\S+)$`),
		victimPattern: regexp.MustCompile(`(?i)killed process (\d+) \(([^)]+)\)`),

		// Memory statistics
		memoryStatsPattern: regexp.MustCompile(`(?i)memory: usage (\d+)kB, limit (\d+)kB`),

		// Container/Pod detection
		containerPattern: regexp.MustCompile(`(?i)(?:container_id|containerid)[:=]([a-f0-9]{64}|[a-f0-9]{12})`),
		podPattern:       regexp.MustCompile(`(?i)pod[:=]([a-zA-Z0-9-]+)/([a-zA-Z0-9-]+)`),
	}
}

// Detect checks if a journal entry is an OOM event and extracts details
func (d *OOMDetector) Detect(entry *JournalEntry) *unified.Event {
	// Check if this is an OOM-related message
	if !d.isOOMRelated(entry) {
		// Check if we have an active OOM state and this might be a continuation
		if d.activeOOM != nil && d.isOOMContinuation(entry) {
			d.updateOOMState(entry)
			return nil // Still collecting info
		}

		// Not OOM related, but check if we need to finalize active OOM
		if d.activeOOM != nil && d.shouldFinalizeOOM(entry) {
			return d.finalizeOOMEvent(entry.RealtimeTimestamp)
		}

		return nil
	}

	// Start new OOM tracking or update existing
	if d.activeOOM == nil {
		d.activeOOM = &oomState{
			startTime: entry.RealtimeTimestamp,
			messages:  []string{entry.Message},
		}
	} else {
		d.activeOOM.messages = append(d.activeOOM.messages, entry.Message)
	}

	// Extract OOM details
	d.extractOOMDetails(entry)

	// Check if this entry completes the OOM event
	if d.isOOMComplete(entry) {
		return d.finalizeOOMEvent(entry.RealtimeTimestamp)
	}

	return nil // Still collecting OOM details
}

// isOOMRelated checks if the entry is related to OOM
func (d *OOMDetector) isOOMRelated(entry *JournalEntry) bool {
	message := entry.Message

	// Quick check for OOM keywords
	oomKeywords := []string{
		"oom",
		"OOM",
		"out of memory",
		"Out of memory",
		"memory cgroup",
		"Memory cgroup",
		"oom-killer",
		"killed process",
		"score adj",
	}

	for _, keyword := range oomKeywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}

	// Check kernel source
	if entry.SyslogIdentifier == "kernel" && entry.Priority <= 3 {
		return d.kernelOOMPattern.MatchString(message) || d.cgroupOOMPattern.MatchString(message)
	}

	return false
}

// extractOOMDetails extracts information from OOM messages
func (d *OOMDetector) extractOOMDetails(entry *JournalEntry) {
	message := entry.Message

	// Extract victim information
	if matches := d.victimPattern.FindStringSubmatch(message); matches != nil {
		pid, _ := strconv.Atoi(matches[1])
		d.activeOOM.victimPID = pid
		d.activeOOM.victimName = matches[2]
	}

	// Extract OOM score
	if matches := d.scorePattern.FindStringSubmatch(message); matches != nil {
		score, _ := strconv.Atoi(matches[4])
		d.activeOOM.victimScore = score
	}

	// Extract memory statistics
	if matches := d.memoryStatsPattern.FindStringSubmatch(message); matches != nil {
		usage, _ := strconv.ParseInt(matches[1], 10, 64)
		limit, _ := strconv.ParseInt(matches[2], 10, 64)
		d.activeOOM.memoryUsage = usage * 1024 // Convert to bytes
		d.activeOOM.memoryLimit = limit * 1024
	}

	// Extract container information
	if matches := d.containerPattern.FindStringSubmatch(message); matches != nil {
		d.activeOOM.containerID = matches[1]
	}

	// Extract pod information
	if matches := d.podPattern.FindStringSubmatch(message); matches != nil {
		d.activeOOM.namespace = matches[1]
		d.activeOOM.podName = matches[2]
	}

	// Try to extract from systemd unit
	if d.activeOOM.containerID == "" && strings.Contains(entry.SystemdUnit, "docker-") {
		parts := strings.Split(entry.SystemdUnit, "docker-")
		if len(parts) > 1 {
			d.activeOOM.containerID = strings.TrimSuffix(parts[1], ".scope")
		}
	}
}

// isOOMComplete checks if we have collected complete OOM information
func (d *OOMDetector) isOOMComplete(entry *JournalEntry) bool {
	// We consider OOM complete if we have victim information
	return d.activeOOM.victimPID > 0 && d.activeOOM.victimName != ""
}

// isOOMContinuation checks if entry continues active OOM event
func (d *OOMDetector) isOOMContinuation(entry *JournalEntry) bool {
	// OOM events usually complete within 1 second
	timeDiff := entry.RealtimeTimestamp - d.activeOOM.startTime
	if timeDiff > 1_000_000_000 { // 1 second in nanoseconds
		return false
	}

	// Check if from same source
	return entry.SyslogIdentifier == "kernel" && entry.Priority <= 3
}

// shouldFinalizeOOM checks if we should finalize the active OOM
func (d *OOMDetector) shouldFinalizeOOM(entry *JournalEntry) bool {
	if d.activeOOM == nil {
		return false
	}

	// Finalize if more than 2 seconds have passed
	timeDiff := entry.RealtimeTimestamp - d.activeOOM.startTime
	return timeDiff > 2_000_000_000
}

// updateOOMState updates active OOM with new information
func (d *OOMDetector) updateOOMState(entry *JournalEntry) {
	d.activeOOM.messages = append(d.activeOOM.messages, entry.Message)
	d.extractOOMDetails(entry)
}

// finalizeOOMEvent creates the final OOM event
func (d *OOMDetector) finalizeOOMEvent(timestamp int64) *unified.Event {
	if d.activeOOM == nil {
		return nil
	}

	oom := d.activeOOM
	d.activeOOM = nil // Reset state

	// Build comprehensive OOM message
	fullMessage := strings.Join(oom.messages, "\n")

	event := &unified.Event{
		Type:     "oom_kill",
		Category: unified.CategoryMemory,
		Severity: unified.SeverityCritical,
		Data: map[string]interface{}{
			"oom_type":       d.determineOOMType(oom),
			"victim_pid":     oom.victimPID,
			"victim_name":    oom.victimName,
			"victim_score":   oom.victimScore,
			"memory_usage":   oom.memoryUsage,
			"memory_limit":   oom.memoryLimit,
			"memory_percent": d.calculateMemoryPercent(oom),
			"container_id":   oom.containerID,
			"pod_name":       oom.podName,
			"namespace":      oom.namespace,
			"full_message":   fullMessage,
			"message_count":  len(oom.messages),
		},
		Attributes: map[string]interface{}{
			"oom_detected": true,
			"parser":       "oom_detector",
		},
		Labels: d.buildLabels(oom),
		Context: &unified.EventContext{
			PID:         uint32(oom.victimPID),
			ProcessName: oom.victimName,
			Container:   oom.containerID,
			Pod:         oom.podName,
			Namespace:   oom.namespace,
		},
		Metadata: unified.EventMetadata{
			CollectedAt: time.Now(),
			ProcessedAt: time.Now(),
		},
		Actionable: d.createActionableItem(oom),
	}

	return event
}

// determineOOMType determines the type of OOM
func (d *OOMDetector) determineOOMType(oom *oomState) string {
	if oom.containerID != "" {
		return "container_oom"
	}
	if strings.Contains(strings.Join(oom.messages, " "), "cgroup") {
		return "cgroup_oom"
	}
	return "system_oom"
}

// calculateMemoryPercent calculates memory usage percentage
func (d *OOMDetector) calculateMemoryPercent(oom *oomState) float64 {
	if oom.memoryLimit == 0 {
		return 100.0
	}
	return float64(oom.memoryUsage) / float64(oom.memoryLimit) * 100
}

// buildLabels creates labels for the OOM event
func (d *OOMDetector) buildLabels(oom *oomState) map[string]string {
	labels := map[string]string{
		"event_type": "oom_kill",
		"severity":   "critical",
	}

	if oom.containerID != "" {
		labels["container_id"] = oom.containerID
	}
	if oom.podName != "" {
		labels["pod"] = oom.podName
	}
	if oom.namespace != "" {
		labels["namespace"] = oom.namespace
	}
	if oom.victimName != "" {
		labels["victim_process"] = oom.victimName
	}

	return labels
}

// createActionableItem creates remediation suggestions for OOM
func (d *OOMDetector) createActionableItem(oom *oomState) *unified.ActionableItem {
	suggestions := []string{
		fmt.Sprintf("Process '%s' (PID: %d) was killed due to memory exhaustion", oom.victimName, oom.victimPID),
	}

	commands := []string{}

	if oom.podName != "" && oom.namespace != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("Pod %s/%s experienced OOM kill", oom.namespace, oom.podName),
			"Consider increasing memory limits for this pod",
		)

		commands = append(commands,
			fmt.Sprintf("kubectl describe pod %s -n %s", oom.podName, oom.namespace),
			fmt.Sprintf("kubectl top pod %s -n %s", oom.podName, oom.namespace),
			fmt.Sprintf("kubectl logs %s -n %s --previous", oom.podName, oom.namespace),
		)
	}

	if oom.memoryLimit > 0 {
		limitMB := oom.memoryLimit / 1024 / 1024
		usageMB := oom.memoryUsage / 1024 / 1024
		suggestions = append(suggestions,
			fmt.Sprintf("Memory usage: %dMB / %dMB (%.1f%%)", usageMB, limitMB, d.calculateMemoryPercent(oom)),
			fmt.Sprintf("Recommended: Increase memory limit to at least %dMB", int(float64(limitMB)*1.5)),
		)
	}

	if oom.containerID != "" {
		commands = append(commands,
			fmt.Sprintf("docker inspect %s | jq '.[0].Config.Labels'", oom.containerID[:12]),
			fmt.Sprintf("docker stats --no-stream %s", oom.containerID[:12]),
		)
	}

	return &unified.ActionableItem{
		Title:       "OOM Remediation",
		Description: strings.Join(suggestions, ". "),
		Risk:        unified.RiskHigh,
		Commands:    commands,
	}
}

// Reset clears any active OOM state (useful for testing)
func (d *OOMDetector) Reset() {
	d.activeOOM = nil
}
