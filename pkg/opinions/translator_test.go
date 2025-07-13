package opinions

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		validate func(t *testing.T, config *OpinionConfig, err error)
	}{
		{
			name: "basic memory configuration",
			markdown: `# Production Cluster Opinions

## Memory Management
- **Acceptable memory usage**: 85%
- OOM happens after **5 minutes** of high memory
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				require.NotNil(t, config)
				
				assert.Equal(t, float32(0.85), config.AnomalyThresholds["memory_usage"])
				assert.Equal(t, 5*time.Minute, config.PredictionConfig.PredictionWindows["oom"])
			},
		},
		{
			name: "correlation windows",
			markdown: `# My Cluster

## Correlation Windows
When OOM happens, pods restart after **45 seconds**.
Database timeout leads to API errors within **10 seconds**.
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Equal(t, 45*time.Second, config.CorrelationWindows["oom_restart"])
				// Should have extracted service dependency
				assert.Len(t, config.ServiceDependencies, 1)
			},
		},
		{
			name: "service weights from yaml",
			markdown: `# Service Configuration

## Service Importance

` + "```yaml" + `
service_weights:
  payment-service: 1.0      # Always critical
  analytics-service: 0.3    # Best effort
  api-gateway: 0.95
` + "```",
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Equal(t, float32(1.0), config.ImportanceWeights["payment-service"])
				assert.Equal(t, float32(0.3), config.ImportanceWeights["analytics-service"])
				assert.Equal(t, float32(0.95), config.ImportanceWeights["api-gateway"])
			},
		},
		{
			name: "time-based sensitivity table",
			markdown: `# Anomaly Detection

## Time-based Sensitivity

| Time Period | Sensitivity | Description |
|-------------|-------------|-------------|
| Business Hours (9-17 PST) | 0.7 | Customer traffic |
| Night (22-06 PST) | 0.9 | Batch processing |
| Weekends | 0.8 | Lower traffic |
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Len(t, config.TimeBasedRules, 3)
				
				// Find business hours rule
				var businessHours *TimeBasedRule
				for _, rule := range config.TimeBasedRules {
					if strings.Contains(rule.Period, "Business") {
						businessHours = &rule
						break
					}
				}
				
				require.NotNil(t, businessHours)
				assert.Equal(t, float32(0.7), businessHours.Sensitivity)
			},
		},
		{
			name: "service-specific limits",
			markdown: `# Resource Limits

## Memory Management

### Service-Specific Limits
- ` + "`batch-processor`" + ` pods can use up to **95% memory**
- ` + "`redis-cache`" + ` pods can use up to **70% memory**
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Equal(t, float32(0.95), config.ServiceLimits["batch-processor"].MemoryLimit)
				assert.Equal(t, float32(0.70), config.ServiceLimits["redis-cache"].MemoryLimit)
			},
		},
		{
			name: "metadata extraction",
			markdown: `# Production API Cluster
Author: John Doe
Date: 2024-01-20
Cluster Type: production
Workload Type: stateless

## Memory Management
Memory should be below 90%
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				// Metadata should be captured
				assert.NotNil(t, config.Metadata)
				
				// Should apply production defaults
				assert.True(t, config.PredictionConfig.EnableOOMPrediction)
			},
		},
		{
			name: "natural language patterns",
			markdown: `# Cluster Configuration

## Memory
I want alerts when memory usage is above **87%** because our apps 
typically run at 85% under normal load.

## Dependencies
When **database-primary** has issues, I expect to see:
- **api-gateway** errors within **10 seconds**
- **payment-service** errors within **15 seconds**
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Equal(t, float32(0.87), config.AnomalyThresholds["memory_usage"])
				
				// Should extract dependencies
				assert.GreaterOrEqual(t, len(config.ServiceDependencies), 2)
			},
		},
		{
			name: "behavioral configuration",
			markdown: `# Learning Configuration

## Behavioral Settings
- Learn from the last **14 days** of data
- Deviation sensitivity: **0.7**
`,
			validate: func(t *testing.T, config *OpinionConfig, err error) {
				require.NoError(t, err)
				
				assert.Equal(t, 14*24*time.Hour, config.BehavioralConfig.LearningWindow)
				assert.Equal(t, float32(0.7), config.BehavioralConfig.DeviationSensitivity)
			},
		},
	}

	translator := NewTranslator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := translator.TranslateMarkdown(tt.markdown)
			tt.validate(t, config, err)
		})
	}
}

