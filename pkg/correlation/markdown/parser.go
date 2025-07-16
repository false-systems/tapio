package markdown

import (
	"bufio"
	"regexp"
	"strings"
	"time"
)

// CorrelationMarkdownParser parses markdown files to extract correlation rules
type CorrelationMarkdownParser struct {
	// Patterns for extracting correlation definitions
	patterns map[string]*regexp.Regexp
}

// NewCorrelationMarkdownParser creates a new parser for correlation markdown
func NewCorrelationMarkdownParser() *CorrelationMarkdownParser {
	return &CorrelationMarkdownParser{
		patterns: map[string]*regexp.Regexp{
			// Pattern headers
			"pattern_header": regexp.MustCompile(`^##\s+(.+?)(?:\s+Pattern)?$`),
			"rule_header":    regexp.MustCompile(`^###\s+Rule:\s+(.+)$`),

			// Condition patterns
			"when_clause":   regexp.MustCompile(`(?i)when\s+(.+?)(?:\s+then|,|\.|$)`),
			"if_clause":     regexp.MustCompile(`(?i)if\s+(.+?)(?:\s+then|,|\.|$)`),
			"and_condition": regexp.MustCompile(`(?i)\s+and\s+(.+?)(?:\s+then|,|\.|$)`),
			"threshold":     regexp.MustCompile(`(\w+)\s*([><=]+)\s*([\d.]+)(%|[a-zA-Z]*)`),
			"duration":      regexp.MustCompile(`for\s+(\d+)\s*(seconds?|minutes?|hours?|m|s|h)`),

			// Action patterns
			"then_clause":    regexp.MustCompile(`(?i)then\s+(.+?)(?:\.|$)`),
			"root_cause":     regexp.MustCompile(`(?i)root\s*cause:?\s*(.+?)(?:\.|$)`),
			"prediction":     regexp.MustCompile(`(?i)(?:predict|expect)\s+(.+?)(?:\.|$)`),
			"recommendation": regexp.MustCompile(`(?i)(?:recommend|fix|action):?\s*(.+?)(?:\.|$)`),

			// Metadata patterns
			"severity":   regexp.MustCompile(`(?i)severity:?\s*(critical|high|medium|low)`),
			"confidence": regexp.MustCompile(`(?i)confidence:?\s*([\d.]+)%?`),
			"category":   regexp.MustCompile(`(?i)category:?\s*(\w+)`),

			// Code blocks
			"code_fence": regexp.MustCompile("^```(\\w*)$"),
			"yaml_key":   regexp.MustCompile(`^\s*(\w+):\s*(.+)$`),
		},
	}
}

// ParseCorrelationMarkdown parses markdown content into correlation definitions
func (p *CorrelationMarkdownParser) ParseCorrelationMarkdown(content string) ([]*MarkdownCorrelation, error) {
	var correlations []*MarkdownCorrelation
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentCorrelation *MarkdownCorrelation
	var currentSection string
	var inCodeBlock bool
	var codeBlockContent strings.Builder
	var codeBlockLang string

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Handle code blocks
		if p.patterns["code_fence"].MatchString(line) {
			if inCodeBlock {
				// End of code block
				if currentCorrelation != nil && codeBlockLang == "yaml" {
					p.parseYAMLBlock(codeBlockContent.String(), currentCorrelation)
				}
				inCodeBlock = false
				codeBlockContent.Reset()
			} else {
				// Start of code block
				matches := p.patterns["code_fence"].FindStringSubmatch(line)
				codeBlockLang = matches[1]
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			codeBlockContent.WriteString(line + "\n")
			continue
		}

		// Check for pattern header (## Pattern Name)
		if matches := p.patterns["pattern_header"].FindStringSubmatch(line); matches != nil {
			// Save previous correlation if exists
			if currentCorrelation != nil {
				correlations = append(correlations, currentCorrelation)
			}

			// Start new correlation
			currentCorrelation = &MarkdownCorrelation{
				Name:           matches[1],
				Conditions:     []Condition{},
				Actions:        []Action{},
				Metadata:       make(map[string]interface{}),
				RawDescription: "",
			}
			currentSection = "description"
			continue
		}

		// Skip if no current correlation
		if currentCorrelation == nil {
			continue
		}

		// Parse different sections based on content
		p.parseLine(line, currentCorrelation, &currentSection)
	}

	// Add last correlation
	if currentCorrelation != nil {
		correlations = append(correlations, currentCorrelation)
	}

	return correlations, nil
}

