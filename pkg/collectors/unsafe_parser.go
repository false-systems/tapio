package collectors

import (
	"fmt"
	"reflect"
	"unsafe"
)

// SafeParser provides memory-safe parsing utilities for eBPF event data
// This package eliminates all unsafe pointer operations without proper validation
type SafeParser struct{}

// NewSafeParser creates a new safe parser instance
func NewSafeParser() *SafeParser {
	return &SafeParser{}
}

// ParseError represents a parsing error with detailed information
type ParseError struct {
	Operation string
	Expected  int
	Actual    int
	Alignment int
	Message   string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error in %s: %s (expected: %d bytes, actual: %d bytes, alignment: %d)",
		e.Operation, e.Message, e.Expected, e.Actual, e.Alignment)
}

// ValidationResult contains the results of buffer validation
type ValidationResult struct {
	Valid        bool
	Size         int
	RequiredSize int
	Alignment    uintptr
	Error        error
}

// ValidateBuffer performs comprehensive validation of a raw byte buffer
// before any unsafe operations are performed
func (p *SafeParser) ValidateBuffer(rawBytes []byte, targetType reflect.Type) *ValidationResult {
	result := &ValidationResult{}

	if rawBytes == nil {
		result.Error = &ParseError{
			Operation: "buffer validation",
			Message:   "buffer is nil",
		}
		return result
	}

	if len(rawBytes) == 0 {
		result.Error = &ParseError{
			Operation: "buffer validation",
			Message:   "buffer is empty",
		}
		return result
	}

	// Get the size of target struct
	result.RequiredSize = int(targetType.Size())
	result.Size = len(rawBytes)

	// Validate buffer size - must be exact match to prevent buffer overruns
	if result.Size != result.RequiredSize {
		result.Error = &ParseError{
			Operation: "size validation",
			Expected:  result.RequiredSize,
			Actual:    result.Size,
			Message:   "buffer size mismatch",
		}
		return result
	}

	// Check alignment requirements based on struct alignment
	requiredAlignment := uintptr(targetType.Align())
	result.Alignment = uintptr(unsafe.Pointer(&rawBytes[0])) % requiredAlignment

	if result.Alignment != 0 {
		result.Error = &ParseError{
			Operation: "alignment validation",
			Alignment: int(requiredAlignment),
			Message:   fmt.Sprintf("buffer not aligned to %d bytes", requiredAlignment),
		}
		return result
	}

	result.Valid = true
	return result
}

// SafeUnmarshal safely unmarshals a byte buffer into a target struct
// with comprehensive validation and error handling
func (p *SafeParser) SafeUnmarshal(rawBytes []byte, target interface{}) error {
	if target == nil {
		return &ParseError{
			Operation: "unmarshal",
			Message:   "target is nil",
		}
	}

	// Get reflection info about target
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return &ParseError{
			Operation: "unmarshal",
			Message:   "target must be a pointer",
		}
	}

	targetElem := targetValue.Elem()
	if !targetElem.CanSet() {
		return &ParseError{
			Operation: "unmarshal",
			Message:   "target cannot be set",
		}
	}

	targetType := targetElem.Type()

	// Validate buffer
	validation := p.ValidateBuffer(rawBytes, targetType)
	if !validation.Valid {
		return validation.Error
	}

	// Perform safe casting with validated buffer
	// Use reflection to safely unmarshal the data
	structPtr := targetElem.Addr().Pointer()
	src := unsafe.Pointer(&rawBytes[0])

	// Copy the validated raw bytes into the target struct
	// This is safe because we've already validated size and alignment
	for i := 0; i < validation.RequiredSize; i++ {
		*(*byte)(unsafe.Pointer(structPtr + uintptr(i))) = *(*byte)(unsafe.Pointer(uintptr(src) + uintptr(i)))
	}

	return nil
}

// SafeCast safely casts a byte buffer to a specific struct type
// Returns the casted struct and any validation errors
func SafeCast[T any](parser *SafeParser, rawBytes []byte) (*T, error) {
	var target T
	targetType := reflect.TypeOf(target)

	// Validate buffer first
	validation := parser.ValidateBuffer(rawBytes, targetType)
	if !validation.Valid {
		return nil, validation.Error
	}

	// Safe cast with validated buffer
	result := (*T)(unsafe.Pointer(&rawBytes[0]))

	// Additional struct-specific validation can be added here
	if err := validateStruct(result); err != nil {
		return nil, fmt.Errorf("struct validation failed: %w", err)
	}

	return result, nil
}

