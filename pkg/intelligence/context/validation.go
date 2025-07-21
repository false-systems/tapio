package context

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventValidator validates UnifiedEvents before processing
type EventValidator struct {
	requiredFields []string
	maxEventAge    time.Duration
}

// NewEventValidator creates a new event validator with default settings
func NewEventValidator() *EventValidator {
	return &EventValidator{
		requiredFields: []string{"ID", "Timestamp", "Type", "Source"},
		maxEventAge:    24 * time.Hour,
	}
}

// NewEventValidatorWithConfig creates a validator with custom configuration
func NewEventValidatorWithConfig(maxAge time.Duration) *EventValidator {
	return &EventValidator{
		requiredFields: []string{"ID", "Timestamp", "Type", "Source"},
		maxEventAge:    maxAge,
	}
}

// Validate performs comprehensive validation on a UnifiedEvent
func (ev *EventValidator) Validate(ue *domain.UnifiedEvent) error {
	if ue == nil {
		return fmt.Errorf("event is nil")
	}

	// Validate required fields
	if err := ev.validateRequiredFields(ue); err != nil {
		return fmt.Errorf("required field validation failed: %w", err)
	}

	// Validate event age
	if err := ev.validateEventAge(ue); err != nil {
		return fmt.Errorf("event age validation failed: %w", err)
	}

	// Validate layer-specific data consistency
	if err := ev.validateLayerData(ue); err != nil {
		return fmt.Errorf("layer data validation failed: %w", err)
	}

	return nil
}

// validateRequiredFields checks that all required fields are present
func (ev *EventValidator) validateRequiredFields(ue *domain.UnifiedEvent) error {
	// Check ID
	if ue.ID == "" {
		return fmt.Errorf("event missing ID")
	}

	// Check Timestamp
	if ue.Timestamp.IsZero() {
		return fmt.Errorf("event missing timestamp")
	}

	// Check Type
	if ue.Type == "" {
		return fmt.Errorf("event missing type")
	}

	// Check Source
	if ue.Source == "" {
		return fmt.Errorf("event missing source")
	}

	return nil
}

// validateEventAge ensures the event is not too old
func (ev *EventValidator) validateEventAge(ue *domain.UnifiedEvent) error {
	age := time.Since(ue.Timestamp)

	// Check for future timestamps
	if age < 0 {
		// Allow small clock skew (up to 5 minutes in the future)
		if age < -5*time.Minute {
			return fmt.Errorf("event timestamp is too far in the future: %v", ue.Timestamp)
		}
	}

	// Check for old events
	if age > ev.maxEventAge {
		return fmt.Errorf("event too old: timestamp=%v, age=%v, max_age=%v",
			ue.Timestamp, age, ev.maxEventAge)
	}

	return nil
}

// validateLayerData ensures appropriate layer data exists for the event type
func (ev *EventValidator) validateLayerData(ue *domain.UnifiedEvent) error {
	switch ue.Type {
	case domain.EventTypeSystem:
		// System events may or may not have layer-specific data
		return nil
	case domain.EventTypeMemory:
		return ev.validateMemoryData(ue)
	case domain.EventTypeNetwork:
		return ev.validateNetworkData(ue)
	case domain.EventTypeLog:
		return ev.validateApplicationData(ue)
	case domain.EventTypeKubernetes:
		return ev.validateInfrastructureData(ue)
	case domain.EventTypeProcess:
		return ev.validateProcessData(ue)
	case domain.EventTypeCPU:
		// CPU events typically have kernel data
		return ev.validateKernelData(ue)
	case domain.EventTypeDisk:
		// Disk events typically have kernel data
		return ev.validateKernelData(ue)
	case domain.EventTypeService:
		// Service events may not have specific layer data
		return nil
	default:
		// Unknown event types are allowed but logged
		return nil
	}
}

// validateKernelData validates kernel-specific event data
func (ev *EventValidator) validateKernelData(ue *domain.UnifiedEvent) error {
	if ue.Kernel == nil {
		return fmt.Errorf("kernel event missing kernel data")
	}

	// Validate kernel data fields
	if ue.Kernel.Syscall == "" && ue.Kernel.Comm == "" {
		return fmt.Errorf("kernel event missing both syscall and comm")
	}

	// PID validation not needed for uint32 as it can't be negative

	return nil
}

