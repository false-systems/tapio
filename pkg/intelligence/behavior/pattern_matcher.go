package behavior

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
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

// Match matches an event against all patterns
func (m *PatternMatcher) Match(ctx context.Context, event *domain.UnifiedEvent) ([]BehaviorPatternMatch, error) {
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

// matchPattern matches a single pattern against an event
func (m *PatternMatcher) matchPattern(ctx context.Context, event *domain.UnifiedEvent, pattern domain.BehaviorPattern) (*BehaviorPatternMatch, error) {
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
			conditionMatch.Message = fmt.Sprintf("Condition not met: %s %s %v (actual: %v)",
				condition.Match.Field, condition.Match.Type, condition.Match.Value, actualValue)
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
		Context: map[string]interface{}{
			"event_type": string(event.Type),
			"source":     event.Source,
		},
	}

	m.logger.Debug("Pattern matched",
		zap.String("pattern", pattern.Name),
		zap.String("event", event.ID),
		zap.Float64("confidence", confidence),
	)

	return match, nil
}

// evaluateCondition evaluates a single condition against an event
func (m *PatternMatcher) evaluateCondition(event *domain.UnifiedEvent, condition domain.Condition) (bool, interface{}) {
	// Check event type first
	if condition.EventType != "" && condition.EventType != string(event.Type) {
		return false, nil
	}

	// Get the field value from the event if specified
	var value interface{}
	if condition.Match.Field != "" {
		value = m.getFieldValue(event, condition.Match.Field)
		if value == nil && condition.Match.Type != "exists" {
			return false, nil
		}
	}

	// Evaluate based on match type
	switch condition.Match.Type {
	case "exact":
		return m.compareEquals(value, condition.Match.Value), value
	case "regex":
		return m.compareRegex(value, condition.Match.Value), value
	case "contains":
		return m.compareContains(value, condition.Match.Value), value
	case "threshold":
		// For threshold, we need to handle aggregation
		if condition.Aggregation != nil {
			// This would require event history, simplified for now
			return m.compareThreshold(value, condition.Match.Threshold, condition.Match.Operator), value
		}
		return false, value
	case "exists":
		return value != nil, value
	default:
		return false, value
	}
}

// getFieldValue extracts a field value from an event
func (m *PatternMatcher) getFieldValue(event *domain.UnifiedEvent, field string) interface{} {
	// Parse field path (e.g., "semantic.intent", "k8s_context.namespace")
	parts := strings.Split(field, ".")

	switch parts[0] {
	case "type":
		return string(event.Type)
	case "source":
		return event.Source
	case "severity":
		return event.Severity
	case "message":
		return event.Message
	case "semantic":
		if event.Semantic != nil && len(parts) > 1 {
			switch parts[1] {
			case "intent":
				return event.Semantic.Intent
			case "category":
				return event.Semantic.Category
			case "confidence":
				return event.Semantic.Confidence
			}
		}
	case "k8s_context":
		if event.K8sContext != nil && len(parts) > 1 {
			switch parts[1] {
			case "kind":
				return event.K8sContext.Kind
			case "name":
				return event.K8sContext.Name
			case "namespace":
				return event.K8sContext.Namespace
			}
		}
	case "kernel":
		if event.Kernel != nil && len(parts) > 1 {
			switch parts[1] {
			case "syscall":
				return event.Kernel.Syscall
			case "pid":
				return event.Kernel.PID
			case "comm":
				return event.Kernel.Comm
			}
		}
	case "attributes":
		if event.Attributes != nil && len(parts) > 1 {
			return event.Attributes[parts[1]]
		}
	}

	return nil
}

// Comparison functions

func (m *PatternMatcher) compareEquals(value, expected interface{}) bool {
	return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", expected)
}

func (m *PatternMatcher) compareGreater(value, expected interface{}) bool {
	// Try to convert to float64 for comparison
	valFloat, valOk := toFloat64(value)
	expFloat, expOk := toFloat64(expected)
	if valOk && expOk {
		return valFloat > expFloat
	}
	return false
}

func (m *PatternMatcher) compareLess(value, expected interface{}) bool {
	valFloat, valOk := toFloat64(value)
	expFloat, expOk := toFloat64(expected)
	if valOk && expOk {
		return valFloat < expFloat
	}
	return false
}

func (m *PatternMatcher) compareContains(value, expected interface{}) bool {
	valStr := fmt.Sprintf("%v", value)
	expStr := fmt.Sprintf("%v", expected)
	return strings.Contains(valStr, expStr)
}

func (m *PatternMatcher) compareRegex(value, expected interface{}) bool {
	valStr := fmt.Sprintf("%v", value)
	expStr := fmt.Sprintf("%v", expected)

	// Check cache
	regex, ok := m.regexCache[expStr]
	if !ok {
		var err error
		regex, err = regexp.Compile(expStr)
		if err != nil {
			m.logger.Warn("Invalid regex pattern", zap.String("pattern", expStr), zap.Error(err))
			return false
		}
		m.regexCache[expStr] = regex
	}

	return regex.MatchString(valStr)
}

func (m *PatternMatcher) compareThreshold(value interface{}, threshold float64, operator string) bool {
	val, ok := toFloat64(value)
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

func (m *PatternMatcher) compareIn(value, expected interface{}) bool {
	// Expected should be a slice
	switch exp := expected.(type) {
	case []interface{}:
		for _, item := range exp {
			if m.compareEquals(value, item) {
				return true
			}
		}
	case []string:
		valStr := fmt.Sprintf("%v", value)
		for _, item := range exp {
			if valStr == item {
				return true
			}
		}
	}
	return false
}

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

// Helper function to convert interface to float64
func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		// Try to parse string as float
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}