// validateStruct performs struct-specific validation
// This function can be extended for custom validation logic
func validateStruct(s interface{}) error {
	// Use reflection to validate struct fields
	value := reflect.ValueOf(s)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return fmt.Errorf("struct pointer is nil")
		}
		value = value.Elem()
	}

	// Basic struct validation - can be extended based on specific needs
	if !value.IsValid() {
		return fmt.Errorf("invalid struct")
	}

	return nil
}

// StringFromByteArray safely converts a null-terminated byte array to string
// with bounds checking to prevent buffer overruns
func (p *SafeParser) StringFromByteArray(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	// Find null terminator with bounds checking
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return string(b[:i])
		}
		// Additional safety check for non-printable characters
		if b[i] < 32 && b[i] != 0 {
			// Return truncated string up to the problematic character
			return string(b[:i])
		}
	}

	// No null terminator found, return entire buffer
	return string(b)
}

// ValidateEventType validates that an event type is within acceptable ranges
func (p *SafeParser) ValidateEventType(eventType uint32, minType, maxType uint32) error {
	if eventType < minType || eventType > maxType {
		return &ParseError{
			Operation: "event type validation",
			Message:   fmt.Sprintf("invalid event type %d, expected range [%d-%d]", eventType, minType, maxType),
		}
	}
	return nil
}

// ValidateNetworkData validates network-specific data fields
func (p *SafeParser) ValidateNetworkData(protocol, direction uint8) error {
	if protocol > 255 {
		return &ParseError{
			Operation: "network validation",
			Message:   fmt.Sprintf("invalid protocol value: %d", protocol),
		}
	}

	if direction > 1 {
		return &ParseError{
			Operation: "network validation",
			Message:   fmt.Sprintf("invalid direction value: %d", direction),
		}
	}

	return nil
}

// ValidateStringField validates string fields in structs to prevent
// non-printable characters that could indicate corruption
func (p *SafeParser) ValidateStringField(field []byte, fieldName string) error {
	hasNullTerm := false

	for i, b := range field {
		if b == 0 {
			hasNullTerm = true
			break
		}

		// Check for invalid characters
		if b < 32 && b != 0 {
			return &ParseError{
				Operation: "string field validation",
				Message:   fmt.Sprintf("field %s contains non-printable character at position %d", fieldName, i),
			}
		}
	}

	if !hasNullTerm && len(field) > 0 {
		// String fills entire buffer without null termination - this could be valid
		// but we should log it for monitoring
		return nil
	}

	return nil
}

// CloneBuffer safely clones a byte buffer to prevent sharing
// This is useful when the original buffer might be reused
func (p *SafeParser) CloneBuffer(src []byte) []byte {
	if src == nil {
		return nil
	}

	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// MarshalStruct safely marshals a struct to a byte buffer for testing
// This function is primarily intended for testing purposes to replace unsafe operations
func (p *SafeParser) MarshalStruct(structData interface{}) ([]byte, error) {
	if structData == nil {
		return nil, &ParseError{
			Operation: "marshal",
			Message:   "struct data is nil",
		}
	}

	// Get reflection info about the struct
	value := reflect.ValueOf(structData)
	var structPtr unsafe.Pointer

	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil, &ParseError{
				Operation: "marshal",
				Message:   "struct pointer is nil",
			}
		}
		structPtr = unsafe.Pointer(value.Pointer())
		value = value.Elem()
	} else {
		if !value.CanAddr() {
			// Create a copy that we can take the address of
			newValue := reflect.New(value.Type()).Elem()
			newValue.Set(value)
			structPtr = unsafe.Pointer(newValue.Addr().Pointer())
		} else {
			structPtr = unsafe.Pointer(value.Addr().Pointer())
		}
	}

	if value.Kind() != reflect.Struct {
		return nil, &ParseError{
			Operation: "marshal",
			Message:   "input is not a struct",
		}
	}

	structType := value.Type()
	size := int(structType.Size())

	// Create appropriately sized and aligned buffer
	buffer := make([]byte, size)

	// Use unsafe to copy struct data - this is safe because we control both sides
	copy(buffer, (*(*[1 << 30]byte)(structPtr))[:size:size])

	return buffer, nil
}
