package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/client"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
)

var (
	checkNamespace    string
	checkAll          bool
	outputFormat      string
	enableCorrelation bool
	enableEBPF        bool
	serverMode        bool
	serverURL         string
)

var checkCmd = &cobra.Command{
	Use:   "check [resource]",
	Short: "Check if your Kubernetes resources are healthy",
	Long: `🔍 Check analyzes your pods, deployments, and services for potential problems.

Check uses intelligent analysis to:
  • Detect failing pods, containers, and services
  • Predict failures before they happen (OOM, crashes, etc.)
  • Correlate issues across multiple resources
  • Provide actionable recommendations
  • Show real-time cluster health status

The analysis combines Kubernetes API data with optional eBPF kernel insights
for deep visibility into your applications.`,

	Example: `  # Check current namespace
  tapio check

  # Check specific app (searches all resource types)
  tapio check my-app

  # Check specific pod with full name
  tapio check pod/my-app-7d4b9c8f-h2x9m

  # Check entire cluster (all namespaces)
  tapio check --all

  # Check with detailed output
  tapio check --verbose

  # Check with JSON output for automation
  tapio check --output json

  # Check with enhanced eBPF monitoring
  tapio check --enable-ebpf

  # Check specific namespace
  tapio check --namespace production`,

	Args: cobra.MaximumNArgs(1),

	// Validate arguments before running
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate output format
		if err := ValidateOutputFormat(outputFormat); err != nil {
			return err
		}

		// Validate namespace
		if err := ValidateNamespace(checkNamespace); err != nil {
			return err
		}

		// Validate resource format if provided
		if len(args) > 0 {
			if err := validateResourceFormat(args[0]); err != nil {
				return err
			}
		}

		// Check for conflicting flags
		if checkAll && checkNamespace != "" {
			return NewCLIError(
				"flag validation",
				"Cannot use --all and --namespace together",
				"Use either --all for all namespaces or --namespace for a specific one",
			).WithExamples(
				"tapio check --all",
				"tapio check --namespace production",
			)
		}

		return nil
	},

	RunE: runCheck,
}

func init() {
	checkCmd.Flags().StringVarP(&checkNamespace, "namespace", "n", "",
		"Target namespace (default: current namespace from kubeconfig)")
	checkCmd.Flags().BoolVar(&checkAll, "all", false,
		"Check all namespaces (requires cluster-wide permissions)")
	checkCmd.Flags().StringVarP(&outputFormat, "output", "o", "human",
		"Output format: human (default), json, yaml")
	checkCmd.Flags().BoolVar(&enableCorrelation, "correlation", true,
		"Enable intelligent correlation analysis to find related issues")
	checkCmd.Flags().BoolVar(&enableEBPF, "enable-ebpf", false,
		"Enable eBPF monitoring for kernel-level insights (requires root)")
	checkCmd.Flags().BoolVar(&serverMode, "server", false,
		"Use server mode (connect to tapio-server instead of local analysis)")
	checkCmd.Flags().StringVar(&serverURL, "server-url", "http://localhost:8080",
		"Tapio server URL for server mode")
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
		cliErr := ErrResourceNotFound(resourceName, currentNamespace)
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	// Ask if user wants to switch
	if PromptForNamespaceSwitch(suggestedNamespace, resourceName) {
		err := switchNamespace(suggestedNamespace)
		if err != nil {
			cliErr := NewCLIError(
				"namespace switch",
				fmt.Sprintf("Failed to switch to namespace '%s'", suggestedNamespace),
				"Check your kubectl configuration and permissions",
			).WithExamples(
				"kubectl config set-context --current --namespace="+suggestedNamespace,
				"tapio use "+suggestedNamespace,
			)
			fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
			return true
		}

		fmt.Printf("✅ Switched to namespace: %s\n", suggestedNamespace)
		fmt.Printf("💡 Re-run your command to check the resource\n")
		return true
	}

	return false
}