func TestTranslateToMarkdown(t *testing.T) {
	config := &OpinionConfig{
		AnomalyThresholds: map[string]float32{
			"memory_usage": 0.85,
			"cpu_usage":    0.75,
		},
		CorrelationWindows: map[string]time.Duration{
			"oom_restart": 45 * time.Second,
		},
		ImportanceWeights: map[string]float32{
			"payment-service": 1.0,
			"analytics":       0.3,
		},
		BehavioralConfig: BehavioralOpinions{
			LearningWindow:       7 * 24 * time.Hour,
			DeviationSensitivity: 0.8,
		},
		ServiceDependencies: []ServiceDependency{
			{
				Source:        "database",
				Target:        "api",
				ExpectedDelay: 10 * time.Second,
			},
		},
		TimeBasedRules: []TimeBasedRule{
			{
				Period:      "Business Hours",
				Sensitivity: 0.7,
				Description: "High sensitivity during work hours",
			},
		},
	}

	translator := NewTranslator()
	markdown, err := translator.TranslateToMarkdown(config)
	
	require.NoError(t, err)
	require.NotEmpty(t, markdown)

	// Check that key sections are present
	assert.Contains(t, markdown, "## 🧠 Memory Management")
	assert.Contains(t, markdown, "85%")
	assert.Contains(t, markdown, "## 🔗 Correlation Windows")
	assert.Contains(t, markdown, "45s")
	assert.Contains(t, markdown, "## ⚖️ Service Importance")
	assert.Contains(t, markdown, "payment-service: 1.0")
	assert.Contains(t, markdown, "analytics: 0.3")
}

func TestMarkdownParser(t *testing.T) {
	markdown := `# Test Document
Author: Test Author
Version: 1.0

## First Section
This is a paragraph.

- List item 1
- List item 2

## Code Section
` + "```yaml" + `
key: value
another: 123
` + "```" + `

## Table Section
| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |
| Cell 3   | Cell 4   |
`

	parser := NewMarkdownParser()
	doc, err := parser.Parse(markdown)
	
	require.NoError(t, err)
	require.NotNil(t, doc)
	
	assert.Equal(t, "Test Document", doc.Title)
	assert.Equal(t, "Test Author", doc.Metadata["author"])
	assert.Equal(t, "1.0", doc.Metadata["version"])
	
	assert.Len(t, doc.Sections, 3)
	
	// Check first section
	firstSection := doc.Sections[0]
	assert.Equal(t, "First Section", firstSection.Title)
	assert.Len(t, firstSection.Content, 2) // paragraph and list
	
	// Check code section
	codeSection := doc.Sections[1]
	codeBlock := codeSection.FindCodeBlock()
	assert.NotNil(t, codeBlock)
	assert.Contains(t, codeBlock.Code, "key: value")
	
	// Check table section
	tableSection := doc.Sections[2]
	table := tableSection.FindTable()
	assert.NotNil(t, table)
	assert.Len(t, table.Headers, 2)
	assert.Len(t, table.Rows, 2)
}

