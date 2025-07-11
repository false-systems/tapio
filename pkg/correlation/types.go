package correlation

import (
	"time"
)

// Event represents a system event that can be correlated
type Event struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Source      SourceType `json:"source"`
	Timestamp   time.Time  `json:"timestamp"`
	Description string     `json:"description"`
	Confidence  float64    `json:"confidence"`
	PID         uint32     `json:"pid,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// FindingType represents additional fields that might be used in telemetry
type FindingType struct {
	Finding
	Type   string  `json:"type"`   // Type of finding (e.g., "memory_pressure", "cpu_throttling")
	Impact string  `json:"impact"` // Impact assessment
}

// GetType returns the type of finding from metadata or tags
func (f *Finding) GetType() string {
	// Check metadata first
	if f.Metadata != nil {
		if typeVal, ok := f.Metadata["type"].(string); ok {
			return typeVal
		}
	}
	
	// Fall back to first tag if available
	if len(f.Tags) > 0 {
		return f.Tags[0]
	}
	
	// Default based on severity
	return f.Severity.String()
}

// GetImpact returns the impact of the finding
func (f *Finding) GetImpact() string {
	// Check metadata first
	if f.Metadata != nil {
		if impactVal, ok := f.Metadata["impact"].(string); ok {
			return impactVal
		}
	}
	
	// Generate impact based on severity and confidence
	if f.Severity == SeverityCritical && f.Confidence > 0.8 {
		return "critical"
	} else if f.Severity == SeverityError || (f.Severity == SeverityWarning && f.Confidence > 0.7) {
		return "high"
	} else if f.Severity == SeverityWarning {
		return "medium"
	}
	return "low"
}

// GetResourceName returns the resource name from the finding
func (f *Finding) GetResourceName() string {
	// Check if resource is defined
	if f.Resource != nil {
		return f.Resource.Name
	}
	
	// Check metadata for resource name
	if f.Metadata != nil {
		if resourceName, ok := f.Metadata["resource_name"].(string); ok {
			return resourceName
		}
	}
	
	return "unknown"
}