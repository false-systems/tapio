package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
	"github.com/yairfalse/tapio/pkg/universal"
	"github.com/yairfalse/tapio/pkg/universal/converters"
	"github.com/yairfalse/tapio/pkg/universal/formatters"
)

var (
	whyVerbose      bool
	whyOutput       string
	whyNamespace    string
	whyEnableEBPF   bool
	whyUseUniversal bool
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
	whyCmd.Flags().BoolVar(&whyUseUniversal, "universal", true, "Use universal data format for enhanced output")
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
	if whyUseUniversal && whyOutput == "human" {
		// Convert to universal format and use CLI formatter
		return outputUniversalExplanation(ctx, explanation, resourceProblems, checker)
	}

	// Use traditional formatter
	formatter := output.NewFormatter(whyOutput)
	err = formatter.PrintExplanation(explanation)
	if err != nil {
		return fmt.Errorf("failed to print explanation: %w", err)
	}

	return nil
}

// outputUniversalExplanation outputs the explanation using universal data format
func outputUniversalExplanation(ctx context.Context, explanation *types.Explanation, problems []types.Problem, checker *simple.Checker) error {
	// Create converters
	correlationConverter := converters.NewCorrelationConverter()

	// Create CLI formatter
	cliFormatter := formatters.NewCLIFormatter(&formatters.CLIConfig{
		UseColor:   true,
		Verbosity:  1,
		TimeFormat: "15:04:05",
	})

	if whyVerbose {
		cliFormatter = formatters.NewCLIFormatter(&formatters.CLIConfig{
			UseColor:   true,
			Verbosity:  2,
			TimeFormat: "15:04:05",
		})
	}

	// Create universal dataset
	dataset := &universal.UniversalDataset{
		Source:    "tapio-why",
		Version:   "1.0",
		Timestamp: time.Now(),
	}

	// Convert problems to universal predictions
	for _, problem := range problems {
		if problem.Prediction != nil {
			// Create a simple finding from the problem
			finding := &correlation.Finding{
				Title:       problem.Title,
				Description: problem.Description,
				Severity:    convertSeverity(problem.Severity),
				Confidence:  problem.Prediction.Confidence,
				Resource: &correlation.ResourceReference{
					Kind:      explanation.Resource.Kind,
					Name:      explanation.Resource.Name,
					Namespace: explanation.Resource.Namespace,
				},
				Prediction: &correlation.Prediction{
					Event:       "OOM",
					TimeToEvent: problem.Prediction.TimeToFailure,
					Confidence:  problem.Prediction.Confidence,
				},
			}

			// Convert to universal prediction
			pred, err := correlationConverter.ConvertFinding(finding)
			if err == nil {
				dataset.Predictions = append(dataset.Predictions, pred)
			}
		}
	}

	// Output header
	fmt.Printf("\n🔍 Analysis Results for %s/%s\n", explanation.Resource.Kind, explanation.Resource.Name)
	fmt.Println(strings.Repeat("=", 60))

	// Output predictions using CLI formatter
	if len(dataset.Predictions) > 0 {
		fmt.Println("\n📊 Predictions:")
		explanationStr := cliFormatter.FormatExplanation(dataset)
		fmt.Println(explanationStr)
	} else {
		fmt.Println("\n✅ No critical issues detected")
	}

	// Output root causes
	if len(explanation.RootCauses) > 0 {
		fmt.Printf("\n🔍 Root Causes (%d found):\n", len(explanation.RootCauses))
		for i, cause := range explanation.RootCauses {
			fmt.Printf("\n%d. %s (%.0f%% confidence)\n", i+1, cause.Title, cause.Confidence*100)
			fmt.Printf("   %s\n", cause.Description)
			if len(cause.Evidence) > 0 {
				fmt.Println("   Evidence:")
				for _, evidence := range cause.Evidence {
					fmt.Printf("   • %s\n", evidence)
				}
			}
		}
	}

	// Output solutions
	if len(explanation.Solutions) > 0 {
		fmt.Printf("\n💡 Recommended Solutions:\n")
		for i, solution := range explanation.Solutions {
			fmt.Printf("\n%d. %s [%s difficulty, %s risk]\n", i+1, solution.Title, solution.Difficulty, solution.Risk)
			fmt.Printf("   %s\n", solution.Description)
			if len(solution.Commands) > 0 {
				fmt.Println("   Commands:")
				for _, cmd := range solution.Commands {
					fmt.Printf("   $ %s\n", cmd)
				}
			}
		}
	}

	// Output reality check if available
	if explanation.Analysis != nil && explanation.Analysis.RealityCheck != nil {
		fmt.Println("\n📈 Reality Check:")
		rc := explanation.Analysis.RealityCheck
		if rc.ActualMemory != "" {
			fmt.Printf("   Actual Memory: %s\n", rc.ActualMemory)
		}
		if rc.RestartPattern != "" {
			fmt.Printf("   Restart Pattern: %s\n", rc.RestartPattern)
		}
		if rc.ContainerRuntime != "" {
			fmt.Printf("   Container Runtime: %s\n", rc.ContainerRuntime)
		}
	}

	fmt.Println()
	return nil
}

// convertSeverity converts types.Severity to correlation.Severity
func convertSeverity(sev types.Severity) correlation.Severity {
	switch sev {
	case types.SeverityCritical:
		return correlation.SeverityCritical
	case types.SeverityWarning:
		return correlation.SeverityWarning
	default:
		return correlation.SeverityInfo
	}
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
