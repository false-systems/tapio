package config

import (
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error with helpful suggestions
type ValidationError struct {
	Field        string      `json:"field"`
	Message      string      `json:"message"`
	Suggestion   string      `json:"suggestion"`
	FixCommand   string      `json:"fix_command,omitempty"`
	Warning      bool        `json:"warning,omitempty"`
	CurrentValue interface{} `json:"current_value,omitempty"`
	ValidValues  []string    `json:"valid_values,omitempty"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error in field '%s': %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error with suggestion
func NewValidationError(field, message, suggestion string) ValidationError {
	return ValidationError{
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
	}
}

// NewValidationErrorWithFix creates a validation error with suggestion and fix command
func NewValidationErrorWithFix(field, message, suggestion, fixCommand string) ValidationError {
	return ValidationError{
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
		FixCommand: fixCommand,
	}
}

// NewValidationWarning creates a validation warning (non-blocking)
func NewValidationWarning(field, message, suggestion string) ValidationError {
	return ValidationError{
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
		Warning:    true,
	}
}

// ValidationErrors represents multiple validation errors
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

func (e ValidationErrors) Error() string {
	if len(e.Errors) == 0 {
		return "no validation errors"
	}

	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}

	var messages []string
	for _, err := range e.Errors {
		messages = append(messages, err.Error())
	}

	return fmt.Sprintf("multiple validation errors:\n  - %s", strings.Join(messages, "\n  - "))
}

// NewValidationErrors creates a ValidationErrors from a slice of ValidationError
func NewValidationErrors(errors []ValidationError) ValidationErrors {
	return ValidationErrors{Errors: errors}
}

// IsEmpty returns true if there are no validation errors
func (e ValidationErrors) IsEmpty() bool {
	return len(e.Errors) == 0
}

// Count returns the number of validation errors
func (e ValidationErrors) Count() int {
	return len(e.Errors)
}

// GetFixSuggestions returns a formatted list of fix suggestions
func (e ValidationErrors) GetFixSuggestions() []string {
	var suggestions []string
	for _, err := range e.Errors {
		if err.Suggestion != "" {
			suggestions = append(suggestions, fmt.Sprintf("%s: %s", err.Field, err.Suggestion))
		}
	}
	return suggestions
}

// ConfigError represents configuration loading/processing errors
type ConfigError struct {
	Type       string `json:"type"`
	File       string `json:"file,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
	Cause      error  `json:"-"`
}

func (e ConfigError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("config %s error in '%s': %s", e.Type, e.File, e.Message)
	}
	return fmt.Sprintf("config %s error: %s", e.Type, e.Message)
}

func (e ConfigError) Unwrap() error {
	return e.Cause
}

// NewConfigError creates a new configuration error
func NewConfigError(errorType, message, suggestion string) ConfigError {
	return ConfigError{
		Type:       errorType,
		Message:    message,
		Suggestion: suggestion,
	}
}

// NewConfigFileError creates a configuration error for a specific file
func NewConfigFileError(errorType, file, message, suggestion string) ConfigError {
	return ConfigError{
		Type:       errorType,
		File:       file,
		Message:    message,
		Suggestion: suggestion,
	}
}

// WithCause adds a cause to the error
func (e ConfigError) WithCause(cause error) ConfigError {
	e.Cause = cause
	return e
}

// Permission errors for eBPF and system access
type PermissionError struct {
	Resource    string   `json:"resource"`
	Required    []string `json:"required_permissions"`
	Current     []string `json:"current_permissions,omitempty"`
	Suggestion  string   `json:"suggestion"`
	AutoFixable bool     `json:"auto_fixable"`
}

func (e PermissionError) Error() string {
	return fmt.Sprintf("insufficient permissions for %s: requires %v",
		e.Resource, e.Required)
}

// NewPermissionError creates a new permission error
func NewPermissionError(resource string, required []string, suggestion string) PermissionError {
	return PermissionError{
		Resource:   resource,
		Required:   required,
		Suggestion: suggestion,
	}
}

// SetAutoFixable marks the error as automatically fixable
func (e PermissionError) SetAutoFixable(fixable bool) PermissionError {
	e.AutoFixable = fixable
	return e
}

// DependencyError represents missing system dependencies
type DependencyError struct {
	Component  string   `json:"component"`
	Missing    []string `json:"missing_dependencies"`
	Optional   bool     `json:"optional"`
	Suggestion string   `json:"suggestion"`
	InstallCmd string   `json:"install_command,omitempty"`
}

func (e DependencyError) Error() string {
	severity := "required"
	if e.Optional {
		severity = "optional"
	}
	return fmt.Sprintf("%s dependency missing for %s: %v",
		severity, e.Component, e.Missing)
}

// NewDependencyError creates a new dependency error
func NewDependencyError(component string, missing []string, optional bool, suggestion string) DependencyError {
	return DependencyError{
		Component:  component,
		Missing:    missing,
		Optional:   optional,
		Suggestion: suggestion,
	}
}

