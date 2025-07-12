package validation

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Validator provides comprehensive input validation for Tapio
type Validator struct {
	// Kubernetes naming patterns
	k8sNameRegex       *regexp.Regexp
	k8sNamespaceRegex  *regexp.Regexp
	k8sLabelKeyRegex   *regexp.Regexp
	k8sLabelValueRegex *regexp.Regexp
}

// ValidationError represents a validation failure
type ValidationError struct {
	Field       string   `json:"field"`
	Value       string   `json:"value"`
	Constraint  string   `json:"constraint"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Message)
}

// ValidationResult contains multiple validation errors
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

func (r *ValidationResult) AddError(field, value, constraint, message string, suggestions ...string) {
	r.Valid = false
	r.Errors = append(r.Errors, ValidationError{
		Field:       field,
		Value:       value,
		Constraint:  constraint,
		Message:     message,
		Suggestions: suggestions,
	})
}

func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

func (r *ValidationResult) Error() string {
	if r.Valid {
		return ""
	}

	var messages []string
	for _, err := range r.Errors {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// NewValidator creates a new validator with compiled regexes
func NewValidator() *Validator {
	return &Validator{
		// Kubernetes DNS-1123 label regex (RFC 1123)
		k8sNameRegex: regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`),
		// Kubernetes namespace regex
		k8sNamespaceRegex: regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`),
		// Kubernetes label key regex
		k8sLabelKeyRegex: regexp.MustCompile(`^([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]$`),
		// Kubernetes label value regex
		k8sLabelValueRegex: regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`),
	}
}

// ValidateKubernetesName validates a Kubernetes resource name
func (v *Validator) ValidateKubernetesName(name string) ValidationError {
	if name == "" {
		return ValidationError{
			Field:       "name",
			Value:       name,
			Constraint:  "required",
			Message:     "name cannot be empty",
			Suggestions: []string{"provide a valid Kubernetes resource name"},
		}
	}

	if len(name) > 253 {
		return ValidationError{
			Field:       "name",
			Value:       name,
			Constraint:  "max_length=253",
			Message:     "name is too long (max 253 characters)",
			Suggestions: []string{"shorten the name to 253 characters or less"},
		}
	}

	if !v.k8sNameRegex.MatchString(name) {
		return ValidationError{
			Field:      "name",
			Value:      name,
			Constraint: "kubernetes_dns_name",
			Message:    "name must be lowercase alphanumeric with hyphens",
			Suggestions: []string{
				"use only lowercase letters, numbers, and hyphens",
				"start and end with alphanumeric characters",
				"example: 'my-app-123'",
			},
		}
	}

	return ValidationError{}
}

// ValidateKubernetesNamespace validates a Kubernetes namespace name
func (v *Validator) ValidateKubernetesNamespace(namespace string) ValidationError {
	if namespace == "" {
		// Empty namespace is valid (means default)
		return ValidationError{}
	}

	if len(namespace) > 63 {
		return ValidationError{
			Field:       "namespace",
			Value:       namespace,
			Constraint:  "max_length=63",
			Message:     "namespace name is too long (max 63 characters)",
			Suggestions: []string{"shorten the namespace name to 63 characters or less"},
		}
	}

	if !v.k8sNamespaceRegex.MatchString(namespace) {
		return ValidationError{
			Field:      "namespace",
			Value:      namespace,
			Constraint: "kubernetes_dns_name",
			Message:    "namespace must be lowercase alphanumeric with hyphens",
			Suggestions: []string{
				"use only lowercase letters, numbers, and hyphens",
				"start and end with alphanumeric characters",
				"example: 'my-namespace'",
			},
		}
	}

	return ValidationError{}
}

