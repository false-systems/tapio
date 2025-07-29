package internal

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	
	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
)

// EventValidator validates and sanitizes systemd events
type EventValidator struct {
	// Security patterns
	pathTraversalPattern *regexp.Regexp
	sqlInjectionPattern  *regexp.Regexp
	commandInjectionPattern *regexp.Regexp
	
	// Validation rules
	maxServiceNameLength int
	maxPropertyLength    int
	maxEventSize        int
	
	// Metrics
	validatedEvents   uint64
	invalidEvents     uint64
	sanitizedEvents   uint64
}

// NewEventValidator creates a new event validator
func NewEventValidator() *EventValidator {
	return &EventValidator{
		pathTraversalPattern:    regexp.MustCompile(`\.\./|\.\.\\|%2e%2e%2f|%252e%252e%252f`),
		sqlInjectionPattern:     regexp.MustCompile(`(?i)(union.*select|select.*from|insert.*into|delete.*from|drop.*table|update.*set|exec.*\(|execute.*\()`),
		commandInjectionPattern: regexp.MustCompile(`[;&|]|\$\(|\${|` + "`"),
		maxServiceNameLength:    256,
		maxPropertyLength:       1024,
		maxEventSize:           1024 * 1024, // 1MB
	}
}

// ValidateEvent validates a systemd raw event
func (v *EventValidator) ValidateEvent(event *core.RawEvent) error {
	if event == nil {
		v.invalidEvents++
		return fmt.Errorf("nil event")
	}

	// Validate basic fields
	if err := v.validateBasicFields(event); err != nil {
		v.invalidEvents++
		return err
	}

	// Validate security patterns
	if err := v.validateSecurity(event); err != nil {
		v.invalidEvents++
		return err
	}

	// Sanitize fields
	v.sanitizeEvent(event)
	
	v.validatedEvents++
	return nil
}

func (v *EventValidator) validateBasicFields(event *core.RawEvent) error {
	// Validate unit name
	if event.UnitName == "" {
		return fmt.Errorf("empty unit name")
	}
	if len(event.UnitName) > v.maxServiceNameLength {
		return fmt.Errorf("unit name too long: %d > %d", len(event.UnitName), v.maxServiceNameLength)
	}
	if !isValidServiceName(event.UnitName) {
		return fmt.Errorf("invalid unit name format: %s", event.UnitName)
	}

	// Validate timestamp
	if event.Timestamp.IsZero() {
		return fmt.Errorf("zero timestamp")
	}
	if event.Timestamp.After(time.Now().Add(5 * time.Minute)) {
		return fmt.Errorf("timestamp in future: %v", event.Timestamp)
	}
	if event.Timestamp.Before(time.Now().Add(-24 * time.Hour)) {
		return fmt.Errorf("timestamp too old: %v", event.Timestamp)
	}

	// Validate state transitions
	if !isValidStateTransition(event.OldState, event.NewState) {
		return fmt.Errorf("invalid state transition: %s -> %s", event.OldState, event.NewState)
	}

	return nil
}

func (v *EventValidator) validateSecurity(event *core.RawEvent) error {
	// Check for path traversal in unit name
	if v.pathTraversalPattern.MatchString(event.UnitName) {
		return fmt.Errorf("potential path traversal detected in unit name")
	}

	// Check for SQL injection
	if v.sqlInjectionPattern.MatchString(event.UnitName) {
		return fmt.Errorf("potential SQL injection detected")
	}

	// Check for command injection
	if v.commandInjectionPattern.MatchString(event.UnitName) {
		return fmt.Errorf("potential command injection detected")
	}

	// Validate properties
	for key, value := range event.Properties {
		keyStr := fmt.Sprintf("%v", key)
		valueStr := fmt.Sprintf("%v", value)
		
		if len(keyStr) > 256 || len(valueStr) > v.maxPropertyLength {
			return fmt.Errorf("property too long: %s", keyStr)
		}
		if v.pathTraversalPattern.MatchString(keyStr) || v.pathTraversalPattern.MatchString(valueStr) {
			return fmt.Errorf("suspicious property content: %s", keyStr)
		}
	}

	return nil
}

