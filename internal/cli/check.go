package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

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

	// Run the check
	result, err := checker.Check(ctx, request)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// Output results
	formatter := output.NewFormatter(outputFormat)
	return formatter.Print(result)
}
