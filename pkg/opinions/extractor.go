package opinions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RuleExtractor extracts configuration rules from markdown
type RuleExtractor struct {
	patterns []ExtractionPattern
}

// ExtractionPattern defines a pattern for extracting rules
type ExtractionPattern struct {
	Name    string
	Pattern *regexp.Regexp
	Section string
	Extract func(matches []string, context string) ExtractedRule
}

// NewRuleExtractor creates a new rule extractor with default patterns
func NewRuleExtractor() *RuleExtractor {
	return &RuleExtractor{
		patterns: []ExtractionPattern{
			// Memory patterns
			{
				Name:    "memory_percentage",
				Pattern: regexp.MustCompile(`(?i)memory.*?(\d+)%`),
				Section: "memory",
				Extract: func(matches []string, context string) ExtractedRule {
					val, _ := strconv.ParseFloat(matches[1], 32)
					return ExtractedRule{
						Section:    "memory",
						Key:        "memory_threshold",
						Value:      float32(val) / 100,
						Context:    context,
						Confidence: 0.9,
					}
				},
			},
			{
				Name:    "oom_window",
				Pattern: regexp.MustCompile(`(?i)oom.*?(\d+)\s*(seconds?|minutes?|s|m)`),
				Section: "memory",
				Extract: func(matches []string, context string) ExtractedRule {
					duration := parseDuration(matches[1], matches[2])
					return ExtractedRule{
						Section:    "memory",
						Key:        "oom_prediction_window",
						Value:      duration,
						Context:    context,
						Confidence: 0.9,
					}
				},
			},
			{
				Name:    "service_memory_limit",
				Pattern: regexp.MustCompile("`([^`]+)`.*?(?:can use up to|maximum|limit).*?(\\d+)%.*?memory"),
				Section: "memory",
				Extract: func(matches []string, context string) ExtractedRule {
					service := matches[1]
					val, _ := strconv.ParseFloat(matches[2], 32)
					return ExtractedRule{
						Section:    "memory",
						Key:        "service_memory_limit",
						Value:      float32(val) / 100,
						Context:    service,
						Confidence: 0.9,
					}
				},
			},

			// Correlation patterns
			{
				Name:    "oom_restart_time",
				Pattern: regexp.MustCompile(`(?i)(?:oom.*restart|restart.*oom).*?(\d+)\s*(seconds?|minutes?|s|m)`),
				Section: "correlation",
				Extract: func(matches []string, context string) ExtractedRule {
					duration := parseDuration(matches[1], matches[2])
					return ExtractedRule{
						Section:    "correlation",
						Key:        "oom_restart_window",
						Value:      duration,
						Context:    context,
						Confidence: 0.95,
					}
				},
			},
			{
				Name:    "service_dependency",
				Pattern: regexp.MustCompile(`\*\*([^*]+)\*\*.*?(?:errors?|issues?).*?within.*?\*\*(\d+)\s*(seconds?|minutes?|s|m)\*\*`),
				Section: "correlation",
				Extract: func(matches []string, context string) ExtractedRule {
					target := matches[1]
					delay := parseDuration(matches[2], matches[3])

					// Try to find source from context
					source := extractSourceFromContext(context)

					return ExtractedRule{
						Section: "correlation",
						Key:     "service_dependency",
						Value: ServiceDependency{
							Source:        source,
							Target:        target,
							ExpectedDelay: delay,
						},
						Context:    context,
						Confidence: 0.85,
					}
				},
			},

			// Weight patterns
			{
				Name:    "service_weight",
				Pattern: regexp.MustCompile(`(?:([a-z-]+)(?:-service)?)\s*(?:is|:).*?(?:weight:\s*|importance:\s*)?(\d*\.?\d+)`),
				Section: "weights",
				Extract: func(matches []string, context string) ExtractedRule {
					service := matches[1]
					weight, _ := strconv.ParseFloat(matches[2], 32)
					return ExtractedRule{
						Section:    "weights",
						Key:        service,
						Value:      float32(weight),
						Context:    context,
						Confidence: 0.8,
					}
				},
			},

			// Time-based sensitivity
			{
				Name:    "time_sensitivity",
				Pattern: regexp.MustCompile(`(?i)(business hours?|night|weekend).*?(?:sensitivity.*?)?(\d*\.?\d+)`),
				Section: "anomaly",
				Extract: func(matches []string, context string) ExtractedRule {
					period := matches[1]
					sensitivity, _ := strconv.ParseFloat(matches[2], 32)
					return ExtractedRule{
						Section: "anomaly",
						Key:     "time_based_sensitivity",
						Value: TimeBasedRule{
							Period:      period,
							Sensitivity: float32(sensitivity),
							Description: context,
						},
						Context:    context,
						Confidence: 0.85,
					}
				},
			},

			// Behavioral patterns
			{
				Name:    "learning_window",
				Pattern: regexp.MustCompile(`(?i)learn.*?(?:from|window|history).*?(\d+)\s*(days?|hours?|d|h)`),
				Section: "behavioral",
				Extract: func(matches []string, context string) ExtractedRule {
					duration := parseDuration(matches[1], matches[2])
					return ExtractedRule{
						Section:    "behavioral",
						Key:        "learning_window",
						Value:      duration,
						Context:    context,
						Confidence: 0.8,
					}
				},
			},
		},
	}
}