func TestRuleExtractor(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected ExtractedRule
	}{
		{
			name: "memory percentage",
			text: "Memory usage should be below 85%",
			expected: ExtractedRule{
				Section:    "memory",
				Key:        "memory_threshold",
				Value:      float32(0.85),
				Confidence: 0.9,
			},
		},
		{
			name: "oom restart window",
			text: "OOM causes restart after 45 seconds",
			expected: ExtractedRule{
				Section:    "correlation",
				Key:        "oom_restart_window",
				Value:      45 * time.Second,
				Confidence: 0.95,
			},
		},
		{
			name: "service weight",
			text: "payment-service is critical with weight: 1.0",
			expected: ExtractedRule{
				Section:    "weights",
				Key:        "payment",
				Value:      float32(1.0),
				Confidence: 0.8,
			},
		},
	}

	extractor := NewRuleExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &MarkdownDocument{
				Sections: []*Section{
					{
						Title: tt.expected.Section,
						Content: []ContentBlock{
							{Type: "paragraph", Text: tt.text},
						},
					},
				},
			}
			
			rules := extractor.ExtractRules(doc)
			require.Len(t, rules, 1)
			
			rule := rules[0]
			assert.Equal(t, tt.expected.Key, rule.Key)
			
			// Check value based on type
			switch expected := tt.expected.Value.(type) {
			case float32:
				assert.InDelta(t, expected, rule.Value, 0.01)
			case time.Duration:
				assert.Equal(t, expected, rule.Value)
			}
		})
	}
}

func TestOpinionEnricher(t *testing.T) {
	tests := []struct {
		name     string
		config   *OpinionConfig
		metadata map[string]string
		validate func(t *testing.T, enriched *OpinionConfig)
	}{
		{
			name:   "production defaults",
			config: &OpinionConfig{},
			metadata: map[string]string{
				"cluster": "production",
			},
			validate: func(t *testing.T, enriched *OpinionConfig) {
				assert.True(t, enriched.PredictionConfig.EnableOOMPrediction)
				assert.Equal(t, float32(0.9), enriched.BehavioralConfig.DeviationSensitivity)
			},
		},
		{
			name:   "stateful workload defaults",
			config: &OpinionConfig{},
			metadata: map[string]string{
				"workload": "stateful",
			},
			validate: func(t *testing.T, enriched *OpinionConfig) {
				assert.Equal(t, float32(0.85), enriched.AnomalyThresholds["memory_usage"])
				assert.Equal(t, 60*time.Second, enriched.CorrelationWindows["oom_restart"])
			},
		},
		{
			name: "service weight inference",
			config: &OpinionConfig{
				ServiceLimits: map[string]ServiceLimit{
					"payment-api":       {},
					"analytics-worker":  {},
					"database-primary":  {},
				},
			},
			metadata: map[string]string{},
			validate: func(t *testing.T, enriched *OpinionConfig) {
				assert.Equal(t, float32(1.0), enriched.ImportanceWeights["payment-api"])
				assert.Equal(t, float32(0.3), enriched.ImportanceWeights["analytics-worker"])
				assert.Equal(t, float32(1.0), enriched.ImportanceWeights["database-primary"])
			},
		},
	}

	enricher := NewOpinionEnricher()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enriched := enricher.Enrich(tt.config, tt.metadata)
			tt.validate(t, enriched)
		})
	}
}

