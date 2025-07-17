package correlation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/falseyair/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// Type alias for SeverityLevel from domain package
type SeverityLevel = domain.SeverityLevel

// Severity level constants for backward compatibility
const (
	SeverityLevelInfo     = domain.SeverityInfo
	SeverityLevelWarning  = domain.SeverityWarning
	SeverityLevelError    = domain.SeverityError
	SeverityLevelCritical = domain.SeverityCritical
)

// ConfidenceLevel represents the confidence level of a finding
type ConfidenceLevel int

const (
	ConfidenceLow ConfidenceLevel = iota
	ConfidenceMedium
	ConfidenceHigh
	ConfidenceVeryHigh
)

// Type alias for Category
type Category = types.Category

// Category constants for backward compatibility
const (
	CategoryPerformance = types.CategoryPerformance
	CategorySecurity    = types.CategorySecurity
	CategoryReliability = types.CategoryReliability
	CategoryCost        = types.CategoryCost
	CategoryCapacity    = types.CategoryCapacity
)

// String returns the string representation of confidence level
func (c ConfidenceLevel) String() string {
	switch c {
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	case ConfidenceVeryHigh:
		return "very_high"
	default:
		return "unknown"
	}
}

// RuleEvidence represents supporting evidence for a finding
type RuleEvidence struct {
	Type        string                 `json:"type"`
	Source      SourceType             `json:"source"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   time.Time              `json:"timestamp"`
	Confidence  float64                `json:"confidence"` // 0.0 to 1.0
}

// ToDomainEvidence converts RuleEvidence to domain.Evidence
func (e RuleEvidence) ToDomainEvidence() domain.Evidence {
	return domain.Evidence{
		ID:          uuid.New().String(),
		Type:        e.Type,
		Source:      string(e.Source),
		Content:     e.Data,
		Confidence:  e.Confidence,
		Description: e.Description,
		Timestamp:   e.Timestamp,
	}
}

// ResourceReference represents a reference to a Kubernetes resource
type ResourceReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid,omitempty"`
}

