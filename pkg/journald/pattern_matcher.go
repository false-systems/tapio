package journald

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PatternMatcher provides log pattern recognition capabilities
type PatternMatcher struct {
	config           *PatternMatcherConfig
	patterns         map[string]*PatternRule
	compiledPatterns map[string]*regexp.Regexp

	// Pattern matches tracking
	matches       map[string]*PatternMatch
	recentMatches []PatternMatch
	matchHistory  map[string][]time.Time

	// State management
	mutex sync.RWMutex
}

// PatternMatcherConfig configures the pattern matcher
type PatternMatcherConfig struct {
	ErrorPatterns       []string
	WarningPatterns     []string
	SecurityPatterns    []string
	PerformancePatterns []string

	// Custom patterns
	CustomPatterns map[string]PatternRule

	// Matching behavior
	CaseSensitive         bool
	EnableRegex           bool
	MaxMatches            int
	MatchHistoryRetention time.Duration
}

// PatternRule defines a pattern matching rule
type PatternRule struct {
	ID          string
	Pattern     string
	Type        PatternType
	Severity    string
	Category    string
	Description string
	Action      string
	Tags        []string
	Regex       *regexp.Regexp
	Metadata    map[string]interface{}
}

// PatternType defines the type of pattern
type PatternType int

const (
	PatternTypeError PatternType = iota
	PatternTypeWarning
	PatternTypeSecurity
	PatternTypePerformance
	PatternTypeCustom
	PatternTypeAnomaly
)

// PatternMatch represents a pattern match result
type PatternMatch struct {
	ID          string
	PatternID   string
	Timestamp   time.Time
	Service     string
	Message     string
	MatchedText string
	PatternType PatternType
	Severity    string
	Category    string
	Confidence  float64
	Context     map[string]interface{}
	Frequency   int
	FirstSeen   time.Time
	LastSeen    time.Time
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(config *PatternMatcherConfig) *PatternMatcher {
	if config == nil {
		config = DefaultPatternMatcherConfig()
	}

	matcher := &PatternMatcher{
		config:           config,
		patterns:         make(map[string]*PatternRule),
		compiledPatterns: make(map[string]*regexp.Regexp),
		matches:          make(map[string]*PatternMatch),
		recentMatches:    make([]PatternMatch, 0),
		matchHistory:     make(map[string][]time.Time),
	}

	// Initialize default patterns
	matcher.initializeDefaultPatterns()

	// Add custom patterns
	for id, rule := range config.CustomPatterns {
		rule.ID = id
		matcher.AddPattern(rule)
	}

	return matcher
}

// DefaultPatternMatcherConfig returns the default configuration
func DefaultPatternMatcherConfig() *PatternMatcherConfig {
	return &PatternMatcherConfig{
		ErrorPatterns: []string{
			"error",
			"failed",
			"failure",
			"exception",
			"panic",
			"fatal",
			"critical",
			"emergency",
			"abort",
			"crash",
			"timeout",
			"refused",
			"denied",
			"invalid",
			"corrupt",
			"missing",
			"not found",
			"permission denied",
			"access denied",
			"out of memory",
			"disk full",
			"no space left",
		},
		WarningPatterns: []string{
			"warning",
			"warn",
			"deprecated",
			"retry",
			"retrying",
			"fallback",
			"slow",
			"delayed",
			"throttle",
			"rate limit",
			"high load",
			"resource exhausted",
			"backpressure",
			"degraded",
		},
		SecurityPatterns: []string{
			"authentication failed",
			"login failed",
			"unauthorized",
			"forbidden",
			"access denied",
			"permission denied",
			"security violation",
			"intrusion",
			"attack",
			"malware",
			"virus",
			"suspicious",
			"breach",
			"vulnerability",
			"exploit",
			"privilege escalation",
		},
		PerformancePatterns: []string{
			"slow query",
			"high latency",
			"performance",
			"bottleneck",
			"memory leak",
			"cpu spike",
			"high cpu",
			"disk pressure",
			"network congestion",
			"connection timeout",
			"deadlock",
			"thread pool exhausted",
			"queue full",
			"backlog",
		},
		CaseSensitive:         false,
		EnableRegex:           true,
		MaxMatches:            10000,
		MatchHistoryRetention: 24 * time.Hour,
	}
}

// initializeDefaultPatterns initializes default pattern rules
func (pm *PatternMatcher) initializeDefaultPatterns() {
	// Error patterns
	for i, pattern := range pm.config.ErrorPatterns {
		pm.AddPattern(PatternRule{
			ID:          fmt.Sprintf("error_%d", i),
			Pattern:     pattern,
			Type:        PatternTypeError,
			Severity:    "error",
			Category:    "error",
			Description: fmt.Sprintf("Error pattern: %s", pattern),
		})
	}

	// Warning patterns
	for i, pattern := range pm.config.WarningPatterns {
		pm.AddPattern(PatternRule{
			ID:          fmt.Sprintf("warning_%d", i),
			Pattern:     pattern,
			Type:        PatternTypeWarning,
			Severity:    "warning",
			Category:    "warning",
			Description: fmt.Sprintf("Warning pattern: %s", pattern),
		})
	}

	// Security patterns
	for i, pattern := range pm.config.SecurityPatterns {
		pm.AddPattern(PatternRule{
			ID:          fmt.Sprintf("security_%d", i),
			Pattern:     pattern,
			Type:        PatternTypeSecurity,
			Severity:    "critical",
			Category:    "security",
			Description: fmt.Sprintf("Security pattern: %s", pattern),
		})
	}

	// Performance patterns
	for i, pattern := range pm.config.PerformancePatterns {
		pm.AddPattern(PatternRule{
			ID:          fmt.Sprintf("performance_%d", i),
			Pattern:     pattern,
			Type:        PatternTypePerformance,
			Severity:    "warning",
			Category:    "performance",
			Description: fmt.Sprintf("Performance pattern: %s", pattern),
		})
	}
}

// AddPattern adds a new pattern rule
func (pm *PatternMatcher) AddPattern(rule PatternRule) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Compile regex if enabled
	if pm.config.EnableRegex {
		if compiled, err := regexp.Compile("(?i)" + rule.Pattern); err == nil {
			rule.Regex = compiled
			pm.compiledPatterns[rule.ID] = compiled
		} else {
			return fmt.Errorf("failed to compile regex for pattern %s: %w", rule.ID, err)
		}
	}

	pm.patterns[rule.ID] = &rule
	return nil
}

