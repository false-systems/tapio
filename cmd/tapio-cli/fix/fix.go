package fix

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/collectors/k8s"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/interfaces/output"
	"k8s.io/client-go/kubernetes"
)

var (
	autoApply bool
	dryRun    bool
)

// NewCommand creates the fix command
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [resource]",
		Short: "Apply automated fixes for detected issues",
		Long: `Apply automated fixes for issues detected by Tapio's correlation engine.

The fix command retrieves actionable items from the correlation analysis and can:
- Show suggested fixes for review
- Apply fixes automatically with --auto flag
- Preview changes with --dry-run flag

Examples:
  # Show available fixes for a pod
  tapio fix my-app-pod

  # Automatically apply safe fixes
  tapio fix my-app-pod --auto

  # Preview what would be changed
  tapio fix my-app-pod --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: runFix,
	}

	cmd.Flags().BoolVar(&autoApply, "auto", false, "Automatically apply safe fixes without prompting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be changed without applying")

	return cmd
}

func runFix(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get Kubernetes client
	k8sClient, err := k8s.NewClientFromConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create fix handler
	fixer, err := cli.NewFixHandler(k8sClient)
	if err != nil {
		return fmt.Errorf("failed to create fix handler: %w", err)
	}

	// Determine what to fix
	if len(args) == 0 {
		// Fix all critical issues in current namespace
		return fixer.FixAllCritical(ctx, autoApply, dryRun)
	}

	// Fix specific resource
	resource := args[0]
	return fixer.FixResource(ctx, resource, autoApply, dryRun)
}

// FixHandler handles fix operations
type FixHandler struct {
	k8sClient  kubernetes.Interface
	corrClient *cli.CorrelationClient
	output     *output.Formatter
}

// NewFixHandler creates a new fix handler
func NewFixHandler(k8sClient kubernetes.Interface) (*FixHandler, error) {
	corrClient, _ := cli.NewCorrelationClient("")

	return &FixHandler{
		k8sClient:  k8sClient,
		corrClient: corrClient,
		output:     output.NewFormatter(),
	}, nil
}

// FixResource applies fixes for a specific resource
func (fh *FixHandler) FixResource(ctx context.Context, resource string, autoApply, dryRun bool) error {
	// Parse resource (simplified - should handle various formats)
	parts := strings.Split(resource, "/")
	resourceName := resource
	namespace := "default" // Should get from context

	fmt.Printf("🔍 Analyzing issues for %s...\n\n", resource)

	// Get actionable items from correlation server
	var items []*correlation.ActionableItem

	if fh.corrClient != nil {
		corrItems, err := fh.corrClient.GetActionableItems(ctx, resourceName, namespace)
		if err == nil {
			items = corrItems
		}
	}

	if len(items) == 0 {
		// Fallback to local analysis
		items = fh.getLocalActionableItems(ctx, resourceName, namespace)
	}

	if len(items) == 0 {
		fmt.Println("✅ No issues found that require fixing!")
		return nil
	}

	// Display available fixes
	fmt.Printf("Found %d fixable issue(s):\n\n", len(items))

	for i, item := range items {
		fmt.Printf("[%d] %s\n", i+1, item.Description)
		if item.Impact != "" {
			fmt.Printf("    Impact: %s\n", item.Impact)
		}
		if item.Risk != "" {
			fmt.Printf("    Risk: %s\n", item.Risk)
		}
		fmt.Printf("    Command: %s\n\n", item.Command)
	}

	// Apply fixes
	if dryRun {
		fmt.Println("🔍 DRY RUN - No changes will be made")
		return nil
	}

	if !autoApply {
		fmt.Print("Apply these fixes? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("❌ Fixes cancelled")
			return nil
		}
	}

	// Execute fixes
	successCount := 0
	for i, item := range items {
		fmt.Printf("\n[%d/%d] Applying: %s\n", i+1, len(items), item.Description)

		if err := fh.executeCommand(item.Command); err != nil {
			fmt.Printf("   ❌ Failed: %v\n", err)

			// Ask whether to continue
			if !autoApply && i < len(items)-1 {
				fmt.Print("   Continue with remaining fixes? [y/N]: ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					break
				}
			}
		} else {
			fmt.Printf("   ✅ Applied successfully\n")
			successCount++
		}
	}

	fmt.Printf("\n📊 Summary: %d/%d fixes applied successfully\n", successCount, len(items))

	if successCount < len(items) {
		return fmt.Errorf("some fixes failed to apply")
	}

	return nil
}

// FixAllCritical fixes all critical issues in namespace
func (fh *FixHandler) FixAllCritical(ctx context.Context, autoApply, dryRun bool) error {
	fmt.Println("🔍 Scanning for critical issues in current namespace...\n")

	// Get all critical actionable items
	// This would query correlation server for all resources
	// For now, return not implemented
	return fmt.Errorf("fix all not yet implemented - please specify a resource")
}

// getLocalActionableItems provides fallback fixes when correlation unavailable
func (fh *FixHandler) getLocalActionableItems(ctx context.Context, resourceName, namespace string) []*correlation.ActionableItem {
	var items []*correlation.ActionableItem

	// Get pod info
	pod, err := fh.k8sClient.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return items
	}

	// Check for common issues
	for _, container := range pod.Spec.Containers {
		// Check for missing resource limits
		if container.Resources.Limits == nil {
			items = append(items, &correlation.ActionableItem{
				Description: fmt.Sprintf("Add resource limits to container %s", container.Name),
				Command: fmt.Sprintf(`kubectl patch pod %s -n %s -p '{"spec":{"containers":[{"name":"%s","resources":{"limits":{"memory":"1Gi","cpu":"500m"}}}]}}'`,
					pod.Name, namespace, container.Name),
				Impact: "Prevents resource starvation and improves stability",
				Risk:   "low",
			})
		}

		// Check for missing liveness probe
		if container.LivenessProbe == nil {
			items = append(items, &correlation.ActionableItem{
				Description: fmt.Sprintf("Add liveness probe to container %s", container.Name),
				Command: fmt.Sprintf(`kubectl patch pod %s -n %s -p '{"spec":{"containers":[{"name":"%s","livenessProbe":{"httpGet":{"path":"/health","port":8080},"initialDelaySeconds":30,"periodSeconds":10}}]}}'`,
					pod.Name, namespace, container.Name),
				Impact: "Enables automatic recovery from failures",
				Risk:   "medium",
			})
		}
	}

	// Check restart count
	for _, status := range pod.Status.ContainerStatuses {
		if status.RestartCount > 5 {
			items = append(items, &correlation.ActionableItem{
				Description: fmt.Sprintf("Container %s has restarted %d times", status.Name, status.RestartCount),
				Command:     fmt.Sprintf("kubectl delete pod %s -n %s", pod.Name, namespace),
				Impact:      "Fresh start may resolve persistent issues",
				Risk:        "medium",
			})
		}
	}

	return items
}

// executeCommand runs a kubectl command
func (fh *FixHandler) executeCommand(command string) error {
	// Security: Only allow kubectl commands
	if !strings.HasPrefix(command, "kubectl ") {
		return fmt.Errorf("only kubectl commands are allowed")
	}

	// Parse command
	parts := strings.Fields(command)
	if len(parts) < 2 {
		return fmt.Errorf("invalid command")
	}

	// Execute
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
