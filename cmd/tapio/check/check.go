package check

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/falseyair/tapio/pkg/health"
	"github.com/falseyair/tapio/pkg/k8s"
	"github.com/falseyair/tapio/pkg/output"
)

var (
	namespace     string
	kubeconfig    string
	allNamespaces bool
)

var Cmd = &cobra.Command{
	Use:   "check",
	Short: "Check Kubernetes cluster health",
	Long: `Perform a comprehensive health check of your Kubernetes cluster.
	
Tapio will analyze your pods, deployments, and services to give you
a beautiful, human-readable health report.`,
	RunE: runCheck,
}

func init() {
	Cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to check (default: current namespace)")
	Cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: $HOME/.kube/config)")
	Cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Check all namespaces")
}

func runCheck(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := output.New()
	out.StartSpinner("Connecting to Kubernetes cluster...")

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		out.StopSpinner()
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	out.StopSpinner()
	out.Success("Connected to Kubernetes cluster")
	out.EmptyLine()

	targetNamespace := namespace
	if allNamespaces {
		targetNamespace = ""
	}

	out.StartSpinner("Analyzing cluster health...")

	checker := health.NewChecker(client)
	report, err := checker.Check(ctx, targetNamespace)
	if err != nil {
		out.StopSpinner()
		return fmt.Errorf("health check failed: %w", err)
	}

	out.StopSpinner()
	out.RenderHealthReport(report)

	return nil
}
