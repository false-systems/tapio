package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/falseyair/tapio/internal/output"
	"github.com/falseyair/tapio/pkg/simple"
	"github.com/falseyair/tapio/pkg/types"
)

var (
	whyVerbose    bool
	whyOutput     string
	whyNamespace  string
	whyEnableEBPF bool
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
	whyCmd.Flags().BoolVar(&whyEnableEBPF, "enable-ebpf", false, "Enable eBPF monitoring for enhanced insights")
}

func runWhy(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	startTime := time.Now()
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

	// Create enhanced checker with eBPF if requested
	var checker *simple.Checker
	if whyEnableEBPF {
		checker, err = simple.NewCheckerWithEBPF()
		if err != nil {
			fmt.Printf("[WARN] eBPF not available, using standard analysis: %v\n", err)
			checker, err = simple.NewChecker()
			if err != nil {
				return fmt.Errorf("failed to initialize checker: %w", err)
			}
		} else {
			fmt.Println("[OK] Enhanced analysis with eBPF enabled")
		}
	} else {
		checker, err = simple.NewChecker()
		if err != nil {
			return fmt.Errorf("failed to initialize checker: %w", err)
		}
	}

	// Get related problems first to provide context
	checkReq := &types.CheckRequest{
		Resource:  resourceRef.Name,
		Namespace: namespace,
		Verbose:   whyVerbose,
	}

	checkResult, err := checker.Check(ctx, checkReq)
	if err != nil {
		fmt.Printf("[WARN] Unable to get full context: %v\n", err)
		checkResult = &types.CheckResult{Problems: []types.Problem{}}
	}

	// Filter problems for this specific resource
	var resourceProblems []types.Problem
	for _, problem := range checkResult.Problems {
		if problem.Resource.Name == resourceRef.Name && problem.Resource.Namespace == resourceRef.Namespace {
			resourceProblems = append(resourceProblems, problem)
		}
	}

	// Get enhanced explanation
	var explanation *types.Explanation
	if enhancedExplainer, ok := checker.GetEnhancedExplainer(); ok {
		explanation, err = enhancedExplainer.ExplainResource(ctx, resourceRef, resourceProblems)
		if err != nil {
			fmt.Printf("[WARN] Enhanced analysis failed: %v\n", err)
			explanation = createFallbackExplanation(resourceRef, resourceProblems)
		} else {
			fmt.Printf("[OK] Enhanced analysis completed in %v\n", time.Since(startTime))
		}
	} else {
		explanation = createFallbackExplanation(resourceRef, resourceProblems)
	}

	// Output explanation
	formatter := output.NewFormatter(whyOutput)
	err = formatter.PrintExplanation(explanation)
	if err != nil {
		return fmt.Errorf("failed to print explanation: %w", err)
	}

	return nil
}

// createFallbackExplanation creates a fallback explanation when enhanced analysis fails
func createFallbackExplanation(resourceRef *types.ResourceRef, problems []types.Problem) *types.Explanation {
	explanation := &types.Explanation{
		Resource: resourceRef,
		Summary:  fmt.Sprintf("Basic analysis of %s/%s", resourceRef.Kind, resourceRef.Name),
		Problems: problems,
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
					fmt.Sprintf("kubectl describe %s %s -n %s", resourceRef.Kind, resourceRef.Name, resourceRef.Namespace),
					fmt.Sprintf("kubectl logs %s -n %s", resourceRef.Name, resourceRef.Namespace),
				},
				Urgency:    types.SeverityWarning,
				Difficulty: "easy",
				Risk:       "low",
			},
		},
		Timestamp: time.Now(),
	}

	// Add insights based on problems
	if len(problems) > 0 {
		explanation.RootCauses = append(explanation.RootCauses, types.RootCause{
			Title:       "Issues Detected",
			Description: fmt.Sprintf("Found %d issues with this resource", len(problems)),
			Evidence:    []string{fmt.Sprintf("%d problems identified", len(problems))},
			Confidence:  0.8,
		})
	}

	return explanation
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