// RemovePattern removes a pattern rule
func (pm *PatternMatcher) RemovePattern(patternID string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	delete(pm.patterns, patternID)
	delete(pm.compiledPatterns, patternID)

	return nil
}

// MatchEntry checks if a log entry matches any patterns
func (pm *PatternMatcher) MatchEntry(entry *LogEntry) []string {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var matchedPatterns []string
	message := entry.Message

	if !pm.config.CaseSensitive {
		message = strings.ToLower(message)
	}

	for patternID, rule := range pm.patterns {
		var matched bool
		var matchedText string

		if pm.config.EnableRegex && rule.Regex != nil {
			if match := rule.Regex.FindString(entry.Message); match != "" {
				matched = true
				matchedText = match
			}
		} else {
			pattern := rule.Pattern
			if !pm.config.CaseSensitive {
				pattern = strings.ToLower(pattern)
			}
			if strings.Contains(message, pattern) {
				matched = true
				matchedText = pattern
			}
		}

		if matched {
			matchedPatterns = append(matchedPatterns, patternID)
			pm.recordMatch(patternID, entry, matchedText)
		}
	}

	return matchedPatterns
}

// recordMatch records a pattern match
func (pm *PatternMatcher) recordMatch(patternID string, entry *LogEntry, matchedText string) {
	now := time.Now()
	matchID := fmt.Sprintf("%s_%s_%d", patternID, entry.Service, now.Unix())

	rule := pm.patterns[patternID]
	if rule == nil {
		return
	}

	// Check if this is a repeated match
	existingMatch, exists := pm.matches[patternID+"_"+entry.Service]
	if exists {
		existingMatch.Frequency++
		existingMatch.LastSeen = now
		existingMatch.Message = entry.Message // Update with latest message
	} else {
		match := &PatternMatch{
			ID:          matchID,
			PatternID:   patternID,
			Timestamp:   now,
			Service:     entry.Service,
			Message:     entry.Message,
			MatchedText: matchedText,
			PatternType: rule.Type,
			Severity:    rule.Severity,
			Category:    rule.Category,
			Confidence:  pm.calculateConfidence(rule, entry),
			Context: map[string]interface{}{
				"priority":      entry.Priority,
				"priority_name": entry.PriorityName,
				"hostname":      entry.Hostname,
				"process_id":    entry.ProcessID,
			},
			Frequency: 1,
			FirstSeen: now,
			LastSeen:  now,
		}

		pm.matches[patternID+"_"+entry.Service] = match

		// Add to recent matches (limited size)
		pm.recentMatches = append(pm.recentMatches, *match)
		if len(pm.recentMatches) > pm.config.MaxMatches {
			pm.recentMatches = pm.recentMatches[1:]
		}
	}

	// Track match history
	if history, exists := pm.matchHistory[patternID]; exists {
		pm.matchHistory[patternID] = append(history, now)
	} else {
		pm.matchHistory[patternID] = []time.Time{now}
	}

	// Clean up old history
	pm.cleanupHistory(patternID)
}

