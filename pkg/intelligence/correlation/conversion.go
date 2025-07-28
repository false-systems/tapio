feat/k8s-advanced-collector



main
package correlation

import (
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// ConvertToInterfacesFinding converts internal Finding to interfaces.Finding
func ConvertToInterfacesFinding(f *Finding) *interfaces.Finding {
	if f == nil {
		return nil
	}

	// Convert SemanticGroupSummary to interfaces.SemanticGroup
	var semanticGroup *interfaces.SemanticGroup
	if f.SemanticGroup != nil {
		semanticGroup = &interfaces.SemanticGroup{
			ID:     f.SemanticGroup.ID,
			Intent: f.SemanticGroup.Intent,
			Type:   f.SemanticGroup.Type,
		}
	}

	// For backward compatibility, use RelatedEvents if available
	// Otherwise, convert UnifiedEvents to Events
	relatedEvents := f.RelatedEvents
	if len(relatedEvents) == 0 && len(f.RelatedUnifiedEvents) > 0 {
		relatedEvents = make([]*domain.Event, 0, len(f.RelatedUnifiedEvents))
		for _, ue := range f.RelatedUnifiedEvents {
			// Convert UnifiedEvent to Event for interface compatibility
			event := &domain.Event{
				ID:        domain.EventID(ue.ID),
				Type:      ue.Type,
				Timestamp: ue.Timestamp,
				Source:    domain.SourceType(ue.Source),
			}
			relatedEvents = append(relatedEvents, event)
		}
	}

	return &interfaces.Finding{
		ID:            f.ID,
		Confidence:    f.Confidence,
		PatternType:   f.PatternType,
		Description:   f.Description,
		RelatedEvents: relatedEvents,
		SemanticGroup: semanticGroup,
	}
}

// ConvertFromInterfacesFinding converts interfaces.Finding to internal Finding
func ConvertFromInterfacesFinding(f *interfaces.Finding) *Finding {
	if f == nil {
		return nil
	}

	// Convert interfaces.SemanticGroup to SemanticGroupSummary
	var semanticGroup *SemanticGroupSummary
	if f.SemanticGroup != nil {
		semanticGroup = &SemanticGroupSummary{
			ID:     f.SemanticGroup.ID,
			Intent: f.SemanticGroup.Intent,
			Type:   f.SemanticGroup.Type,
		}
	}

	return &Finding{
		ID:            f.ID,
		PatternType:   f.PatternType,
		Confidence:    f.Confidence,
		Description:   f.Description,
		RelatedEvents: f.RelatedEvents,
		SemanticGroup: semanticGroup,
	}
}
