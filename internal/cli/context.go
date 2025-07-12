package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/pkg/simple"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show current Kubernetes context and namespace",
	Long: `🎯 Display current Kubernetes cluster context and namespace information.

This helps you understand which cluster and namespace Tapio is currently targeting.
Understanding your current context is essential for debugging and ensures you're
working with the right resources.

The command shows:
  • Current Kubernetes cluster and context
  • Active namespace
  • Authentication user
  • Available namespaces (with --verbose)
  • Pod counts per namespace (with --verbose)`,

	Example: `  # Show current context
  tapio context
  
  # Show with detailed cluster information
  tapio context --verbose
  
  # Check context before running other commands
  tapio context && tapio check`,

	RunE: runContext,
}

var useCmd = &cobra.Command{
	Use:   "use [namespace]",
	Short: "Switch to a different namespace",
	Long: `🔄 Switch the current namespace context for Tapio commands.

This changes which namespace Tapio will target by default for subsequent commands.
The change persists across command runs by updating your kubeconfig file.

Features:
  • Interactive namespace selection (when no namespace specified)
  • Shows pod counts to help you choose the right namespace
  • Validates namespace exists before switching
  • Updates kubeconfig context permanently
  • Shows confirmation of the switch`,

	Example: `  # Switch to a specific namespace
  tapio use production
  
  # Interactive namespace selection with pod counts
  tapio use
  
  # Switch back to default namespace
  tapio use default
  
  # Check current context after switching
  tapio use production && tapio context`,

	Args: cobra.MaximumNArgs(1),

	// Validate arguments before running
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// If namespace is provided, validate it
		if len(args) > 0 {
			if err := ValidateNamespace(args[0]); err != nil {
				return err
			}
		}
		return nil
	},

	RunE: runUse,
}

func runContext(cmd *cobra.Command, args []string) error {
	// Get current kubeconfig context
	config, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return NewCLIError(
			"kubeconfig access",
			"Failed to load kubeconfig file",
			"Check if your kubeconfig exists and is readable",
		).WithExamples(
			"kubectl config view  # Check kubeconfig",
			"export KUBECONFIG=/path/to/kubeconfig  # Set custom kubeconfig",
		).WithDocs(
			"Kubeconfig docs: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/",
		)
	}

	currentContext := config.CurrentContext
	if currentContext == "" {
		return NewCLIError(
			"context configuration",
			"No current context set in kubeconfig",
			"Set a current context using kubectl",
		).WithExamples(
			"kubectl config get-contexts  # Show available contexts",
			"kubectl config use-context [context-name]  # Set context",
		)
	}

	kubeContext := config.Contexts[currentContext]
	if kubeContext == nil {
		return NewCLIError(
			"context configuration",
			fmt.Sprintf("Context '%s' not found in kubeconfig", currentContext),
			"Check available contexts and switch to a valid one",
		).WithExamples(
			"kubectl config get-contexts",
			"kubectl config use-context [valid-context]",
		)
	}

	namespace := kubeContext.Namespace
	if namespace == "" {
		namespace = "default"
	}

	cluster := kubeContext.Cluster
	user := kubeContext.AuthInfo

	fmt.Printf("🎯 Tapio Context Information\n")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("📍 Current cluster: %s\n", cluster)
	fmt.Printf("📂 Current namespace: %s\n", namespace)
	fmt.Printf("👤 Current user: %s\n", user)
	fmt.Printf("⚙️  Kubeconfig context: %s\n", currentContext)

	if verbose {
		// Show additional details about the namespace
		checker, err := simple.NewChecker()
		if err != nil {
			fmt.Printf("\n⚠️  Unable to connect to cluster: %v\n", err)
			return nil
		}

		ctx := context.Background()
		pods, err := checker.GetPods(ctx, namespace, false)
		if err == nil {
			fmt.Printf("\n📊 Namespace '%s' contains %d pods\n", namespace, len(pods))
		} else {
			fmt.Printf("\n⚠️  Unable to count pods in namespace '%s': %v\n", namespace, err)
		}

		// List available namespaces
		fmt.Printf("\n📋 Available namespaces:\n")
		if err := showAvailableNamespaces(); err != nil {
			cliErr := ErrNoNamespaceAccess()
			fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Error())
		}
	} else {
		fmt.Printf("\n💡 Use --verbose to see namespace details and available namespaces\n")
	}

	return nil
}

