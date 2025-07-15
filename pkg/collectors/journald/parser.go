//go:build linux
// +build linux

package journald

import (
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// Parser implements OPINIONATED log parsing focused on critical events
// We don't parse everything - only what matters for Kubernetes debugging
type Parser struct {
	// Compiled patterns for performance
	patterns map[string]*compiledPattern

	// Event categorizers
	categorizers []eventCategorizer
}

type compiledPattern struct {
	regex    *regexp.Regexp
	severity unified.Severity
	category unified.Category
	extract  func(matches []string) map[string]interface{}
}

type eventCategorizer func(entry *JournalEntry) (unified.Category, unified.Severity)

// NewParser creates a new OPINIONATED parser
func NewParser() *Parser {
	p := &Parser{
		patterns: make(map[string]*compiledPattern),
	}

	// Initialize OPINIONATED patterns - only what matters
	p.initializeCriticalPatterns()
	p.initializeCategorizers()

	return p
}

// ParseCritical parses only critical events that matter for debugging
func (p *Parser) ParseCritical(entry *JournalEntry) *unified.Event {
	// Skip noise early
	if p.isNoise(entry) {
		return nil
	}

	// Check patterns for critical events
	for name, pattern := range p.patterns {
		if matches := pattern.regex.FindStringSubmatch(entry.Message); matches != nil {
			return p.createEvent(entry, name, pattern, matches)
		}
	}

	// Check if it's a critical system event by other indicators
	category, severity := p.categorizeEntry(entry)
	if severity < unified.SeverityWarning {
		return nil // Not critical enough
	}

	// Create generic critical event
	return &unified.Event{
		Type:     "log",
		Category: category,
		Severity: severity,
		Data: map[string]interface{}{
			"message":    entry.Message,
			"unit":       entry.SystemdUnit,
			"priority":   entry.Priority,
			"service":    entry.SyslogIdentifier,
			"pid":        entry.PID,
			"uid":        entry.UID,
			"gid":        entry.GID,
			"machine_id": entry.MachineID,
			"boot_id":    entry.BootID,
		},
		Attributes: map[string]interface{}{
			"parsed_by": "generic_critical",
		},
		Labels: map[string]string{
			"hostname": entry.Hostname,
			"unit":     entry.SystemdUnit,
		},
		Context: &unified.EventContext{
			Node:        entry.Hostname,
			PID:         uint32(entry.PID),
			ProcessName: entry.SyslogIdentifier,
		},
		Metadata: unified.EventMetadata{
			CollectedAt: time.Now(),
			ProcessedAt: time.Now(),
		},
	}
}

// initializeCriticalPatterns sets up OPINIONATED patterns for critical events
func (p *Parser) initializeCriticalPatterns() {
	// Service crash patterns
	p.patterns["service_crash"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(service|unit) .* (failed|crashed|stopped unexpectedly|terminated abnormally)`),
		severity: unified.SeverityCritical,
		category: unified.CategoryReliability,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"failure_type": "service_crash",
				"full_match":   matches[0],
			}
		},
	}

	// Process failure patterns
	p.patterns["process_failure"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(main process exited|process .* failed|failed with result|exit-code|signal)`),
		severity: unified.SeverityError,
		category: unified.CategoryReliability,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"failure_type": "process_failure",
			}
		},
	}

	// Resource exhaustion patterns (non-OOM)
	p.patterns["resource_exhaustion"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(no space left|cannot allocate|resource temporarily unavailable|too many open files)`),
		severity: unified.SeverityCritical,
		category: unified.CategoryMemory,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"resource_type": extractResourceType(matches[0]),
			}
		},
	}

	// Network failure patterns
	p.patterns["network_failure"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(connection refused|connection reset|no route to host|network is unreachable|name resolution failed)`),
		severity: unified.SeverityError,
		category: unified.CategoryNetwork,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"network_error": extractNetworkError(matches[0]),
			}
		},
	}

	// Timeout patterns
	p.patterns["timeout"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(timeout|timed out|deadline exceeded|context canceled)`),
		severity: unified.SeverityWarning,
		category: unified.CategorySystem,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"timeout_type": extractTimeoutType(matches[0]),
			}
		},
	}

	// Security/Permission failures
	p.patterns["permission_denied"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(permission denied|access denied|unauthorized|forbidden|authentication failed)`),
		severity: unified.SeverityError,
		category: unified.CategorySecurity,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"security_issue": "permission_denied",
			}
		},
	}

	// Kernel panics and critical errors
	p.patterns["kernel_panic"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(kernel panic|bug:|oops:|general protection fault|segfault|segmentation fault)`),
		severity: unified.SeverityCritical,
		category: unified.CategoryReliability,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"kernel_error": matches[0],
			}
		},
	}

	// Application panics
	p.patterns["app_panic"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(panic:|fatal error:|runtime error:|stack trace:|goroutine \d+)`),
		severity: unified.SeverityCritical,
		category: unified.CategoryReliability,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"panic_type": "application_panic",
			}
		},
	}

	// Watchdog timeouts
	p.patterns["watchdog"] = &compiledPattern{
		regex:    regexp.MustCompile(`(?i)(watchdog:|watchdog timeout|service hold-off time over|start request repeated too quickly)`),
		severity: unified.SeverityError,
		category: unified.CategoryReliability,
		extract: func(matches []string) map[string]interface{} {
			return map[string]interface{}{
				"watchdog_event": matches[0],
			}
		},
	}
}

