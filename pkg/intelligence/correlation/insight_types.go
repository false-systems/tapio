package correlation

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationInsight extends domain.Insight with correlation-specific fields
type CorrelationInsight struct {
	// Embed domain.Insight
	domain.Insight

	// Additional correlation fields
	RelatedEvents []string           `json:"related_events,omitempty"`
	Resources     []AffectedResource `json:"resources,omitempty"`
	Actions       []ActionableItem   `json:"actions,omitempty"`
	Prediction    *Prediction        `json:"prediction,omitempty"`
}

// ToInsight converts CorrelationInsight to domain.Insight
func (ci CorrelationInsight) ToInsight() domain.Insight {
	insight := ci.Insight

	// Add correlation-specific data to metadata
	if insight.Metadata == nil {
		insight.Metadata = make(map[string]interface{})
	}

	if len(ci.RelatedEvents) > 0 {
		insight.Metadata["related_events"] = ci.RelatedEvents
	}

	if len(ci.Resources) > 0 {
		insight.Metadata["affected_resources"] = ci.Resources
	}

	if len(ci.Actions) > 0 {
		insight.Metadata["recommended_actions"] = ci.Actions
	}

	if ci.Prediction != nil {
		insight.Metadata["prediction"] = ci.Prediction
	}

	return insight
}

// NewCorrelationInsight creates a new CorrelationInsight
func NewCorrelationInsight() CorrelationInsight {
	return CorrelationInsight{
		Insight: domain.Insight{
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		},
		RelatedEvents: []string{},
		Resources:     []AffectedResource{},
		Actions:       []ActionableItem{},
	}
}