// ExtractRules extracts all rules from a markdown document
func (e *RuleExtractor) ExtractRules(doc *MarkdownDocument) []ExtractedRule {
	var rules []ExtractedRule

	// Extract from each section
	for _, section := range doc.Sections {
		sectionRules := e.extractFromSection(section)
		rules = append(rules, sectionRules...)
	}

	// Extract from tables
	tableRules := e.extractFromTables(doc)
	rules = append(rules, tableRules...)

	// Extract from code blocks
	codeRules := e.extractFromCodeBlocks(doc)
	rules = append(rules, codeRules...)

	// Sort by confidence
	e.sortByConfidence(rules)

	return rules
}

// extractFromSection extracts rules from a section
func (e *RuleExtractor) extractFromSection(section *Section) []ExtractedRule {
	var rules []ExtractedRule

	// Get section text
	text := section.GetContentText()

	// Apply patterns
	for _, pattern := range e.patterns {
		// Check if pattern applies to this section
		if !e.sectionMatches(section.Title, pattern.Section) {
			continue
		}

		// Find all matches
		matches := pattern.Pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			rule := pattern.Extract(match, text)
			rule.Section = e.normalizeSection(section.Title)
			rules = append(rules, rule)
		}
	}

	// Special handling for lists
	for _, block := range section.Content {
		if block.Type == "list" {
			listRules := e.extractFromList(block.Items, section.Title)
			rules = append(rules, listRules...)
		}
	}

	return rules
}

// extractFromList handles list items specially
func (e *RuleExtractor) extractFromList(items []string, sectionTitle string) []ExtractedRule {
	var rules []ExtractedRule

	for _, item := range items {
		// Apply all patterns to each list item
		for _, pattern := range e.patterns {
			if matches := pattern.Pattern.FindStringSubmatch(item); len(matches) > 0 {
				rule := pattern.Extract(matches, item)
				rule.Section = e.normalizeSection(sectionTitle)
				rules = append(rules, rule)
			}
		}

		// Special handling for key-value in lists
		if kv := parseKeyValue(item); kv != nil {
			rule := ExtractedRule{
				Section:    e.normalizeSection(sectionTitle),
				Key:        kv.Key,
				Value:      kv.Value,
				Context:    item,
				Confidence: 0.8,
			}
			rules = append(rules, rule)
		}
	}

	return rules
}