// ValidateResourceReference validates a Kubernetes resource reference
func (v *Validator) ValidateResourceReference(reference string) ValidationError {
	if reference == "" {
		return ValidationError{
			Field:      "resource",
			Value:      reference,
			Constraint: "required",
			Message:    "resource reference cannot be empty",
			Suggestions: []string{
				"use format 'name' or 'kind/name'",
				"examples: 'my-pod', 'deployment/api-service'",
			},
		}
	}

	parts := strings.Split(reference, "/")
	switch len(parts) {
	case 1:
		// Just a name
		if err := v.ValidateKubernetesName(parts[0]); err.Message != "" {
			err.Field = "resource"
			err.Suggestions = append(err.Suggestions, "use format 'kind/name' if you meant to specify a resource type")
			return err
		}
	case 2:
		// kind/name format
		kind, name := parts[0], parts[1]

		if err := v.ValidateKubernetesResourceKind(kind); err.Message != "" {
			return err
		}

		if err := v.ValidateKubernetesName(name); err.Message != "" {
			err.Field = "resource.name"
			return err
		}
	default:
		return ValidationError{
			Field:      "resource",
			Value:      reference,
			Constraint: "format",
			Message:    "invalid resource format",
			Suggestions: []string{
				"use format 'name' or 'kind/name'",
				"examples: 'my-pod', 'deployment/api-service', 'pod/my-pod-xyz'",
			},
		}
	}

	return ValidationError{}
}

// ValidateKubernetesResourceKind validates a Kubernetes resource kind
func (v *Validator) ValidateKubernetesResourceKind(kind string) ValidationError {
	if kind == "" {
		return ValidationError{
			Field:      "resource.kind",
			Value:      kind,
			Constraint: "required",
			Message:    "resource kind cannot be empty",
		}
	}

	// List of common Kubernetes resource kinds
	validKinds := map[string]bool{
		"pod": true, "pods": true,
		"deployment": true, "deployments": true, "deploy": true,
		"service": true, "services": true, "svc": true,
		"configmap": true, "configmaps": true, "cm": true,
		"secret": true, "secrets": true,
		"ingress": true, "ingresses": true, "ing": true,
		"namespace": true, "namespaces": true, "ns": true,
		"node": true, "nodes": true,
		"persistentvolume": true, "persistentvolumes": true, "pv": true,
		"persistentvolumeclaim": true, "persistentvolumeclaims": true, "pvc": true,
		"daemonset": true, "daemonsets": true, "ds": true,
		"statefulset": true, "statefulsets": true, "sts": true,
		"replicaset": true, "replicasets": true, "rs": true,
		"job": true, "jobs": true,
		"cronjob": true, "cronjobs": true, "cj": true,
		"horizontalpodautoscaler": true, "hpa": true,
		"networkpolicy": true, "networkpolicies": true, "netpol": true,
		"serviceaccount": true, "serviceaccounts": true, "sa": true,
		"role": true, "roles": true,
		"rolebinding": true, "rolebindings": true,
		"clusterrole": true, "clusterroles": true,
		"clusterrolebinding": true, "clusterrolebindings": true,
		"event": true, "events": true, "ev": true,
	}

	lowerKind := strings.ToLower(kind)
	if !validKinds[lowerKind] {
		suggestions := []string{
			"common kinds: pod, deployment, service, configmap, secret",
			"use 'kubectl api-resources' to see all available resource types",
		}

		// Try to suggest similar kinds
		for validKind := range validKinds {
			if strings.Contains(validKind, lowerKind) || strings.Contains(lowerKind, validKind) {
				suggestions = append(suggestions, fmt.Sprintf("did you mean '%s'?", validKind))
				break
			}
		}

		return ValidationError{
			Field:       "resource.kind",
			Value:       kind,
			Constraint:  "valid_kubernetes_kind",
			Message:     "unknown or unsupported resource kind",
			Suggestions: suggestions,
		}
	}

	return ValidationError{}
}

// ValidateOutputFormat validates output format
func (v *Validator) ValidateOutputFormat(format string) ValidationError {
	if format == "" {
		return ValidationError{
			Field:       "output",
			Value:       format,
			Constraint:  "required",
			Message:     "output format cannot be empty",
			Suggestions: []string{"use one of: human, json, yaml"},
		}
	}

	validFormats := map[string]bool{
		"human": true,
		"json":  true,
		"yaml":  true,
		"yml":   true, // alias for yaml
		"table": true, // alternative human-readable format
	}

	if !validFormats[strings.ToLower(format)] {
		return ValidationError{
			Field:      "output",
			Value:      format,
			Constraint: "valid_format",
			Message:    "unsupported output format",
			Suggestions: []string{
				"supported formats: human, json, yaml",
				"human: colored text output (default)",
				"json: machine-readable JSON",
				"yaml: YAML format",
			},
		}
	}

	return ValidationError{}
}

