package validation

import (
	"testing"
)

func TestValidator_ValidateKubernetesName(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
		constraint  string
	}{
		{
			name:        "valid name",
			input:       "my-app-123",
			shouldError: false,
		},
		{
			name:        "valid simple name",
			input:       "app",
			shouldError: false,
		},
		{
			name:        "empty name",
			input:       "",
			shouldError: true,
			constraint:  "required",
		},
		{
			name:        "name with uppercase",
			input:       "My-App",
			shouldError: true,
			constraint:  "kubernetes_dns_name",
		},
		{
			name:        "name with underscore",
			input:       "my_app",
			shouldError: true,
			constraint:  "kubernetes_dns_name",
		},
		{
			name:        "name starting with hyphen",
			input:       "-myapp",
			shouldError: true,
			constraint:  "kubernetes_dns_name",
		},
		{
			name:        "name ending with hyphen",
			input:       "myapp-",
			shouldError: true,
			constraint:  "kubernetes_dns_name",
		},
		{
			name:        "too long name",
			input:       string(make([]byte, 254)),
			shouldError: true,
			constraint:  "max_length=253",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateKubernetesName(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
				if tt.constraint != "" && err.Constraint != tt.constraint {
					t.Errorf("expected constraint '%s', got '%s'", tt.constraint, err.Constraint)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateResourceReference(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "valid name only",
			input:       "my-pod",
			shouldError: false,
		},
		{
			name:        "valid kind/name",
			input:       "pod/my-pod",
			shouldError: false,
		},
		{
			name:        "valid deployment reference",
			input:       "deployment/api-service",
			shouldError: false,
		},
		{
			name:        "valid service reference",
			input:       "service/my-service",
			shouldError: false,
		},
		{
			name:        "empty reference",
			input:       "",
			shouldError: true,
		},
		{
			name:        "invalid format with multiple slashes",
			input:       "namespace/pod/my-pod",
			shouldError: true,
		},
		{
			name:        "invalid kind",
			input:       "invalidkind/my-resource",
			shouldError: true,
		},
		{
			name:        "invalid name",
			input:       "pod/Invalid-Name",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateResourceReference(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateKubernetesResourceKind(t *testing.T) {
	validator := NewValidator()

	validKinds := []string{
		"pod", "pods",
		"deployment", "deployments", "deploy",
		"service", "services", "svc",
		"configmap", "configmaps", "cm",
		"secret", "secrets",
		"ingress", "ingresses", "ing",
		"namespace", "namespaces", "ns",
	}

	for _, kind := range validKinds {
		t.Run("valid_"+kind, func(t *testing.T) {
			err := validator.ValidateKubernetesResourceKind(kind)
			if err.Message != "" {
				t.Errorf("unexpected validation error for valid kind '%s': %s", kind, err.Message)
			}
		})
	}

	invalidKinds := []string{
		"",
		"invalidkind",
		"customresource",
		"unknowntype",
	}

	for _, kind := range invalidKinds {
		t.Run("invalid_"+kind, func(t *testing.T) {
			err := validator.ValidateKubernetesResourceKind(kind)
			if err.Message == "" {
				t.Errorf("expected validation error for invalid kind '%s'", kind)
			}
		})
	}
}

func TestValidator_ValidateOutputFormat(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "valid human format",
			input:       "human",
			shouldError: false,
		},
		{
			name:        "valid json format",
			input:       "json",
			shouldError: false,
		},
		{
			name:        "valid yaml format",
			input:       "yaml",
			shouldError: false,
		},
		{
			name:        "valid yml alias",
			input:       "yml",
			shouldError: false,
		},
		{
			name:        "valid table format",
			input:       "table",
			shouldError: false,
		},
		{
			name:        "case insensitive",
			input:       "JSON",
			shouldError: false,
		},
		{
			name:        "empty format",
			input:       "",
			shouldError: true,
		},
		{
			name:        "invalid format",
			input:       "xml",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOutputFormat(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateTimeout(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
		constraint  string
	}{
		{
			name:        "valid seconds",
			input:       "30s",
			shouldError: false,
		},
		{
			name:        "valid minutes",
			input:       "5m",
			shouldError: false,
		},
		{
			name:        "valid hours",
			input:       "1h",
			shouldError: false,
		},
		{
			name:        "valid compound",
			input:       "2m30s",
			shouldError: false,
		},
		{
			name:        "empty timeout",
			input:       "",
			shouldError: true,
			constraint:  "required",
		},
		{
			name:        "invalid format",
			input:       "30seconds",
			shouldError: true,
			constraint:  "valid_duration",
		},
		{
			name:        "too short",
			input:       "500ms",
			shouldError: true,
			constraint:  "min_duration=1s",
		},
		{
			name:        "too long",
			input:       "2h",
			shouldError: true,
			constraint:  "max_duration=1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateTimeout(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
				if tt.constraint != "" && err.Constraint != tt.constraint {
					t.Errorf("expected constraint '%s', got '%s'", tt.constraint, err.Constraint)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidatePort(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "valid port 80",
			input:       "80",
			shouldError: false,
		},
		{
			name:        "valid port 8080",
			input:       "8080",
			shouldError: false,
		},
		{
			name:        "valid port 65535",
			input:       "65535",
			shouldError: false,
		},
		{
			name:        "empty port",
			input:       "",
			shouldError: true,
		},
		{
			name:        "non-numeric port",
			input:       "http",
			shouldError: true,
		},
		{
			name:        "port zero",
			input:       "0",
			shouldError: true,
		},
		{
			name:        "negative port",
			input:       "-1",
			shouldError: true,
		},
		{
			name:        "port too high",
			input:       "65536",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidatePort(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateIPAddress(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "valid IPv4",
			input:       "192.168.1.1",
			shouldError: false,
		},
		{
			name:        "valid IPv6",
			input:       "2001:db8::1",
			shouldError: false,
		},
		{
			name:        "localhost IPv4",
			input:       "127.0.0.1",
			shouldError: false,
		},
		{
			name:        "localhost IPv6",
			input:       "::1",
			shouldError: false,
		},
		{
			name:        "empty IP",
			input:       "",
			shouldError: true,
		},
		{
			name:        "invalid IPv4",
			input:       "256.256.256.256",
			shouldError: true,
		},
		{
			name:        "invalid format",
			input:       "not-an-ip",
			shouldError: true,
		},
		{
			name:        "incomplete IPv4",
			input:       "192.168.1",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateIPAddress(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateMemorySize(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "valid Mi suffix",
			input:       "128Mi",
			shouldError: false,
		},
		{
			name:        "valid Gi suffix",
			input:       "2Gi",
			shouldError: false,
		},
		{
			name:        "valid Ki suffix",
			input:       "512Ki",
			shouldError: false,
		},
		{
			name:        "valid decimal",
			input:       "1.5Gi",
			shouldError: false,
		},
		{
			name:        "valid without suffix",
			input:       "1024",
			shouldError: false,
		},
		{
			name:        "empty size",
			input:       "",
			shouldError: true,
		},
		{
			name:        "invalid suffix",
			input:       "128MB",
			shouldError: true,
		},
		{
			name:        "invalid format",
			input:       "not-a-size",
			shouldError: true,
		},
		{
			name:        "space in size",
			input:       "128 Mi",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateMemorySize(tt.input)

			if tt.shouldError {
				if err.Message == "" {
					t.Errorf("expected validation error for input '%s'", tt.input)
				}
			} else {
				if err.Message != "" {
					t.Errorf("unexpected validation error for input '%s': %s", tt.input, err.Message)
				}
			}
		})
	}
}

func TestValidator_ValidateLabels(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		labels      map[string]string
		shouldError bool
		errorCount  int
	}{
		{
			name: "valid labels",
			labels: map[string]string{
				"app":     "tapio",
				"version": "1.0",
			},
			shouldError: false,
		},
		{
			name: "valid complex labels",
			labels: map[string]string{
				"app.kubernetes.io/name":    "tapio",
				"app.kubernetes.io/version": "1.0.0",
			},
			shouldError: false,
		},
		{
			name: "empty value allowed",
			labels: map[string]string{
				"app":         "tapio",
				"environment": "",
			},
			shouldError: false,
		},
		{
			name: "invalid key",
			labels: map[string]string{
				"app-":    "tapio", // ends with hyphen
				"version": "1.0",
			},
			shouldError: true,
			errorCount:  1,
		},
		{
			name: "invalid value",
			labels: map[string]string{
				"app":     "tapio",
				"version": "v1.0-", // ends with hyphen
			},
			shouldError: true,
			errorCount:  1,
		},
		{
			name: "multiple errors",
			labels: map[string]string{
				"-app":    "tapio-", // both key and value invalid
				"version": "1.0",
			},
			shouldError: true,
			errorCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateLabels(tt.labels)

			if tt.shouldError {
				if result.Valid {
					t.Errorf("expected validation errors")
				}
				if tt.errorCount > 0 && len(result.Errors) != tt.errorCount {
					t.Errorf("expected %d errors, got %d", tt.errorCount, len(result.Errors))
				}
			} else {
				if !result.Valid {
					t.Errorf("unexpected validation errors: %v", result.Errors)
				}
			}
		})
	}
}

func TestValidator_ValidateAll(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		inputs      map[string]interface{}
		shouldError bool
		errorCount  int
	}{
		{
			name: "all valid inputs",
			inputs: map[string]interface{}{
				"name":      "my-app",
				"namespace": "default",
				"output":    "json",
				"timeout":   "30s",
				"port":      "8080",
			},
			shouldError: false,
		},
		{
			name: "mixed valid and invalid",
			inputs: map[string]interface{}{
				"name":      "my-app",
				"namespace": "Invalid-Namespace", // invalid
				"output":    "xml",               // invalid
				"timeout":   "30s",
				"port":      "8080",
			},
			shouldError: true,
			errorCount:  2,
		},
		{
			name: "unknown fields ignored",
			inputs: map[string]interface{}{
				"name":          "my-app",
				"unknown_field": "value", // should be ignored
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateAll(tt.inputs)

			if tt.shouldError {
				if result.Valid {
					t.Errorf("expected validation errors")
				}
				if tt.errorCount > 0 && len(result.Errors) != tt.errorCount {
					t.Errorf("expected %d errors, got %d", tt.errorCount, len(result.Errors))
				}
			} else {
				if !result.Valid {
					t.Errorf("unexpected validation errors: %v", result.Errors)
				}
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := ValidationError{
		Field:      "name",
		Value:      "Invalid-Name",
		Constraint: "kubernetes_dns_name",
		Message:    "name must be lowercase",
	}

	expected := "validation failed for name: name must be lowercase"
	if err.Error() != expected {
		t.Errorf("expected error message '%s', got '%s'", expected, err.Error())
	}
}

func TestValidationResult_Error(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{Field: "name", Message: "invalid name"},
			{Field: "port", Message: "invalid port"},
		},
	}

	errorStr := result.Error()
	if errorStr == "" {
		t.Error("expected non-empty error string")
	}

	// Should contain both error messages
	if !contains(errorStr, "invalid name") || !contains(errorStr, "invalid port") {
		t.Errorf("error string should contain both messages: %s", errorStr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || (len(s) > len(substr) && s[len(s)-len(substr)-1:len(s)-1] == substr)))
}

// Benchmark tests
func BenchmarkValidator_ValidateKubernetesName(b *testing.B) {
	validator := NewValidator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateKubernetesName("my-app-123")
	}
}

func BenchmarkValidator_ValidateResourceReference(b *testing.B) {
	validator := NewValidator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateResourceReference("deployment/api-service")
	}
}

func BenchmarkValidator_ValidateAll(b *testing.B) {
	validator := NewValidator()
	inputs := map[string]interface{}{
		"name":      "my-app",
		"namespace": "default",
		"output":    "json",
		"timeout":   "30s",
		"port":      "8080",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateAll(inputs)
	}
}
