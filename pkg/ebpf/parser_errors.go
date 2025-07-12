package ebpf

import (
	"fmt"
)

// ParserError provides detailed context for parsing failures
type ParserError struct {
	Parser      string // Parser name (e.g., "NetworkEventParser")
	EventType   string // Event type being parsed
	DataSize    int    // Size of data received
	ExpectedMin int    // Minimum expected size
	Field       string // Field being parsed when error occurred
	Offset      int    // Byte offset where error occurred
	Err         error  // Underlying error
}

func (e *ParserError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: failed to parse %s at offset %d (data size: %d, expected min: %d): %v",
			e.Parser, e.Field, e.Offset, e.DataSize, e.ExpectedMin, e.Err)
	}
	return fmt.Sprintf("%s: insufficient data for %s event (got %d bytes, need at least %d)",
		e.Parser, e.EventType, e.DataSize, e.ExpectedMin)
}

func (e *ParserError) Unwrap() error {
	return e.Err
}

// NewParserError creates a new parser error with context
func NewParserError(parser, eventType string, dataSize, expectedMin int) *ParserError {
	return &ParserError{
		Parser:      parser,
		EventType:   eventType,
		DataSize:    dataSize,
		ExpectedMin: expectedMin,
	}
}

// NewFieldParseError creates an error for field parsing failures
func NewFieldParseError(parser, eventType, field string, offset int, err error) *ParserError {
	return &ParserError{
		Parser:    parser,
		EventType: eventType,
		Field:     field,
		Offset:    offset,
		Err:       err,
	}
}

// Common parsing errors
var (
	ErrInvalidTimestamp = fmt.Errorf("invalid timestamp value")
	ErrInvalidPID       = fmt.Errorf("invalid process ID")
	ErrInvalidIPAddress = fmt.Errorf("invalid IP address")
	ErrInvalidPort      = fmt.Errorf("invalid port number")
	ErrStringTooLong    = fmt.Errorf("string exceeds maximum length")
	ErrInvalidEventType = fmt.Errorf("invalid event type value")
)