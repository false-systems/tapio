package grpc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RealTimeCorrelationEngine implements CorrelationEngine with real-time pattern detection
type RealTimeCorrelationEngine struct {
	logger *zap.Logger

	// Pattern matchers
	patterns []PatternMatcher

	// Correlation storage
	mu             sync.RWMutex
	correlations   map[string]*pb.Correlation
	semanticGroups map[string]*pb.SemanticGroup

	// Event buffers for correlation analysis
	recentEvents *CircularEventBuffer
	eventIndex   map[string][]*pb.Correlation // event ID -> correlations

	// Configuration
	config CorrelationConfig

	// Statistics
	eventsProcessed   atomic.Uint64
	correlationsFound atomic.Uint64
	patternsMatched   atomic.Uint64

	// Lifecycle
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// CorrelationConfig holds correlation engine configuration
type CorrelationConfig struct {
	BufferSize        int
	TimeWindow        time.Duration
	MaxCorrelations   int
	PatternConfidence float64
	CleanupInterval   time.Duration
	RetentionPeriod   time.Duration
}

// PatternMatcher defines the interface for correlation pattern matchers
type PatternMatcher interface {
	Name() string
	Description() string
	Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool)
}

// CorrelationMatch represents a pattern match result
type CorrelationMatch struct {
	Pattern     string
	Confidence  float64
	Description string
	EventIDs    []string
	Metadata    map[string]string
}

// CircularEventBuffer maintains a sliding window of recent events
type CircularEventBuffer struct {
	mu       sync.RWMutex
	events   []*domain.UnifiedEvent
	capacity int
	head     int
	size     int
}

// NewRealTimeCorrelationEngine creates a new correlation engine
func NewRealTimeCorrelationEngine(logger *zap.Logger, config CorrelationConfig) *RealTimeCorrelationEngine {
	engine := &RealTimeCorrelationEngine{
		logger:         logger,
		correlations:   make(map[string]*pb.Correlation),
		semanticGroups: make(map[string]*pb.SemanticGroup),
		eventIndex:     make(map[string][]*pb.Correlation),
		recentEvents:   NewCircularEventBuffer(config.BufferSize),
		config:         config,
		shutdown:       make(chan struct{}),
	}

	// Initialize pattern matchers
	engine.initializePatterns()

	// Start background cleanup
	engine.wg.Add(1)
	go engine.cleanupRoutine()

	return engine
}

// NewCircularEventBuffer creates a new circular buffer
func NewCircularEventBuffer(capacity int) *CircularEventBuffer {
	return &CircularEventBuffer{
		events:   make([]*domain.UnifiedEvent, capacity),
		capacity: capacity,
	}
}

// Add adds an event to the buffer
func (cb *CircularEventBuffer) Add(event *domain.UnifiedEvent) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.events[cb.head] = event
	cb.head = (cb.head + 1) % cb.capacity
	if cb.size < cb.capacity {
		cb.size++
	}
}

// GetRecent returns the N most recent events
func (cb *CircularEventBuffer) GetRecent(n int) []*domain.UnifiedEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if n > cb.size {
		n = cb.size
	}

	events := make([]*domain.UnifiedEvent, n)
	for i := 0; i < n; i++ {
		idx := (cb.head - 1 - i + cb.capacity) % cb.capacity
		events[n-1-i] = cb.events[idx]
	}

	return events
}

// GetTimeWindow returns events within a time window
func (cb *CircularEventBuffer) GetTimeWindow(duration time.Duration) []*domain.UnifiedEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var events []*domain.UnifiedEvent

	for i := 0; i < cb.size; i++ {
		idx := (cb.head - 1 - i + cb.capacity) % cb.capacity
		event := cb.events[idx]
		if event != nil && event.Timestamp.After(cutoff) {
			events = append([]*domain.UnifiedEvent{event}, events...)
		} else {
			break
		}
	}

	return events
}

