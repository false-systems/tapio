package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
)

var (
	checkNamespace    string
	checkAll          bool
	outputFormat      string
	enableCorrelation bool
	enableEBPF        bool
)

var checkCmd = &cobra.Command{
	Use:   "check [resource]",
	Short: "Check if your Kubernetes resources are healthy",
	Long: `Check analyzes your pods, deployments, and services for potential problems.
    
It correlates Kubernetes API data with kernel-level insights to predict
failures before they happen and suggest immediate fixes.`,
	Example: `  # Check current namespace
  tapio check

  # Check specific app
  tapio check my-app

  # Check specific pod
  tapio check pod/my-app-7d4b9c8f-h2x9m

  # Check entire cluster
  tapio check --all

  # Check with JSON output
  tapio check --output json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCheck,
}

func init() {
	checkCmd.Flags().StringVarP(&checkNamespace, "namespace", "n", "", "Kubernetes namespace")
	checkCmd.Flags().BoolVar(&checkAll, "all", false, "Check all namespaces")
	checkCmd.Flags().StringVarP(&outputFormat, "output", "o", "human", "Output format: human, json, yaml")
	checkCmd.Flags().BoolVar(&enableCorrelation, "correlation", true, "Enable intelligent correlation analysis")
	checkCmd.Flags().BoolVar(&enableEBPF, "enable-ebpf", false, "Enable eBPF monitoring for enhanced insights")
}

func showCurrentContext(request *types.CheckRequest) {
	if request.All {
		fmt.Printf("Checking all namespaces...\n\n")
		return
	}

	namespace := request.Namespace
	if namespace == "" {
		// Get current namespace from context
		namespace = getCurrentNamespace()
		if namespace == "" {
			namespace = "default"
		}
	}

	fmt.Printf("Checking namespace: %s\n", namespace)
	if request.Resource != "" {
		fmt.Printf("Looking for: %s\n", request.Resource)
	}
	fmt.Println()
}

func handleNoResourceFound(resourceName, currentNamespace string) bool {
	// Try to find the resource in other namespaces
	suggestedNamespace, err := SuggestNamespaceForResource(resourceName)
	if err != nil {
		fmt.Printf("Resource '%s' not found in any namespace\n", resourceName)
		fmt.Printf("Try: tapio check --all\n")
		return true
	}

	// Ask if user wants to switch
	if PromptForNamespaceSwitch(suggestedNamespace, resourceName) {
		err := switchNamespace(suggestedNamespace)
		if err != nil {
			fmt.Printf("Failed to switch namespace: %v\n", err)
			return true
		}

		fmt.Printf("Switched to namespace: %s\n", suggestedNamespace)
		fmt.Printf("Re-run your command to check the resource\n")
		return true
	}

	return false
}

func offerNamespaceSelection() bool {
	fmt.Printf("No pods found in current namespace\n\n")

	checker, err := simple.NewChecker()
	if err != nil {
		return false
	}

	ctx := context.Background()
	client := checker.GetClient()
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false
	}

	fmt.Println("Available namespaces:")
	namespacesWithPods := []string{}

	for _, ns := range nsList.Items {
		pods, err := checker.GetPods(ctx, ns.Name, false)
		podCount := 0
		if err == nil {
			podCount = len(pods)
		}

		if podCount > 0 {
			fmt.Printf("  %d. %s (%d pods)\n", len(namespacesWithPods)+1, ns.Name, podCount)
			namespacesWithPods = append(namespacesWithPods, ns.Name)
		}
	}

	if len(namespacesWithPods) == 0 {
		fmt.Printf("No namespaces with pods found\n")
		return true
	}

	fmt.Printf("\nSwitch to namespace [1-%d], 'a' for all, or Enter to cancel: ", len(namespacesWithPods))

	var input string
	_, _ = fmt.Scanln(&input)

	if input == "" {
		return true
	}

	if input == "a" || input == "all" {
		fmt.Printf("Run: tapio check --all\n")
		return true
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(namespacesWithPods) {
		fmt.Printf("Invalid selection\n")
		return true
	}

	selectedNamespace := namespacesWithPods[choice-1]
	err = switchNamespace(selectedNamespace)
	if err != nil {
		fmt.Printf("Failed to switch namespace: %v\n", err)
		return true
	}

	fmt.Printf("Switched to namespace: %s\n", selectedNamespace)
	fmt.Printf("Re-run: tapio check\n")
	return true
}

func getCurrentNamespace() string {
	config, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return "default"
	}

	currentContext := config.CurrentContext
	if currentContext == "" {
		return "default"
	}

	context := config.Contexts[currentContext]
	if context == nil || context.Namespace == "" {
		return "default"
	}

	return context.Namespace
}

func runCheck(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	startTime := time.Now()

	// Create checker with eBPF support if requested
	var checker *simple.Checker
	var err error

	if enableEBPF {
		// Try to create enhanced checker with eBPF
		checker, err = simple.NewCheckerWithEBPF()
		if err != nil {
			// Fall back to standard checker if eBPF fails
			fmt.Printf("[WARN] eBPF not available, using standard checking: %v\n", err)
			checker, err = simple.NewChecker()
			if err != nil {
				return fmt.Errorf("failed to initialize checker: %w", err)
			}
		} else {
			fmt.Println("[OK] Enhanced checking with eBPF enabled")
		}
	} else {
		checker, err = simple.NewChecker()
		if err != nil {
			return fmt.Errorf("failed to initialize checker: %w", err)
		}
	}

	// Build check request
	request := &types.CheckRequest{
		Namespace: checkNamespace,
		All:       checkAll,
		Verbose:   verbose,
	}

	if len(args) > 0 {
		request.Resource = args[0]
	}

	// Show current context before check
	showCurrentContext(request)

	// Run the check
	result, err := checker.Check(ctx, request)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// Add intelligent correlation analysis if enabled and we have multiple problems
	if enableCorrelation && len(result.Problems) > 1 {
		if correlationResult, corrErr := addCorrelationAnalysis(ctx, checker, result.Problems); corrErr == nil {
			result.CorrelationAnalysis = correlationResult
			fmt.Printf("[OK] Correlation analysis completed in %v\n", time.Since(startTime))
		} else {
			fmt.Printf("[WARN] Correlation analysis failed: %v\n", corrErr)
		}
	}

	// Check if the only problem is "No pods found" - this means we should offer alternatives
	noPods := len(result.Problems) == 1 && result.Problems[0].Title == "No pods found"

	// If no pods found and specific resource requested, try smart suggestions
	if noPods && request.Resource != "" && !request.All {
		if handled := handleNoResourceFound(request.Resource, request.Namespace); handled {
			return nil
		}
	}

	// If no pods found in current namespace, offer interactive selection
	if noPods && request.Resource == "" && !request.All {
		if handled := offerNamespaceSelection(); handled {
			return nil
		}
	}

	// Output results
	formatter := output.NewFormatter(outputFormat)
	return formatter.Print(result)
}

// addCorrelationAnalysis runs correlation analysis on problems
func addCorrelationAnalysis(ctx context.Context, checker *simple.Checker, problems []types.Problem) (interface{}, error) {
	// Create enhanced explainer if available
	if enhancedChecker, ok := checker.GetEnhancedExplainer(); ok {
		return enhancedChecker.AnalyzeProblems(ctx, problems)
	}

	// Fallback to simple correlation analysis
	return analyzeProblemsSimple(problems), nil
}

// analyzeProblemsSimple provides basic correlation analysis
func analyzeProblemsSimple(problems []types.Problem) map[string]interface{} {
	// Basic pattern detection
	patterns := make(map[string]int)
	namespaces := make(map[string]int)

	for _, problem := range problems {
		// Count problem patterns
		if strings.Contains(strings.ToLower(problem.Title), "memory") {
			patterns["memory"]++
		}
		if strings.Contains(strings.ToLower(problem.Title), "restart") {
			patterns["restart"]++
		}
		if strings.Contains(strings.ToLower(problem.Title), "network") {
			patterns["network"]++
		}

		// Count namespace distribution
		namespaces[problem.Resource.Namespace]++
	}

	result := map[string]interface{}{
		"patterns":               patterns,
		"namespace_distribution": namespaces,
		"total_problems":         len(problems),
		"analysis_type":          "simple",
	}

	// Add insights
	insights := []string{}
	if patterns["memory"] >= 2 {
		insights = append(insights, "Memory pressure detected across multiple resources")
	}
	if patterns["restart"] >= 2 {
		insights = append(insights, "Restart pattern suggests instability")
	}
	if len(namespaces) == 1 && len(problems) >= 3 {
		insights = append(insights, "Issues concentrated in single namespace")
	}

	result["insights"] = insights
	return result
}
