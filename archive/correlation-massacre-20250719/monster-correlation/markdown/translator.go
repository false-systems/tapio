package markdown

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// CorrelationTranslator converts markdown correlations to correlation engine rules
type CorrelationTranslator struct {
	parser *CorrelationMarkdownParser
}

// NewCorrelationTranslator creates a new translator
func NewCorrelationTranslator() *CorrelationTranslator {
	return &CorrelationTranslator{
		parser: NewCorrelationMarkdownParser(),
	}
}

// TranslateMarkdownToRules converts markdown content to correlation rules
func (t *CorrelationTranslator) TranslateMarkdownToRules(markdownContent string) ([]*correlation.SemanticRule, error) {
	// Parse markdown to extract correlations
	markdownCorrelations, err := t.parser.ParseCorrelationMarkdown(markdownContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Convert to semantic rules
	var rules []*correlation.SemanticRule
	for _, mc := range markdownCorrelations {
		rule, err := t.convertToSemanticRule(mc)
		if err != nil {
			// Log error but continue with other rules
			fmt.Printf("Warning: failed to convert rule '%s': %v\n", mc.Name, err)
			continue
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

// TranslateMarkdownToJSON converts markdown to JSON rules for loading
func (t *CorrelationTranslator) TranslateMarkdownToJSON(markdownContent string) ([]byte, error) {
	rules, err := t.TranslateMarkdownToRules(markdownContent)
	if err != nil {
		return nil, err
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rules to JSON: %w", err)
	}

	return jsonData, nil
}

// convertToSemanticRule converts a markdown correlation to a semantic rule
func (t *CorrelationTranslator) convertToSemanticRule(mc *MarkdownCorrelation) (*correlation.SemanticRule, error) {
	// Generate rule ID from name
	ruleID := strings.ToLower(strings.ReplaceAll(mc.Name, " ", "_"))
	ruleID = "user_" + ruleID

	// Build semantic conditions
	semanticConditions := t.buildSemanticConditions(mc.Conditions)

	// Build actions
	actions := t.buildSemanticActions(mc.Actions)

	// Extract metadata
	severity := t.extractSeverity(mc.Metadata)
	confidence := t.extractConfidence(mc.Metadata)
	category := t.extractCategory(mc.Metadata)

	rule := &correlation.SemanticRule{
		ID:          ruleID,
		Name:        mc.Name,
		Description: mc.RawDescription,
		Version:     "1.0",
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),

		// Conditions
		SemanticConditions: semanticConditions,

		// Actions
		Actions: actions,

		// Metadata
		Metadata: map[string]interface{}{
			"source":           "markdown",
			"user_defined":     true,
			"confidence_score": confidence,
			"category":         category,
			"severity":         severity,
			"author":           "markdown-translator",
		},

		// Set priority based on severity
		Priority:   t.severityToPriority(severity),
		Confidence: float32(confidence),
		
		// Performance hints
		Performance: &correlation.PerformanceHints{
			CacheKey:       "semantic_rule",
			CacheDuration:  300, // 5 minutes in seconds
			Parallelizable: true,
			Priority:       t.severityToPriority(severity),
		},
	}

	return rule, nil
}

// buildSemanticConditions creates semantic conditions from parsed conditions
func (t *CorrelationTranslator) buildSemanticConditions(conditions []Condition) *correlation.SemanticConditions {
	sc := &correlation.SemanticConditions{
		EventTypePatterns: []string{},
		SemanticFeatures:  make(map[string]*correlation.FeatureCondition),
	}

	for _, cond := range conditions {
		switch cond.Type {
		case "threshold":
			// Create a feature condition for threshold
			sc.SemanticFeatures[cond.Resource] = &correlation.FeatureCondition{
				Feature:  cond.Resource,
				Operator: cond.Operator,
				Value:    t.parseValue(cond.Value),
			}

		case "event_type":
			// Add event type pattern
			sc.EventTypePatterns = append(sc.EventTypePatterns, cond.Value)

		case "text":
			// Create a description matching condition
			if sc.DescriptionMatching == nil {
				sc.DescriptionMatching = &correlation.DescriptionMatcher{
					Pattern:      cond.Description,
					MinScore:     0.8,
					UseEmbedding: true,
				}
			}

		default:
			// Store other conditions as features
			sc.SemanticFeatures[cond.Type] = &correlation.FeatureCondition{
				Feature:  cond.Type,
				Operator: "equals",
				Value:    cond.Value,
			}
		}
	}

	// If we have multiple conditions, also create natural language description
	return sc
}

// buildSemanticActions creates semantic actions from parsed actions
func (t *CorrelationTranslator) buildSemanticActions(actions []Action) []*correlation.SemanticAction {
	var semanticActions []*correlation.SemanticAction

	for _, action := range actions {
		sa := &correlation.SemanticAction{
			Type:   t.mapActionType(action.Type),
			Target: action.Description,
			Parameters: map[string]interface{}{
				"description": action.Description,
			},
		}

		// Add specific configurations based on action type
		switch action.Type {
		case "root_cause":
			sa.Parameters["root_cause_description"] = action.Description
			sa.Confidence = 0.8

		case "prediction":
			sa.Parameters["prediction_description"] = action.Description
			sa.Parameters["time_horizon"] = "5m"

		case "recommendation":
			sa.Parameters["recommendation"] = action.Description
			sa.Parameters["auto_apply"] = false
		}

		semanticActions = append(semanticActions, sa)
	}

	// Always add a create finding action
	semanticActions = append(semanticActions, &correlation.SemanticAction{
		Type:   "create_finding",
		Target: "finding",
		Parameters: map[string]interface{}{
			"template": "User-defined correlation detected: {{.rule.Name}}",
		},
	})

	return semanticActions
}

// Helper methods

func (t *CorrelationTranslator) parseValue(value string) interface{} {
	// Try to parse as float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	// Try to parse as int
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}
	// Return as string
	return value
}

func (t *CorrelationTranslator) mapActionType(actionType string) string {
	switch actionType {
	case "root_cause":
		return "identify_root_cause"
	case "prediction":
		return "predict_issue"
	case "recommendation":
		return "recommend_action"
	default:
		return "create_insight"
	}
}

func (t *CorrelationTranslator) extractSeverity(metadata map[string]interface{}) string {
	if severity, ok := metadata["severity"].(string); ok {
		return severity
	}
	return "medium"
}

func (t *CorrelationTranslator) extractConfidence(metadata map[string]interface{}) float64 {
	if conf, ok := metadata["confidence"].(string); ok {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(conf, "%"), 64); err == nil {
			return f / 100.0
		}
	}
	return 0.7 // default confidence
}

func (t *CorrelationTranslator) extractCategory(metadata map[string]interface{}) string {
	if category, ok := metadata["category"].(string); ok {
		return category
	}
	return "user_defined"
}

func (t *CorrelationTranslator) buildNaturalLanguageDescription(conditions []Condition) string {
	var parts []string
	for _, cond := range conditions {
		switch cond.Type {
		case "threshold":
			part := fmt.Sprintf("%s %s %s%s", cond.Resource, cond.Operator, cond.Value, cond.Unit)
			if cond.Duration > 0 {
				part += fmt.Sprintf(" for %s", cond.Duration)
			}
			parts = append(parts, part)
		case "text":
			parts = append(parts, cond.Description)
		}
	}
	return "When " + strings.Join(parts, " and ")
}

// LoadMarkdownRulesIntoEngine loads markdown rules directly into a semantic rules engine
func (t *CorrelationTranslator) LoadMarkdownRulesIntoEngine(markdownContent string, engine *correlation.SemanticRulesEngine) error {
	// Convert markdown to JSON
	jsonData, err := t.TranslateMarkdownToJSON(markdownContent)
	if err != nil {
		return fmt.Errorf("failed to translate markdown to JSON: %w", err)
	}

	// Load JSON rules into engine
	if err := engine.LoadSemanticRulesFromJSON(jsonData); err != nil {
		return fmt.Errorf("failed to load rules into engine: %w", err)
	}

	return nil
}

// UpdateMarkdownRulesInEngine updates/creates markdown rules in a semantic rules engine
func (t *CorrelationTranslator) UpdateMarkdownRulesInEngine(markdownContent string, engine *correlation.SemanticRulesEngine) error {
	// Convert markdown to rules
	rules, err := t.TranslateMarkdownToRules(markdownContent)
	if err != nil {
		return fmt.Errorf("failed to translate markdown to rules: %w", err)
	}

	// Update each rule individually
	for _, rule := range rules {
		if err := engine.UpdateSemanticRule(rule); err != nil {
			return fmt.Errorf("failed to update rule %s: %w", rule.ID, err)
		}
	}

	return nil
}

// DeleteRulesFromEngine deletes rules by ID from a semantic rules engine
func (t *CorrelationTranslator) DeleteRulesFromEngine(ruleIDs []string, engine *correlation.SemanticRulesEngine) error {
	var errors []string

	for _, ruleID := range ruleIDs {
		if err := engine.DeleteSemanticRule(ruleID); err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete rule %s: %v", ruleID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("delete errors:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// Helper methods

func (t *CorrelationTranslator) severityToPriority(severity string) int {
	switch severity {
	case "critical":
		return 10
	case "high":
		return 8
	case "medium":
		return 5
	case "low":
		return 3
	default:
		return 1
	}
}

// parseValue (already defined above)
