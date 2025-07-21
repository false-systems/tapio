package internal

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// EventValidator validates raw eBPF events for security and correctness
type EventValidator struct {
	mu               sync.RWMutex
	maxEventSize     int
	maxCommLength    int
	validEventTypes  map[string]bool
	processNameRegex *regexp.Regexp
	metrics          ValidatorMetrics
}

// ValidatorMetrics tracks validation statistics
type ValidatorMetrics struct {
	TotalValidated     uint64
	InvalidEvents      uint64
	SecurityViolations uint64
	MalformedEvents    uint64
	OversizedEvents    uint64
}

// NewEventValidator creates a new event validator
func NewEventValidator() *EventValidator {
	return &EventValidator{
		maxEventSize:  1024 * 1024, // 1MB max event size
		maxCommLength: 256,         // Max process name length
		validEventTypes: map[string]bool{
			"syscall":  true,
			"network":  true,
			"file":     true,
			"process":  true,
			"memory":   true,
			"security": true,
		},
		processNameRegex: regexp.MustCompile(`^[a-zA-Z0-9\-_./:]+$`),
	}
}

// ValidateEvent validates a raw eBPF event
func (v *EventValidator) ValidateEvent(event core.RawEvent) error {
	v.mu.Lock()
	v.metrics.TotalValidated++
	v.mu.Unlock()

	// Check event size
	if len(event.Data) > v.maxEventSize {
		v.recordInvalid("oversized")
		return fmt.Errorf("event data too large: %d bytes (max: %d)", len(event.Data), v.maxEventSize)
	}

	// Validate timestamp
	if event.Timestamp.IsZero() || event.Timestamp.After(time.Now().Add(time.Hour)) {
		v.recordInvalid("malformed")
		return fmt.Errorf("invalid timestamp: %v", event.Timestamp)
	}

	// Validate event type
	if event.Type != "" && !v.validEventTypes[event.Type] {
		v.recordInvalid("invalid")
		return fmt.Errorf("invalid event type: %s", event.Type)
	}

	// Validate process information
	if err := v.validateProcessInfo(event); err != nil {
		v.recordInvalid("security")
		return err
	}

	// Validate numeric fields
	if err := v.validateNumericFields(event); err != nil {
		v.recordInvalid("malformed")
		return err
	}

	// Check for potential security issues
	if err := v.checkSecurityViolations(event); err != nil {
		v.recordInvalid("security")
		return err
	}

	return nil
}

// validateProcessInfo validates process-related fields
func (v *EventValidator) validateProcessInfo(event core.RawEvent) error {
	// Validate comm (process name)
	if len(event.Comm) > v.maxCommLength {
		return fmt.Errorf("process name too long: %d chars (max: %d)", len(event.Comm), v.maxCommLength)
	}

	// Check for suspicious process names
	if event.Comm != "" && !v.processNameRegex.MatchString(event.Comm) {
		return fmt.Errorf("suspicious process name format: %s", event.Comm)
	}

	// Validate PID/TID ranges
	if event.PID > 4194304 || event.TID > 4194304 { // Max PID on Linux
		return fmt.Errorf("invalid PID/TID: %d/%d", event.PID, event.TID)
	}

	return nil
}

// validateNumericFields validates numeric event fields
func (v *EventValidator) validateNumericFields(event core.RawEvent) error {
	// CPU must be reasonable
	if event.CPU > 1024 { // Arbitrary but reasonable max CPU count
		return fmt.Errorf("invalid CPU number: %d", event.CPU)
	}

	// UID/GID validation (0 is valid for root)
	if event.UID > 65535 || event.GID > 65535 {
		return fmt.Errorf("invalid UID/GID: %d/%d", event.UID, event.GID)
	}

	return nil
}

// checkSecurityViolations checks for potential security issues
func (v *EventValidator) checkSecurityViolations(event core.RawEvent) error {
	// Check for suspicious patterns in decoded data
	if event.Decoded != nil {
		// Check for path traversal attempts
		for key, value := range event.Decoded {
			if strVal, ok := value.(string); ok {
				if containsPathTraversal(strVal) {
					return fmt.Errorf("potential path traversal in field %s", key)
				}
				if containsSQLInjection(strVal) {
					return fmt.Errorf("potential SQL injection in field %s", key)
				}
			}
		}
	}

	// Check for privilege escalation indicators
	if event.Type == "process" && event.UID == 0 && event.PID > 1 {
		if parent, ok := event.Decoded["parent_uid"]; ok {
			if parentUID, ok := parent.(uint32); ok && parentUID != 0 {
				return fmt.Errorf("potential privilege escalation: non-root to root")
			}
		}
	}

	return nil
}

// containsPathTraversal checks for path traversal patterns
func containsPathTraversal(s string) bool {
	patterns := []string{
		"../",
		"..\\",
		"%2e%2e/",
		"%2e%2e\\",
		"..%2f",
		"..%5c",
	}

	for _, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(s) {
			return true
		}
	}
	return false
}

// containsSQLInjection checks for basic SQL injection patterns
func containsSQLInjection(s string) bool {
	patterns := []string{
		`(?i)(union.*select)`,
		`(?i)(select.*from)`,
		`(?i)(insert.*into)`,
		`(?i)(delete.*from)`,
		`(?i)(drop.*table)`,
		`(?i)(';|--|/\*|\*/|xp_|sp_)`,
	}

	for _, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(s) {
			return true
		}
	}
	return false
}

// recordInvalid records invalid event metrics
func (v *EventValidator) recordInvalid(reason string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.metrics.InvalidEvents++

	switch reason {
	case "oversized":
		v.metrics.OversizedEvents++
	case "malformed":
		v.metrics.MalformedEvents++
	case "security":
		v.metrics.SecurityViolations++
	}
}

// GetMetrics returns validator metrics
func (v *EventValidator) GetMetrics() ValidatorMetrics {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.metrics
}

// Reset resets validator metrics
func (v *EventValidator) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.metrics = ValidatorMetrics{}
}
