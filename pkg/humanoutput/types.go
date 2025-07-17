package humanoutput

import (
	"time"
)

// HumanInsight represents a human-readable insight
type HumanInsight struct {
	// Core explanation
	Title               string            `json:"title"`
	WhatHappened        string            `json:"what_happened"`
	WhyItHappened       string            `json:"why_it_happened"`
	WhatItMeans         string            `json:"what_it_means"`
	WhatToDo            string            `json:"what_to_do"`
	HowToPrevent        string            `json:"how_to_prevent"`
	
	// Context
	BusinessImpact      string            `json:"business_impact,omitempty"`
	TechnicalDetails    string            `json:"technical_details,omitempty"`
	UserImpact          string            `json:"user_impact,omitempty"`
	Timeline            string            `json:"timeline,omitempty"`
	
	// Metadata
	Severity            string            `json:"severity"`
	Confidence          float64           `json:"confidence"`
	Language            string            `json:"language"`
	Style               string            `json:"style"`
	Audience            string            `json:"audience"`
	Emoji               string            `json:"emoji,omitempty"`
	
	// Quality metrics
	IsUrgent            bool              `json:"is_urgent"`
	IsActionable        bool              `json:"is_actionable"`
	RequiresEscalation  bool              `json:"requires_escalation"`
	ReadabilityScore    float64           `json:"readability_score"`
	ComplexityScore     float64           `json:"complexity_score"`
	EstimatedReadTime   time.Duration     `json:"estimated_read_time"`
	
	// Interactive elements
	Commands            []string          `json:"commands,omitempty"`
	Links               []string          `json:"links,omitempty"`
	RelatedIncidents    []string          `json:"related_incidents,omitempty"`
	RecommendedActions  []RecommendedAction `json:"recommended_actions,omitempty"`
	
	// Generation metadata
	GeneratedAt         time.Time         `json:"generated_at"`
	GeneratedBy         string            `json:"generated_by"` // "template", "ai", "hybrid"
	TemplateUsed        string            `json:"template_used,omitempty"`
}

// RecommendedAction represents a recommended action
type RecommendedAction struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	Link        string `json:"link,omitempty"`
	Priority    string `json:"priority"` // "high", "medium", "low"
	Type        string `json:"type"`     // "command", "documentation", "configuration"
}

// HumanReport represents a comprehensive report
type HumanReport struct {
	Title              string             `json:"title"`
	Summary            string             `json:"summary"`
	Period             TimePeriod         `json:"period"`
	Insights           []*HumanInsight    `json:"insights"`
	Trends             []Trend            `json:"trends"`
	Recommendations    []string           `json:"recommendations"`
	OverallHealth      string             `json:"overall_health"`
	GeneratedAt        time.Time          `json:"generated_at"`
	EstimatedReadTime  time.Duration      `json:"estimated_read_time"`
}

// HumanSummary represents a system state summary
type HumanSummary struct {
	Title              string             `json:"title"`
	Overview           string             `json:"overview"`
	KeyMetrics         map[string]string  `json:"key_metrics"`
	ActiveIssues       []IssueSummary     `json:"active_issues"`
	RecentChanges      []string           `json:"recent_changes"`
	SystemHealth       string             `json:"system_health"`
	NextSteps          []string           `json:"next_steps"`
	GeneratedAt        time.Time          `json:"generated_at"`
}

// TimePeriod represents a time range
type TimePeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Trend represents a trend in the system
type Trend struct {
	Name        string  `json:"name"`
	Direction   string  `json:"direction"` // "improving", "degrading", "stable"
	Description string  `json:"description"`
	Impact      string  `json:"impact"`
	Confidence  float64 `json:"confidence"`
}

// IssueSummary represents a summary of an active issue
type IssueSummary struct {
	Title       string        `json:"title"`
	Severity    string        `json:"severity"`
	Duration    time.Duration `json:"duration"`
	Impact      string        `json:"impact"`
	Status      string        `json:"status"`
}

// Config represents configuration for human output generation
type Config struct {
	// Language settings
	DefaultLanguage      string   `json:"default_language"`
	SupportedLanguages   []string `json:"supported_languages"`
	
	// Style settings
	ExplanationStyle     string   `json:"explanation_style"` // "technical", "simple", "executive"
	Audience             string   `json:"audience"`          // "developer", "operator", "business"
	
	// Content settings
	MaxExplanationLength int      `json:"max_explanation_length"`
	IncludeRecommendations bool   `json:"include_recommendations"`
	IncludeContext       bool     `json:"include_context"`
	IncludeCommands      bool     `json:"include_commands"`
	IncludeEmoji         bool     `json:"include_emoji"`
	
	// Quality settings
	EnableQualityCheck   bool     `json:"enable_quality_check"`
	MinReadabilityScore  float64  `json:"min_readability_score"`
	MaxComplexityScore   float64  `json:"max_complexity_score"`
	
	// Template settings
	TemplateDirectory    string   `json:"template_directory"`
	FallbackToDefault    bool     `json:"fallback_to_default"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultLanguage:        "en",
		SupportedLanguages:     []string{"en"},
		ExplanationStyle:       "simple",
		Audience:              "developer",
		MaxExplanationLength:   500,
		IncludeRecommendations: true,
		IncludeContext:        true,
		IncludeCommands:       true,
		IncludeEmoji:          true,
		EnableQualityCheck:    true,
		MinReadabilityScore:   0.6,
		MaxComplexityScore:    0.8,
		FallbackToDefault:     true,
	}
}