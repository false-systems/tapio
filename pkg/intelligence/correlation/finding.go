package correlation

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Finding represents a correlation finding with semantic context
type Finding struct {
	ID            string
	PatternType   string
	Confidence    float64
	RelatedEvents []*domain.Event
	SemanticGroup *SemanticGroupSummary
	Timestamp     time.Time
	Description   string
}

// SemanticGroupSummary is a simplified view of SemanticTraceGroup for findings
type SemanticGroupSummary struct {
	ID         string
	Intent     string
	Type       string
	Impact     *ImpactAssessment
	Prediction *PredictedOutcome
}
