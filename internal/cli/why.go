package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	
	"github.com/falseyair/tapio/internal/output"
	"github.com/falseyair/tapio/pkg/simple"
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
	ctx := context.Background()
	resource := args[0]

	// Parse resource reference
	resourceRef, err := parseResourceReference(resource)
	if err != nil {
		return fmt.Errorf("invalid resource reference: %w", err)
	}

	// Create enhanced explainer (with eBPF support if available)
	explainer, err := simple.NewEnhancedExplainer()
	if err != nil {
		return fmt.Errorf("failed to initialize explainer: %w", err)
	}
	defer explainer.Close()

	// Build explanation request
	request := &types.ExplainRequest{
		Resource:  resourceRef,
		Verbose:   whyVerbose,
		Namespace: whyNamespace,
	}

	// Generate explanation
	explanation, err := explainer.Explain(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to explain resource: %w", err)
	}

	// Output explanation
	formatter := output.NewFormatter(whyOutput)
	return formatter.PrintExplanation(explanation)
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