func (v *EventValidator) sanitizeEvent(event *core.RawEvent) {
	// Sanitize unit name
	event.UnitName = sanitizeString(event.UnitName)
	
	// Sanitize properties
	sanitizedProps := make(map[string]interface{})
	for key, value := range event.Properties {
		sanitizedKey := sanitizeString(fmt.Sprintf("%v", key))
		
		// Handle different value types
		switch val := value.(type) {
		case string:
			sanitizedValue := sanitizeString(val)
			if len(sanitizedValue) > v.maxPropertyLength {
				sanitizedValue = sanitizedValue[:v.maxPropertyLength] + "..."
			}
			sanitizedProps[sanitizedKey] = sanitizedValue
		case []byte:
			// Convert byte arrays to string and sanitize
			sanitizedValue := sanitizeString(string(val))
			if len(sanitizedValue) > v.maxPropertyLength {
				sanitizedValue = sanitizedValue[:v.maxPropertyLength] + "..."
			}
			sanitizedProps[sanitizedKey] = sanitizedValue
		default:
			// Keep other types as-is
			sanitizedProps[sanitizedKey] = value
		}
	}
	event.Properties = sanitizedProps

	v.sanitizedEvents++
}

// isValidServiceName checks if service name follows systemd naming conventions
func isValidServiceName(name string) bool {
	// Must end with .service, .socket, .device, .mount, .automount, .swap, .target, .path, .timer, .slice, or .scope
	validSuffixes := []string{".service", ".socket", ".device", ".mount", ".automount", ".swap", ".target", ".path", ".timer", ".slice", ".scope"}
	
	hasValidSuffix := false
	for _, suffix := range validSuffixes {
		if strings.HasSuffix(name, suffix) {
			hasValidSuffix = true
			break
		}
	}
	
	if !hasValidSuffix {
		return false
	}

	// Check for valid characters (alphanumeric, dash, underscore, dot, @)
	validChars := regexp.MustCompile(`^[a-zA-Z0-9_.\-@]+$`)
	return validChars.MatchString(name)
}

// isValidStateTransition checks if the state transition is valid
func isValidStateTransition(oldState, newState string) bool {
	// Define valid systemd states
	validStates := map[string]bool{
		"inactive":      true,
		"active":        true,
		"activating":    true,
		"deactivating":  true,
		"failed":        true,
		"reloading":     true,
		"maintenance":   true,
	}

	// Empty old state is valid (initial state)
	if oldState != "" && !validStates[oldState] {
		return false
	}

	return validStates[newState]
}

// sanitizeString removes potentially dangerous characters
func sanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	
	// Remove control characters except newline and tab
	result := strings.Builder{}
	for _, r := range s {
		if r == '\n' || r == '\t' || (r >= 32 && r < 127) || r > 127 {
			result.WriteRune(r)
		}
	}
	
	return strings.TrimSpace(result.String())
}

// DeterminePriority determines event priority based on content
func (v *EventValidator) DeterminePriority(event *core.RawEvent) EventPriority {
	// Critical: System-critical services failing
	criticalServices := []string{
		"systemd-journald.service",
		"systemd-logind.service",
		"dbus.service",
		"NetworkManager.service",
		"systemd-resolved.service",
	}
	
	for _, service := range criticalServices {
		if event.UnitName == service && event.NewState == "failed" {
			return PriorityCritical
		}
	}

	// High: Service failures or security-relevant events
	if event.NewState == "failed" || event.ExitCode != 0 {
		return PriorityHigh
	}

	// Normal: State changes
	if event.OldState != event.NewState {
		return PriorityNormal
	}

	// Low: Everything else
	return PriorityLow
}

// Metrics returns validation metrics
func (v *EventValidator) Metrics() map[string]interface{} {
	return map[string]interface{}{
		"validated_events": v.validatedEvents,
		"invalid_events":   v.invalidEvents,
		"sanitized_events": v.sanitizedEvents,
		"validation_rate":  float64(v.validatedEvents) / float64(v.validatedEvents + v.invalidEvents + 1),
	}
}