// WithInstallCommand adds an install command to the error
func (e DependencyError) WithInstallCommand(cmd string) DependencyError {
	e.InstallCmd = cmd
	return e
}

// ConnectivityError represents network/cluster connectivity issues
type ConnectivityError struct {
	Target      string `json:"target"`
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion"`
	TestCommand string `json:"test_command,omitempty"`
	Cause       error  `json:"-"`
}

func (e ConnectivityError) Error() string {
	return fmt.Sprintf("connectivity error to %s: %s", e.Target, e.Message)
}

func (e ConnectivityError) Unwrap() error {
	return e.Cause
}

// NewConnectivityError creates a new connectivity error
func NewConnectivityError(target, message, suggestion string) ConnectivityError {
	return ConnectivityError{
		Target:     target,
		Message:    message,
		Suggestion: suggestion,
	}
}

// WithTestCommand adds a test command to verify connectivity
func (e ConnectivityError) WithTestCommand(cmd string) ConnectivityError {
	e.TestCommand = cmd
	return e
}

// WithCause adds a cause to the connectivity error
func (e ConnectivityError) WithCause(cause error) ConnectivityError {
	e.Cause = cause
	return e
}

// ComprehensiveError represents multiple types of configuration issues
type ComprehensiveError struct {
	ValidationErrors   []ValidationError   `json:"validation_errors,omitempty"`
	PermissionErrors   []PermissionError   `json:"permission_errors,omitempty"`
	DependencyErrors   []DependencyError   `json:"dependency_errors,omitempty"`
	ConnectivityErrors []ConnectivityError `json:"connectivity_errors,omitempty"`
	ConfigErrors       []ConfigError       `json:"config_errors,omitempty"`
}

func (e ComprehensiveError) Error() string {
	var issues []string

	if len(e.ValidationErrors) > 0 {
		issues = append(issues, fmt.Sprintf("%d validation issues", len(e.ValidationErrors)))
	}
	if len(e.PermissionErrors) > 0 {
		issues = append(issues, fmt.Sprintf("%d permission issues", len(e.PermissionErrors)))
	}
	if len(e.DependencyErrors) > 0 {
		issues = append(issues, fmt.Sprintf("%d dependency issues", len(e.DependencyErrors)))
	}
	if len(e.ConnectivityErrors) > 0 {
		issues = append(issues, fmt.Sprintf("%d connectivity issues", len(e.ConnectivityErrors)))
	}
	if len(e.ConfigErrors) > 0 {
		issues = append(issues, fmt.Sprintf("%d config issues", len(e.ConfigErrors)))
	}

	if len(issues) == 0 {
		return "no configuration issues"
	}

	return fmt.Sprintf("configuration problems detected: %s", strings.Join(issues, ", "))
}

// IsEmpty returns true if there are no errors
func (e ComprehensiveError) IsEmpty() bool {
	return len(e.ValidationErrors) == 0 &&
		len(e.PermissionErrors) == 0 &&
		len(e.DependencyErrors) == 0 &&
		len(e.ConnectivityErrors) == 0 &&
		len(e.ConfigErrors) == 0
}

// HasCriticalErrors returns true if there are errors that prevent operation
func (e ComprehensiveError) HasCriticalErrors() bool {
	// Validation errors are always critical
	if len(e.ValidationErrors) > 0 {
		return true
	}

	// Required permission errors are critical
	for _, pe := range e.PermissionErrors {
		if strings.Contains(pe.Resource, "required") {
			return true
		}
	}

	// Required dependency errors are critical
	for _, de := range e.DependencyErrors {
		if !de.Optional {
			return true
		}
	}

	// Connectivity errors might be critical depending on context
	if len(e.ConnectivityErrors) > 0 {
		return true
	}

	return len(e.ConfigErrors) > 0
}

// GetAllSuggestions returns all suggestions for fixing the errors
func (e ComprehensiveError) GetAllSuggestions() []string {
	var suggestions []string

	for _, ve := range e.ValidationErrors {
		if ve.Suggestion != "" {
			suggestions = append(suggestions, ve.Suggestion)
		}
	}

	for _, pe := range e.PermissionErrors {
		if pe.Suggestion != "" {
			suggestions = append(suggestions, pe.Suggestion)
		}
	}

	for _, de := range e.DependencyErrors {
		if de.Suggestion != "" {
			suggestions = append(suggestions, de.Suggestion)
		}
	}

	for _, ce := range e.ConnectivityErrors {
		if ce.Suggestion != "" {
			suggestions = append(suggestions, ce.Suggestion)
		}
	}

	for _, cfg := range e.ConfigErrors {
		if cfg.Suggestion != "" {
			suggestions = append(suggestions, cfg.Suggestion)
		}
	}

	return suggestions
}