// ValidateTimeout validates timeout duration
func (v *Validator) ValidateTimeout(timeout string) ValidationError {
	if timeout == "" {
		return ValidationError{
			Field:       "timeout",
			Value:       timeout,
			Constraint:  "required",
			Message:     "timeout cannot be empty",
			Suggestions: []string{"use format like '30s', '5m', '1h'"},
		}
	}

	duration, err := time.ParseDuration(timeout)
	if err != nil {
		return ValidationError{
			Field:      "timeout",
			Value:      timeout,
			Constraint: "valid_duration",
			Message:    "invalid timeout format",
			Suggestions: []string{
				"use Go duration format: '30s', '5m', '1h'",
				"examples: '10s' (10 seconds), '2m30s' (2 minutes 30 seconds)",
			},
		}
	}

	if duration < time.Second {
		return ValidationError{
			Field:       "timeout",
			Value:       timeout,
			Constraint:  "min_duration=1s",
			Message:     "timeout too short (minimum 1 second)",
			Suggestions: []string{"use at least '1s'"},
		}
	}

	if duration > 1*time.Hour {
		return ValidationError{
			Field:       "timeout",
			Value:       timeout,
			Constraint:  "max_duration=1h",
			Message:     "timeout too long (maximum 1 hour)",
			Suggestions: []string{"use a shorter timeout (maximum '1h')"},
		}
	}

	return ValidationError{}
}

// ValidatePort validates a network port number
func (v *Validator) ValidatePort(port string) ValidationError {
	if port == "" {
		return ValidationError{
			Field:       "port",
			Value:       port,
			Constraint:  "required",
			Message:     "port cannot be empty",
			Suggestions: []string{"use a port number between 1 and 65535"},
		}
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return ValidationError{
			Field:       "port",
			Value:       port,
			Constraint:  "numeric",
			Message:     "port must be a number",
			Suggestions: []string{"use a port number between 1 and 65535"},
		}
	}

	if portNum < 1 || portNum > 65535 {
		return ValidationError{
			Field:      "port",
			Value:      port,
			Constraint: "range=1-65535",
			Message:    "port number out of valid range",
			Suggestions: []string{
				"use a port number between 1 and 65535",
				"common ports: 80 (HTTP), 443 (HTTPS), 8080 (alt-HTTP)",
			},
		}
	}

	return ValidationError{}
}

// ValidateIPAddress validates an IP address
func (v *Validator) ValidateIPAddress(ip string) ValidationError {
	if ip == "" {
		return ValidationError{
			Field:       "ip",
			Value:       ip,
			Constraint:  "required",
			Message:     "IP address cannot be empty",
			Suggestions: []string{"use a valid IPv4 or IPv6 address"},
		}
	}

	if parsedIP := net.ParseIP(ip); parsedIP == nil {
		return ValidationError{
			Field:      "ip",
			Value:      ip,
			Constraint: "valid_ip",
			Message:    "invalid IP address format",
			Suggestions: []string{
				"use valid IPv4 format (e.g., '192.168.1.1')",
				"use valid IPv6 format (e.g., '2001:db8::1')",
			},
		}
	}

	return ValidationError{}
}

// ValidateLabels validates Kubernetes labels
func (v *Validator) ValidateLabels(labels map[string]string) *ValidationResult {
	result := &ValidationResult{Valid: true}

	for key, value := range labels {
		if err := v.ValidateKubernetesLabelKey(key); err.Message != "" {
			result.AddError("label.key", key, err.Constraint, err.Message, err.Suggestions...)
		}

		if err := v.ValidateKubernetesLabelValue(value); err.Message != "" {
			result.AddError("label.value", value, err.Constraint, err.Message, err.Suggestions...)
		}
	}

	return result
}

