package engine

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/performance"
)

// ValidationStage validates events before processing
type ValidationStage struct {
	name string
}

// NewValidationStage creates a new validation stage
func NewValidationStage(name string) *ValidationStage {
	return &ValidationStage{name: name}
}

func (s *ValidationStage) Name() string {
	return s.name
}

func (s *ValidationStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Extract unified event from metadata
	unifiedEvent := (*domain.UnifiedEvent)(unsafe.Pointer(uintptr(event.Metadata[0])))

	// Validate required fields
	if unifiedEvent.ID == "" {
		return nil, fmt.Errorf("event ID is required")
	}
	if unifiedEvent.Timestamp.IsZero() {
		return nil, fmt.Errorf("event timestamp is required")
	}
	if unifiedEvent.Type == "" {
		return nil, fmt.Errorf("event type is required")
	}
	if unifiedEvent.Source == "" {
		return nil, fmt.Errorf("event source is required")
	}

	// Validate layer-specific data
	layerCount := 0
	if unifiedEvent.Kernel != nil {
		layerCount++
	}
	if unifiedEvent.Network != nil {
		layerCount++
	}
	if unifiedEvent.Application != nil {
		layerCount++
	}
	if unifiedEvent.Kubernetes != nil {
		layerCount++
	}
	if unifiedEvent.Metrics != nil {
		layerCount++
	}

	// At least one layer should be populated
	if layerCount == 0 {
		return nil, fmt.Errorf("event must have at least one layer of data")
	}

	// Mark as validated
	event.Metadata[1] = 1 // Validation flag

	return event, nil
}

func (s *ValidationStage) CanProcess(event *performance.Event) bool {
	// Process all events that haven't been validated yet
	return event.Metadata[1] == 0
}

// EnrichmentStage enriches events with additional context
type EnrichmentStage struct {
	name string
}

// NewEnrichmentStage creates a new enrichment stage
func NewEnrichmentStage(name string) *EnrichmentStage {
	return &EnrichmentStage{name: name}
}

func (s *EnrichmentStage) Name() string {
	return s.name
}

func (s *EnrichmentStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Extract unified event from metadata
	unifiedEvent := (*domain.UnifiedEvent)(unsafe.Pointer(uintptr(event.Metadata[0])))

	// Enrich with default trace context if missing
	if !unifiedEvent.HasTraceContext() {
		unifiedEvent.TraceContext = &domain.TraceContext{
			TraceID: fmt.Sprintf("synthetic-%s", unifiedEvent.ID),
			SpanID:  unifiedEvent.ID,
			Sampled: true,
		}
	}

	// Enrich with severity if missing
	if unifiedEvent.Impact == nil {
		severity := inferSeverity(unifiedEvent)
		unifiedEvent.Impact = &domain.ImpactContext{
			Severity: severity,
		}
	}

	// Enrich with semantic intent if missing
	if unifiedEvent.Semantic == nil {
		intent := inferSemanticIntent(unifiedEvent)
		if intent != "" {
			unifiedEvent.Semantic = &domain.SemanticContext{
				Intent:     intent,
				Category:   inferCategory(unifiedEvent),
				Confidence: 0.7, // Medium confidence for inferred semantics
			}
		}
	}

	// Enrich correlation context
	if unifiedEvent.Correlation == nil {
		unifiedEvent.Correlation = &domain.CorrelationContext{
			CorrelationID: unifiedEvent.TraceContext.TraceID,
		}
	}

	// Mark as enriched
	event.Metadata[2] = 1 // Enrichment flag

	return event, nil
}

func (s *EnrichmentStage) CanProcess(event *performance.Event) bool {
	// Process validated events that haven't been enriched
	return event.Metadata[1] == 1 && event.Metadata[2] == 0
}

// CorrelationStage performs event correlation
type CorrelationStage struct {
	name             string
	correlationCache map[string]*correlationGroup
}

type correlationGroup struct {
	id         string
	events     []string
	lastUpdate time.Time
	pattern    string
}

// NewCorrelationStage creates a new correlation stage
func NewCorrelationStage(name string) *CorrelationStage {
	return &CorrelationStage{
		name:             name,
		correlationCache: make(map[string]*correlationGroup),
	}
}

func (s *CorrelationStage) Name() string {
	return s.name
}

