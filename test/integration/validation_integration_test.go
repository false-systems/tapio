package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yairfalse/tapio/internal/cli"
	"github.com/yairfalse/tapio/pkg/validation"
)

func TestValidationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	validator := validation.NewValidator()

	tests := []struct {
		name     string
		inputs   map[string]interface{}
		expected bool
	}{
		{
			name: "valid kubernetes resource inputs",
			inputs: map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
				"resource":  "deployment/test-app",
				"output":    "json",
				"timeout":   "30s",
			},
			expected: true,
		},
		{
			name: "invalid kubernetes resource inputs",
			inputs: map[string]interface{}{
				"name":      "Test-App", // invalid: uppercase
				"namespace": "default",
				"resource":  "invalidkind/test-app", // invalid: unknown kind
				"output":    "xml",                  // invalid: unsupported format
				"timeout":   "invalid",              // invalid: bad format
			},
			expected: false,
		},
		{
			name: "edge case inputs",
			inputs: map[string]interface{}{
				"name":      "a", // valid: single character
				"namespace": "",  // valid: empty namespace
				"resource":  "pod/very-long-name-that-is-still-valid-because-its-under-253-chars",
				"output":    "human",
				"timeout":   "1s", // valid: minimum timeout
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateAll(tt.inputs)

			if result.Valid != tt.expected {
				t.Errorf("expected validation result %v, got %v", tt.expected, result.Valid)
				if !result.Valid {
					for _, err := range result.Errors {
						t.Logf("Validation error: %s", err.Error())
					}
				}
			}
		})
	}
}

func TestCLIValidationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "valid check command",
			args:        []string{"check", "--output", "json", "--timeout", "30s"},
			expectError: false,
		},
		{
			name:        "invalid output format",
			args:        []string{"check", "--output", "xml"},
			expectError: true,
		},
		{
			name:        "invalid timeout format",
			args:        []string{"check", "--timeout", "invalid"},
			expectError: true,
		},
		{
			name:        "unknown command",
			args:        []string{"unknowncommand"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary context for CLI execution
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Capture output and errors
			originalArgs := os.Args
			defer func() { os.Args = originalArgs }()

			os.Args = append([]string{"tapio"}, tt.args...)

			// Execute CLI command (this would need to be adapted based on actual CLI structure)
			err := cli.Execute()

			if tt.expectError && err == nil {
				t.Errorf("expected error for args %v, but got none", tt.args)
			}

			if !tt.expectError && err != nil {
				t.Errorf("unexpected error for args %v: %v", tt.args, err)
			}
		})
	}
}

func TestResourceValidationWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	validator := validation.NewValidator()

	// Test complete workflow: resource parsing -> validation -> suggestion
	scenarios := []struct {
		name               string
		resourceRef        string
		shouldBeValid      bool
		expectedSuggestion string
	}{
		{
			name:          "valid pod reference",
			resourceRef:   "pod/test-pod-123",
			shouldBeValid: true,
		},
		{
			name:               "invalid kind suggestion",
			resourceRef:        "podss/test-pod", // typo in kind
			shouldBeValid:      false,
			expectedSuggestion: "pods",
		},
		{
			name:          "valid deployment shorthand",
			resourceRef:   "deploy/api-service",
			shouldBeValid: true,
		},
		{
			name:               "invalid name format",
			resourceRef:        "pod/Test_Pod",
			shouldBeValid:      false,
			expectedSuggestion: "lowercase",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			err := validator.ValidateResourceReference(scenario.resourceRef)

			if scenario.shouldBeValid {
				if err.Message != "" {
					t.Errorf("expected valid resource reference %s, got error: %s", scenario.resourceRef, err.Message)
				}
			} else {
				if err.Message == "" {
					t.Errorf("expected invalid resource reference %s to fail validation", scenario.resourceRef)
				}

				if scenario.expectedSuggestion != "" {
					found := false
					for _, suggestion := range err.Suggestions {
						if contains(suggestion, scenario.expectedSuggestion) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected suggestion containing '%s' for %s, got suggestions: %v",
							scenario.expectedSuggestion, scenario.resourceRef, err.Suggestions)
					}
				}
			}
		})
	}
}

func TestPerformanceValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	validator := validation.NewValidator()

	// Test validation performance with large datasets
	t.Run("bulk validation performance", func(t *testing.T) {
		start := time.Now()

		// Validate 1000 resource references
		for i := 0; i < 1000; i++ {
			resourceRef := "pod/test-pod-" + string(rune(i))
			validator.ValidateResourceReference(resourceRef)
		}

		duration := time.Since(start)

		// Should complete within reasonable time
		if duration > time.Second {
			t.Errorf("bulk validation took too long: %v", duration)
		}

		t.Logf("Validated 1000 resources in %v", duration)
	})

	t.Run("complex validation performance", func(t *testing.T) {
		inputs := make(map[string]interface{})

		// Create complex input set
		for i := 0; i < 100; i++ {
			inputs["name_"+string(rune(i))] = "test-app-" + string(rune(i))
			inputs["namespace_"+string(rune(i))] = "namespace-" + string(rune(i))
		}

		start := time.Now()
		result := validator.ValidateAll(inputs)
		duration := time.Since(start)

		// Should complete quickly even with many inputs
		if duration > 100*time.Millisecond {
			t.Errorf("complex validation took too long: %v", duration)
		}

		if !result.Valid {
			t.Errorf("expected complex validation to pass, got %d errors", len(result.Errors))
		}

		t.Logf("Validated %d complex inputs in %v", len(inputs), duration)
	})
}

func TestValidationErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	validator := validation.NewValidator()

	// Test edge cases and error conditions
	edgeCases := []struct {
		name  string
		input string
		field string
	}{
		{
			name:  "empty string",
			input: "",
			field: "name",
		},
		{
			name:  "very long string",
			input: string(make([]byte, 1000)),
			field: "name",
		},
		{
			name:  "unicode characters",
			input: "test-🚀-app",
			field: "name",
		},
		{
			name:  "special characters",
			input: "test@app#123",
			field: "name",
		},
		{
			name:  "whitespace",
			input: "  test app  ",
			field: "name",
		},
	}

	for _, edgeCase := range edgeCases {
		t.Run(edgeCase.name, func(t *testing.T) {
			// Should not panic on any input
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("validation panicked on input '%s': %v", edgeCase.input, r)
				}
			}()

			var err validation.ValidationError
			switch edgeCase.field {
			case "name":
				err = validator.ValidateKubernetesName(edgeCase.input)
			default:
				t.Fatalf("unknown field: %s", edgeCase.field)
			}

			// Error should have proper structure
			if err.Message != "" {
				if err.Field == "" {
					t.Error("validation error should have field set")
				}
				if err.Constraint == "" {
					t.Error("validation error should have constraint set")
				}
			}
		})
	}
}

func TestConcurrentValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	validator := validation.NewValidator()

	// Test concurrent validation safety
	t.Run("concurrent resource validation", func(t *testing.T) {
		done := make(chan bool)
		errorChan := make(chan error, 100)

		// Run 10 concurrent validators
		for i := 0; i < 10; i++ {
			go func(id int) {
				defer func() { done <- true }()

				for j := 0; j < 100; j++ {
					resourceRef := "pod/test-pod-" + string(rune(id)) + "-" + string(rune(j))
					err := validator.ValidateResourceReference(resourceRef)

					// All these should be valid
					if err.Message != "" {
						errorChan <- err
						return
					}
				}
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		close(errorChan)

		// Check for any errors
		var errors []error
		for err := range errorChan {
			errors = append(errors, err)
		}

		if len(errors) > 0 {
			t.Errorf("concurrent validation produced %d errors, first error: %v", len(errors), errors[0])
		}
	})
}

func TestValidationMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test for memory leaks in validation
	validator := validation.NewValidator()

	// Get initial memory stats
	var initialMem, finalMem uint64

	// Force GC before measurement
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	initialMem = m1.Alloc

	// Perform many validations
	for i := 0; i < 10000; i++ {
		inputs := map[string]interface{}{
			"name":      "test-app-" + string(rune(i%100)),
			"namespace": "namespace-" + string(rune(i%50)),
			"resource":  "pod/test-pod-" + string(rune(i%200)),
			"output":    "json",
			"timeout":   "30s",
		}
		validator.ValidateAll(inputs)
	}

	// Force GC after operations
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	finalMem = m2.Alloc

	// Memory should not have grown significantly
	memGrowth := finalMem - initialMem
	maxAllowedGrowth := uint64(10 * 1024 * 1024) // 10MB

	if memGrowth > maxAllowedGrowth {
		t.Errorf("validation memory usage grew by %d bytes (>%d allowed)", memGrowth, maxAllowedGrowth)
	}

	t.Logf("Memory growth after 10k validations: %d bytes", memGrowth)
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(strings.Contains(strings.ToLower(s), strings.ToLower(substr)))))
}
