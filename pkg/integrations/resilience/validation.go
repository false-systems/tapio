package resilience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrValidationFailed = errors.New("validation failed")
	ErrInvalidSchema    = errors.New("invalid schema")
	ErrDataCorrupted    = errors.New("data corrupted")
)

// Validator defines the interface for data validators
type Validator interface {
	Validate(ctx context.Context, data interface{}) error
	Name() string
}

// ValidationRule represents a single validation rule
type ValidationRule struct {
	Field    string
	Required bool
	Type     string
	Min      *float64
	Max      *float64
	Pattern  string
	Enum     []interface{}
	Custom   func(value interface{}) error
	compiled *regexp.Regexp
}

// SchemaValidator validates data against a schema
type SchemaValidator struct {
	name  string
	rules []ValidationRule
	mu    sync.RWMutex

	// Metrics
	totalValidations atomic.Uint64
	validationErrors atomic.Uint64
	validationTime   atomic.Int64
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator(name string, rules []ValidationRule) (*SchemaValidator, error) {
	sv := &SchemaValidator{
		name:  name,
		rules: rules,
	}

	// Compile regex patterns
	for i := range sv.rules {
		if sv.rules[i].Pattern != "" {
			compiled, err := regexp.Compile(sv.rules[i].Pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid pattern for field %s: %w", sv.rules[i].Field, err)
			}
			sv.rules[i].compiled = compiled
		}
	}

	return sv, nil
}

// Validate validates data against the schema
func (sv *SchemaValidator) Validate(ctx context.Context, data interface{}) error {
	start := time.Now()
	defer func() {
		sv.validationTime.Add(time.Since(start).Nanoseconds())
	}()

	sv.totalValidations.Add(1)

	// Convert to map for easier field access
	dataMap, err := sv.toMap(data)
	if err != nil {
		sv.validationErrors.Add(1)
		return fmt.Errorf("failed to convert data: %w", err)
	}

	var validationErrors []string

	sv.mu.RLock()
	rules := sv.rules
	sv.mu.RUnlock()

	for _, rule := range rules {
		if err := sv.validateField(dataMap, rule); err != nil {
			validationErrors = append(validationErrors, err.Error())
		}
	}

	if len(validationErrors) > 0 {
		sv.validationErrors.Add(1)
		return fmt.Errorf("%w: %s", ErrValidationFailed, strings.Join(validationErrors, "; "))
	}

	return nil
}

// validateField validates a single field against a rule
func (sv *SchemaValidator) validateField(data map[string]interface{}, rule ValidationRule) error {
	value, exists := sv.getFieldValue(data, rule.Field)

	// Check required
	if rule.Required && (!exists || value == nil) {
		return fmt.Errorf("field %s is required", rule.Field)
	}

	if !exists || value == nil {
		return nil
	}

	// Check type
	if rule.Type != "" && !sv.checkType(value, rule.Type) {
		return fmt.Errorf("field %s must be of type %s", rule.Field, rule.Type)
	}

	// Check numeric ranges
	if rule.Min != nil || rule.Max != nil {
		numVal, ok := sv.toFloat64(value)
		if !ok {
			return fmt.Errorf("field %s must be numeric for range validation", rule.Field)
		}

		if rule.Min != nil && numVal < *rule.Min {
			return fmt.Errorf("field %s value %v is below minimum %v", rule.Field, numVal, *rule.Min)
		}

		if rule.Max != nil && numVal > *rule.Max {
			return fmt.Errorf("field %s value %v exceeds maximum %v", rule.Field, numVal, *rule.Max)
		}
	}

	// Check pattern
	if rule.compiled != nil {
		strVal, ok := value.(string)
		if !ok {
			return fmt.Errorf("field %s must be string for pattern validation", rule.Field)
		}

		if !rule.compiled.MatchString(strVal) {
			return fmt.Errorf("field %s value does not match pattern %s", rule.Field, rule.Pattern)
		}
	}

	// Check enum
	if len(rule.Enum) > 0 {
		found := false
		for _, enumVal := range rule.Enum {
			if reflect.DeepEqual(value, enumVal) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("field %s value must be one of %v", rule.Field, rule.Enum)
		}
	}

	// Custom validation
	if rule.Custom != nil {
		if err := rule.Custom(value); err != nil {
			return fmt.Errorf("field %s custom validation failed: %w", rule.Field, err)
		}
	}

	return nil
}

// getFieldValue retrieves a field value supporting nested fields
func (sv *SchemaValidator) getFieldValue(data map[string]interface{}, field string) (interface{}, bool) {
	parts := strings.Split(field, ".")
	current := data

	for i, part := range parts {
		value, exists := current[part]
		if !exists {
			return nil, false
		}

		if i == len(parts)-1 {
			return value, true
		}

		next, ok := value.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}

	return nil, false
}

// toMap converts various data types to map
func (sv *SchemaValidator) toMap(data interface{}) (map[string]interface{}, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		return v, nil
	case []byte:
		var m map[string]interface{}
		if err := json.Unmarshal(v, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		// Use JSON marshal/unmarshal for struct conversion
		bytes, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		var m map[string]interface{}
		if err := json.Unmarshal(bytes, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
}

// checkType checks if a value matches the expected type
func (sv *SchemaValidator) checkType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := sv.toFloat64(value)
		return ok
	case "integer":
		f, ok := sv.toFloat64(value)
		return ok && f == float64(int64(f))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	default:
		return false
	}
}

// toFloat64 converts various numeric types to float64
func (sv *SchemaValidator) toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

// Name returns the validator name
func (sv *SchemaValidator) Name() string {
	return sv.name
}

// GetMetrics returns validation metrics
func (sv *SchemaValidator) GetMetrics() ValidationMetrics {
	totalTime := time.Duration(sv.validationTime.Load())
	totalValidations := sv.totalValidations.Load()
	avgTime := time.Duration(0)
	if totalValidations > 0 {
		avgTime = totalTime / time.Duration(totalValidations)
	}

	return ValidationMetrics{
		Name:             sv.name,
		TotalValidations: totalValidations,
		ValidationErrors: sv.validationErrors.Load(),
		AverageTime:      avgTime,
	}
}

// ValidationMetrics represents validation metrics
type ValidationMetrics struct {
	Name             string
	TotalValidations uint64
	ValidationErrors uint64
	AverageTime      time.Duration
}

// ValidationPipeline chains multiple validators
type ValidationPipeline struct {
	name       string
	validators []Validator
	parallel   bool

	// Metrics
	totalValidations  atomic.Uint64
	failedValidations atomic.Uint64
}

// NewValidationPipeline creates a new validation pipeline
func NewValidationPipeline(name string, parallel bool, validators ...Validator) *ValidationPipeline {
	return &ValidationPipeline{
		name:       name,
		validators: validators,
		parallel:   parallel,
	}
}

// Validate runs all validators in the pipeline
func (vp *ValidationPipeline) Validate(ctx context.Context, data interface{}) error {
	vp.totalValidations.Add(1)

	if vp.parallel {
		return vp.validateParallel(ctx, data)
	}

	return vp.validateSequential(ctx, data)
}

// validateSequential runs validators one by one
func (vp *ValidationPipeline) validateSequential(ctx context.Context, data interface{}) error {
	for _, validator := range vp.validators {
		if err := validator.Validate(ctx, data); err != nil {
			vp.failedValidations.Add(1)
			return fmt.Errorf("validation failed in %s: %w", validator.Name(), err)
		}
	}
	return nil
}

// validateParallel runs validators concurrently
func (vp *ValidationPipeline) validateParallel(ctx context.Context, data interface{}) error {
	errChan := make(chan error, len(vp.validators))
	var wg sync.WaitGroup

	for _, validator := range vp.validators {
		wg.Add(1)
		go func(v Validator) {
			defer wg.Done()
			if err := v.Validate(ctx, data); err != nil {
				errChan <- fmt.Errorf("validation failed in %s: %w", v.Name(), err)
			}
		}(validator)
	}

	wg.Wait()
	close(errChan)

	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		vp.failedValidations.Add(1)
		return fmt.Errorf("%w: %s", ErrValidationFailed, strings.Join(errors, "; "))
	}

	return nil
}