// ResourceInfo represents resource information with Type field for compatibility
type ResourceInfo struct {
	Type      string `json:"type"` // Resource type (e.g., "pod", "service")
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RulePrediction represents a time-based prediction
type RulePrediction struct {
	Event       string        `json:"event"`
	TimeToEvent time.Duration `json:"time_to_event"`
	Confidence  float64       `json:"confidence"`
	Factors     []string      `json:"factors"`
	Mitigation  []string      `json:"mitigation"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Finding represents a correlation finding
type Finding struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    SeverityLevel          `json:"severity"`
	Confidence  float64                `json:"confidence"` // 0.0 to 1.0
	Resource    ResourceInfo           `json:"resource,omitempty"`
	Evidence    []RuleEvidence         `json:"evidence"`
	Prediction  *RulePrediction        `json:"prediction,omitempty"`
	Tags        []string               `json:"tags"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Metadata    map[string]interface{} `json:"metadata"`
	// Additional fields needed by correlation rules
	Impact            string              `json:"impact,omitempty"`
	RootCause         string              `json:"root_cause,omitempty"`
	Recommendations   []string            `json:"recommendations,omitempty"`
	AffectedResources []ResourceReference `json:"affected_resources,omitempty"`
}

// GetConfidenceLevel returns the confidence level based on the numeric confidence
func (f *Finding) GetConfidenceLevel() ConfidenceLevel {
	switch {
	case f.Confidence >= 0.9:
		return ConfidenceVeryHigh
	case f.Confidence >= 0.7:
		return ConfidenceHigh
	case f.Confidence >= 0.5:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// AddEvidence adds supporting evidence to the finding
func (f *Finding) AddEvidence(evidence RuleEvidence) {
	f.Evidence = append(f.Evidence, evidence)
	f.UpdatedAt = time.Now()
}

// AddTag adds a tag to the finding
func (f *Finding) AddTag(tag string) {
	for _, existingTag := range f.Tags {
		if existingTag == tag {
			return // Tag already exists
		}
	}
	f.Tags = append(f.Tags, tag)
}

// SetMetadata sets a metadata field
func (f *Finding) SetMetadata(key string, value interface{}) {
	if f.Metadata == nil {
		f.Metadata = make(map[string]interface{})
	}
	f.Metadata[key] = value
	f.UpdatedAt = time.Now()
}

// GetType returns the type/category of finding from metadata or tags
func (f *Finding) GetType() string {
	// Check metadata first
	if f.Metadata != nil {
		if typeVal, ok := f.Metadata["type"].(string); ok {
			return typeVal
		}
	}

	// Fall back to first tag if available
	if len(f.Tags) > 0 {
		return f.Tags[0]
	}

	// Default based on severity
	return f.Severity.String()
}

// GetImpact returns the impact of the finding
func (f *Finding) GetImpact() string {
	if f.Impact != "" {
		return f.Impact
	}

	// Check metadata for impact
	if f.Metadata != nil {
		if impactVal, ok := f.Metadata["impact"].(string); ok {
			return impactVal
		}
	}

	// Default based on severity
	switch f.Severity {
	case SeverityLevelCritical:
		return "high"
	case SeverityLevelError:
		return "medium"
	default:
		return "low"
	}
}

// GetResourceName returns the name of the affected resource
func (f *Finding) GetResourceName() string {
	if f.Resource.Name != "" {
		return f.Resource.Name
	}
	return "unknown"
}

// RuleRequirement represents a requirement for a rule
type RuleRequirement struct {
	SourceType SourceType `json:"source_type"`
	DataType   string     `json:"data_type"`
	Required   bool       `json:"required"`
	Fallback   string     `json:"fallback,omitempty"`
}

// RuleMetadata contains metadata about a rule
type RuleMetadata struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Author       string            `json:"author"`
	Tags         []string          `json:"tags"`
	Requirements []RuleRequirement `json:"requirements"`
	Enabled      bool              `json:"enabled"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// RuleContext provides context for rule execution
type RuleContext struct {
	DataCollection   *DataCollection
	PreviousFindings []Finding
	ExecutionTime    time.Time
	Metadata         map[string]interface{}
}

// CorrelationRule defines the interface for correlation rules
type CorrelationRule interface {
	// GetMetadata returns metadata about the rule
	GetMetadata() RuleMetadata

	// CheckRequirements verifies that required data sources are available
	CheckRequirements(ctx context.Context, data *DataCollection) error

	// Execute runs the rule and returns findings
	Execute(ctx context.Context, ruleCtx *RuleContext) ([]Finding, error)

	// GetConfidenceFactors returns factors that affect confidence scoring
	GetConfidenceFactors() []string

	// Validate validates the rule configuration
	Validate() error
}

// RuleAdapter adapts correlation.Rule to domain.Rule interface
type RuleAdapter struct {
	rule CorrelationRule
}

// NewRuleAdapter creates a new rule adapter
func NewRuleAdapter(rule CorrelationRule) *RuleAdapter {
	return &RuleAdapter{rule: rule}
}

// ToDomainRule converts to domain.Rule
func (a *RuleAdapter) ToDomainRule() domain.Rule {
	metadata := a.rule.GetMetadata()
	return domain.Rule{
		ID:          metadata.ID,
		Name:        metadata.Name,
		Description: metadata.Description,
		Enabled:     metadata.Enabled,
		Priority:    1, // Default priority
		Conditions:  []domain.RuleCondition{},
		Actions:     []domain.RuleAction{},
		Metadata: map[string]interface{}{
			"version":    metadata.Version,
			"author":     metadata.Author,
			"tags":       metadata.Tags,
			"created_at": metadata.CreatedAt,
			"updated_at": metadata.UpdatedAt,
		},
	}
}

// RuleRegistry manages available rules
type RuleRegistry struct {
	rules   map[string]CorrelationRule
	enabled map[string]bool
}

// NewRuleRegistry creates a new rule registry
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules:   make(map[string]CorrelationRule),
		enabled: make(map[string]bool),
	}
}

// RegisterRule registers a new rule
func (r *RuleRegistry) RegisterRule(rule CorrelationRule) error {
	metadata := rule.GetMetadata()
	if err := rule.Validate(); err != nil {
		return err
	}

	r.rules[metadata.ID] = rule
	r.enabled[metadata.ID] = metadata.Enabled

	return nil
}

// GetRule retrieves a rule by ID
func (r *RuleRegistry) GetRule(id string) (Rule, bool) {
	rule, exists := r.rules[id]
	return rule, exists
}

// GetEnabledRules returns all enabled rules
func (r *RuleRegistry) GetEnabledRules() []CorrelationRule {
	var enabled []CorrelationRule
	for id, rule := range r.rules {
		if r.enabled[id] {
			enabled = append(enabled, rule)
		}
	}
	return enabled
}

// GetAllRules returns all registered rules
func (r *RuleRegistry) GetAllRules() []CorrelationRule {
	var all []CorrelationRule
	for _, rule := range r.rules {
		all = append(all, rule)
	}
	return all
}

// EnableRule enables a rule
func (r *RuleRegistry) EnableRule(id string) error {
	if _, exists := r.rules[id]; !exists {
		return NewRuleNotFoundError(id)
	}
	r.enabled[id] = true
	return nil
}

// DisableRule disables a rule
func (r *RuleRegistry) DisableRule(id string) error {
	if _, exists := r.rules[id]; !exists {
		return NewRuleNotFoundError(id)
	}
	r.enabled[id] = false
	return nil
}

// IsEnabled checks if a rule is enabled
func (r *RuleRegistry) IsEnabled(id string) bool {
	return r.enabled[id]
}

// GetRulesByTag returns rules with the specified tag
func (r *RuleRegistry) GetRulesByTag(tag string) []CorrelationRule {
	var tagged []CorrelationRule
	for _, rule := range r.rules {
		metadata := rule.GetMetadata()
		for _, ruleTag := range metadata.Tags {
			if ruleTag == tag {
				tagged = append(tagged, rule)
				break
			}
		}
	}
	return tagged
}

// BaseRule provides common functionality for rules
type BaseRule struct {
	metadata RuleMetadata
}

// NewBaseRule creates a new base rule
func NewBaseRule(metadata RuleMetadata) *BaseRule {
	return &BaseRule{
		metadata: metadata,
	}
}

// GetMetadata returns the rule metadata
func (r *BaseRule) GetMetadata() RuleMetadata {
	return r.metadata
}

// GetID returns the rule ID (foundation.Rule interface)
func (r *BaseRule) GetID() string {
	return r.metadata.ID
}

// GetName returns the rule name (foundation.Rule interface)
func (r *BaseRule) GetName() string {
	return r.metadata.Name
}

// GetDescription returns the rule description (foundation.Rule interface)
func (r *BaseRule) GetDescription() string {
	return r.metadata.Description
}

// GetCategory returns the rule category (foundation.Rule interface)
func (r *BaseRule) GetCategory() Category {
	return CategoryPerformance // Default category
}

// GetVersion returns the rule version (foundation.Rule interface)
func (r *BaseRule) GetVersion() string {
	return r.metadata.Version
}

// GetAuthor returns the rule author (foundation.Rule interface)
func (r *BaseRule) GetAuthor() string {
	return r.metadata.Author
}

// GetTags returns the rule tags (foundation.Rule interface)
func (r *BaseRule) GetTags() []string {
	return r.metadata.Tags
}

// IsEnabled returns whether the rule is enabled (foundation.Rule interface)
func (r *BaseRule) IsEnabled() bool {
	return r.metadata.Enabled
}

// GetMinConfidence returns the minimum confidence (foundation.Rule interface)
func (r *BaseRule) GetMinConfidence() float64 {
	return 0.7 // Default confidence
}

// GetCooldown returns the cooldown duration (foundation.Rule interface)
func (r *BaseRule) GetCooldown() time.Duration {
	return 5 * time.Minute // Default cooldown
}

// GetTTL returns the TTL duration (foundation.Rule interface)
func (r *BaseRule) GetTTL() time.Duration {
	return 24 * time.Hour // Default TTL
}

// GetRequiredSources returns required sources (foundation.Rule interface)
func (r *BaseRule) GetRequiredSources() []SourceType {
	var sources []SourceType
	for _, req := range r.metadata.Requirements {
		if req.Required {
			sources = append(sources, req.SourceType)
		}
	}
	return sources
}

// GetOptionalSources returns optional sources (foundation.Rule interface)
func (r *BaseRule) GetOptionalSources() []SourceType {
	var sources []SourceType
	for _, req := range r.metadata.Requirements {
		if !req.Required {
			sources = append(sources, req.SourceType)
		}
	}
	return sources
}

// GetPerformance returns performance metrics (foundation.Rule interface)
func (r *BaseRule) GetPerformance() RulePerformance {
	return RulePerformance{
		ExecutionCount:  0,
		AverageLatency:  0,
		SuccessRate:     1.0,
		LastExecuted:    time.Time{},
		TotalDuration:   0,
		ErrorCount:      0,
	}
}

// UpdatePerformance updates performance metrics (foundation.Rule interface)
func (r *BaseRule) UpdatePerformance(execution RuleExecution) {
	// Default implementation - can be overridden by specific rules
}

// GetConfidenceFactors returns default confidence factors
func (r *BaseRule) GetConfidenceFactors() []string {
	return []string{
		"data_quality",
		"source_reliability",
		"pattern_strength",
		"historical_accuracy",
	}
}

// Validate validates the rule configuration
func (r *BaseRule) Validate() error {
	if r.metadata.ID == "" {
		return NewRuleValidationError("rule ID is required")
	}
	if r.metadata.Name == "" {
		return NewRuleValidationError("rule name is required")
	}
	if r.metadata.Version == "" {
		return NewRuleValidationError("rule version is required")
	}
	return nil
}

// CreateFinding creates a new finding with common fields populated
func (r *BaseRule) CreateFinding(title, description string, severity SeverityLevel, confidence float64) *Finding {
	now := time.Now()
	return &Finding{
		ID:          generateFindingID(),
		RuleID:      r.metadata.ID,
		Title:       title,
		Description: description,
		Severity:    severity,
		Confidence:  confidence,
		Evidence:    make([]RuleEvidence, 0),
		Tags:        make([]string, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
		Metadata:    make(map[string]interface{}),
	}
}

// RuleNotFoundError represents an error when a rule is not found
type RuleNotFoundError struct {
	RuleID string
}

func (e *RuleNotFoundError) Error() string {
	return "rule not found: " + e.RuleID
}

// NewRuleNotFoundError creates a new rule not found error
func NewRuleNotFoundError(ruleID string) *RuleNotFoundError {
	return &RuleNotFoundError{RuleID: ruleID}
}

// RuleValidationError represents a rule validation error
type RuleValidationError struct {
	Message string
}

func (e *RuleValidationError) Error() string {
	return "rule validation error: " + e.Message
}

// NewRuleValidationError creates a new rule validation error
func NewRuleValidationError(message string) *RuleValidationError {
	return &RuleValidationError{Message: message}
}

// RequirementNotMetError represents an error when rule requirements are not met
type RequirementNotMetError struct {
	RuleID      string
	Requirement RuleRequirement
}

func (e *RequirementNotMetError) Error() string {
	return "requirement not met for rule " + e.RuleID + ": " + string(e.Requirement.SourceType) + "/" + e.Requirement.DataType
}

// NewRequirementNotMetError creates a new requirement not met error
func NewRequirementNotMetError(ruleID string, requirement RuleRequirement) *RequirementNotMetError {
	return &RequirementNotMetError{
		RuleID:      ruleID,
		Requirement: requirement,
	}
}


// generateFindingID generates a unique finding ID
func generateFindingID() string {
	return "finding_" + uuid.New().String()
}