// validateMemoryData validates memory-specific event data
func (ev *EventValidator) validateMemoryData(ue *domain.UnifiedEvent) error {
	// Memory events can have kernel data or application data
	if ue.Kernel == nil && ue.Application == nil {
		return fmt.Errorf("memory event missing both kernel and application data")
	}

	// If kernel data exists with memory-related syscalls, it's valid
	if ue.Kernel != nil && (ue.Kernel.Syscall == "mmap" || ue.Kernel.Syscall == "brk" || ue.Kernel.Syscall == "munmap") {
		// Memory-related syscalls are valid for memory events
		return nil
	}

	return nil
}

// validateNetworkData validates network-specific event data
func (ev *EventValidator) validateNetworkData(ue *domain.UnifiedEvent) error {
	if ue.Network == nil {
		return fmt.Errorf("network event missing network data")
	}

	// Validate network data fields
	if ue.Network.Protocol == "" {
		return fmt.Errorf("network event missing protocol")
	}

	// Validate IP addresses if present
	if ue.Network.SourceIP != "" {
		if !isValidIP(ue.Network.SourceIP) {
			return fmt.Errorf("network event has invalid source IP: %s", ue.Network.SourceIP)
		}
	}

	if ue.Network.DestIP != "" {
		if !isValidIP(ue.Network.DestIP) {
			return fmt.Errorf("network event has invalid destination IP: %s", ue.Network.DestIP)
		}
	}

	// Port validation not needed for uint16 as it's already 0-65535 by definition
	// Could validate if port 0 is allowed based on protocol
	if ue.Network.Protocol == "TCP" && ue.Network.SourcePort == 0 && ue.Network.DestPort == 0 {
		return fmt.Errorf("TCP network event missing both source and destination ports")
	}

	return nil
}

// validateApplicationData validates application-specific event data
func (ev *EventValidator) validateApplicationData(ue *domain.UnifiedEvent) error {
	if ue.Application == nil {
		return fmt.Errorf("application event missing application data")
	}

	// Validate application data fields
	if ue.Application.Level == "" {
		return fmt.Errorf("application event missing log level")
	}

	if ue.Application.Message == "" {
		return fmt.Errorf("application event missing message")
	}

	// Logger name is optional but recommended
	if ue.Application.Logger == "" {
		// Could log a warning but not an error
	}

	return nil
}

// validateInfrastructureData validates infrastructure-specific event data
func (ev *EventValidator) validateInfrastructureData(ue *domain.UnifiedEvent) error {
	// Infrastructure events should have either Kubernetes or Entity data
	if ue.Kubernetes == nil && ue.Entity == nil {
		return fmt.Errorf("infrastructure event missing both kubernetes and entity data")
	}

	// If Kubernetes data exists, validate it
	if ue.Kubernetes != nil {
		if ue.Kubernetes.EventType == "" {
			return fmt.Errorf("kubernetes event missing event type")
		}
		if ue.Kubernetes.Object == "" {
			return fmt.Errorf("kubernetes event missing object reference")
		}
	}

	// If Entity data exists, validate it
	if ue.Entity != nil {
		if ue.Entity.Type == "" {
			return fmt.Errorf("entity missing type")
		}
		if ue.Entity.Name == "" {
			return fmt.Errorf("entity missing name")
		}
	}

	return nil
}

// validateProcessData validates process-specific event data
func (ev *EventValidator) validateProcessData(ue *domain.UnifiedEvent) error {
	// Process events typically have kernel data
	if ue.Kernel == nil {
		return fmt.Errorf("process event missing kernel data")
	}

	// Process events should have PID
	if ue.Kernel.PID == 0 {
		return fmt.Errorf("process event missing PID")
	}

	// Process events should have comm or syscall
	if ue.Kernel.Comm == "" && ue.Kernel.Syscall == "" {
		return fmt.Errorf("process event missing process name and syscall")
	}

	return nil
}

// isValidIP performs basic IP address validation
func isValidIP(ip string) bool {
	// Basic validation - check for IPv4 format
	// In production, use net.ParseIP for proper validation
	if ip == "" {
		return false
	}

	// Simple check for dots in IPv4 or colons in IPv6
	hasDots := false
	hasColons := false
	for _, c := range ip {
		if c == '.' {
			hasDots = true
		} else if c == ':' {
			hasColons = true
		}
	}

	// Should have either dots (IPv4) or colons (IPv6), not both
	return (hasDots && !hasColons) || (!hasDots && hasColons)
}