func offerNamespaceSelection() bool {
	fmt.Printf("💡 No pods found in current namespace\n\n")

	checker, err := simple.NewChecker()
	if err != nil {
		cliErr := ErrKubernetesConnection(err)
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	ctx := context.Background()
	client := checker.GetClient()
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		cliErr := ErrNoNamespaceAccess()
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	fmt.Println("📋 Available namespaces:")
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
		cliErr := NewCLIError(
			"namespace discovery",
			"No namespaces with pods found",
			"Check if you have access to namespaces with running pods",
		).WithExamples(
			"kubectl auth can-i list pods --all-namespaces",
			"kubectl get namespaces",
			"tapio check --all  # Check all accessible namespaces",
		)
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	fmt.Printf("\n🔀 Switch to namespace [1-%d], 'a' for all, or Enter to cancel: ", len(namespacesWithPods))

	var input string
	_, _ = fmt.Scanln(&input)

	if input == "" {
		fmt.Println("Cancelled.")
		return true
	}

	if input == "a" || input == "all" {
		fmt.Printf("💡 Run: tapio check --all\n")
		return true
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(namespacesWithPods) {
		cliErr := NewCLIError(
			"namespace selection",
			"Invalid selection",
			fmt.Sprintf("Choose a number between 1 and %d", len(namespacesWithPods)),
		).WithExamples(
			"1  # Select first namespace",
			"a  # Check all namespaces",
		)
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	selectedNamespace := namespacesWithPods[choice-1]
	err = switchNamespace(selectedNamespace)
	if err != nil {
		cliErr := NewCLIError(
			"namespace switch",
			fmt.Sprintf("Failed to switch to namespace '%s'", selectedNamespace),
			"Check your kubectl configuration and permissions",
		).WithExamples(
			"kubectl config set-context --current --namespace="+selectedNamespace,
			"tapio use "+selectedNamespace,
		)
		fmt.Fprintf(os.Stderr, "%s\n", cliErr.Error())
		return true
	}

	fmt.Printf("✅ Switched to namespace: %s\n", selectedNamespace)
	fmt.Printf("💡 Re-run: tapio check\n")
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

// validateResourceFormat validates the resource format
func validateResourceFormat(resource string) error {
	if resource == "" {
		return NewCLIError(
			"resource validation",
			"Resource name cannot be empty",
			"Provide a resource name or use 'kind/name' format",
		).WithExamples(
			"tapio check my-pod",
			"tapio check deployment/api-service",
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
		validKinds := []string{"pod", "deployment", "service", "configmap", "secret", "daemonset", "statefulset", "job", "cronjob"}
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
				"tapio check pod/my-pod",
				"tapio check deployment/api-service",
			)
		}
	}

	return nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	
	// Handle server mode
	if serverMode {
		return runServerCheck(ctx, args)
	}
	
	// Local mode (existing implementation)
	return runLocalCheck(ctx, args)
}

func runServerCheck(ctx context.Context, args []string) error {
	// Create REST client
	restClient := client.NewRESTClient(serverURL)
	
	// Test connection
	if err := restClient.HealthCheck(ctx); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	
	// Build check request
	request := client.RESTCheckRequest{
		Namespace: checkNamespace,
	}
	
	// If resource specified, add it
	if len(args) > 0 {
		request.Resource = args[0]
	}
	
	// Perform check
	response, err := restClient.Check(ctx, request)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}
	
	// Format output
	formatter := output.NewFormatter(outputFormat)
	if outputFormat == "human" {
		fmt.Printf("🌳 Tapio Server Check Results\n\n")
		if response.Namespace != "" {
			fmt.Printf("Namespace: %s\n", response.Namespace)
		}
		if response.Resource != "" {
			fmt.Printf("Resource: %s\n", response.Resource)
		}
		fmt.Println()
	}
	
	// Display insights
	if len(response.Insights) > 0 {
		formatter.PrintFindings(response.Insights)
	} else {
		fmt.Println("✓ No issues found")
	}
	
	return nil
}

func runLocalCheck(ctx context.Context, args []string) error {

	// Setup progress tracking
	steps := []string{
		"Initializing checker",
		"Connecting to Kubernetes",
		"Analyzing resources",
	}

	if enableCorrelation {
		steps = append(steps, "Running correlation analysis")
	}

	progress := NewStepProgress(steps).WithVerbose(verbose)
	progress.Start()

	// Create checker with eBPF support if requested
	var checker *simple.Checker
	var err error

	if enableEBPF {
		// Try to create enhanced checker with eBPF
		checker, err = simple.NewCheckerWithEBPF()
		if err != nil {
			// Provide platform-specific guidance
			if runtime.GOOS != "linux" {
				progress.Warning(fmt.Sprintf("eBPF is only supported on Linux (current platform: %s)", runtime.GOOS))
			} else {
				progress.Warning("eBPF not available. Possible reasons:")
				progress.Warning("  • Not running as root or with CAP_BPF capability")
				progress.Warning("  • Kernel version too old (requires 4.18+)")
				progress.Warning("  • eBPF support not compiled in (use 'make build-ebpf')")
			}
			progress.Info("Falling back to standard Kubernetes API checks")

			checker, err = simple.NewChecker()
			if err != nil {
				progress.Error(err)
				return ErrKubernetesConnection(err)
			}
		} else {
			if verbose {
				fmt.Println("✨ Enhanced checking with eBPF enabled")
				fmt.Println("   • Kernel-level memory tracking active")
				fmt.Println("   • OOM prediction available")
				fmt.Println("   • Process-level insights enabled")
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

	progress.NextStep() // Move to "Analyzing resources"

	// Run the check
	result, err := checker.Check(ctx, request)
	if err != nil {
		progress.Error(err)
		return NewCLIError(
			"health check",
			"Failed to analyze Kubernetes resources",
			"Check if your cluster is accessible and try again",
		).WithExamples(
			"kubectl get pods  # Test basic connectivity",
			"tapio check --verbose  # Get detailed output",
			"tapio context  # Check current context",
		)
	}

	// Add intelligent correlation analysis if enabled and we have multiple problems
	if enableCorrelation && len(result.Problems) > 1 {
		progress.NextStep() // Move to "Running correlation analysis"

		correlationResult, corrErr := addCorrelationAnalysis(ctx, checker, result)
		if corrErr == nil {
			result.CorrelationAnalysis = correlationResult
			if verbose {
				fmt.Printf("✅ Correlation analysis completed\n")
			}
		} else {
			progress.Warning(fmt.Sprintf("Correlation analysis failed: %v", corrErr))
		}
	}

	// Complete progress tracking
	progress.Finish("Health check completed")

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
	if err := formatter.Print(result); err != nil {
		return NewCLIError(
			"output formatting",
			"Failed to display results",
			"Try using a different output format",
		).WithExamples(
			"tapio check --output json",
			"tapio check --output yaml",
		)
	}

	return nil
}

// addCorrelationAnalysis runs correlation analysis on problems
func addCorrelationAnalysis(ctx context.Context, checker *simple.Checker, result *types.CheckResult) (interface{}, error) {
	// Create correlation service
	correlationService, err := correlation.NewService()
	if err != nil {
		// Fallback to simple analysis if correlation service fails
		return analyzeProblemsSimple(result.Problems), nil
	}

	// Start the service
	if err := correlationService.Start(ctx); err != nil {
		return analyzeProblemsSimple(result.Problems), nil
	}
	defer correlationService.Stop()

	// Analyze the check result
	correlationResult, err := correlationService.AnalyzeCheckResult(ctx, result)
	if err != nil {
		return analyzeProblemsSimple(result.Problems), nil
	}

	// Build enhanced correlation analysis
	analysis := map[string]interface{}{
		"analysis_type": "intelligent",
		"insights":      correlationResult.GetMostCriticalInsights(5),
		"patterns":      correlationResult.Patterns,
		"timeline":      correlationResult.Timeline,
		"statistics":    correlationResult.Statistics,
	}

	// Add actionable recommendations
	recommendations := correlationResult.GetActionableRecommendations()
	if len(recommendations) > 0 {
		analysis["recommendations"] = recommendations
	}

	// Add critical pattern warnings
	if correlationResult.HasCriticalPatterns() {
		analysis["critical_patterns_detected"] = true
	}

	return analysis, nil
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