// ProcessEvent processes a single event for correlations
func (e *RealTimeCorrelationEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) ([]*pb.Correlation, error) {
	e.eventsProcessed.Add(1)

	// Add to recent events buffer
	e.recentEvents.Add(event)

	// Get events in time window for correlation
	windowEvents := e.recentEvents.GetTimeWindow(e.config.TimeWindow)

	// Check all patterns
	var correlations []*pb.Correlation

	for _, matcher := range e.patterns {
		if match, found := matcher.Match(windowEvents); found && match.Confidence >= e.config.PatternConfidence {
			e.patternsMatched.Add(1)

			// Create correlation
			correlation := e.createCorrelation(match, event)
			correlations = append(correlations, correlation)

			// Store correlation
			e.mu.Lock()
			e.correlations[correlation.Id] = correlation
			e.correlationsFound.Add(1)

			// Update event index
			for _, eventID := range match.EventIDs {
				e.eventIndex[eventID] = append(e.eventIndex[eventID], correlation)
			}
			e.mu.Unlock()
		}
	}

	// Update semantic groups if event has semantic context
	if event.Semantic != nil && event.Semantic.Intent != "" {
		e.updateSemanticGroup(event)
	}

	return correlations, nil
}

// GetCorrelations retrieves correlations with filtering
func (e *RealTimeCorrelationEngine) GetCorrelations(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange) ([]*pb.Correlation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var correlations []*pb.Correlation

	for _, corr := range e.correlations {
		if e.matchesFilter(corr, filter) && e.inTimeRange(corr, timeRange) {
			correlations = append(correlations, corr)
		}
	}

	// Sort by creation time (newest first)
	sort.Slice(correlations, func(i, j int) bool {
		return correlations[i].CreatedAt.AsTime().After(correlations[j].CreatedAt.AsTime())
	})

	return correlations, nil
}

// GetSemanticGroups retrieves semantic groups
func (e *RealTimeCorrelationEngine) GetSemanticGroups(ctx context.Context, filter *pb.Filter) ([]*pb.SemanticGroup, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var groups []*pb.SemanticGroup

	for _, group := range e.semanticGroups {
		if e.matchesGroupFilter(group, filter) {
			groups = append(groups, group)
		}
	}

	// Sort by event count (largest first)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].EventCount > groups[j].EventCount
	})

	return groups, nil
}

// AnalyzeEvents performs on-demand correlation analysis
func (e *RealTimeCorrelationEngine) AnalyzeEvents(ctx context.Context, events []*domain.UnifiedEvent) ([]*pb.Correlation, error) {
	var correlations []*pb.Correlation

	// Sort events by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Check each pattern
	for _, matcher := range e.patterns {
		if match, found := matcher.Match(events); found {
			correlation := &pb.Correlation{
				Id:          fmt.Sprintf("correlation-%d", time.Now().UnixNano()),
				Type:        pb.CorrelationType_CORRELATION_TYPE_PATTERN,
				Pattern:     match.Pattern,
				Confidence:  match.Confidence,
				Description: match.Description,
				EventIds:    match.EventIDs,
				Metadata:    match.Metadata,
				CreatedAt:   timestamppb.Now(),
				UpdatedAt:   timestamppb.Now(),
			}
			correlations = append(correlations, correlation)
		}
	}

	// Analyze temporal patterns
	temporalCorrelations := e.analyzeTemporalPatterns(events)
	correlations = append(correlations, temporalCorrelations...)

	// Analyze causal relationships
	causalCorrelations := e.analyzeCausalPatterns(events)
	correlations = append(correlations, causalCorrelations...)

	return correlations, nil
}

// Health returns engine health status
func (e *RealTimeCorrelationEngine) Health() HealthStatus {
	e.mu.RLock()
	correlationCount := len(e.correlations)
	groupCount := len(e.semanticGroups)
	e.mu.RUnlock()

	status := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	message := "Correlation engine is healthy"

	// Check if we're at capacity
	if e.config.MaxCorrelations > 0 && correlationCount > int(float64(e.config.MaxCorrelations)*0.9) {
		status = pb.HealthStatus_HEALTH_STATUS_DEGRADED
		message = "Correlation storage near capacity"
	}

	return HealthStatus{
		Status:      status,
		Message:     message,
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"events_processed":    float64(e.eventsProcessed.Load()),
			"correlations_found":  float64(e.correlationsFound.Load()),
			"patterns_matched":    float64(e.patternsMatched.Load()),
			"active_correlations": float64(correlationCount),
			"semantic_groups":     float64(groupCount),
			"pattern_matchers":    float64(len(e.patterns)),
		},
	}
}

// Close shuts down the correlation engine
func (e *RealTimeCorrelationEngine) Close() error {
	close(e.shutdown)
	e.wg.Wait()
	return nil
}