func TestOpinionValidator(t *testing.T) {
	tests := []struct {
		name      string
		config    *OpinionConfig
		wantError bool
		errMsg    string
	}{
		{
			name: "valid config",
			config: &OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage": 0.85,
				},
				CorrelationWindows: map[string]time.Duration{
					"oom_restart": 30 * time.Second,
				},
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 0.8,
					LearningWindow:       7 * 24 * time.Hour,
				},
			},
			wantError: false,
		},
		{
			name: "invalid threshold range",
			config: &OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage": 1.5, // > 1
				},
			},
			wantError: true,
			errMsg:    "must be between 0 and 1",
		},
		{
			name: "negative correlation window",
			config: &OpinionConfig{
				CorrelationWindows: map[string]time.Duration{
					"oom_restart": -5 * time.Second,
				},
			},
			wantError: true,
			errMsg:    "must be at least 1 second",
		},
		{
			name: "circular dependency",
			config: &OpinionConfig{
				ServiceDependencies: []ServiceDependency{
					{Source: "a", Target: "b", ExpectedDelay: 1 * time.Second},
					{Source: "b", Target: "c", ExpectedDelay: 1 * time.Second},
					{Source: "c", Target: "a", ExpectedDelay: 1 * time.Second},
				},
			},
			wantError: true,
			errMsg:    "circular dependency",
		},
	}

	validator := NewOpinionValidator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.config)
			
			if tt.wantError {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestComplexMarkdownExample(t *testing.T) {
	markdown := `# Production E-Commerce Cluster
Author: DevOps Team
Date: 2024-01-20
Cluster Type: production
Workload Type: stateless

## Overview
We run a high-traffic e-commerce platform with Java microservices.
Our pods need graceful shutdown time and we have strict SLAs.

## 🧠 Memory Management

### General Settings
- **Acceptable memory usage**: 85%
  - Why: Our JVM apps with G1GC run stable at 87%
- **OOM prediction window**: 5 minutes
  - Why: Gives us time to scale horizontally

### Service-Specific Limits
- ` + "`payment-processor`" + ` can use up to **90% memory**
- ` + "`batch-reports`" + ` can use up to **95% memory**
- ` + "`redis-cache`" + ` should never exceed **70% memory**

## 🔗 Correlation Rules

### Pod Lifecycle
- **OOM → Pod Restart**: 45 seconds
  - Why: Graceful shutdown takes ~30s plus buffer

### Service Dependencies
When **postgres-primary** has issues, I expect to see:
- **api-gateway** errors within **10 seconds**
- **payment-service** errors within **15 seconds**
- **notification-service** errors within **30 seconds**

## 🚨 Anomaly Detection

### Time-based Sensitivity

| Time Period | Sensitivity | Why |
|-------------|-------------|-----|
| Business Hours (9-17 PST) | High (0.7) | Peak customer traffic |
| Night (22-06 PST) | Low (0.9) | Batch processing time |
| Weekends | Medium (0.8) | Reduced traffic |
| Black Friday | Very High (0.6) | Critical sales period |

## ⚖️ Service Importance

` + "```yaml" + `
service_weights:
  payment-processor: 1.0    # Always critical
  api-gateway: 0.95         # Almost always critical
  search-service: 0.8       # Important but not critical
  analytics-worker: 0.3     # Best effort
  debug-logger: 0.1         # Very low priority
` + "```" + `

## 📊 Behavioral Learning

### Learning Configuration
- Learn from the last **14 days** of patterns
- Minimum **200 samples** before alerting
- Behavioral deviation sensitivity: **0.8**
- Trend detection window: **2 hours**

### Known Patterns
- Monday morning traffic spike is normal
- Memory grows 5% per hour during business hours
- CPU spikes at :00 and :30 due to cron jobs
`

	translator := NewTranslator()
	config, err := translator.TranslateMarkdown(markdown)
	
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify comprehensive extraction
	assert.Equal(t, float32(0.85), config.AnomalyThresholds["memory_usage"])
	assert.Equal(t, 5*time.Minute, config.PredictionConfig.PredictionWindows["oom"])
	assert.Equal(t, 45*time.Second, config.CorrelationWindows["oom_restart"])
	
	// Service limits
	assert.Equal(t, float32(0.90), config.ServiceLimits["payment-processor"].MemoryLimit)
	assert.Equal(t, float32(0.95), config.ServiceLimits["batch-reports"].MemoryLimit)
	assert.Equal(t, float32(0.70), config.ServiceLimits["redis-cache"].MemoryLimit)
	
	// Service weights
	assert.Equal(t, float32(1.0), config.ImportanceWeights["payment-processor"])
	assert.Equal(t, float32(0.3), config.ImportanceWeights["analytics-worker"])
	
	// Time-based rules
	assert.GreaterOrEqual(t, len(config.TimeBasedRules), 4)
	
	// Service dependencies
	assert.GreaterOrEqual(t, len(config.ServiceDependencies), 3)
	
	// Behavioral config
	assert.Equal(t, 14*24*time.Hour, config.BehavioralConfig.LearningWindow)
	assert.Equal(t, 200, config.BehavioralConfig.MinSamplesRequired)
	assert.Equal(t, float32(0.8), config.BehavioralConfig.DeviationSensitivity)
	assert.Equal(t, 2*time.Hour, config.BehavioralConfig.TrendWindow)

	// Validate the config
	validator := NewOpinionValidator()
	err = validator.Validate(config)
	assert.NoError(t, err)
}