// Name returns the pipeline name
func (vp *ValidationPipeline) Name() string {
	return vp.name
}

// DataIntegrityChecker checks data integrity
type DataIntegrityChecker struct {
	checksumFunc func(data []byte) string

	// Metrics
	totalChecks   atomic.Uint64
	corruptedData atomic.Uint64
}

// NewDataIntegrityChecker creates a new data integrity checker
func NewDataIntegrityChecker(checksumFunc func(data []byte) string) *DataIntegrityChecker {
	return &DataIntegrityChecker{
		checksumFunc: checksumFunc,
	}
}

// CheckIntegrity verifies data integrity
func (dic *DataIntegrityChecker) CheckIntegrity(data []byte, expectedChecksum string) error {
	dic.totalChecks.Add(1)

	actualChecksum := dic.checksumFunc(data)
	if actualChecksum != expectedChecksum {
		dic.corruptedData.Add(1)
		return fmt.Errorf("%w: checksum mismatch (expected: %s, got: %s)", ErrDataCorrupted, expectedChecksum, actualChecksum)
	}

	return nil
}

// GetMetrics returns integrity check metrics
func (dic *DataIntegrityChecker) GetMetrics() IntegrityMetrics {
	return IntegrityMetrics{
		TotalChecks:   dic.totalChecks.Load(),
		CorruptedData: dic.corruptedData.Load(),
	}
}

// IntegrityMetrics represents data integrity metrics
type IntegrityMetrics struct {
	TotalChecks   uint64
	CorruptedData uint64
}