// initializePatterns sets up pattern matchers
func (e *RealTimeCorrelationEngine) initializePatterns() {
	e.patterns = []PatternMatcher{
		&ErrorCascadePattern{},
		&LatencySpikePattern{},
		&ResourceExhaustionPattern{},
		&ServiceFailurePattern{},
		&SecurityAnomalyPattern{},
		&PerformanceDegradationPattern{},
	}
}

// createCorrelation creates a new correlation from a match
func (e *RealTimeCorrelationEngine) createCorrelation(match *CorrelationMatch, triggerEvent *domain.UnifiedEvent) *pb.Correlation {
	return &pb.Correlation{
		Id:          fmt.Sprintf("corr-%d", time.Now().UnixNano()),
		Type:        pb.CorrelationType_CORRELATION_TYPE_PATTERN,
		Pattern:     match.Pattern,
		Confidence:  match.Confidence,
		Description: match.Description,
		EventIds:    match.EventIDs,
		CreatedAt:   timestamppb.Now(),
		UpdatedAt:   timestamppb.Now(),
		Metadata:    match.Metadata,
	}
}

// updateSemanticGroup updates or creates a semantic group
func (e *RealTimeCorrelationEngine) updateSemanticGroup(event *domain.UnifiedEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	groupID := fmt.Sprintf("sg-%s", event.Semantic.Intent)

	if group, exists := e.semanticGroups[groupID]; exists {
		group.EventCount++
		group.UpdatedAt = timestamppb.Now()
		if len(group.EventIds) < 1000 { // Limit stored event IDs
			group.EventIds = append(group.EventIds, event.ID)
		}
	} else {
		e.semanticGroups[groupID] = &pb.SemanticGroup{
			Id:          groupID,
			Name:        event.Semantic.Intent,
			Description: fmt.Sprintf("Events with semantic intent: %s", event.Semantic.Intent),
			EventCount:  1,
			EventIds:    []string{event.ID},
			CreatedAt:   timestamppb.Now(),
			UpdatedAt:   timestamppb.Now(),
			Metadata: map[string]string{
				"category":   event.Semantic.Category,
				"confidence": fmt.Sprintf("%.2f", event.Semantic.Confidence),
			},
		}
	}
}

// cleanupRoutine periodically cleans up old correlations
func (e *RealTimeCorrelationEngine) cleanupRoutine() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.shutdown:
			return
		case <-ticker.C:
			e.cleanup()
		}
	}
}

// cleanup removes old correlations and groups
func (e *RealTimeCorrelationEngine) cleanup() {
	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-e.config.RetentionPeriod)

	// Clean up correlations
	for id, corr := range e.correlations {
		if corr.CreatedAt.AsTime().Before(cutoff) {
			delete(e.correlations, id)

			// Clean up event index
			for _, eventID := range corr.EventIds {
				e.eventIndex[eventID] = e.removeCorrelationFromSlice(e.eventIndex[eventID], corr.Id)
			}
		}
	}

	// Clean up semantic groups
	for id, group := range e.semanticGroups {
		if group.UpdatedAt.AsTime().Before(cutoff) {
			delete(e.semanticGroups, id)
		}
	}
}

// Helper methods

func (e *RealTimeCorrelationEngine) matchesFilter(corr *pb.Correlation, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	if filter.Query != "" {
		if !contains(corr.Pattern, filter.Query) && !contains(corr.Description, filter.Query) {
			return false
		}
	}

	return true
}

func (e *RealTimeCorrelationEngine) inTimeRange(corr *pb.Correlation, timeRange *pb.TimeRange) bool {
	if timeRange == nil {
		return true
	}

	if timeRange.Start != nil && corr.CreatedAt.AsTime().Before(timeRange.Start.AsTime()) {
		return false
	}

	if timeRange.End != nil && corr.CreatedAt.AsTime().After(timeRange.End.AsTime()) {
		return false
	}

	return true
}

func (e *RealTimeCorrelationEngine) matchesGroupFilter(group *pb.SemanticGroup, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	if filter.Query != "" {
		return contains(group.Name, filter.Query) || contains(group.Description, filter.Query)
	}

	return true
}

