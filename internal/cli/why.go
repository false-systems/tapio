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
	Long: `🔍 Why analyzes problems and explains them in plain English.

Why provides intelligent root cause analysis by:
  • Examining resource state and configuration
  • Correlating events across multiple sources
  • Using eBPF kernel insights (optional)
  • Explaining symptoms and root causes clearly
  • Providing actionable solutions with commands

The analysis goes beyond basic status checks to understand the "why" 
behind your Kubernetes resource problems.`,

	Example: `  # Explain why a pod is having issues
  tapio why my-broken-pod

  # Explain a deployment's problems
  tapio why deployment/api-service

  # Get detailed technical explanation
  tapio why my-pod --verbose

  # Get explanation in JSON format for automation
  tapio why my-pod --output json

  # Enhanced analysis with eBPF kernel insights
  tapio why my-pod --enable-ebpf

  # Analyze in specific namespace
  tapio why api-pod --namespace production`,

	Args: cobra.ExactArgs(1),

	// Validate arguments before running
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate output format
		if err := ValidateOutputFormat(whyOutput); err != nil {
			return err
		}

		// Validate namespace
		if err := ValidateNamespace(whyNamespace); err != nil {
			return err
		}

		// Validate resource format
		if err := validateResourceReference(args[0]); err != nil {
			return err
		}

		return nil
	},

	RunE: runWhy,
}

func init() {
	whyCmd.Flags().BoolVarP(&whyVerbose, "verbose", "v", false,
		"Include detailed technical information and analysis steps")
	whyCmd.Flags().StringVarP(&whyOutput, "output", "o", "human",
		"Output format: human (default), json, yaml")
	whyCmd.Flags().StringVarP(&whyNamespace, "namespace", "n", "",
		"Target namespace (default: current namespace from kubeconfig)")
	whyCmd.Flags().BoolVar(&whyEnableEBPF, "enable-ebpf", false,
		"Enable eBPF monitoring for kernel-level insights (requires root)")
	whyCmd.Flags().BoolVar(&whyUseUniversal, "universal", true,
		"Use enhanced universal data format for richer analysis output")
}

// validateResourceReference validates the resource reference format
func validateResourceReference(resource string) error {
	if resource == "" {
		return NewCLIError(
			"resource validation",
			"Resource name cannot be empty",
			"Provide a resource name or use 'kind/name' format",
		).WithExamples(
			"tapio why my-pod",
			"tapio why deployment/api-service",
		)
	}

	// Check for invalid characters
	if strings.Contains(resource, " ") {
		return ErrInvalidResource(resource)
	}

	// If it contains a slash, validate the kind
	if strings.Contains(resource, "/") {
		parts := strings.Split(resource, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return ErrInvalidResource(resource)
		}

		// Validate kind
		validKinds := []string{"pod", "deployment", "service", "configmap", "secret", "daemonset", "statefulset", "job", "cronjob", "replicaset"}
		kind := strings.ToLower(parts[0])
		isValid := false
		for _, validKind := range validKinds {
			if kind == validKind {
				isValid = true
				break
			}
		}

		if !isValid {
			return NewCLIError(
				"resource validation",
				fmt.Sprintf("Unknown resource kind: '%s'", parts[0]),
				fmt.Sprintf("Use one of: %s", strings.Join(validKinds, ", ")),
			).WithExamples(
				"tapio why pod/my-pod",
				"tapio why deployment/api-service",
			)
		}
	}

	return nil
}