func runUse(cmd *cobra.Command, args []string) error {
	var targetNamespace string

	if len(args) == 0 {
		// Interactive namespace selection
		fmt.Println("🔄 Interactive namespace selection")
		ns, err := selectNamespaceInteractively()
		if err != nil {
			if err.Error() == "canceled" {
				fmt.Println("Namespace switch canceled.")
				return nil
			}
			return NewCLIError(
				"namespace selection",
				"Failed to select namespace interactively",
				"Try specifying a namespace directly or check your cluster access",
			).WithExamples(
				"tapio use production  # Switch to specific namespace",
				"kubectl get namespaces  # List available namespaces",
				"tapio context --verbose  # Show current context",
			)
		}
		targetNamespace = ns
	} else {
		targetNamespace = args[0]

		// Verify namespace exists before switching
		if err := verifyNamespaceExists(targetNamespace); err != nil {
			return err
		}
	}

	// Switch namespace by updating kubeconfig
	err := switchNamespace(targetNamespace)
	if err != nil {
		return NewCLIError(
			"namespace switch",
			fmt.Sprintf("Failed to switch to namespace '%s'", targetNamespace),
			"Check your kubeconfig permissions and try again",
		).WithExamples(
			"kubectl config set-context --current --namespace="+targetNamespace,
			"chmod 600 ~/.kube/config  # Fix kubeconfig permissions",
		)
	}

	fmt.Printf("✅ Switched to namespace: %s\n", targetNamespace)
	fmt.Printf("💡 All tapio commands will now target this namespace by default\n")

	// Show pod count in new namespace for confirmation
	if checker, err := simple.NewChecker(); err == nil {
		ctx := context.Background()
		if pods, err := checker.GetPods(ctx, targetNamespace, false); err == nil {
			fmt.Printf("📊 Namespace '%s' contains %d pods\n", targetNamespace, len(pods))
		}
	}

	return nil
}

// verifyNamespaceExists checks if a namespace exists before switching
func verifyNamespaceExists(namespace string) error {
	checker, err := simple.NewChecker()
	if err != nil {
		return ErrKubernetesConnection(err)
	}

	ctx := context.Background()
	client := checker.GetClient()

	_, err = client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return NewCLIError(
			"namespace verification",
			fmt.Sprintf("Namespace '%s' does not exist", namespace),
			"Check the namespace name and your access permissions",
		).WithExamples(
			"kubectl get namespaces  # List available namespaces",
			"tapio use  # Interactive selection",
			"tapio context --verbose  # Show available namespaces",
		)
	}

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
	fmt.Printf("📂 Current namespace: %s\n\n", currentNS)

	fmt.Println("📋 Available namespaces:")

	// Count pods in each namespace for context
	namespacesWithCounts := make([]string, 0, len(nsList.Items))
	for i, ns := range nsList.Items {
		pods, err := checker.GetPods(ctx, ns.Name, false)
		podCount := 0
		if err == nil {
			podCount = len(pods)
		}

		status := ""
		icon := "📂"
		if ns.Name == currentNS {
			status = " (current)"
			icon = "📍"
		}

		fmt.Printf("  %d. %s %s (%d pods)%s\n", i+1, icon, ns.Name, podCount, status)
		namespacesWithCounts = append(namespacesWithCounts, ns.Name)
	}

	fmt.Printf("\n🔀 Select namespace [1-%d] or press Enter to cancel: ", len(nsList.Items))

	var input string
	_, _ = fmt.Scanln(&input)

	if input == "" {
		return "", fmt.Errorf("canceled")
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(nsList.Items) {
		fmt.Printf("❌ Invalid selection '%s'. Please choose a number between 1 and %d.\n", input, len(nsList.Items))
		return "", fmt.Errorf("invalid selection")
	}

	selectedNS := namespacesWithCounts[choice-1]
	fmt.Printf("✅ Selected namespace: %s\n", selectedNS)
	return selectedNS, nil
}

func switchNamespace(namespace string) error {
	// Load current kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := loadingRules.Load()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Update the current context's namespace
	currentContext := config.CurrentContext
	if currentContext == "" {
		return fmt.Errorf("no current context set in kubeconfig")
	}

	if config.Contexts[currentContext] == nil {
		return fmt.Errorf("context '%s' not found in kubeconfig", currentContext)
	}

	// Store the old namespace for logging
	oldNamespace := config.Contexts[currentContext].Namespace
	if oldNamespace == "" {
		oldNamespace = "default"
	}

	// Update namespace
	config.Contexts[currentContext].Namespace = namespace

	// Write back to kubeconfig
	kubeconfigPath := loadingRules.GetDefaultFilename()
	if err := clientcmd.WriteToFile(*config, kubeconfigPath); err != nil {
		return fmt.Errorf("failed to update kubeconfig at %s: %w", kubeconfigPath, err)
	}

	if verbose {
		fmt.Printf("🔄 Switched from namespace '%s' to '%s'\n", oldNamespace, namespace)
		fmt.Printf("📝 Updated kubeconfig: %s\n", kubeconfigPath)
	}

	return nil
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
	_, _ = fmt.Scanln(&input)

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
	_, _ = fmt.Scanln(&input)

	input = strings.ToLower(strings.TrimSpace(input))
	return input == "" || input == "y" || input == "yes"
}

func init() {
	// Commands are added in root.go
}
