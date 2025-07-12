package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// CLIError represents a user-friendly CLI error
type CLIError struct {
	Operation   string
	Problem     string
	Suggestion  string
	ExitCode    int
	ShowHelp    bool
	Examples    []string
	RelatedDocs []string
}

// Error implements the error interface
func (e *CLIError) Error() string {
	var msg strings.Builder

	// Main error message
	msg.WriteString(fmt.Sprintf("Error: %s", e.Problem))

	// Add suggestion if available
	if e.Suggestion != "" {
		msg.WriteString(fmt.Sprintf("\n\n💡 Try: %s", e.Suggestion))
	}

	// Add examples if available
	if len(e.Examples) > 0 {
		msg.WriteString("\n\n📚 Examples:")
		for _, example := range e.Examples {
			msg.WriteString(fmt.Sprintf("\n  %s", example))
		}
	}

	// Add related docs if available
	if len(e.RelatedDocs) > 0 {
		msg.WriteString("\n\n📖 Related:")
		for _, doc := range e.RelatedDocs {
			msg.WriteString(fmt.Sprintf("\n  %s", doc))
		}
	}

	return msg.String()
}

// NewCLIError creates a new user-friendly CLI error
func NewCLIError(operation, problem, suggestion string) *CLIError {
	return &CLIError{
		Operation:  operation,
		Problem:    problem,
		Suggestion: suggestion,
		ExitCode:   1,
	}
}

// WithExamples adds examples to the error
func (e *CLIError) WithExamples(examples ...string) *CLIError {
	e.Examples = examples
	return e
}

// WithDocs adds documentation links to the error
func (e *CLIError) WithDocs(docs ...string) *CLIError {
	e.RelatedDocs = docs
	return e
}

// WithHelp indicates that help should be shown
func (e *CLIError) WithHelp() *CLIError {
	e.ShowHelp = true
	return e
}

// WithExitCode sets a custom exit code
func (e *CLIError) WithExitCode(code int) *CLIError {
	e.ExitCode = code
	return e
}

// Common error constructors
func ErrInvalidResource(resource string) *CLIError {
	return NewCLIError(
		"resource validation",
		fmt.Sprintf("Invalid resource format: '%s'", resource),
		"Use format 'name' or 'kind/name' (e.g., 'my-pod' or 'deployment/api-service')",
	).WithExamples(
		"tapio check my-pod",
		"tapio check deployment/api-service",
		"tapio why pod/my-broken-pod",
	)
}

func ErrResourceNotFound(resource, namespace string) *CLIError {
	suggestion := fmt.Sprintf("Check if the resource exists: kubectl get pods -n %s", namespace)
	if namespace == "" {
		suggestion = "Try checking all namespaces with --all or specify --namespace"
	}

	return NewCLIError(
		"resource lookup",
		fmt.Sprintf("Resource '%s' not found in namespace '%s'", resource, namespace),
		suggestion,
	).WithExamples(
		"tapio check --all",
		"tapio check --namespace kube-system",
		"tapio use [namespace]  # Switch namespace first",
	)
}

func ErrKubernetesConnection(err error) *CLIError {
	var suggestion string
	errStr := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errStr, "connection refused"):
		suggestion = "Check if your Kubernetes cluster is running and kubectl works"
	case strings.Contains(errStr, "permission denied"):
		suggestion = "Check your Kubernetes permissions with: kubectl auth can-i get pods"
	case strings.Contains(errStr, "context"):
		suggestion = "Check your kubeconfig context: kubectl config current-context"
	case strings.Contains(errStr, "timeout"):
		suggestion = "Your cluster may be slow to respond. Try: kubectl get nodes"
	default:
		suggestion = "Verify your kubeconfig is correct: kubectl config view"
	}

	return NewCLIError(
		"kubernetes connection",
		fmt.Sprintf("Cannot connect to Kubernetes cluster: %s", err.Error()),
		suggestion,
	).WithExamples(
		"kubectl config get-contexts",
		"kubectl config use-context [context-name]",
		"tapio context  # Show current context",
	).WithDocs(
		"Kubernetes config: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/",
	)
}

