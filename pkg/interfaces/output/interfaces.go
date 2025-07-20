package output

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// HumanOutputGenerator converts technical events into human-readable insights
type HumanOutputGenerator interface {
	// GenerateInsight converts a finding into human-readable text
	GenerateInsight(ctx context.Context, finding *domain.Finding) (*HumanInsight, error)

	// GenerateEventExplanation converts an event into human-readable text
	GenerateEventExplanation(ctx context.Context, event *domain.Event) (*HumanInsight, error)

	// GenerateReport creates a human-readable report from multiple findings
	GenerateReport(ctx context.Context, findings []*domain.Finding) (*HumanReport, error)

	// GenerateSummary creates a summary of system state from events
	GenerateSummary(ctx context.Context, events []*domain.Event) (*HumanSummary, error)
}