// extractFromTables extracts rules from tables
func (e *RuleExtractor) extractFromTables(doc *MarkdownDocument) []ExtractedRule {
	var rules []ExtractedRule

	for _, section := range doc.Sections {
		if table := section.FindTable(); table != nil {
			// Handle time-based sensitivity tables
			if e.isTimeSensitivityTable(table) {
				rules = append(rules, e.extractTimeSensitivityRules(table)...)
			}

			// Handle other table types
			// Add more table handlers as needed
		}
	}

	return rules
}

// extractFromCodeBlocks extracts rules from YAML/JSON code blocks
func (e *RuleExtractor) extractFromCodeBlocks(doc *MarkdownDocument) []ExtractedRule {
	var rules []ExtractedRule

	for _, section := range doc.Sections {
		if codeBlock := section.FindCodeBlock(); codeBlock != nil {
			// Parse YAML code blocks
			if strings.Contains(codeBlock.Code, "service_weights:") {
				rules = append(rules, e.parseServiceWeights(codeBlock.Code)...)
			}
		}
	}

	return rules
}

// isTimeSensitivityTable checks if a table contains time-based sensitivity
func (e *RuleExtractor) isTimeSensitivityTable(table *Table) bool {
	if len(table.Headers) < 2 {
		return false
	}

	// Check headers
	for _, header := range table.Headers {
		lower := strings.ToLower(header)
		if strings.Contains(lower, "time") || strings.Contains(lower, "period") {
			return true
		}
	}

	return false
}

// extractTimeSensitivityRules extracts rules from time sensitivity table
func (e *RuleExtractor) extractTimeSensitivityRules(table *Table) []ExtractedRule {
	var rules []ExtractedRule

	// Find column indices
	timeCol := -1
	sensitivityCol := -1
	descCol := -1

	for i, header := range table.Headers {
		lower := strings.ToLower(header)
		if strings.Contains(lower, "time") || strings.Contains(lower, "period") {
			timeCol = i
		} else if strings.Contains(lower, "sensitivity") {
			sensitivityCol = i
		} else if strings.Contains(lower, "desc") || strings.Contains(lower, "why") {
			descCol = i
		}
	}

	if timeCol == -1 || sensitivityCol == -1 {
		return rules
	}

	// Extract from rows
	for _, row := range table.Rows {
		if timeCol < len(row) && sensitivityCol < len(row) {
			period := row[timeCol]
			sensitivityStr := row[sensitivityCol]

			// Parse sensitivity (could be "High (0.7)" or just "0.7")
			sensitivity := parseSensitivity(sensitivityStr)

			desc := ""
			if descCol >= 0 && descCol < len(row) {
				desc = row[descCol]
			}

			rule := ExtractedRule{
				Section: "anomaly",
				Key:     "time_based_sensitivity",
				Value: TimeBasedRule{
					Period:      period,
					Sensitivity: sensitivity,
					Description: desc,
				},
				Context:    fmt.Sprintf("Table row: %s", strings.Join(row, " | ")),
				Confidence: 0.95,
			}
			rules = append(rules, rule)
		}
	}

	return rules
}

// parseServiceWeights parses service weights from YAML
func (e *RuleExtractor) parseServiceWeights(code string) []ExtractedRule {
	var rules []ExtractedRule

	lines := strings.Split(code, "\n")
	inServiceWeights := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.Contains(trimmed, "service_weights:") {
			inServiceWeights = true
			continue
		}

		if inServiceWeights && strings.HasPrefix(line, "  ") {
			// Parse service: weight
			parts := strings.Split(trimmed, ":")
			if len(parts) >= 2 {
				service := strings.TrimSpace(parts[0])
				weightStr := strings.TrimSpace(parts[1])

				// Remove comments
				if idx := strings.Index(weightStr, "#"); idx >= 0 {
					weightStr = strings.TrimSpace(weightStr[:idx])
				}

				if weight, err := strconv.ParseFloat(weightStr, 32); err == nil {
					rule := ExtractedRule{
						Section:    "weights",
						Key:        service,
						Value:      float32(weight),
						Context:    line,
						Confidence: 1.0, // Direct from YAML
					}
					rules = append(rules, rule)
				}
			}
		} else if inServiceWeights && !strings.HasPrefix(line, " ") {
			// End of service_weights section
			inServiceWeights = false
		}
	}

	return rules
}