// initializeCategorizers sets up event categorization logic
func (p *Parser) initializeCategorizers() {
	p.categorizers = []eventCategorizer{
		// Categorize by systemd unit
		func(entry *JournalEntry) (unified.Category, unified.Severity) {
			unit := entry.SystemdUnit
			switch {
			case strings.Contains(unit, "kubelet"):
				return unified.CategoryReliability, unified.SeverityError
			case strings.Contains(unit, "docker") || strings.Contains(unit, "containerd"):
				return unified.CategoryReliability, unified.SeverityError
			case strings.Contains(unit, "etcd"):
				return unified.CategoryReliability, unified.SeverityCritical
			case strings.Contains(unit, "kernel"):
				return unified.CategoryReliability, unified.SeverityCritical
			default:
				return unified.CategoryReliability, unified.SeverityWarning
			}
		},
		// Categorize by priority
		func(entry *JournalEntry) (unified.Category, unified.Severity) {
			switch entry.Priority {
			case 0, 1, 2: // Emergency, Alert, Critical
				return unified.CategoryReliability, unified.SeverityCritical
			case 3: // Error
				return unified.CategoryReliability, unified.SeverityError
			case 4: // Warning
				return unified.CategoryReliability, unified.SeverityWarning
			default:
				return unified.CategoryReliability, unified.SeverityInfo
			}
		},
	}
}

// isNoise checks if the entry is noise we should ignore
func (p *Parser) isNoise(entry *JournalEntry) bool {
	// OPINIONATED: We ignore most info/debug logs
	if entry.Priority > 4 {
		return true
	}

	// Ignore common noise patterns
	noisePatterns := []string{
		"systemd[1]: Started Session",
		"systemd[1]: Starting Session",
		"systemd[1]: Removed slice",
		"systemd[1]: Created slice",
		"systemd[1]: Reached target",
		"systemd[1]: Stopped target",
		"CRON[",
		"Created slice user-",
		"Starting user-",
		"Removed session",
		"New session",
		"pam_unix",
		"Got notification message from PID",
	}

	message := entry.Message
	for _, noise := range noisePatterns {
		if strings.Contains(message, noise) {
			return true
		}
	}

	return false
}

// categorizeEntry determines category and severity for an entry
func (p *Parser) categorizeEntry(entry *JournalEntry) (unified.Category, unified.Severity) {
	var category unified.Category
	var severity unified.Severity

	// Run through all categorizers and take the highest severity
	for _, categorizer := range p.categorizers {
		cat, sev := categorizer(entry)
		if sev > severity {
			category = cat
			severity = sev
		}
	}

	return category, severity
}

