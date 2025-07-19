package resilience

import (
	"context"
	"errors"
	"testing"
)

func TestSchemaValidator(t *testing.T) {
	minVal := 0.0
	maxVal := 100.0

	rules := []ValidationRule{
		{
			Field:    "name",
			Required: true,
			Type:     "string",
			Pattern:  "^[a-zA-Z]+$",
		},
		{
			Field:    "age",
			Required: true,
			Type:     "integer",
			Min:      &minVal,
			Max:      &maxVal,
		},
		{
			Field:    "email",
			Required: false,
			Type:     "string",
			Pattern:  `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
		},
		{
			Field: "status",
			Type:  "string",
			Enum:  []interface{}{"active", "inactive", "pending"},
		},
		{
			Field:    "metadata.version",
			Required: true,
			Type:     "string",
		},
	}

	validator, err := NewSchemaValidator("test-schema", rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		data      map[string]interface{}
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_data",
			data: map[string]interface{}{
				"name":   "John",
				"age":    25,
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: false,
		},
		{
			name: "missing_required_field",
			data: map[string]interface{}{
				"age":    25,
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "field name is required",
		},
		{
			name: "invalid_type",
			data: map[string]interface{}{
				"name":   "John",
				"age":    "twenty-five", // Should be integer
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "field age must be of type integer",
		},
		{
			name: "value_below_minimum",
			data: map[string]interface{}{
				"name":   "John",
				"age":    -5,
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "below minimum",
		},
		{
			name: "value_exceeds_maximum",
			data: map[string]interface{}{
				"name":   "John",
				"age":    150,
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "exceeds maximum",
		},
		{
			name: "invalid_pattern",
			data: map[string]interface{}{
				"name":   "John123", // Pattern only allows letters
				"age":    25,
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "does not match pattern",
		},
		{
			name: "invalid_enum",
			data: map[string]interface{}{
				"name":   "John",
				"age":    25,
				"status": "unknown", // Not in enum
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "must be one of",
		},
		{
			name: "valid_email",
			data: map[string]interface{}{
				"name":   "John",
				"age":    25,
				"email":  "john@example.com",
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: false,
		},
		{
			name: "invalid_email",
			data: map[string]interface{}{
				"name":   "John",
				"age":    25,
				"email":  "invalid-email",
				"status": "active",
				"metadata": map[string]interface{}{
					"version": "1.0.0",
				},
			},
			wantError: true,
			errorMsg:  "does not match pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(context.Background(), tt.data)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorMsg != "" && !errors.Is(err, ErrValidationFailed) {
					t.Errorf("expected validation error, got %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}

	// Test metrics
	metrics := validator.GetMetrics()
	if metrics.TotalValidations == 0 {
		t.Error("expected non-zero total validations")
	}
}

func TestSchemaValidator_CustomValidation(t *testing.T) {
	rules := []ValidationRule{
		{
			Field:    "password",
			Required: true,
			Type:     "string",
			Custom: func(value interface{}) error {
				pwd, ok := value.(string)
				if !ok {
					return errors.New("password must be string")
				}
				if len(pwd) < 8 {
					return errors.New("password must be at least 8 characters")
				}
				return nil
			},
		},
	}

	validator, err := NewSchemaValidator("password-validator", rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Test valid password
	err = validator.Validate(context.Background(), map[string]interface{}{
		"password": "securepassword123",
	})
	if err != nil {
		t.Errorf("unexpected error for valid password: %v", err)
	}

	// Test invalid password
	err = validator.Validate(context.Background(), map[string]interface{}{
		"password": "short",
	})
	if err == nil {
		t.Error("expected error for short password")
	}
}

func TestValidationPipeline(t *testing.T) {
	// Create multiple validators
	validator1, _ := NewSchemaValidator("validator1", []ValidationRule{
		{Field: "field1", Required: true, Type: "string"},
	})

	validator2, _ := NewSchemaValidator("validator2", []ValidationRule{
		{Field: "field2", Required: true, Type: "number"},
	})

	// Test sequential pipeline
	seqPipeline := NewValidationPipeline("sequential", false, validator1, validator2)

	// Valid data
	validData := map[string]interface{}{
		"field1": "value1",
		"field2": 42.0,
	}

	err := seqPipeline.Validate(context.Background(), validData)
	if err != nil {
		t.Errorf("unexpected error for valid data: %v", err)
	}

	// Invalid data
	invalidData := map[string]interface{}{
		"field1": "value1",
		// Missing field2
	}

	err = seqPipeline.Validate(context.Background(), invalidData)
	if err == nil {
		t.Error("expected error for invalid data")
	}

	// Test parallel pipeline
	parPipeline := NewValidationPipeline("parallel", true, validator1, validator2)

	err = parPipeline.Validate(context.Background(), invalidData)
	if err == nil {
		t.Error("expected error for invalid data in parallel pipeline")
	}
}

func TestDataIntegrityChecker(t *testing.T) {
	// Simple checksum function for testing
	checksumFunc := func(data []byte) string {
		sum := 0
		for _, b := range data {
			sum += int(b)
		}
		return string(rune(sum))
	}

	checker := NewDataIntegrityChecker(checksumFunc)

	data := []byte("test data")
	checksum := checksumFunc(data)

	// Valid checksum
	err := checker.CheckIntegrity(data, checksum)
	if err != nil {
		t.Errorf("unexpected error for valid checksum: %v", err)
	}

	// Invalid checksum
	err = checker.CheckIntegrity(data, "wrong-checksum")
	if err == nil {
		t.Error("expected error for invalid checksum")
	}
	if !errors.Is(err, ErrDataCorrupted) {
		t.Errorf("expected data corrupted error, got %v", err)
	}

	// Check metrics
	metrics := checker.GetMetrics()
	if metrics.TotalChecks != 2 {
		t.Errorf("expected 2 total checks, got %d", metrics.TotalChecks)
	}
	if metrics.CorruptedData != 1 {
		t.Errorf("expected 1 corrupted data, got %d", metrics.CorruptedData)
	}
}

func TestSchemaValidator_NestedFields(t *testing.T) {
	rules := []ValidationRule{
		{
			Field:    "user.profile.name",
			Required: true,
			Type:     "string",
		},
		{
			Field:    "user.profile.age",
			Required: true,
			Type:     "integer",
		},
		{
			Field: "user.settings.theme",
			Type:  "string",
			Enum:  []interface{}{"light", "dark"},
		},
	}

	validator, err := NewSchemaValidator("nested-validator", rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Valid nested data
	validData := map[string]interface{}{
		"user": map[string]interface{}{
			"profile": map[string]interface{}{
				"name": "Alice",
				"age":  30,
			},
			"settings": map[string]interface{}{
				"theme": "dark",
			},
		},
	}

	err = validator.Validate(context.Background(), validData)
	if err != nil {
		t.Errorf("unexpected error for valid nested data: %v", err)
	}

	// Missing nested field
	invalidData := map[string]interface{}{
		"user": map[string]interface{}{
			"profile": map[string]interface{}{
				"name": "Alice",
				// Missing age
			},
		},
	}

	err = validator.Validate(context.Background(), invalidData)
	if err == nil {
		t.Error("expected error for missing nested field")
	}
}
