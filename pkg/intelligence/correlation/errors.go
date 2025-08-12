package correlation

import "fmt"

// CorrelationError represents an error in the correlation system
type CorrelationError struct {
	Type    string
	Field   string
	Message string
	Wrapped error
}

// Error implements the error interface
func (e *CorrelationError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s (wrapped: %v)", e.Type, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Unwrap returns the wrapped error
func (e *CorrelationError) Unwrap() error {
	return e.Wrapped
}

// ErrMissingRequiredField creates an error for missing required fields
func ErrMissingRequiredField(field string) error {
	return &CorrelationError{
		Type:    "MissingRequiredField",
		Field:   field,
		Message: fmt.Sprintf("required field '%s' is missing", field),
	}
}

// ErrInvalidFieldValue creates an error for invalid field values
func ErrInvalidFieldValue(field, reason string) error {
	return &CorrelationError{
		Type:    "InvalidFieldValue",
		Field:   field,
		Message: fmt.Sprintf("invalid value for field '%s': %s", field, reason),
	}
}

// ErrQueryFailed creates an error for failed queries
func ErrQueryFailed(query string, err error) error {
	return &CorrelationError{
		Type:    "QueryFailed",
		Message: fmt.Sprintf("query execution failed: %s", query),
		Wrapped: err,
	}
}

// ErrNodeNotFound creates an error when a node is not found
func ErrNodeNotFound(nodeType, identifier string) error {
	return &CorrelationError{
		Type:    "NodeNotFound",
		Message: fmt.Sprintf("%s node not found: %s", nodeType, identifier),
	}
}

// ErrInvalidNodeType creates an error for invalid node types
func ErrInvalidNodeType(expected, actual NodeType) error {
	return &CorrelationError{
		Type:    "InvalidNodeType",
		Message: fmt.Sprintf("expected node type %s, got %s", expected, actual),
	}
}

// ErrParsingFailed creates an error for parsing failures
func ErrParsingFailed(dataType string, err error) error {
	return &CorrelationError{
		Type:    "ParsingFailed",
		Message: fmt.Sprintf("failed to parse %s", dataType),
		Wrapped: err,
	}
}
