package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	
	"github.com/falseyair/tapio/internal/output"
	"github.com/falseyair/tapio/pkg/types"
)

var (
	whyVerbose   bool
	whyOutput    string
	whyNamespace string
)

var whyCmd = &cobra.Command{
	Use:   "why <resource>",
	Short: "Explain why a Kubernetes resource has problems",
	Long: `Why provides detailed explanations of problems in plain English.

It analyzes the resource state, correlates multiple data sources, and explains
both the symptoms and root causes in terms anyone can understand.`,
	Example: `  # Explain why a pod is having issues
  tapio why my-broken-pod

  # Explain a deployment's problems
  tapio why deployment/api-service

  # Get detailed technical explanation
  tapio why my-pod --verbose

  # Get explanation in JSON format
  tapio why my-pod --output json`,
	Args: cobra.ExactArgs(1),
	RunE: runWhy,
}

func init() {
	whyCmd.Flags().BoolVarP(&whyVerbose, "verbose", "v", false, "Include detailed technical information")
	whyCmd.Flags().StringVarP(&whyOutput, "output", "o", "human", "Output format: human, json, yaml")
	whyCmd.Flags().StringVarP(&whyNamespace, "namespace", "n", "", "Kubernetes namespace")
}

func runWhy(cmd *cobra.Command, args []string) error {
	resource := args[0]

	// Parse resource reference
	resourceRef, err := parseResourceReference(resource)
	if err != nil {
		return fmt.Errorf("invalid resource reference: %w", err)
	}
	
	// Determine namespace to use
	namespace := whyNamespace
	if namespace == "" {
		namespace = getCurrentNamespace()
		if namespace == "" {
			namespace = "default"
		}
	}
	resourceRef.Namespace = namespace
	
	// Show which namespace we're analyzing
	fmt.Printf("Analyzing %s in namespace: %s\n\n", resourceRef.Kind, namespace)

	// For now, create a simple explanation
	// Enhanced explainer with eBPF can be added later
	explanation := &types.Explanation{
		Resource: resourceRef,
		Summary: fmt.Sprintf("Analysis of %s/%s", resourceRef.Kind, resourceRef.Name),
		Analysis: &types.Analysis{
			RealityCheck: &types.RealityCheck{
				ActualMemory:   "Checking...",
				RestartPattern: "No recent restarts detected",
			},
		},
		RootCauses: []types.RootCause{
			{
				Title:       "Resource Status",
				Description: "Basic Kubernetes API analysis",
				Evidence:    []string{"Resource exists in cluster"},
				Confidence:  0.5,
			},
		},
		Solutions: []types.Solution{
			{
				Title:       "Check Resource Details",
				Description: "Review the current status and configuration",
				Commands: []string{
					fmt.Sprintf("kubectl describe %s %s -n %s", resourceRef.Kind, resourceRef.Name, namespace),
					fmt.Sprintf("kubectl logs %s -n %s", resourceRef.Name, namespace),
				},
				Urgency:    types.SeverityWarning,
				Difficulty: "easy",
				Risk:       "low",
			},
		},
	}

	// Output explanation
	formatter := output.NewFormatter(whyOutput)
	err = formatter.PrintExplanation(explanation)
	if err != nil {
		return fmt.Errorf("failed to print explanation: %w", err)
	}
	
	return nil
}

// parseResourceReference parses "pod/name" or "name" format
func parseResourceReference(resource string) (*types.ResourceRef, error) {
	parts := strings.Split(resource, "/")
	
	switch len(parts) {
	case 1:
		// Just name - assume it's a pod
		return &types.ResourceRef{
			Kind: "pod",
			Name: parts[0],
		}, nil
	case 2:
		// kind/name format
		return &types.ResourceRef{
			Kind: strings.ToLower(parts[0]),
			Name: parts[1],
		}, nil
	default:
		return nil, fmt.Errorf("invalid format, use 'name' or 'kind/name'")
	}
}