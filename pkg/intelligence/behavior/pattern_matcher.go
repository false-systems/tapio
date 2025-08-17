package behavior

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"go.uber.org/zap"
)

// PatternMatcher matches events against behavior patterns
type PatternMatcher struct {
	logger   *zap.Logger
	patterns []domain.BehaviorPattern

	// Compiled regex cache
	regexCache map[string]*regexp.Regexp
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(logger *zap.Logger) *PatternMatcher {
	return &PatternMatcher{
		logger:     logger,
		patterns:   make([]domain.BehaviorPattern, 0),
		regexCache: make(map[string]*regexp.Regexp),
	}
}

// UpdatePatterns updates the patterns used for matching
func (m *PatternMatcher) UpdatePatterns(patterns []domain.BehaviorPattern) {
	m.patterns = patterns
	// Clear regex cache when patterns update
	m.regexCache = make(map[string]*regexp.Regexp)
}

// Match matches an observation event against all patterns
func (m *PatternMatcher) Match(ctx context.Context, event *domain.ObservationEvent) ([]BehaviorPatternMatch, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	matches := make([]BehaviorPatternMatch, 0)

	for _, pattern := range m.patterns {
		if !pattern.Enabled {
			continue
		}

		match, err := m.matchPattern(ctx, event, pattern)
		if err != nil {
			m.logger.Warn("Pattern matching error",
				zap.String("pattern", pattern.Name),
				zap.Error(err),
			)
			continue
		}

		if match != nil {
			matches = append(matches, *match)
		}
	}

	return matches, nil
}

// matchPattern matches a single pattern against an observation event
func (m *PatternMatcher) matchPattern(ctx context.Context, event *domain.ObservationEvent, pattern domain.BehaviorPattern) (*BehaviorPatternMatch, error) {
	conditionMatches := make([]BehaviorConditionMatch, 0, len(pattern.Conditions))
	allRequired := true
	anyMatched := false

	for _, condition := range pattern.Conditions {
		matched, actualValue := m.evaluateCondition(event, condition)

		conditionMatch := BehaviorConditionMatch{
			Condition:   condition,
			Matched:     matched,
			ActualValue: actualValue,
		}

		if matched {
			anyMatched = true
			conditionMatch.Message = fmt.Sprintf("Condition met: %s %s %v", condition.Match.Field, condition.Match.Type, condition.Match.Value)
		} else {
			conditionMatch.Message = fmt.Sprintf("Condition not met: %s %s %v (actual: %s)",
				condition.Match.Field, condition.Match.Type, condition.Match.Value, actualValue.ToString())
			if condition.Required {
				allRequired = false
			}
		}

		conditionMatches = append(conditionMatches, conditionMatch)
	}

	// Pattern matches if all required conditions are met and at least one condition matched
	if !allRequired || !anyMatched {
		return nil, nil
	}

	// Calculate confidence based on matched conditions
	confidence := m.calculateConfidence(pattern, conditionMatches)

	match := &BehaviorPatternMatch{
		PatternID:   pattern.ID,
		PatternName: pattern.Name,
		EventID:     event.ID,
		Confidence:  confidence,
		MatchedAt:   time.Now(),
		Conditions:  conditionMatches,
		Context:     extractContext(event),
	}

	m.logger.Debug("Pattern matched",
		zap.String("pattern", pattern.Name),
		zap.String("event", event.ID),
		zap.Float64("confidence", confidence),
	)

	return match, nil
}

// evaluateCondition evaluates a single condition against an observation event
func (m *PatternMatcher) evaluateCondition(event *domain.ObservationEvent, condition domain.Condition) (bool, *intelligence.ConditionValue) {
	// Check event type first
	if condition.EventType != "" && condition.EventType != event.Type {
		return false, intelligence.NewConditionValue(nil)
	}

	// Get the field value from the event if specified
	var actualValue *intelligence.ConditionValue
	if condition.Match.Field != "" {
		fieldValue := m.getFieldValue(event, condition.Match.Field)
		if fieldValue.IsNil && condition.Match.Type != "exists" {
			actualValue = intelligence.NewNilConditionValue()
			return false, actualValue
		}
		actualValue = intelligence.NewConditionValue(fieldValue.ToInterface())
	} else {
		actualValue = intelligence.NewNilConditionValue()
	}

	// Evaluate based on match type using strongly-typed comparison
	expectedValue := intelligence.NewConditionValue(condition.Match.Value)

	switch condition.Match.Type {
	case "exact":
		return m.compareEqualsTyped(actualValue, expectedValue), actualValue
	case "regex":
		return m.compareRegexTyped(actualValue, expectedValue), actualValue
	case "contains":
		return m.compareContainsTyped(actualValue, expectedValue), actualValue
	case "threshold":
		// For threshold, we need to handle aggregation
		if condition.Aggregation != nil {
			// This would require event history, simplified for now
			return m.compareThresholdTyped(actualValue, condition.Match.Threshold, condition.Match.Operator), actualValue
		}
		return false, actualValue
	case "exists":
		return !actualValue.IsNil(), actualValue
	default:
		return false, actualValue
	}
}

// FieldValue represents a strongly-typed field value extracted from an event
type FieldValue struct {
	StringValue *string
	IntValue    *int64
	UintValue   *uint64
	MapValue    map[string]string
	IsNil       bool
}

// ToInterface returns the underlying value as an interface{} for compatibility
func (fv *FieldValue) ToInterface() any {
	if fv.IsNil {
		return nil
	}
	if fv.StringValue != nil {
		return *fv.StringValue
	}
	if fv.IntValue != nil {
		return *fv.IntValue
	}
	if fv.UintValue != nil {
		return *fv.UintValue
	}
	if fv.MapValue != nil {
		return fv.MapValue
	}
	return nil
}

// getFieldValue extracts a field value from an observation event
func (m *PatternMatcher) getFieldValue(event *domain.ObservationEvent, field string) *FieldValue {
	// Parse field path (e.g., "action", "data.hostname")
	parts := strings.Split(field, ".")

	switch parts[0] {
	case "type":
		return &FieldValue{StringValue: &event.Type}
	case "source":
		return &FieldValue{StringValue: &event.Source}
	case "pid":
		if event.PID != nil {
			val := int64(*event.PID)
			return &FieldValue{IntValue: &val}
		}
	case "container_id":
		if event.ContainerID != nil {
			return &FieldValue{StringValue: event.ContainerID}
		}
	case "pod_name":
		if event.PodName != nil {
			return &FieldValue{StringValue: event.PodName}
		}
	case "namespace":
		if event.Namespace != nil {
			return &FieldValue{StringValue: event.Namespace}
		}
	case "service_name":
		if event.ServiceName != nil {
			return &FieldValue{StringValue: event.ServiceName}
		}
	case "node_name":
		if event.NodeName != nil {
			return &FieldValue{StringValue: event.NodeName}
		}
	case "action":
		if event.Action != nil {
			return &FieldValue{StringValue: event.Action}
		}
	case "target":
		if event.Target != nil {
			return &FieldValue{StringValue: event.Target}
		}
	case "result":
		if event.Result != nil {
			return &FieldValue{StringValue: event.Result}
		}
	case "reason":
		if event.Reason != nil {
			return &FieldValue{StringValue: event.Reason}
		}
	case "duration":
		if event.Duration != nil {
			val := uint64(*event.Duration)
			return &FieldValue{UintValue: &val}
		}
	case "size":
		if event.Size != nil {
			val := uint64(*event.Size)
			return &FieldValue{UintValue: &val}
		}
	case "count":
		if event.Count != nil {
			val := uint64(*event.Count)
			return &FieldValue{UintValue: &val}
		}
	case "data":
		if len(parts) > 1 {
			if value, exists := event.Data[parts[1]]; exists {
				return &FieldValue{StringValue: &value}
			}
		} else {
			return &FieldValue{MapValue: event.Data}
		}
	case "caused_by":
		if event.CausedBy != nil {
			return &FieldValue{StringValue: event.CausedBy}
		}
	case "parent_id":
		if event.ParentID != nil {
			return &FieldValue{StringValue: event.ParentID}
		}
	}

	return &FieldValue{IsNil: true}
}

// Legacy comparison functions removed - use strongly-typed versions below

// calculateConfidence calculates the confidence score for a pattern match
func (m *PatternMatcher) calculateConfidence(pattern domain.BehaviorPattern, matches []BehaviorConditionMatch) float64 {
	// Start with base confidence
	confidence := pattern.BaseConfidence

	// Adjust based on condition matches
	totalConditions := len(pattern.Conditions)
	matchedConditions := 0
	for _, match := range matches {
		if match.Matched {
			matchedConditions++
		}
	}

	// Scale confidence based on match ratio
	matchRatio := float64(matchedConditions) / float64(totalConditions)
	confidence *= matchRatio

	// Apply adjusted confidence if available
	if pattern.AdjustedConfidence > 0 {
		confidence = pattern.AdjustedConfidence * matchRatio
	}

	// Ensure confidence is within bounds
	if confidence > 1.0 {
		confidence = 1.0
	} else if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// Helper function to convert FieldValue to float64
func (fv *FieldValue) ToFloat64() (float64, bool) {
	if fv.IsNil {
		return 0, false
	}
	if fv.IntValue != nil {
		return float64(*fv.IntValue), true
	}
	if fv.UintValue != nil {
		return float64(*fv.UintValue), true
	}
	if fv.StringValue != nil {
		// Try to parse string as float
		var f float64
		_, err := fmt.Sscanf(*fv.StringValue, "%f", &f)
		return f, err == nil
	}
	return 0, false
}

// Strongly-typed comparison methods

func (m *PatternMatcher) compareEqualsTyped(actual, expected *intelligence.ConditionValue) bool {
	return actual.Equals(expected)
}

func (m *PatternMatcher) compareContainsTyped(actual, expected *intelligence.ConditionValue) bool {
	return actual.Contains(expected)
}

func (m *PatternMatcher) compareRegexTyped(actual, expected *intelligence.ConditionValue) bool {
	actualStr := actual.ToString()
	expectedStr := expected.ToString()

	// Check cache
	regex, ok := m.regexCache[expectedStr]
	if !ok {
		var err error
		regex, err = regexp.Compile(expectedStr)
		if err != nil {
			m.logger.Warn("Invalid regex pattern", zap.String("pattern", expectedStr), zap.Error(err))
			return false
		}
		m.regexCache[expectedStr] = regex
	}

	return regex.MatchString(actualStr)
}

func (m *PatternMatcher) compareThresholdTyped(actual *intelligence.ConditionValue, threshold float64, operator string) bool {
	val, ok := actual.ToFloat64()
	if !ok {
		return false
	}

	switch operator {
	case ">=":
		return val >= threshold
	case ">":
		return val > threshold
	case "<=":
		return val <= threshold
	case "<":
		return val < threshold
	case "==":
		return val == threshold
	default:
		return false
	}
}
