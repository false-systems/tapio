package opinions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Translator converts markdown templates to OpinionConfig
type Translator struct {
	parser    *MarkdownParser
	extractor *RuleExtractor
	enricher  *OpinionEnricher
	validator *OpinionValidator
}

// NewTranslator creates a new markdown to opinions translator
func NewTranslator() *Translator {
	return &Translator{
		parser:    NewMarkdownParser(),
		extractor: NewRuleExtractor(),
		enricher:  NewOpinionEnricher(),
		validator: NewOpinionValidator(),
	}
}

// TranslateMarkdown converts a markdown file to OpinionConfig
func (t *Translator) TranslateMarkdown(markdown string) (*OpinionConfig, error) {
	// 1. Parse markdown into sections
	doc, err := t.parser.Parse(markdown)
	if err != nil {
		return nil, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// 2. Extract rules from each section
	rules := t.extractor.ExtractRules(doc)

	// 3. Build initial config from rules
	config := t.buildConfig(rules)

	// 4. Enrich with smart defaults
	enriched := t.enricher.Enrich(config, doc.Metadata)

	// 5. Validate the configuration
	if err := t.validator.Validate(enriched); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return enriched, nil
}

// TranslateToMarkdown converts OpinionConfig to markdown
func (t *Translator) TranslateToMarkdown(config *OpinionConfig) (string, error) {
	var builder strings.Builder

	// Header
	builder.WriteString("# Cluster Opinions Configuration\n\n")
	builder.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	// Memory Management section
	t.writeMemorySection(&builder, config)

	// Correlation section
	t.writeCorrelationSection(&builder, config)

	// Anomaly Detection section
	t.writeAnomalySection(&builder, config)

	// Behavioral section
	t.writeBehavioralSection(&builder, config)

	// Service Weights section
	t.writeServiceWeightsSection(&builder, config)

	return builder.String(), nil
}

// MarkdownDocument represents a parsed markdown document
type MarkdownDocument struct {
	Title    string
	Metadata map[string]string
	Sections []*Section
}

// Section represents a markdown section
type Section struct {
	Level   int
	Title   string
	Content []ContentBlock
}

// ContentBlock represents a block of content
type ContentBlock struct {
	Type  string // paragraph, list, code, table
	Text  string
	Items []string // for lists
	Code  string   // for code blocks
	Table *Table   // for tables
}

// Table represents a markdown table
type Table struct {
	Headers []string
	Rows    [][]string
}

// ExtractedRule represents a rule extracted from markdown
type ExtractedRule struct {
	Section  string
	Key      string
	Value    interface{}
	Context  string // surrounding text for context
	Confidence float32
}

// buildConfig builds OpinionConfig from extracted rules
func (t *Translator) buildConfig(rules []ExtractedRule) *OpinionConfig {
	config := &OpinionConfig{
		ImportanceWeights:  make(map[string]float32),
		CorrelationWindows: make(map[string]time.Duration),
		AnomalyThresholds:  make(map[string]float32),
		ServiceLimits:      make(map[string]ServiceLimit),
		Metadata:           make(map[string]string),
	}

	for _, rule := range rules {
		t.applyRule(config, rule)
	}

	return config
}

// applyRule applies an extracted rule to the config
func (t *Translator) applyRule(config *OpinionConfig, rule ExtractedRule) {
	switch rule.Section {
	case "memory":
		t.applyMemoryRule(config, rule)
	case "correlation":
		t.applyCorrelationRule(config, rule)
	case "anomaly":
		t.applyAnomalyRule(config, rule)
	case "behavioral":
		t.applyBehavioralRule(config, rule)
	case "weights":
		t.applyWeightRule(config, rule)
	}
}

// Write sections for markdown generation
func (t *Translator) writeMemorySection(builder *strings.Builder, config *OpinionConfig) {
	builder.WriteString("## 🧠 Memory Management\n\n")

	if threshold, exists := config.AnomalyThresholds["memory_usage"]; exists {
		builder.WriteString(fmt.Sprintf("### Memory Usage Threshold\n"))
		builder.WriteString(fmt.Sprintf("- **Acceptable memory usage**: %.0f%%\n", threshold*100))
		builder.WriteString(fmt.Sprintf("  - Alert when memory exceeds this threshold\n\n"))
	}

	if window, exists := config.PredictionConfig.PredictionWindows["oom"]; exists {
		builder.WriteString(fmt.Sprintf("### OOM Prediction\n"))
		builder.WriteString(fmt.Sprintf("- **Prediction window**: %s\n", window))
		builder.WriteString(fmt.Sprintf("  - Start predicting OOM this far in advance\n\n"))
	}

	// Service-specific limits
	if len(config.ServiceLimits) > 0 {
		builder.WriteString("### Service-Specific Limits\n")
		for service, limit := range config.ServiceLimits {
			if limit.MemoryLimit > 0 {
				builder.WriteString(fmt.Sprintf("- `%s` pods can use up to **%.0f%% memory**\n", 
					service, limit.MemoryLimit*100))
			}
		}
		builder.WriteString("\n")
	}
}

func (t *Translator) writeCorrelationSection(builder *strings.Builder, config *OpinionConfig) {
	builder.WriteString("## 🔗 Correlation Windows\n\n")

	if window, exists := config.CorrelationWindows["oom_restart"]; exists {
		builder.WriteString(fmt.Sprintf("### OOM to Restart Correlation\n"))
		builder.WriteString(fmt.Sprintf("- **Expected time**: %s\n", window))
		builder.WriteString(fmt.Sprintf("  - How long after OOM to expect pod restart\n\n"))
	}

	// Service dependencies
	if len(config.ServiceDependencies) > 0 {
		builder.WriteString("### Service Dependencies\n")
		builder.WriteString("When issues occur, expect cascading effects:\n\n")
		
		for _, dep := range config.ServiceDependencies {
			builder.WriteString(fmt.Sprintf("- **%s** → **%s** within **%s**\n",
				dep.Source, dep.Target, dep.ExpectedDelay))
		}
		builder.WriteString("\n")
	}
}

func (t *Translator) writeAnomalySection(builder *strings.Builder, config *OpinionConfig) {
	builder.WriteString("## 🚨 Anomaly Detection\n\n")

	// Time-based rules
	if len(config.TimeBasedRules) > 0 {
		builder.WriteString("### Time-based Sensitivity\n\n")
		builder.WriteString("| Time Period | Sensitivity | Description |\n")
		builder.WriteString("|-------------|-------------|-------------|\n")
		
		for _, rule := range config.TimeBasedRules {
			builder.WriteString(fmt.Sprintf("| %s | %.1f | %s |\n",
				rule.Period, rule.Sensitivity, rule.Description))
		}
		builder.WriteString("\n")
	}
}

func (t *Translator) writeBehavioralSection(builder *strings.Builder, config *OpinionConfig) {
	builder.WriteString("## 📊 Behavioral Settings\n\n")

	if config.BehavioralConfig.LearningWindow > 0 {
		builder.WriteString(fmt.Sprintf("- **Learning window**: %s\n", config.BehavioralConfig.LearningWindow))
		builder.WriteString("  - How much history to use for behavior analysis\n")
	}

	if config.BehavioralConfig.DeviationSensitivity > 0 {
		builder.WriteString(fmt.Sprintf("- **Deviation sensitivity**: %.1f\n", config.BehavioralConfig.DeviationSensitivity))
		builder.WriteString("  - How sensitive to behavioral changes (0-1)\n")
	}

	builder.WriteString("\n")
}

func (t *Translator) writeServiceWeightsSection(builder *strings.Builder, config *OpinionConfig) {
	if len(config.ImportanceWeights) == 0 {
		return
	}

	builder.WriteString("## ⚖️ Service Importance\n\n")
	builder.WriteString("```yaml\n")
	builder.WriteString("service_weights:\n")
	
	// Sort by weight for readability
	type weightPair struct {
		service string
		weight  float32
	}
	pairs := make([]weightPair, 0, len(config.ImportanceWeights))
	for svc, weight := range config.ImportanceWeights {
		pairs = append(pairs, weightPair{svc, weight})
	}
	
	// Sort by weight descending
	for i := range pairs {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].weight > pairs[i].weight {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	
	for _, pair := range pairs {
		comment := ""
		if pair.weight == 1.0 {
			comment = "    # Always critical"
		} else if pair.weight < 0.5 {
			comment = "    # Best effort"
		}
		builder.WriteString(fmt.Sprintf("  %s: %.1f%s\n", pair.service, pair.weight, comment))
	}
	
	builder.WriteString("```\n\n")
}

// Helper methods for rule application
func (t *Translator) applyMemoryRule(config *OpinionConfig, rule ExtractedRule) {
	switch rule.Key {
	case "memory_threshold":
		if val, ok := rule.Value.(float32); ok {
			config.AnomalyThresholds["memory_usage"] = val
		}
	case "oom_prediction_window":
		if val, ok := rule.Value.(time.Duration); ok {
			if config.PredictionConfig.PredictionWindows == nil {
				config.PredictionConfig.PredictionWindows = make(map[string]time.Duration)
			}
			config.PredictionConfig.PredictionWindows["oom"] = val
		}
	case "service_memory_limit":
		if service, ok := rule.Context.(string); ok {
			if val, ok := rule.Value.(float32); ok {
				if config.ServiceLimits == nil {
					config.ServiceLimits = make(map[string]ServiceLimit)
				}
				limit := config.ServiceLimits[service]
				limit.MemoryLimit = val
				config.ServiceLimits[service] = limit
			}
		}
	}
}

func (t *Translator) applyCorrelationRule(config *OpinionConfig, rule ExtractedRule) {
	switch rule.Key {
	case "oom_restart_window":
		if val, ok := rule.Value.(time.Duration); ok {
			config.CorrelationWindows["oom_restart"] = val
		}
	case "service_dependency":
		if dep, ok := rule.Value.(ServiceDependency); ok {
			config.ServiceDependencies = append(config.ServiceDependencies, dep)
		}
	}
}

func (t *Translator) applyAnomalyRule(config *OpinionConfig, rule ExtractedRule) {
	switch rule.Key {
	case "time_based_sensitivity":
		if tbr, ok := rule.Value.(TimeBasedRule); ok {
			config.TimeBasedRules = append(config.TimeBasedRules, tbr)
		}
	case "anomaly_threshold":
		if val, ok := rule.Value.(float32); ok {
			if metric, ok := rule.Context.(string); ok {
				config.AnomalyThresholds[metric] = val
			}
		}
	}
}

func (t *Translator) applyBehavioralRule(config *OpinionConfig, rule ExtractedRule) {
	switch rule.Key {
	case "learning_window":
		if val, ok := rule.Value.(time.Duration); ok {
			config.BehavioralConfig.LearningWindow = val
		}
	case "deviation_sensitivity":
		if val, ok := rule.Value.(float32); ok {
			config.BehavioralConfig.DeviationSensitivity = val
		}
	}
}

func (t *Translator) applyWeightRule(config *OpinionConfig, rule ExtractedRule) {
	if service, ok := rule.Key.(string); ok {
		if weight, ok := rule.Value.(float32); ok {
			config.ImportanceWeights[service] = weight
		}
	}
}