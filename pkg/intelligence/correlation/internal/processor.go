package internal
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
// eventProcessor implements core.EventProcessor
type eventProcessor struct {
	enrichmentRules []enrichmentRule
}
// enrichmentRule defines rules for event enrichment
type enrichmentRule struct {
	Name      string
	Condition func(domain.Event) bool
	Action    func(domain.Event) domain.Event
}
// NewEventProcessor creates a new event processor
func NewEventProcessor() core.EventProcessor {
	processor := &eventProcessor{
		enrichmentRules: make([]enrichmentRule, 0),
	}
	// Register default enrichment rules
	processor.registerDefaultEnrichmentRules()
	return processor
}
// Process processes an event through the full pipeline
func (p *eventProcessor) Process(ctx context.Context, event domain.Event) error {
	// Validate the event
	if err := p.Validate(event); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}
	// Preprocess the event
	processedEvent, err := p.Preprocess(event)
	if err != nil {
		return fmt.Errorf("event preprocessing failed: %w", err)
	}
	// Enrich the event
	enrichedEvent, err := p.Enrich(ctx, processedEvent)
	if err != nil {
		return fmt.Errorf("event enrichment failed: %w", err)
	}
	// The enriched event would typically be sent to the next stage
	_ = enrichedEvent
	return nil
}
// Preprocess performs initial event preprocessing
func (p *eventProcessor) Preprocess(event domain.Event) (domain.Event, error) {
	// Create a copy to avoid modifying the original
	processed := event
	// Normalize timestamps
	processed = p.normalizeTimestamps(processed)
	// Standardize labels and tags
	processed = p.standardizeLabelsAndTags(processed)
	// Clean and validate payload
	processed = p.cleanPayload(processed)
	// Set processing metadata
	processed = p.setProcessingMetadata(processed)
	return processed, nil
}
// Validate validates an event
func (p *eventProcessor) Validate(event domain.Event) error {
	// Check required fields
	if event.ID == "" {
		return core.ErrEventValidation
	}
	if event.Source == "" {
		return core.ErrEventValidation
	}
	if event.Type == "" {
		return core.ErrEventValidation
	}
	if event.Timestamp.IsZero() {
		return core.ErrEventValidation
	}
	// Validate confidence range
	if event.Confidence < 0 || event.Confidence > 1 {
		return core.ErrEventValidation
	}
	// Validate severity
	if event.Severity < domain.SeverityDebug || event.Severity > domain.SeverityCritical {
		return core.ErrEventValidation
	}
	// Validate payload based on event type
	if err := p.validatePayload(event); err != nil {
		return fmt.Errorf("payload validation failed: %w", err)
	}
	return nil
}
// Enrich enriches an event with additional context and information
func (p *eventProcessor) Enrich(ctx context.Context, event domain.Event) (domain.Event, error) {
	enriched := event
	// Apply enrichment rules
	for _, rule := range p.enrichmentRules {
		if rule.Condition(enriched) {
			enriched = rule.Action(enriched)
		}
	}
	// Add correlation-specific enrichment
	enriched = p.addCorrelationContext(enriched)
	// Add temporal context
	enriched = p.addTemporalContext(enriched)
	return enriched, nil
}
// AddContext adds additional context to an event
func (p *eventProcessor) AddContext(event domain.Event, context map[string]interface{}) domain.Event {
	enhanced := event
	// Add to metadata annotations
	if enhanced.Metadata.Annotations == nil {
		enhanced.Metadata.Annotations = make(map[string]string)
	}
	for key, value := range context {
		enhanced.Metadata.Annotations[key] = fmt.Sprintf("%v", value)
	}
	return enhanced
}
// Filter checks if an event matches the given criteria
func (p *eventProcessor) Filter(event domain.Event, criteria core.FilterCriteria) bool {
	// Check sources
	if len(criteria.Sources) > 0 {
		sourceMatch := false
		for _, source := range criteria.Sources {
			if event.Source == source {
				sourceMatch = true
				break
			}
		}
		if !sourceMatch {
			return false
		}
	}
	// Check event types
	if len(criteria.EventTypes) > 0 {
		typeMatch := false
		for _, eventType := range criteria.EventTypes {
			if event.Type == eventType {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}
	// Check severities
	if len(criteria.Severities) > 0 {
		severityMatch := false
		for _, severity := range criteria.Severities {
			if event.Severity == severity {
				severityMatch = true
				break
			}
		}
		if !severityMatch {
			return false
		}
	}
	// Check time range
	if !criteria.TimeRange.Start.IsZero() || !criteria.TimeRange.End.IsZero() {
		if !criteria.TimeRange.Start.IsZero() && event.Timestamp.Before(criteria.TimeRange.Start) {
			return false
		}
		if !criteria.TimeRange.End.IsZero() && event.Timestamp.After(criteria.TimeRange.End) {
			return false
		}
	}
	// Check labels
	if len(criteria.Labels) > 0 {
		for key, value := range criteria.Labels {
			if eventValue, exists := event.Context.Labels[key]; !exists || eventValue != value {
				return false
			}
		}
	}
	// Check tags
	if len(criteria.Tags) > 0 {
		for _, requiredTag := range criteria.Tags {
			tagFound := false
			for _, eventTag := range event.Context.Tags {
				if eventTag == requiredTag {
					tagFound = true
					break
				}
			}
			if !tagFound {
				return false
			}
		}
	}
	// Check minimum confidence
	if event.Confidence < criteria.MinConfidence {
		return false
	}
	return true
}
// ShouldProcess determines if an event should be processed
func (p *eventProcessor) ShouldProcess(event domain.Event) bool {
	// Skip events that are too old
	maxAge := 24 * time.Hour
	if time.Since(event.Timestamp) > maxAge {
		return false
	}
	// Skip events with very low confidence
	if event.Confidence < 0.1 {
		return false
	}
	// Skip debug events unless specifically needed
	if event.Severity == domain.SeverityDebug {
		return false
	}
	return true
}
// Helper methods for preprocessing
// normalizeTimestamps ensures timestamps are within reasonable bounds
func (p *eventProcessor) normalizeTimestamps(event domain.Event) domain.Event {
	normalized := event
	// Ensure timestamp is not in the future
	now := time.Now()
	if normalized.Timestamp.After(now) {
		normalized.Timestamp = now
	}
	// Ensure timestamp is not too far in the past
	maxAge := 30 * 24 * time.Hour // 30 days
	if time.Since(normalized.Timestamp) > maxAge {
		normalized.Timestamp = now.Add(-maxAge)
	}
	return normalized
}
// standardizeLabelsAndTags standardizes labels and tags
func (p *eventProcessor) standardizeLabelsAndTags(event domain.Event) domain.Event {
	standardized := event
	// Normalize label keys and values
	if standardized.Context.Labels != nil {
		normalizedLabels := make(map[string]string)
		for key, value := range standardized.Context.Labels {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			normalizedValue := strings.TrimSpace(value)
			if normalizedKey != "" && normalizedValue != "" {
				normalizedLabels[normalizedKey] = normalizedValue
			}
		}
		standardized.Context.Labels = normalizedLabels
	}
	// Normalize tags
	if standardized.Context.Tags != nil {
		var normalizedTags []string
		for _, tag := range standardized.Context.Tags {
			normalizedTag := strings.ToLower(strings.TrimSpace(tag))
			if normalizedTag != "" {
				normalizedTags = append(normalizedTags, normalizedTag)
			}
		}
		standardized.Context.Tags = normalizedTags
	}
	return standardized
}
// cleanPayload performs payload-specific cleaning
func (p *eventProcessor) cleanPayload(event domain.Event) domain.Event {
	cleaned := event
	// Clean based on payload type
	switch payload := cleaned.Payload.(type) {
	case domain.LogEventPayload:
		// Trim whitespace from log messages
		payload.Message = strings.TrimSpace(payload.Message)
		cleaned.Payload = payload
	case domain.ServiceEventPayload:
		// Standardize service names
		payload.ServiceName = strings.ToLower(strings.TrimSpace(payload.ServiceName))
		cleaned.Payload = payload
	case domain.KubernetesEventPayload:
		// Standardize K8s object names  
		payload.Resource.Name = strings.ToLower(strings.TrimSpace(payload.Resource.Name))
		payload.Resource.Namespace = strings.ToLower(strings.TrimSpace(payload.Resource.Namespace))
		cleaned.Payload = payload
	}
	return cleaned
}
// setProcessingMetadata sets metadata related to processing
func (p *eventProcessor) setProcessingMetadata(event domain.Event) domain.Event {
	processed := event
	// Initialize metadata if needed
	if processed.Metadata.Annotations == nil {
		processed.Metadata.Annotations = make(map[string]string)
	}
	// Add processing timestamp
	processed.Metadata.Annotations["processed_at"] = time.Now().Format(time.RFC3339)
	processed.Metadata.Annotations["processed_by"] = "correlation_engine"
	return processed
}
// validatePayload validates event payload based on type
func (p *eventProcessor) validatePayload(event domain.Event) error {
	switch payload := event.Payload.(type) {
	case domain.MemoryEventPayload:
		if payload.Usage < 0 || payload.Usage > 100 {
			return fmt.Errorf("invalid memory usage: %f", payload.Usage)
		}
	case domain.NetworkEventPayload:
		if payload.BytesSent < 0 || payload.BytesReceived < 0 {
			return fmt.Errorf("invalid network byte counts")
		}
	case domain.LogEventPayload:
		if payload.Message == "" {
			return fmt.Errorf("log message cannot be empty")
		}
	case domain.ServiceEventPayload:
		if payload.ServiceName == "" {
			return fmt.Errorf("service name cannot be empty")
		}
	case domain.KubernetesEventPayload:
		if payload.Resource.Name == "" {
			return fmt.Errorf("kubernetes object name cannot be empty")
		}
	}
	return nil
}
// registerDefaultEnrichmentRules registers default enrichment rules
func (p *eventProcessor) registerDefaultEnrichmentRules() {
	// Rule 1: Add correlation tags for memory events
	p.enrichmentRules = append(p.enrichmentRules, enrichmentRule{
		Name: "memory_correlation_tags",
		Condition: func(event domain.Event) bool {
			return event.Type == domain.EventTypeMemory
		},
		Action: func(event domain.Event) domain.Event {
			enhanced := event
			enhanced.Context.Tags = append(enhanced.Context.Tags, "memory", "resource")
			return enhanced
		},
	})
	// Rule 2: Add correlation tags for network events
	p.enrichmentRules = append(p.enrichmentRules, enrichmentRule{
		Name: "network_correlation_tags",
		Condition: func(event domain.Event) bool {
			return event.Type == domain.EventTypeNetwork
		},
		Action: func(event domain.Event) domain.Event {
			enhanced := event
			enhanced.Context.Tags = append(enhanced.Context.Tags, "network", "connectivity")
			return enhanced
		},
	})
	// Rule 3: Add correlation tags for service events
	p.enrichmentRules = append(p.enrichmentRules, enrichmentRule{
		Name: "service_correlation_tags",
		Condition: func(event domain.Event) bool {
			return event.Type == domain.EventTypeService
		},
		Action: func(event domain.Event) domain.Event {
			enhanced := event
			enhanced.Context.Tags = append(enhanced.Context.Tags, "service", "availability")
			return enhanced
		},
	})
	// Rule 4: Boost confidence for critical events
	p.enrichmentRules = append(p.enrichmentRules, enrichmentRule{
		Name: "critical_event_boost",
		Condition: func(event domain.Event) bool {
			return event.Severity == domain.SeverityCritical
		},
		Action: func(event domain.Event) domain.Event {
			enhanced := event
			// Boost confidence slightly for critical events
			enhanced.Confidence = enhanced.Confidence * 1.1
			if enhanced.Confidence > 1.0 {
				enhanced.Confidence = 1.0
			}
			return enhanced
		},
	})
	// Rule 5: Add source reliability indicators
	p.enrichmentRules = append(p.enrichmentRules, enrichmentRule{
		Name: "source_reliability",
		Condition: func(event domain.Event) bool {
			return true // Apply to all events
		},
		Action: func(event domain.Event) domain.Event {
			enhanced := event
			if enhanced.Metadata.Annotations == nil {
				enhanced.Metadata.Annotations = make(map[string]string)
			}
			reliability := p.getSourceReliability(event.Source)
			enhanced.Metadata.Annotations["source_reliability"] = fmt.Sprintf("%.2f", reliability)
			return enhanced
		},
	})
}
// addCorrelationContext adds context relevant to correlation analysis
func (p *eventProcessor) addCorrelationContext(event domain.Event) domain.Event {
	enhanced := event
	if enhanced.Metadata.Annotations == nil {
		enhanced.Metadata.Annotations = make(map[string]string)
	}
	// Add correlation readiness indicators
	enhanced.Metadata.Annotations["correlation_ready"] = "true"
	enhanced.Metadata.Annotations["correlation_priority"] = p.getCorrelationPriority(event)
	// Add event fingerprint for deduplication
	enhanced.Metadata.Annotations["event_fingerprint"] = p.generateEventFingerprint(event)
	return enhanced
}
// addTemporalContext adds temporal context for correlation
func (p *eventProcessor) addTemporalContext(event domain.Event) domain.Event {
	enhanced := event
	if enhanced.Metadata.Annotations == nil {
		enhanced.Metadata.Annotations = make(map[string]string)
	}
	// Add temporal buckets for grouping
	enhanced.Metadata.Annotations["time_bucket_1m"] = event.Timestamp.Truncate(time.Minute).Format(time.RFC3339)
	enhanced.Metadata.Annotations["time_bucket_5m"] = event.Timestamp.Truncate(5 * time.Minute).Format(time.RFC3339)
	enhanced.Metadata.Annotations["time_bucket_15m"] = event.Timestamp.Truncate(15 * time.Minute).Format(time.RFC3339)
	// Add day of week and hour for pattern detection
	enhanced.Metadata.Annotations["day_of_week"] = event.Timestamp.Weekday().String()
	enhanced.Metadata.Annotations["hour_of_day"] = fmt.Sprintf("%d", event.Timestamp.Hour())
	return enhanced
}
// Helper methods
func (p *eventProcessor) getSourceReliability(source domain.Source) float64 {
	reliability := map[domain.Source]float64{
		domain.SourceEBPF:       0.95,
		domain.SourceKubernetes: 0.90,
		domain.SourceSystemd:    0.85,
		domain.SourceJournald:   0.80,
	}
	if score, exists := reliability[source]; exists {
		return score
	}
	return 0.70 // Default
}
func (p *eventProcessor) getCorrelationPriority(event domain.Event) string {
	if event.Severity >= domain.SeverityError {
		return "high"
	} else if event.Severity >= domain.SeverityWarn {
		return "medium"
	} else {
		return "low"
	}
}
func (p *eventProcessor) generateEventFingerprint(event domain.Event) string {
	// Simple fingerprint based on key event characteristics
	return fmt.Sprintf("%s_%s_%s_%d", 
		event.Source, 
		event.Type, 
		event.Context.Host, 
		event.Timestamp.Unix()/60) // Minute-level granularity
}