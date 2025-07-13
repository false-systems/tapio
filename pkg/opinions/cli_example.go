package opinions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CLITranslator provides CLI functionality for opinion translation
type CLITranslator struct {
	translator *Translator
	validator  *OpinionValidator
}

// NewCLITranslator creates a new CLI translator
func NewCLITranslator() *CLITranslator {
	return &CLITranslator{
		translator: NewTranslator(),
		validator:  NewOpinionValidator(),
	}
}

// TranslateFile translates a markdown file to opinion config
func (c *CLITranslator) TranslateFile(inputPath, outputPath string) error {
	// Read input file
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Translate markdown to config
	config, err := c.translator.TranslateMarkdown(string(content))
	if err != nil {
		return fmt.Errorf("translation failed: %w", err)
	}

	// Validate the config
	if err := c.validator.Validate(config); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Marshal to YAML
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write output file
	if err := os.WriteFile(outputPath, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// ExportToMarkdown exports opinion config to markdown
func (c *CLITranslator) ExportToMarkdown(inputPath, outputPath string) error {
	// Read YAML file
	yamlData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Unmarshal config
	var config OpinionConfig
	if err := yaml.Unmarshal(yamlData, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Convert to markdown
	markdown, err := c.translator.TranslateToMarkdown(&config)
	if err != nil {
		return fmt.Errorf("failed to convert to markdown: %w", err)
	}

	// Write output file
	if err := os.WriteFile(outputPath, []byte(markdown), 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// ValidateFile validates an opinion config file
func (c *CLITranslator) ValidateFile(path string) (*ValidationResult, error) {
	// Determine file type
	ext := filepath.Ext(path)
	
	var config *OpinionConfig
	var err error

	switch ext {
	case ".md", ".markdown":
		// Read and translate markdown
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		config, err = c.translator.TranslateMarkdown(string(content))
		if err != nil {
			return nil, fmt.Errorf("translation failed: %w", err)
		}

	case ".yaml", ".yml":
		// Read YAML directly
		yamlData, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		config = &OpinionConfig{}
		if err := yaml.Unmarshal(yamlData, config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	// Validate
	result := c.validator.ValidateWithDetails(config)
	return result, nil
}

// InteractiveBuilder builds opinions interactively
type InteractiveBuilder struct {
	reader io.Reader
	writer io.Writer
}

// NewInteractiveBuilder creates a new interactive builder
func NewInteractiveBuilder(reader io.Reader, writer io.Writer) *InteractiveBuilder {
	return &InteractiveBuilder{
		reader: reader,
		writer: writer,
	}
}

// Build creates markdown opinions interactively
func (b *InteractiveBuilder) Build() (string, error) {
	fmt.Fprintln(b.writer, "🎯 Tapio Opinion Builder")
	fmt.Fprintln(b.writer, "========================")
	fmt.Fprintln(b.writer)

	// Collect basic information
	clusterName := b.prompt("Cluster name", "my-cluster")
	clusterType := b.promptChoice("Cluster type", []string{"production", "staging", "development"}, "production")
	workloadType := b.promptChoice("Workload type", []string{"stateless", "stateful", "batch", "mixed"}, "stateless")

	// Memory configuration
	fmt.Fprintln(b.writer, "\n## Memory Configuration")
	memoryThreshold := b.promptInt("Acceptable memory usage (%)", 90)
	oomWindow := b.promptDuration("OOM prediction window", "5m")

	// Correlation configuration
	fmt.Fprintln(b.writer, "\n## Correlation Windows")
	restartWindow := b.promptDuration("OOM to restart time", "30s")

	// Service importance
	fmt.Fprintln(b.writer, "\n## Service Importance")
	criticalServices := b.promptList("Critical services (comma-separated)", "api-gateway,payment-service")

	// Build markdown
	markdown := fmt.Sprintf(`# %s Opinions
Cluster Type: %s
Workload Type: %s

## 🧠 Memory Management
- **Acceptable memory usage**: %d%%
- **OOM prediction window**: %s

## 🔗 Correlation Windows
- **OOM → Pod Restart**: %s

## ⚖️ Service Importance
`, clusterName, clusterType, workloadType, memoryThreshold, oomWindow, restartWindow)

	// Add service weights
	if len(criticalServices) > 0 {
		markdown += "```yaml\nservice_weights:\n"
		for _, service := range criticalServices {
			markdown += fmt.Sprintf("  %s: 1.0    # Critical\n", service)
		}
		markdown += "```\n"
	}

	return markdown, nil
}

func (b *InteractiveBuilder) prompt(question, defaultValue string) string {
	fmt.Fprintf(b.writer, "%s [%s]: ", question, defaultValue)
	
	var input string
	fmt.Fscanln(b.reader, &input)
	
	if input == "" {
		return defaultValue
	}
	return input
}

func (b *InteractiveBuilder) promptChoice(question string, choices []string, defaultChoice string) string {
	fmt.Fprintf(b.writer, "%s\n", question)
	for i, choice := range choices {
		marker := " "
		if choice == defaultChoice {
			marker = "*"
		}
		fmt.Fprintf(b.writer, "  %s [%d] %s\n", marker, i+1, choice)
	}
	fmt.Fprintf(b.writer, "Choice [%s]: ", defaultChoice)
	
	var input string
	fmt.Fscanln(b.reader, &input)
	
	if input == "" {
		return defaultChoice
	}
	
	// Try to parse as number
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err == nil && idx > 0 && idx <= len(choices) {
		return choices[idx-1]
	}
	
	// Return as-is if it matches a choice
	for _, choice := range choices {
		if input == choice {
			return input
		}
	}
	
	return defaultChoice
}

func (b *InteractiveBuilder) promptInt(question string, defaultValue int) int {
	fmt.Fprintf(b.writer, "%s [%d]: ", question, defaultValue)
	
	var input string
	fmt.Fscanln(b.reader, &input)
	
	if input == "" {
		return defaultValue
	}
	
	var value int
	if _, err := fmt.Sscanf(input, "%d", &value); err == nil {
		return value
	}
	
	return defaultValue
}

func (b *InteractiveBuilder) promptDuration(question, defaultValue string) string {
	fmt.Fprintf(b.writer, "%s [%s]: ", question, defaultValue)
	
	var input string
	fmt.Fscanln(b.reader, &input)
	
	if input == "" {
		return defaultValue
	}
	return input
}

func (b *InteractiveBuilder) promptList(question, defaultValue string) []string {
	fmt.Fprintf(b.writer, "%s [%s]: ", question, defaultValue)
	
	var input string
	fmt.Fscanln(b.reader, &input)
	
	if input == "" {
		input = defaultValue
	}
	
	// Split by comma and trim
	var result []string
	for _, item := range strings.Split(input, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	
	return result
}

// Example templates that can be used as starting points
var ExampleTemplates = map[string]string{
	"simple": `# My Cluster Opinions

## Memory Management
- **Acceptable memory usage**: 90%

## Correlation Windows
- **OOM → Pod Restart**: 30 seconds
`,

	"detailed": `# Production API Cluster
Author: [Your Name]
Date: [Date]
Cluster Type: production
Workload Type: stateless

## Overview
[Describe your cluster and workload characteristics]

## 🧠 Memory Management
- **Acceptable memory usage**: 85%
  - Why: [Explain your reasoning]
- **OOM prediction window**: 5 minutes
  - Why: [Explain your reasoning]

## 🔗 Correlation Windows
- **OOM → Pod Restart**: 45 seconds
  - Why: [Explain graceful shutdown needs]

## 🚨 Anomaly Detection

### Time-based Sensitivity
| Time Period | Sensitivity | Description |
|-------------|-------------|-------------|
| Business Hours | 0.7 | High sensitivity |
| Night | 0.9 | Low sensitivity |
| Weekends | 0.8 | Medium sensitivity |

## ⚖️ Service Importance
` + "```yaml" + `
service_weights:
  api-gateway: 1.0      # Always critical
  worker: 0.5           # Medium importance
  analytics: 0.3        # Best effort
` + "```",

	"stateful": `# Stateful Database Cluster
Cluster Type: production
Workload Type: stateful

## Overview
This cluster runs stateful database workloads that require careful memory management
and longer shutdown times for data persistence.

## 🧠 Memory Management
- **Acceptable memory usage**: 80%
  - Why: Databases need buffer for unexpected queries
- **Disk usage threshold**: 85%
  - Why: Need space for WAL and temporary files

## 🔗 Correlation Windows
- **OOM → Pod Restart**: 60 seconds
  - Why: Database needs time for checkpoint and clean shutdown
- **Replication lag threshold**: 30 seconds
  - Why: Indicates potential issues with replica

## 📊 Behavioral Settings
- **Learning window**: 14 days
  - Why: Databases have weekly patterns
- **Deviation sensitivity**: 0.9
  - Why: Database behavior should be very predictable
`,
}