// createEvent creates a structured event from a pattern match
func (p *Parser) createEvent(entry *JournalEntry, patternName string, pattern *compiledPattern, matches []string) *unified.Event {
	event := &unified.Event{
		Type:     "log",
		Category: pattern.category,
		Severity: pattern.severity,
		Data: map[string]interface{}{
			"message":  entry.Message,
			"unit":     entry.SystemdUnit,
			"priority": entry.Priority,
			"service":  entry.SyslogIdentifier,
			"pid":      entry.PID,
			"uid":      entry.UID,
			"pattern":  patternName,
		},
		Attributes: map[string]interface{}{
			"parsed_by":    patternName,
			"pattern_type": "critical",
		},
		Labels: map[string]string{
			"hostname": entry.Hostname,
			"unit":     entry.SystemdUnit,
			"pattern":  patternName,
		},
		Context: &unified.EventContext{
			Node:        entry.Hostname,
			PID:         uint32(entry.PID),
			ProcessName: entry.SyslogIdentifier,
		},
		Metadata: unified.EventMetadata{
			CollectedAt: time.Now(),
			ProcessedAt: time.Now(),
		},
	}

	// Add pattern-specific extracted data
	if pattern.extract != nil {
		extracted := pattern.extract(matches)
		for k, v := range extracted {
			event.Data[k] = v
		}
	}

	// Add actionable item for critical events
	if pattern.severity >= unified.SeverityError {
		event.Actionable = &unified.ActionableItem{
			Title:       "Investigation Required",
			Description: p.getActionableDescription(patternName, entry),
			Risk:        p.getRisk(pattern.severity),
			Commands:    p.getSuggestedCommands(patternName, entry),
		}
	}

	return event
}

// Helper functions

func (p *Parser) getRisk(severity unified.Severity) unified.Risk {
	switch severity {
	case unified.SeverityCritical:
		return unified.RiskHigh
	case unified.SeverityError:
		return unified.RiskMedium
	default:
		return unified.RiskLow
	}
}

func (p *Parser) getActionableDescription(pattern string, entry *JournalEntry) string {
	switch pattern {
	case "service_crash":
		return "Service " + entry.SystemdUnit + " has crashed and requires investigation"
	case "resource_exhaustion":
		return "System is experiencing resource exhaustion"
	case "network_failure":
		return "Network connectivity issues detected"
	case "kernel_panic":
		return "Kernel panic detected - system stability compromised"
	default:
		return "Critical system event requires investigation"
	}
}

func (p *Parser) getSuggestedCommands(pattern string, entry *JournalEntry) []string {
	unit := entry.SystemdUnit

	switch pattern {
	case "service_crash":
		return []string{
			"systemctl status " + unit,
			"journalctl -u " + unit + " -n 100",
			"systemctl restart " + unit,
		}
	case "resource_exhaustion":
		return []string{
			"df -h",
			"du -sh /* | sort -h",
			"lsof | wc -l",
		}
	case "network_failure":
		return []string{
			"ping -c 3 8.8.8.8",
			"nslookup kubernetes.default",
			"netstat -tuln",
		}
	default:
		return []string{
			"journalctl -n 100 -p err",
			"systemctl list-units --failed",
		}
	}
}

func extractResourceType(message string) string {
	switch {
	case strings.Contains(message, "no space left"):
		return "disk"
	case strings.Contains(message, "cannot allocate"):
		return "memory"
	case strings.Contains(message, "too many open files"):
		return "file_descriptors"
	default:
		return "unknown"
	}
}

func extractNetworkError(message string) string {
	switch {
	case strings.Contains(message, "connection refused"):
		return "connection_refused"
	case strings.Contains(message, "connection reset"):
		return "connection_reset"
	case strings.Contains(message, "no route to host"):
		return "no_route"
	case strings.Contains(message, "network is unreachable"):
		return "network_unreachable"
	case strings.Contains(message, "name resolution failed"):
		return "dns_failure"
	default:
		return "unknown"
	}
}

func extractTimeoutType(message string) string {
	switch {
	case strings.Contains(message, "context"):
		return "context_timeout"
	case strings.Contains(message, "deadline"):
		return "deadline_exceeded"
	case strings.Contains(message, "i/o timeout"):
		return "io_timeout"
	default:
		return "generic_timeout"
	}
}
