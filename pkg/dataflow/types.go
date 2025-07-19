package dataflow

import (
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Finding extends correlation.Finding with helper methods
type Finding struct {
	*correlation.Finding
}

// GetRelatedEventIDs returns the IDs of all related events
func (f *Finding) GetRelatedEventIDs() []string {
	if f.Finding == nil || f.Finding.RelatedEvents == nil {
		return []string{}
	}

	ids := make([]string, 0, len(f.Finding.RelatedEvents))
	for _, event := range f.Finding.RelatedEvents {
		ids = append(ids, string(event.ID))
	}
	return ids
}

// WrapFinding wraps a correlation finding with helper methods
func WrapFinding(finding *correlation.Finding) *Finding {
	return &Finding{Finding: finding}
}

// CorrelationExtensions adds helper methods for correlation types
type CorrelationExtensions struct{}

// GetRelatedEventIDs extracts event IDs from a correlation finding
func (ce *CorrelationExtensions) GetRelatedEventIDs(finding *correlation.Finding) []string {
	if finding == nil || finding.RelatedEvents == nil {
		return []string{}
	}

	ids := make([]string, 0, len(finding.RelatedEvents))
	for _, event := range finding.RelatedEvents {
		ids = append(ids, string(event.ID))
	}
	return ids
}

// ExtractEventIDs is a utility function to get event IDs from domain events
func ExtractEventIDs(events []*domain.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event != nil {
			ids = append(ids, string(event.ID))
		}
	}
	return ids
}