func (s *CorrelationStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Extract unified event from metadata
	unifiedEvent := (*domain.UnifiedEvent)(unsafe.Pointer(uintptr(event.Metadata[0])))

	// Find or create correlation group
	groupID := unifiedEvent.TraceContext.TraceID
	group, exists := s.correlationCache[groupID]
	if !exists {
		group = &correlationGroup{
			id:         groupID,
			events:     []string{},
			lastUpdate: time.Now(),
		}
		s.correlationCache[groupID] = group
	}

	// Add event to group
	group.events = append(group.events, unifiedEvent.ID)
	group.lastUpdate = time.Now()

	// Detect patterns
	if len(group.events) >= 3 {
		pattern := detectPattern(unifiedEvent, len(group.events))
		if pattern != "" {
			group.pattern = pattern
			unifiedEvent.Correlation.Pattern = pattern
		}
	}

	// Update correlation context
	if len(group.events) > 1 {
		unifiedEvent.Correlation.RelatedEvents = group.events

		// Build causal chain for certain patterns
		if isCausalPattern(unifiedEvent) {
			unifiedEvent.Correlation.CausalChain = buildCausalChain(group.events)
		}
	}

	// Clean old groups (simple TTL)
	for id, g := range s.correlationCache {
		if time.Since(g.lastUpdate) > 5*time.Minute {
			delete(s.correlationCache, id)
		}
	}

	// Mark as correlated
	event.Metadata[3] = 1 // Correlation flag

	return event, nil
}

func (s *CorrelationStage) CanProcess(event *performance.Event) bool {
	// Process enriched events
	return event.Metadata[2] == 1
}

// AnalyticsStage performs final analytics
type AnalyticsStage struct {
	name string
}

// NewAnalyticsStage creates a new analytics stage
func NewAnalyticsStage(name string) *AnalyticsStage {
	return &AnalyticsStage{name: name}
}

func (s *AnalyticsStage) Name() string {
	return s.name
}

func (s *AnalyticsStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Extract unified event from metadata
	unifiedEvent := (*domain.UnifiedEvent)(unsafe.Pointer(uintptr(event.Metadata[0])))

	// Calculate event score
	score := calculateEventScore(unifiedEvent)
	event.Metadata[4] = uint64(score * 100) // Store score as percentage

	// Determine if this is an anomaly
	if isAnomaly(unifiedEvent, score) {
		event.Metadata[5] = 1 // Anomaly flag
		event.Priority = 0    // Highest priority for anomalies
	}

	// Mark processing time
	processingTime := time.Since(time.Unix(0, event.Timestamp))
	event.Metadata[6] = uint64(processingTime.Nanoseconds())

	// Mark as analyzed
	event.Metadata[7] = 1 // Analytics flag

	return event, nil
}

func (s *AnalyticsStage) CanProcess(event *performance.Event) bool {
	// Process all correlated events
	return event.Metadata[3] == 1
}

// Helper functions

func inferSeverity(event *domain.UnifiedEvent) string {
	// Kernel events
	if event.IsKernelEvent() && event.Kernel != nil {
		if event.Kernel.Syscall == "oom_kill" {
			return "critical"
		}
		if event.Kernel.ReturnCode < 0 {
			return "high"
		}
	}

	// Network events
	if event.IsNetworkEvent() && event.Network != nil {
		if event.Network.StatusCode >= 500 {
			return "high"
		}
		if event.Network.StatusCode >= 400 {
			return "medium"
		}
		if event.Network.Latency > 5000000000 { // > 5 seconds
			return "high"
		}
	}

	// Application events
	if event.IsApplicationEvent() && event.Application != nil {
		switch event.Application.Level {
		case "critical", "fatal":
			return "critical"
		case "error":
			return "high"
		case "warn", "warning":
			return "medium"
		}
	}

	// Kubernetes events
	if event.IsKubernetesEvent() && event.Kubernetes != nil {
		if event.Kubernetes.EventType == "Warning" {
			switch event.Kubernetes.Reason {
			case "OOMKilled", "CrashLoopBackOff":
				return "critical"
			case "BackOff", "FailedScheduling":
				return "high"
			default:
				return "medium"
			}
		}
	}

	return "low"
}