func (e *RealTimeCorrelationEngine) removeCorrelationFromSlice(slice []*pb.Correlation, corrID string) []*pb.Correlation {
	for i, corr := range slice {
		if corr.Id == corrID {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// Pattern implementations

// ErrorCascadePattern detects cascading errors across services
type ErrorCascadePattern struct{}

func (p *ErrorCascadePattern) Name() string        { return "error_cascade" }
func (p *ErrorCascadePattern) Description() string { return "Multiple errors from related services" }

func (p *ErrorCascadePattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	errorsByService := make(map[string][]*domain.UnifiedEvent)

	for _, event := range events {
		if event.GetSeverity() == "error" || event.GetSeverity() == "critical" {
			if event.Entity != nil {
				service := event.Entity.Name
				errorsByService[service] = append(errorsByService[service], event)
			}
		}
	}

	// Check if multiple services have errors
	if len(errorsByService) >= 2 {
		var eventIDs []string
		serviceCount := 0

		for service, serviceErrors := range errorsByService {
			if len(serviceErrors) >= 2 {
				serviceCount++
				for _, event := range serviceErrors {
					eventIDs = append(eventIDs, event.ID)
				}
			}
		}

		if serviceCount >= 2 {
			return &CorrelationMatch{
				Pattern:     p.Name(),
				Confidence:  0.85,
				Description: fmt.Sprintf("Error cascade detected across %d services", serviceCount),
				EventIDs:    eventIDs,
				Metadata: map[string]string{
					"service_count": fmt.Sprintf("%d", serviceCount),
					"error_count":   fmt.Sprintf("%d", len(eventIDs)),
				},
			}, true
		}
	}

	return nil, false
}

// LatencySpikePattern detects latency spikes followed by errors
type LatencySpikePattern struct{}

func (p *LatencySpikePattern) Name() string        { return "latency_spike" }
func (p *LatencySpikePattern) Description() string { return "High latency followed by errors" }

func (p *LatencySpikePattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	var latencyEvent *domain.UnifiedEvent
	var errorEvents []*domain.UnifiedEvent

	for _, event := range events {
		if event.Network != nil && event.Network.Latency > 1000000000 { // > 1 second
			latencyEvent = event
		} else if event.GetSeverity() == "error" && latencyEvent != nil {
			if event.Timestamp.After(latencyEvent.Timestamp) {
				errorEvents = append(errorEvents, event)
			}
		}
	}

	if latencyEvent != nil && len(errorEvents) > 0 {
		eventIDs := []string{latencyEvent.ID}
		for _, event := range errorEvents {
			eventIDs = append(eventIDs, event.ID)
		}

		return &CorrelationMatch{
			Pattern:     p.Name(),
			Confidence:  0.75,
			Description: fmt.Sprintf("Latency spike (%.2fs) followed by %d errors", float64(latencyEvent.Network.Latency)/1e9, len(errorEvents)),
			EventIDs:    eventIDs,
			Metadata: map[string]string{
				"latency_ms":  fmt.Sprintf("%.0f", float64(latencyEvent.Network.Latency)/1e6),
				"error_count": fmt.Sprintf("%d", len(errorEvents)),
			},
		}, true
	}

	return nil, false
}

// ResourceExhaustionPattern detects resource exhaustion
type ResourceExhaustionPattern struct{}

func (p *ResourceExhaustionPattern) Name() string { return "resource_exhaustion" }
func (p *ResourceExhaustionPattern) Description() string {
	return "System resource exhaustion detected"
}

func (p *ResourceExhaustionPattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	var oomEvents []*domain.UnifiedEvent
	var cpuEvents []*domain.UnifiedEvent
	var diskEvents []*domain.UnifiedEvent

	for _, event := range events {
		if event.Kernel != nil && event.Kernel.Syscall == "oom_kill" {
			oomEvents = append(oomEvents, event)
		}
		if event.Type == domain.EventTypeCPU && event.GetSeverity() == "warning" {
			cpuEvents = append(cpuEvents, event)
		}
		if event.Type == domain.EventTypeDisk && event.GetSeverity() == "warning" {
			diskEvents = append(diskEvents, event)
		}
	}

	resourceTypes := 0
	var eventIDs []string
	description := "Resource exhaustion: "

	if len(oomEvents) > 0 {
		resourceTypes++
		description += "memory "
		for _, e := range oomEvents {
			eventIDs = append(eventIDs, e.ID)
		}
	}
	if len(cpuEvents) > 2 {
		resourceTypes++
		description += "cpu "
		for _, e := range cpuEvents {
			eventIDs = append(eventIDs, e.ID)
		}
	}
	if len(diskEvents) > 2 {
		resourceTypes++
		description += "disk "
		for _, e := range diskEvents {
			eventIDs = append(eventIDs, e.ID)
		}
	}

	if resourceTypes > 0 {
		return &CorrelationMatch{
			Pattern:     p.Name(),
			Confidence:  0.9,
			Description: description,
			EventIDs:    eventIDs,
			Metadata: map[string]string{
				"resource_types": fmt.Sprintf("%d", resourceTypes),
				"event_count":    fmt.Sprintf("%d", len(eventIDs)),
			},
		}, true
	}

	return nil, false
}

// ServiceFailurePattern detects service failures
type ServiceFailurePattern struct{}

func (p *ServiceFailurePattern) Name() string        { return "service_failure" }
func (p *ServiceFailurePattern) Description() string { return "Service failure or restart detected" }

func (p *ServiceFailurePattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	serviceEvents := make(map[string][]*domain.UnifiedEvent)

	for _, event := range events {
		if event.Entity != nil && event.Entity.Type == "service" {
			// Look for failure indicators
			if event.GetSeverity() == "critical" ||
				(event.Semantic != nil && event.Semantic.Intent == "service-crash") ||
				(event.Type == domain.EventTypeKubernetes && contains(event.GetEntityID(), "restart")) {
				serviceEvents[event.Entity.Name] = append(serviceEvents[event.Entity.Name], event)
			}
		}
	}

	for service, events := range serviceEvents {
		if len(events) >= 2 {
			var eventIDs []string
			for _, e := range events {
				eventIDs = append(eventIDs, e.ID)
			}

			return &CorrelationMatch{
				Pattern:     p.Name(),
				Confidence:  0.8,
				Description: fmt.Sprintf("Service '%s' failure detected with %d events", service, len(events)),
				EventIDs:    eventIDs,
				Metadata: map[string]string{
					"service":     service,
					"event_count": fmt.Sprintf("%d", len(events)),
				},
			}, true
		}
	}

	return nil, false
}

// SecurityAnomalyPattern detects security-related anomalies
type SecurityAnomalyPattern struct{}

func (p *SecurityAnomalyPattern) Name() string        { return "security_anomaly" }
func (p *SecurityAnomalyPattern) Description() string { return "Potential security issue detected" }

func (p *SecurityAnomalyPattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	var suspiciousEvents []*domain.UnifiedEvent

	for _, event := range events {
		// Check for security indicators
		if event.Kernel != nil {
			// Suspicious syscalls
			if event.Kernel.Syscall == "ptrace" || event.Kernel.Syscall == "execve" {
				if event.Kernel.UID == 0 { // Root execution
					suspiciousEvents = append(suspiciousEvents, event)
				}
			}
		}

		// Check semantic context
		if event.Semantic != nil && event.Semantic.Category == "security" {
			suspiciousEvents = append(suspiciousEvents, event)
		}

		// Network anomalies
		if event.Network != nil && event.Network.DestPort < 1024 && event.Network.Direction == "egress" {
			suspiciousEvents = append(suspiciousEvents, event)
		}
	}

	if len(suspiciousEvents) >= 3 {
		var eventIDs []string
		for _, e := range suspiciousEvents {
			eventIDs = append(eventIDs, e.ID)
		}

		return &CorrelationMatch{
			Pattern:     p.Name(),
			Confidence:  0.7,
			Description: fmt.Sprintf("Security anomaly: %d suspicious events detected", len(suspiciousEvents)),
			EventIDs:    eventIDs,
			Metadata: map[string]string{
				"event_count": fmt.Sprintf("%d", len(suspiciousEvents)),
			},
		}, true
	}

	return nil, false
}

// PerformanceDegradationPattern detects performance degradation
type PerformanceDegradationPattern struct{}

func (p *PerformanceDegradationPattern) Name() string        { return "performance_degradation" }
func (p *PerformanceDegradationPattern) Description() string { return "System performance degradation" }

func (p *PerformanceDegradationPattern) Match(events []*domain.UnifiedEvent) (*CorrelationMatch, bool) {
	var slowEvents []*domain.UnifiedEvent
	baselineLatency := int64(100000000) // 100ms

	for _, event := range events {
		// Check for slow operations
		if event.Network != nil && event.Network.Latency > baselineLatency*5 {
			slowEvents = append(slowEvents, event)
		}

		// Check for performance warnings
		if event.Semantic != nil && event.Semantic.Category == "performance" {
			slowEvents = append(slowEvents, event)
		}
	}

	if len(slowEvents) >= 5 {
		var eventIDs []string
		var totalLatency int64
		for _, e := range slowEvents {
			eventIDs = append(eventIDs, e.ID)
			if e.Network != nil {
				totalLatency += e.Network.Latency
			}
		}

		avgLatency := totalLatency / int64(len(slowEvents))

		return &CorrelationMatch{
			Pattern:     p.Name(),
			Confidence:  0.8,
			Description: fmt.Sprintf("Performance degradation: %d slow operations, avg latency %.0fms", len(slowEvents), float64(avgLatency)/1e6),
			EventIDs:    eventIDs,
			Metadata: map[string]string{
				"slow_events":    fmt.Sprintf("%d", len(slowEvents)),
				"avg_latency_ms": fmt.Sprintf("%.0f", float64(avgLatency)/1e6),
			},
		}, true
	}

	return nil, false
}

// Helper pattern analysis methods

func (e *RealTimeCorrelationEngine) analyzeTemporalPatterns(events []*domain.UnifiedEvent) []*pb.Correlation {
	var correlations []*pb.Correlation

	if len(events) < 10 {
		return findings
	}

	// Calculate event rate
	timeSpan := events[len(events)-1].Timestamp.Sub(events[0].Timestamp)
	if timeSpan > 0 {
		eventsPerSecond := float64(len(events)) / timeSpan.Seconds()

		if eventsPerSecond > 100 {
			eventIDs := make([]string, len(events))
			for i, e := range events {
				eventIDs[i] = e.ID
			}

			correlations = append(correlations, &pb.Correlation{
				Id:          fmt.Sprintf("temporal-%d", time.Now().UnixNano()),
				Type:        pb.CorrelationType_CORRELATION_TYPE_TEMPORAL,
				Pattern:     "event_burst",
				Confidence:  0.9,
				Description: fmt.Sprintf("Event burst detected: %.1f events/second", eventsPerSecond),
				EventIds:    eventIDs,
				Metadata: map[string]string{
					"events_per_second": fmt.Sprintf("%.1f", eventsPerSecond),
					"duration_seconds":  fmt.Sprintf("%.1f", timeSpan.Seconds()),
				},
				CreatedAt:   timestamppb.Now(),
				UpdatedAt:   timestamppb.Now(),
			})
		}
	}

	return correlations
}

func (e *RealTimeCorrelationEngine) analyzeCausalPatterns(events []*domain.UnifiedEvent) []*pb.Correlation {
	var correlations []*pb.Correlation

	// Look for cause-effect patterns
	for i := 0; i < len(events)-1; i++ {
		cause := events[i]
		effect := events[i+1]

		// Check for syscall -> error pattern
		if cause.Kernel != nil && effect.GetSeverity() == "error" {
			timeDiff := effect.Timestamp.Sub(cause.Timestamp)
			if timeDiff < 5*time.Second && timeDiff > 0 {
				findings = append(findings, &pb.CorrelationFinding{
					Id:          fmt.Sprintf("causal-%d", time.Now().UnixNano()),
					Type:        pb.CorrelationType_CORRELATION_TYPE_CAUSAL,
					Pattern:     "syscall_error",
					Confidence:  0.7,
					Description: fmt.Sprintf("Error occurred %.1fs after syscall %s", timeDiff.Seconds(), cause.Kernel.Syscall),
					EventIds:    []string{cause.ID, effect.ID},
					Metadata: map[string]string{
						"syscall":     cause.Kernel.Syscall,
						"time_diff_s": fmt.Sprintf("%.1f", timeDiff.Seconds()),
					},
				})
			}
		}

		// Check for network -> application error pattern
		if cause.Network != nil && cause.Network.StatusCode >= 500 && effect.Application != nil && effect.Application.Level == "error" {
			timeDiff := effect.Timestamp.Sub(cause.Timestamp)
			if timeDiff < 10*time.Second && timeDiff > 0 {
				findings = append(findings, &pb.CorrelationFinding{
					Id:          fmt.Sprintf("causal-%d", time.Now().UnixNano()),
					Type:        pb.CorrelationType_CORRELATION_TYPE_CAUSAL,
					Pattern:     "network_app_error",
					Confidence:  0.8,
					Description: fmt.Sprintf("Application error after network failure (status %d)", cause.Network.StatusCode),
					EventIds:    []string{cause.ID, effect.ID},
					Metadata: map[string]string{
						"status_code": fmt.Sprintf("%d", cause.Network.StatusCode),
						"time_diff_s": fmt.Sprintf("%.1f", timeDiff.Seconds()),
					},
				})
			}
		}
	}

	return findings
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
