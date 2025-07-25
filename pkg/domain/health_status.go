package domain

// SimpleHealthStatus provides a basic implementation of the HealthStatus interface
type SimpleHealthStatus struct {
	status  HealthStatusValue
	message string
	details map[string]interface{}
}

// NewHealthStatus creates a new SimpleHealthStatus
func NewHealthStatus(status HealthStatusValue, message string, details map[string]interface{}) HealthStatus {
	if details == nil {
		details = make(map[string]interface{})
	}
	return &SimpleHealthStatus{
		status:  status,
		message: message,
		details: details,
	}
}

// NewHealthStatusFromValue creates a HealthStatus from just a status value (for backward compatibility)
func NewHealthStatusFromValue(status HealthStatusValue) HealthStatus {
	message := ""
	switch status {
	case HealthHealthy:
		message = "Service is healthy"
	case HealthDegraded:
		message = "Service is degraded"
	case HealthUnhealthy:
		message = "Service is unhealthy"
	case HealthUnknown:
		message = "Service health is unknown"
	default:
		message = "Unknown health status"
	}

	return NewHealthStatus(status, message, nil)
}

// Status returns the health status value
func (h *SimpleHealthStatus) Status() HealthStatusValue {
	return h.status
}

// Message returns a human-readable health message
func (h *SimpleHealthStatus) Message() string {
	return h.message
}

// Details returns collector-specific health details
func (h *SimpleHealthStatus) Details() map[string]interface{} {
	// Return a copy to prevent external modification
	result := make(map[string]interface{}, len(h.details))
	for k, v := range h.details {
		result[k] = v
	}
	return result
}

// StringToHealthStatusValue converts a string to HealthStatusValue
func StringToHealthStatusValue(s string) HealthStatusValue {
	switch s {
	case "healthy":
		return HealthHealthy
	case "degraded":
		return HealthDegraded
	case "unhealthy":
		return HealthUnhealthy
	case "unknown":
		return HealthUnknown
	default:
		return HealthUnknown
	}
}