// Helper functions

func (e *RuleExtractor) sectionMatches(sectionTitle, targetSection string) bool {
	lower := strings.ToLower(sectionTitle)
	target := strings.ToLower(targetSection)
	return strings.Contains(lower, target)
}

func (e *RuleExtractor) normalizeSection(title string) string {
	lower := strings.ToLower(title)

	if strings.Contains(lower, "memory") {
		return "memory"
	}
	if strings.Contains(lower, "correlation") || strings.Contains(lower, "depend") {
		return "correlation"
	}
	if strings.Contains(lower, "anomaly") || strings.Contains(lower, "sensitivity") {
		return "anomaly"
	}
	if strings.Contains(lower, "weight") || strings.Contains(lower, "importance") {
		return "weights"
	}
	if strings.Contains(lower, "behav") || strings.Contains(lower, "learn") {
		return "behavioral"
	}

	return "general"
}

func (e *RuleExtractor) sortByConfidence(rules []ExtractedRule) {
	// Simple bubble sort for confidence
	for i := range rules {
		for j := i + 1; j < len(rules); j++ {
			if rules[j].Confidence > rules[i].Confidence {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
}

// parseDuration converts number + unit to time.Duration
func parseDuration(value, unit string) time.Duration {
	num, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}

	switch strings.ToLower(unit) {
	case "s", "second", "seconds":
		return time.Duration(num) * time.Second
	case "m", "minute", "minutes":
		return time.Duration(num) * time.Minute
	case "h", "hour", "hours":
		return time.Duration(num) * time.Hour
	case "d", "day", "days":
		return time.Duration(num) * 24 * time.Hour
	default:
		return time.Duration(num) * time.Second
	}
}

// parseSensitivity extracts float value from sensitivity string
func parseSensitivity(s string) float32 {
	// Try to find a number
	re := regexp.MustCompile(`(\d*\.?\d+)`)
	if matches := re.FindStringSubmatch(s); len(matches) > 0 {
		if val, err := strconv.ParseFloat(matches[1], 32); err == nil {
			return float32(val)
		}
	}

	// Parse descriptive values
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "high"):
		return 0.7
	case strings.Contains(lower, "medium"):
		return 0.8
	case strings.Contains(lower, "low"):
		return 0.9
	default:
		return 0.8
	}
}

// extractSourceFromContext tries to find the source service from context
func extractSourceFromContext(context string) string {
	// Look for "When X has issues" pattern
	re := regexp.MustCompile(`(?i)when\s+\*?\*?([a-z-]+)(?:-service)?\*?\*?\s+(?:has|have)\s+(?:issues?|problems?)`)
	if matches := re.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// Default
	return "unknown"
}

// KeyValue represents a key-value pair
type KeyValue struct {
	Key   string
	Value interface{}
}

// parseKeyValue parses key-value from text
func parseKeyValue(text string) *KeyValue {
	// Look for "key: value" pattern
	re := regexp.MustCompile(`^\s*[-*]?\s*([^:]+):\s*(.+)$`)
	if matches := re.FindStringSubmatch(text); len(matches) > 2 {
		key := strings.TrimSpace(matches[1])
		value := strings.TrimSpace(matches[2])

		// Try to parse value type
		if floatVal, err := strconv.ParseFloat(value, 32); err == nil {
			return &KeyValue{Key: key, Value: float32(floatVal)}
		}

		return &KeyValue{Key: key, Value: value}
	}

	return nil
}