func inferSemanticIntent(event *domain.UnifiedEvent) string {
	// Kernel intents
	if event.IsKernelEvent() && event.Kernel != nil {
		switch event.Kernel.Syscall {
		case "oom_kill":
			return "oom-kill"
		case "connect":
			if event.Kernel.ReturnCode < 0 {
				return "connection-failed"
			}
		case "open", "openat":
			if event.Kernel.ReturnCode < 0 {
				return "file-access-denied"
			}
		}
	}

	// Network intents
	if event.IsNetworkEvent() && event.Network != nil {
		if event.Network.StatusCode == 429 {
			return "rate-limit"
		}
		if event.Network.StatusCode == 401 || event.Network.StatusCode == 403 {
			return "authentication-failure"
		}
		if event.Network.StatusCode >= 500 {
			return "service-error"
		}
		if event.Network.StatusCode == 0 {
			return "connection-timeout"
		}
		if event.Network.Latency > 10000000000 { // > 10 seconds
			return "slow-response"
		}
	}

	// Application intents
	if event.IsApplicationEvent() && event.Application != nil {
		if event.Application.ErrorType != "" {
			// Simple intent inference from error type
			return fmt.Sprintf("%s-error", event.Application.ErrorType)
		}
	}

	// Kubernetes intents
	if event.IsKubernetesEvent() && event.Kubernetes != nil {
		switch event.Kubernetes.Reason {
		case "BackOff", "CrashLoopBackOff":
			return "container-crash"
		case "OOMKilled":
			return "oom-kill"
		case "FailedScheduling":
			return "resource-exhaustion"
		case "FailedMount":
			return "storage-failure"
		}

		if event.Kubernetes.Action == "ADDED" && event.Kubernetes.ObjectKind == "Deployment" {
			return "deployment-started"
		}
	}

	return ""
}

func inferCategory(event *domain.UnifiedEvent) string {
	if event.Semantic != nil && event.Semantic.Category != "" {
		return event.Semantic.Category
	}

	severity := event.GetSeverity()
	switch severity {
	case "critical", "high":
		return "availability"
	}

	if event.IsNetworkEvent() {
		return "performance"
	}
	if event.IsKernelEvent() {
		return "system"
	}
	if event.IsApplicationEvent() {
		return "application"
	}

	return "general"
}

func detectPattern(event *domain.UnifiedEvent, groupSize int) string {
	// Simple pattern detection based on event characteristics
	if event.IsKernelEvent() && event.Kernel != nil && event.Kernel.Syscall == "oom_kill" {
		return "memory-exhaustion-pattern"
	}

	if event.IsNetworkEvent() && event.Network != nil {
		if event.Network.StatusCode >= 500 && groupSize > 5 {
			return "service-degradation-pattern"
		}
		if event.Network.StatusCode == 429 && groupSize > 3 {
			return "rate-limiting-pattern"
		}
	}

	if event.IsApplicationEvent() && event.Application != nil {
		if event.Application.Level == "error" && groupSize > 10 {
			return "error-spike-pattern"
		}
	}

	if groupSize > 20 {
		return "high-volume-pattern"
	}

	return ""
}

func isCausalPattern(event *domain.UnifiedEvent) bool {
	// Determine if this event type typically has causal relationships
	if event.Correlation != nil && event.Correlation.Pattern != "" {
		switch event.Correlation.Pattern {
		case "memory-exhaustion-pattern",
			"service-degradation-pattern",
			"error-spike-pattern":
			return true
		}
	}

	// OOM kills cause cascading failures
	if event.IsKernelEvent() && event.Kernel != nil && event.Kernel.Syscall == "oom_kill" {
		return true
	}

	// Service errors can cascade
	if event.IsNetworkEvent() && event.Network != nil && event.Network.StatusCode >= 500 {
		return true
	}

	return false
}

func buildCausalChain(events []string) []string {
	// Simple causal chain - in production this would use graph analysis
	if len(events) <= 1 {
		return events
	}

	// For now, return events in order (oldest to newest)
	return events
}

func calculateEventScore(event *domain.UnifiedEvent) float64 {
	score := 0.5 // Base score

	// Severity contributes to score
	switch event.GetSeverity() {
	case "critical":
		score += 0.4
	case "high":
		score += 0.3
	case "medium":
		score += 0.2
	case "low":
		score += 0.1
	}

	// Impact contributes to score
	if event.Impact != nil {
		score += event.Impact.BusinessImpact * 0.3
		if event.Impact.CustomerFacing {
			score += 0.1
		}
		if event.Impact.RevenueImpacting {
			score += 0.1
		}
	}

	// Correlation patterns increase score
	if event.Correlation != nil && event.Correlation.Pattern != "" {
		score += 0.2
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

func isAnomaly(event *domain.UnifiedEvent, score float64) bool {
	// High score events are potential anomalies
	if score > 0.8 {
		return true
	}

	// Specific patterns are anomalies
	if event.Correlation != nil {
		switch event.Correlation.Pattern {
		case "memory-exhaustion-pattern",
			"service-degradation-pattern",
			"error-spike-pattern":
			return true
		}
	}

	// Critical events are anomalies
	if event.GetSeverity() == "critical" {
		return true
	}

	return false
}
