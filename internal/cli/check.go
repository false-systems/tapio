package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/falseyair/tapio/internal/output"
	"github.com/falseyair/tapio/pkg/simple"
	"github.com/falseyair/tapio/pkg/types"
)

var (
	checkNamespace string
	checkAll       bool
	outputFormat   string
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
	fmt.Scanln(&input)

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

	// Create checker
	checker, err := simple.NewChecker()
	if err != nil {
		return fmt.Errorf("failed to initialize checker: %w", err)
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