// calculateConfidence calculates the confidence score for a pattern match
func (pm *PatternMatcher) calculateConfidence(rule *PatternRule, entry *LogEntry) float64 {
	confidence := 0.8 // Base confidence

	// Increase confidence for exact matches
	if strings.Contains(entry.Message, rule.Pattern) {
		confidence += 0.1
	}

	// Increase confidence for high priority messages
	if entry.Priority <= 3 { // error level and above
		confidence += 0.1
	}

	// Increase confidence for security patterns
	if rule.Type == PatternTypeSecurity {
		confidence += 0.1
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// cleanupHistory removes old entries from match history
func (pm *PatternMatcher) cleanupHistory(patternID string) {
	cutoff := time.Now().Add(-pm.config.MatchHistoryRetention)

	if history, exists := pm.matchHistory[patternID]; exists {
		var recentHistory []time.Time
		for _, timestamp := range history {
			if timestamp.After(cutoff) {
				recentHistory = append(recentHistory, timestamp)
			}
		}
		pm.matchHistory[patternID] = recentHistory
	}
}

// GetMatches returns current pattern matches
func (pm *PatternMatcher) GetMatches() map[string]*PatternMatch {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	// Return a copy to avoid race conditions
	matches := make(map[string]*PatternMatch)
	for k, v := range pm.matches {
		matchCopy := *v
		matches[k] = &matchCopy
	}

	return matches
}

// GetRecentMatches returns recent pattern matches
func (pm *PatternMatcher) GetRecentMatches(limit int) []PatternMatch {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if limit <= 0 || limit > len(pm.recentMatches) {
		limit = len(pm.recentMatches)
	}

	matches := make([]PatternMatch, limit)
	start := len(pm.recentMatches) - limit
	copy(matches, pm.recentMatches[start:])

	return matches
}

// GetMatchesByType returns matches filtered by pattern type
func (pm *PatternMatcher) GetMatchesByType(patternType PatternType) []*PatternMatch {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var matches []*PatternMatch
	for _, match := range pm.matches {
		if match.PatternType == patternType {
			matchCopy := *match
			matches = append(matches, &matchCopy)
		}
	}

	return matches
}

// GetMatchesByService returns matches filtered by service
func (pm *PatternMatcher) GetMatchesByService(serviceName string) []*PatternMatch {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var matches []*PatternMatch
	for _, match := range pm.matches {
		if match.Service == serviceName {
			matchCopy := *match
			matches = append(matches, &matchCopy)
		}
	}

	return matches
}

// GetMatchesBySeverity returns matches filtered by severity
func (pm *PatternMatcher) GetMatchesBySeverity(severity string) []*PatternMatch {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var matches []*PatternMatch
	for _, match := range pm.matches {
		if match.Severity == severity {
			matchCopy := *match
			matches = append(matches, &matchCopy)
		}
	}

	return matches
}

// GetTopPatterns returns the most frequently matched patterns
func (pm *PatternMatcher) GetTopPatterns(limit int) []PatternSummary {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	// Count pattern frequencies
	patternCounts := make(map[string]int)
	for _, match := range pm.matches {
		patternCounts[match.PatternID] += match.Frequency
	}

	// Convert to summary and sort
	var summaries []PatternSummary
	for patternID, count := range patternCounts {
		if rule, exists := pm.patterns[patternID]; exists {
			summaries = append(summaries, PatternSummary{
				PatternID:   patternID,
				Pattern:     rule.Pattern,
				Type:        rule.Type,
				Severity:    rule.Severity,
				Category:    rule.Category,
				MatchCount:  count,
				Description: rule.Description,
			})
		}
	}

	// Simple sorting by match count (in a real implementation, use sort package)
	for i := 0; i < len(summaries)-1; i++ {
		for j := i + 1; j < len(summaries); j++ {
			if summaries[i].MatchCount < summaries[j].MatchCount {
				summaries[i], summaries[j] = summaries[j], summaries[i]
			}
		}
	}

	if limit > 0 && limit < len(summaries) {
		summaries = summaries[:limit]
	}

	return summaries
}

// PatternSummary represents a summary of pattern matches
type PatternSummary struct {
	PatternID   string
	Pattern     string
	Type        PatternType
	Severity    string
	Category    string
	MatchCount  int
	Description string
}

// ClearMatches clears all recorded matches
func (pm *PatternMatcher) ClearMatches() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.matches = make(map[string]*PatternMatch)
	pm.recentMatches = make([]PatternMatch, 0)
	pm.matchHistory = make(map[string][]time.Time)
}

// GetStatistics returns pattern matcher statistics
func (pm *PatternMatcher) GetStatistics() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_patterns":    len(pm.patterns),
		"compiled_patterns": len(pm.compiledPatterns),
		"total_matches":     len(pm.matches),
		"recent_matches":    len(pm.recentMatches),
		"case_sensitive":    pm.config.CaseSensitive,
		"regex_enabled":     pm.config.EnableRegex,
	}

	// Count matches by type
	typeCounts := make(map[string]int)
	for _, match := range pm.matches {
		switch match.PatternType {
		case PatternTypeError:
			typeCounts["error"]++
		case PatternTypeWarning:
			typeCounts["warning"]++
		case PatternTypeSecurity:
			typeCounts["security"]++
		case PatternTypePerformance:
			typeCounts["performance"]++
		case PatternTypeCustom:
			typeCounts["custom"]++
		case PatternTypeAnomaly:
			typeCounts["anomaly"]++
		}
	}
	stats["matches_by_type"] = typeCounts

	return stats
}