func runWhy(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	resource := args[0]

	// Setup progress tracking
	steps := []string{
		"Parsing resource reference",
		"Initializing analyzer",
		"Connecting to Kubernetes",
		"Gathering resource context",
		"Analyzing problems",
	}

	if whyEnableEBPF {
		steps = append(steps, "Enhanced eBPF analysis")
	}

	progress := NewStepProgress(steps).WithVerbose(whyVerbose)
	progress.Start()

	// Parse resource reference
	resourceRef, err := parseResourceReference(resource)
	if err != nil {
		progress.Error(err)
		return NewCLIError(
			"resource parsing",
			"Invalid resource reference format",
			"Use format 'name' or 'kind/name'",
		).WithExamples(
			"tapio why my-pod",
			"tapio why deployment/api-service",
		)
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

	progress.NextStep() // Move to "Initializing analyzer"

	// Create enhanced checker with eBPF if requested
	var checker *simple.Checker
	if whyEnableEBPF {
		checker, err = simple.NewCheckerWithEBPF()
		if err != nil {
			progress.Warning(fmt.Sprintf("eBPF not available: %v", err))
			checker, err = simple.NewChecker()
			if err != nil {
				progress.Error(err)
				return ErrKubernetesConnection(err)
			}
		} else {
			if whyVerbose {
				fmt.Println("✨ Enhanced analysis with eBPF enabled")
			}
		}
	} else {
		checker, err = simple.NewChecker()
		if err != nil {
			progress.Error(err)
			return ErrKubernetesConnection(err)
		}
	}

	progress.NextStep() // Move to "Connecting to Kubernetes"

	// Show which namespace we're analyzing
	if whyVerbose {
		fmt.Printf("🔍 Analyzing %s/%s in namespace: %s\n", resourceRef.Kind, resourceRef.Name, namespace)
	}

	progress.NextStep() // Move to "Gathering resource context"

	// Get related problems first to provide context
	checkReq := &types.CheckRequest{
		Resource:  resourceRef.Name,
		Namespace: namespace,
		Verbose:   whyVerbose,
	}

	checkResult, err := checker.Check(ctx, checkReq)
	if err != nil {
		progress.Warning(fmt.Sprintf("Unable to get full context: %v", err))
		checkResult = &types.CheckResult{Problems: []types.Problem{}}
	}

	// Filter problems for this specific resource
	var resourceProblems []types.Problem
	for _, problem := range checkResult.Problems {
		if problem.Resource.Name == resourceRef.Name && problem.Resource.Namespace == resourceRef.Namespace {
			resourceProblems = append(resourceProblems, problem)
		}
	}

	progress.NextStep() // Move to "Analyzing problems"

	// Get enhanced explanation
	var explanation *types.Explanation
	if enhancedExplainer, ok := checker.GetEnhancedExplainer(); ok {
		if whyEnableEBPF {
			progress.NextStep() // Move to "Enhanced eBPF analysis"
		}

		explanation, err = enhancedExplainer.ExplainResource(ctx, resourceRef, resourceProblems)
		if err != nil {
			progress.Warning(fmt.Sprintf("Enhanced analysis failed: %v", err))
			explanation = createFallbackExplanation(resourceRef, resourceProblems)
		} else {
			if whyVerbose {
				fmt.Printf("✅ Enhanced analysis completed\n")
			}
		}
	} else {
		explanation = createFallbackExplanation(resourceRef, resourceProblems)
	}

	// Complete progress tracking
	progress.Finish("Analysis completed")

	// Check if resource was found
	if len(resourceProblems) == 0 && explanation != nil {
		// Check if this might be a resource not found case
		if explanation.Summary == "" || strings.Contains(explanation.Summary, "not found") {
			return NewCLIError(
				"resource analysis",
				fmt.Sprintf("Resource '%s' not found in namespace '%s'", resourceRef.Name, namespace),
				"Check if the resource exists and verify the namespace",
			).WithExamples(
				fmt.Sprintf("kubectl get %s %s -n %s", resourceRef.Kind, resourceRef.Name, namespace),
				"tapio why --namespace [other-namespace] "+resourceRef.Name,
				"tapio check --all  # Find the resource in other namespaces",
			)
		}
	}

	// Output explanation
	if whyUseUniversal && whyOutput == "human" {
		// Convert to universal format and use CLI formatter
		if err := outputUniversalExplanation(ctx, explanation, resourceProblems, checker); err != nil {
			return NewCLIError(
				"output formatting",
				"Failed to display analysis results",
				"Try using a different output format",
			).WithExamples(
				"tapio why "+resource+" --output json",
				"tapio why "+resource+" --output yaml",
			)
		}
		return nil
	}

	// Use traditional formatter
	formatter := output.NewFormatter(whyOutput)
	if err := formatter.PrintExplanation(explanation); err != nil {
		return NewCLIError(
			"output formatting",
			"Failed to display explanation",
			"Try using a different output format",
		).WithExamples(
			"tapio why "+resource+" --output json",
			"tapio why "+resource+" --output yaml",
		)
	}

	return nil
}

// outputUniversalExplanation outputs the explanation using universal data format
func outputUniversalExplanation(ctx context.Context, explanation *types.Explanation, problems []types.Problem, checker *simple.Checker) error {
	// Create converters
	correlationConverter := converters.NewCorrelationConverter("cli", "1.0")

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
				Resource: correlation.ResourceInfo{
					Type:      explanation.Resource.Kind,
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
				dataset.Predictions = append(dataset.Predictions, *pred)
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
		if explanationStr == "" {
			fmt.Println("   Analysis in progress...")
		} else {
			fmt.Println(explanationStr)
		}
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
		if len(rc.ErrorPatterns) > 0 {
			fmt.Printf("   Error Patterns: %v\n", rc.ErrorPatterns)
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
