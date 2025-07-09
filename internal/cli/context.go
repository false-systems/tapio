package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/falseyair/tapio/pkg/simple"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show current Kubernetes context and namespace",
	Long: `Display current Kubernetes cluster context and namespace information.
	
This helps you understand which cluster and namespace Tapio is currently targeting.`,
	Example: `  # Show current context
  tapio context
  
  # Show with cluster details
  tapio context --verbose`,
	RunE: runContext,
}

var useCmd = &cobra.Command{
	Use:   "use [namespace]",
	Short: "Switch to a different namespace",
	Long: `Switch the current namespace context for Tapio commands.
	
This changes which namespace Tapio will target by default for subsequent commands.`,
	Example: `  # Switch to a specific namespace
  tapio use test-workloads
  
  # Interactive namespace selection
  tapio use
  
  # Switch back to default
  tapio use default`,
	RunE: runUse,
}

func runContext(cmd *cobra.Command, args []string) error {
	// Get current kubeconfig context
	config, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	currentContext := config.CurrentContext
	if currentContext == "" {
		return fmt.Errorf("no current context set in kubeconfig")
	}

	kubeContext := config.Contexts[currentContext]
	if kubeContext == nil {
		return fmt.Errorf("context %s not found", currentContext)
	}

	namespace := kubeContext.Namespace
	if namespace == "" {
		namespace = "default"
	}

	cluster := kubeContext.Cluster
	user := kubeContext.AuthInfo

	fmt.Printf("Tapio Context Information\n\n")
	fmt.Printf("Current cluster: %s\n", cluster)
	fmt.Printf("Current namespace: %s\n", namespace)
	fmt.Printf("Current user: %s\n", user)
	fmt.Printf("Kubeconfig context: %s\n", currentContext)

	if verbose {
		// Show additional details about the namespace
		checker, err := simple.NewChecker()
		if err != nil {
			return fmt.Errorf("failed to create checker: %w", err)
		}

		ctx := context.Background()
		pods, err := checker.GetPods(ctx, namespace, false)
		if err == nil {
			fmt.Printf("\nNamespace '%s' contains %d pods\n", namespace, len(pods))
		}

		// List available namespaces
		fmt.Printf("\nAvailable namespaces:\n")
		if err := showAvailableNamespaces(); err != nil {
			fmt.Printf("  (Unable to list namespaces: %v)\n", err)
		}
	}

	return nil
}

func runUse(cmd *cobra.Command, args []string) error {
	var targetNamespace string

	if len(args) == 0 {
		// Interactive namespace selection
		ns, err := selectNamespaceInteractively()
		if err != nil {
			return err
		}
		targetNamespace = ns
	} else {
		targetNamespace = args[0]
	}

	// Switch namespace by updating kubeconfig
	err := switchNamespace(targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to switch namespace: %w", err)
	}

	fmt.Printf("Switched to namespace: %s\n", targetNamespace)
	fmt.Printf("All tapio commands will now target this namespace by default\n")

	return nil
}

func selectNamespaceInteractively() (string, error) {
	checker, err := simple.NewChecker()
	if err != nil {
		return "", fmt.Errorf("failed to create checker: %w", err)
	}

	ctx := context.Background()

	// Get all namespaces
	client := checker.GetClient()
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list namespaces: %w", err)
	}

	if len(nsList.Items) == 0 {
		return "", fmt.Errorf("no namespaces found")
	}

	// Show current namespace
	currentNS := getCurrentNamespace()
	fmt.Printf("Current namespace: %s\n\n", currentNS)

	fmt.Println("Available namespaces:")

	// Count pods in each namespace for context
	namespacesWithCounts := make([]string, 0, len(nsList.Items))
	for i, ns := range nsList.Items {
		pods, err := checker.GetPods(ctx, ns.Name, false)
		podCount := 0
		if err == nil {
			podCount = len(pods)
		}

		status := ""
		if ns.Name == currentNS {
			status = " (current)"
		}

		fmt.Printf("  %d. %s (%d pods)%s\n", i+1, ns.Name, podCount, status)
		namespacesWithCounts = append(namespacesWithCounts, ns.Name)
	}

	fmt.Printf("\nSelect namespace [1-%d] or press Enter to cancel: ", len(nsList.Items))

	var input string
	fmt.Scanln(&input)

	if input == "" {
		return "", fmt.Errorf("cancelled")
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(nsList.Items) {
		return "", fmt.Errorf("invalid selection")
	}

	return namespacesWithCounts[choice-1], nil
}

func switchNamespace(namespace string) error {
	// Load current kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := loadingRules.Load()
	if err != nil {
		return err
	}

	// Update the current context's namespace
	currentContext := config.CurrentContext
	if currentContext == "" {
		return fmt.Errorf("no current context set")
	}

	if config.Contexts[currentContext] == nil {
		return fmt.Errorf("context %s not found", currentContext)
	}

	config.Contexts[currentContext].Namespace = namespace

	// Write back to kubeconfig
	return clientcmd.WriteToFile(*config, loadingRules.GetDefaultFilename())
}

func showAvailableNamespaces() error {
	checker, err := simple.NewChecker()
	if err != nil {
		return err
	}

	ctx := context.Background()
	client := checker.GetClient()
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	currentNS := getCurrentNamespace()

	for _, ns := range nsList.Items {
		pods, err := checker.GetPods(ctx, ns.Name, false)
		podCount := 0
		if err == nil {
			podCount = len(pods)
		}

		status := ""
		if ns.Name == currentNS {
			status = " (current)"
		}

		fmt.Printf("  • %s (%d pods)%s\n", ns.Name, podCount, status)
	}

	return nil
}

// Helper function to suggest namespace when resource not found
func SuggestNamespaceForResource(resourceName string) (string, error) {
	checker, err := simple.NewChecker()
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	client := checker.GetClient()

	// Get all namespaces
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	// Search for the resource in all namespaces
	foundNamespaces := []string{}

	for _, ns := range nsList.Items {
		pods, err := checker.GetPods(ctx, ns.Name, false)
		if err != nil {
			continue
		}

		for _, pod := range pods {
			if strings.Contains(pod.Name, resourceName) {
				foundNamespaces = append(foundNamespaces, ns.Name)
				break
			}
		}
	}

	if len(foundNamespaces) == 0 {
		return "", fmt.Errorf("resource not found in any namespace")
	}

	if len(foundNamespaces) == 1 {
		return foundNamespaces[0], nil
	}

	// Multiple matches - let user choose
	fmt.Printf("Resource '%s' found in multiple namespaces:\n", resourceName)
	for i, ns := range foundNamespaces {
		fmt.Printf("  %d. %s\n", i+1, ns)
	}

	fmt.Printf("Select namespace [1-%d]: ", len(foundNamespaces))
	var input string
	fmt.Scanln(&input)

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(foundNamespaces) {
		return "", fmt.Errorf("invalid selection")
	}

	return foundNamespaces[choice-1], nil
}

// PromptForNamespaceSwitch asks user if they want to switch to suggested namespace
func PromptForNamespaceSwitch(suggestedNamespace, resourceName string) bool {
	fmt.Printf("Resource '%s' not found in current namespace.\n", resourceName)
	fmt.Printf("Found in namespace: %s\n", suggestedNamespace)
	fmt.Printf("Switch to %s? [Y/n]: ", suggestedNamespace)

	var input string
	fmt.Scanln(&input)

	input = strings.ToLower(strings.TrimSpace(input))
	return input == "" || input == "y" || input == "yes"
}

func init() {
	// Add context and use commands to root
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(useCmd)
}