// parseLine parses a single line and updates the correlation
func (p *CorrelationMarkdownParser) parseLine(line string, corr *MarkdownCorrelation, section *string) {
	trimmedLine := strings.TrimSpace(line)

	// Skip empty lines
	if trimmedLine == "" {
		return
	}

	// Extract conditions (When/If clauses)
	if matches := p.patterns["when_clause"].FindStringSubmatch(line); matches != nil {
		conditions := p.extractConditions(matches[1])
		corr.Conditions = append(corr.Conditions, conditions...)
		*section = "conditions"

		// Check for inline then clause
		if thenMatches := p.patterns["then_clause"].FindStringSubmatch(line); thenMatches != nil {
			actions := p.extractActions(thenMatches[1])
			corr.Actions = append(corr.Actions, actions...)
		}
		return
	}

	if matches := p.patterns["if_clause"].FindStringSubmatch(line); matches != nil {
		conditions := p.extractConditions(matches[1])
		corr.Conditions = append(corr.Conditions, conditions...)
		*section = "conditions"

		// Check for inline then clause
		if thenMatches := p.patterns["then_clause"].FindStringSubmatch(line); thenMatches != nil {
			actions := p.extractActions(thenMatches[1])
			corr.Actions = append(corr.Actions, actions...)
		}
		return
	}

	// Extract AND conditions
	if *section == "conditions" && strings.Contains(strings.ToLower(line), " and ") {
		if matches := p.patterns["and_condition"].FindStringSubmatch(line); matches != nil {
			conditions := p.extractConditions(matches[1])
			corr.Conditions = append(corr.Conditions, conditions...)
		}
		return
	}

	// Extract actions (Then clauses)
	if matches := p.patterns["then_clause"].FindStringSubmatch(line); matches != nil {
		actions := p.extractActions(matches[1])
		corr.Actions = append(corr.Actions, actions...)
		*section = "actions"
		return
	}

	// Extract root cause
	if matches := p.patterns["root_cause"].FindStringSubmatch(line); matches != nil {
		corr.Actions = append(corr.Actions, Action{
			Type:        "root_cause",
			Description: strings.TrimSpace(matches[1]),
		})
		return
	}

	// Extract predictions
	if matches := p.patterns["prediction"].FindStringSubmatch(line); matches != nil {
		corr.Actions = append(corr.Actions, Action{
			Type:        "prediction",
			Description: strings.TrimSpace(matches[1]),
		})
		return
	}

	// Extract recommendations
	if matches := p.patterns["recommendation"].FindStringSubmatch(line); matches != nil {
		corr.Actions = append(corr.Actions, Action{
			Type:        "recommendation",
			Description: strings.TrimSpace(matches[1]),
		})
		return
	}

	// Extract metadata
	if matches := p.patterns["severity"].FindStringSubmatch(line); matches != nil {
		corr.Metadata["severity"] = strings.ToLower(matches[1])
		return
	}

	if matches := p.patterns["confidence"].FindStringSubmatch(line); matches != nil {
		corr.Metadata["confidence"] = matches[1]
		return
	}

	if matches := p.patterns["category"].FindStringSubmatch(line); matches != nil {
		corr.Metadata["category"] = matches[1]
		return
	}

	// Otherwise, add to description
	if *section == "description" {
		if corr.RawDescription != "" {
			corr.RawDescription += " "
		}
		corr.RawDescription += trimmedLine
	}
}

// extractConditions parses condition text into structured conditions
func (p *CorrelationMarkdownParser) extractConditions(text string) []Condition {
	var conditions []Condition

	// Look for threshold patterns (e.g., "memory > 80%")
	thresholdMatches := p.patterns["threshold"].FindAllStringSubmatch(text, -1)
	for _, match := range thresholdMatches {
		condition := Condition{
			Type:     "threshold",
			Resource: match[1],
			Operator: match[2],
			Value:    match[3],
			Unit:     match[4],
		}

		// Extract duration if present
		if durationMatch := p.patterns["duration"].FindStringSubmatch(text); durationMatch != nil {
			duration, _ := parseDuration(durationMatch[1], durationMatch[2])
			condition.Duration = duration
		}

		conditions = append(conditions, condition)
	}

	// If no threshold found, create a text condition
	if len(conditions) == 0 {
		conditions = append(conditions, Condition{
			Type:        "text",
			Description: strings.TrimSpace(text),
		})
	}

	return conditions
}

// extractActions parses action text into structured actions
func (p *CorrelationMarkdownParser) extractActions(text string) []Action {
	var actions []Action

	// Default to insight type if not specific
	actions = append(actions, Action{
		Type:        "insight",
		Description: strings.TrimSpace(text),
	})

	return actions
}

// parseYAMLBlock parses YAML content within code blocks
func (p *CorrelationMarkdownParser) parseYAMLBlock(yaml string, corr *MarkdownCorrelation) {
	// Simple YAML parsing for common patterns
	lines := strings.Split(yaml, "\n")
	for _, line := range lines {
		if matches := p.patterns["yaml_key"].FindStringSubmatch(line); matches != nil {
			key := matches[1]
			value := strings.TrimSpace(matches[2])

			// Remove quotes if present
			if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				value = value[1 : len(value)-1]
			}

			corr.Metadata[key] = value
		}
	}
}

// parseDuration converts duration text to time.Duration
func parseDuration(value, unit string) (time.Duration, error) {
	// Normalize unit
	unit = strings.ToLower(unit)
	switch unit {
	case "second", "seconds", "s":
		unit = "s"
	case "minute", "minutes", "m":
		unit = "m"
	case "hour", "hours", "h":
		unit = "h"
	}

	// Parse duration
	return time.ParseDuration(value + unit)
}

// MarkdownCorrelation represents a correlation rule extracted from markdown
type MarkdownCorrelation struct {
	Name           string
	RawDescription string
	Conditions     []Condition
	Actions        []Action
	Metadata       map[string]interface{}
}

// Condition represents a correlation condition
type Condition struct {
	Type        string        // threshold, text, pattern
	Resource    string        // e.g., memory, cpu, latency
	Operator    string        // >, <, =, >=, <=
	Value       string        // threshold value
	Unit        string        // %, ms, etc.
	Duration    time.Duration // how long condition must be true
	Description string        // for text conditions
}

// Action represents a correlation action/outcome
type Action struct {
	Type        string // insight, prediction, root_cause, recommendation
	Description string
	Metadata    map[string]interface{}
}