// ValidateKubernetesLabelKey validates a Kubernetes label key
func (v *Validator) ValidateKubernetesLabelKey(key string) ValidationError {
	if key == "" {
		return ValidationError{
			Field:      "label.key",
			Value:      key,
			Constraint: "required",
			Message:    "label key cannot be empty",
		}
	}

	if len(key) > 63 {
		return ValidationError{
			Field:      "label.key",
			Value:      key,
			Constraint: "max_length=63",
			Message:    "label key is too long (max 63 characters)",
		}
	}

	if !v.k8sLabelKeyRegex.MatchString(key) {
		return ValidationError{
			Field:      "label.key",
			Value:      key,
			Constraint: "kubernetes_label_key",
			Message:    "invalid label key format",
			Suggestions: []string{
				"use alphanumeric characters, hyphens, underscores, and dots",
				"start and end with alphanumeric characters",
				"example: 'app.kubernetes.io/name'",
			},
		}
	}

	return ValidationError{}
}

// ValidateKubernetesLabelValue validates a Kubernetes label value
func (v *Validator) ValidateKubernetesLabelValue(value string) ValidationError {
	if len(value) > 63 {
		return ValidationError{
			Field:      "label.value",
			Value:      value,
			Constraint: "max_length=63",
			Message:    "label value is too long (max 63 characters)",
		}
	}

	if !v.k8sLabelValueRegex.MatchString(value) {
		return ValidationError{
			Field:      "label.value",
			Value:      value,
			Constraint: "kubernetes_label_value",
			Message:    "invalid label value format",
			Suggestions: []string{
				"use alphanumeric characters, hyphens, underscores, and dots",
				"start and end with alphanumeric characters (if not empty)",
				"empty values are allowed",
			},
		}
	}

	return ValidationError{}
}

// ValidateMemorySize validates memory size strings like "128Mi", "1Gi"
func (v *Validator) ValidateMemorySize(size string) ValidationError {
	if size == "" {
		return ValidationError{
			Field:       "memory",
			Value:       size,
			Constraint:  "required",
			Message:     "memory size cannot be empty",
			Suggestions: []string{"use format like '128Mi', '1Gi', '512Ki'"},
		}
	}

	// Simple regex for Kubernetes memory format
	memoryRegex := regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|K|M|G|T|P|E)?$`)
	if !memoryRegex.MatchString(size) {
		return ValidationError{
			Field:      "memory",
			Value:      size,
			Constraint: "kubernetes_memory_format",
			Message:    "invalid memory size format",
			Suggestions: []string{
				"use Kubernetes memory format: '128Mi', '1Gi', '512Ki'",
				"suffixes: Ki (1024), Mi (1024²), Gi (1024³)",
				"examples: '256Mi' (256 MiB), '2Gi' (2 GiB)",
			},
		}
	}

	return ValidationError{}
}

// ValidateAll performs comprehensive validation on a set of inputs
func (v *Validator) ValidateAll(inputs map[string]interface{}) *ValidationResult {
	result := &ValidationResult{Valid: true}

	for field, value := range inputs {
		strValue := fmt.Sprintf("%v", value)

		var err ValidationError
		switch field {
		case "name":
			err = v.ValidateKubernetesName(strValue)
		case "namespace":
			err = v.ValidateKubernetesNamespace(strValue)
		case "resource":
			err = v.ValidateResourceReference(strValue)
		case "output", "format":
			err = v.ValidateOutputFormat(strValue)
		case "timeout":
			err = v.ValidateTimeout(strValue)
		case "port":
			err = v.ValidatePort(strValue)
		case "ip", "address":
			err = v.ValidateIPAddress(strValue)
		case "memory":
			err = v.ValidateMemorySize(strValue)
		default:
			// Skip unknown fields
			continue
		}

		if err.Message != "" {
			err.Field = field
			result.Valid = false
			result.Errors = append(result.Errors, err)
		}
	}

	return result
}