func ErrInvalidFlag(flag, value, expected string) *CLIError {
	return NewCLIError(
		"flag validation",
		fmt.Sprintf("Invalid value '%s' for flag --%s", value, flag),
		fmt.Sprintf("Expected: %s", expected),
	).WithExamples(
		"tapio check --output json",
		"tapio check --namespace default",
	)
}

func ErrEBPFNotAvailable(err error) *CLIError {
	return NewCLIError(
		"ebpf initialization",
		"eBPF monitoring is not available on this system",
		"Try running without --enable-ebpf flag or run as root",
	).WithExamples(
		"tapio check  # Without eBPF",
		"sudo tapio check --enable-ebpf  # With root privileges",
	).WithDocs(
		"eBPF requirements: Root privileges and kernel support required",
	)
}

func ErrNoNamespaceAccess() *CLIError {
	return NewCLIError(
		"namespace access",
		"No access to any namespaces",
		"Check your Kubernetes RBAC permissions",
	).WithExamples(
		"kubectl auth can-i list namespaces",
		"kubectl auth can-i get pods --all-namespaces",
	)
}

// DidYouMeanSuggestion provides command suggestions for typos
func DidYouMeanSuggestion(input string, validCommands []string) string {
	// Simple edit distance for suggestions
	suggestions := []string{}
	for _, cmd := range validCommands {
		if editDistance(input, cmd) <= 2 && len(cmd) > 2 {
			suggestions = append(suggestions, cmd)
		}
	}

	if len(suggestions) == 0 {
		return ""
	}

	if len(suggestions) == 1 {
		return fmt.Sprintf("Did you mean '%s'?", suggestions[0])
	}

	return fmt.Sprintf("Did you mean one of: %s?", strings.Join(suggestions, ", "))
}

// editDistance calculates simple edit distance between two strings
func editDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// HandleCLIError handles CLI errors gracefully
func HandleCLIError(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}

	// Check if it's a CLIError
	if cliErr, ok := err.(*CLIError); ok {
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())

		if cliErr.ShowHelp {
			fmt.Fprintf(os.Stderr, "\n")
			cmd.Help()
		}

		os.Exit(cliErr.ExitCode)
	}

	// Handle cobra errors specially
	if strings.Contains(err.Error(), "unknown command") {
		parts := strings.Split(err.Error(), "\"")
		if len(parts) >= 2 {
			unknownCmd := parts[1]
			validCommands := []string{"check", "why", "prometheus", "context", "use", "version"}
			suggestion := DidYouMeanSuggestion(unknownCmd, validCommands)

			fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n", unknownCmd)
			if suggestion != "" {
				fmt.Fprintf(os.Stderr, "\n💡 %s\n", suggestion)
			}
			fmt.Fprintf(os.Stderr, "\nAvailable commands: %s\n", strings.Join(validCommands, ", "))
			fmt.Fprintf(os.Stderr, "\nRun 'tapio --help' for more information.\n")
			os.Exit(1)
		}
	}

	// Generic error handling
	fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
	fmt.Fprintf(os.Stderr, "\n💡 Try: tapio --help\n")
	os.Exit(1)
}

// ValidateOutputFormat validates output format flags
func ValidateOutputFormat(format string) error {
	validFormats := []string{"human", "json", "yaml"}
	for _, valid := range validFormats {
		if format == valid {
			return nil
		}
	}

	return ErrInvalidFlag("output", format, strings.Join(validFormats, ", "))
}

// ValidateNamespace validates namespace names
func ValidateNamespace(namespace string) error {
	if namespace == "" {
		return nil // Empty is valid (means default)
	}

	// Basic Kubernetes namespace validation
	if len(namespace) > 63 {
		return ErrInvalidFlag("namespace", namespace, "namespace name must be 63 characters or less")
	}

	// Check for invalid characters (simplified)
	if strings.Contains(namespace, " ") || strings.Contains(namespace, ".") {
		return ErrInvalidFlag("namespace", namespace, "namespace name must be lowercase alphanumeric with hyphens")
	}

	return nil
}
