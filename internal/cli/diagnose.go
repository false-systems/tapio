package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/yairfalse/tapio/pkg/diagnostics"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/k8s"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Run diagnostics on Tapio components and dependencies",
	Long: `Run comprehensive diagnostics to check the health of all Tapio components,
including Kubernetes connectivity, eBPF availability, and network access.`,
	RunE: runDiagnose,
}

var diagnoseFlags struct {
	verbose bool
	json    bool
}

func init() {
	diagnoseCmd.Flags().BoolVarP(&diagnoseFlags.verbose, "verbose", "v", false, "Show detailed diagnostic information")
	diagnoseCmd.Flags().BoolVar(&diagnoseFlags.json, "json", false, "Output in JSON format")
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("🔍 Running Tapio diagnostics...\n")

	// Create Kubernetes client with enhanced error handling
	var kubeClient kubernetes.Interface
	var kubeConfig *rest.Config

	k8sClient, err := k8s.NewClient("")
	if err != nil {
		fmt.Printf("⚠️  Warning: %v\n", err)
		fmt.Println("   Continuing with limited diagnostics...\n")
	} else {
		kubeClient = k8sClient.Clientset
		kubeConfig = k8sClient.Config
	}

	// Create eBPF monitor
	ebpfMonitor := ebpf.NewMonitor(nil)

	// Create and run health checker
	healthChecker := diagnostics.NewHealthChecker(kubeClient, kubeConfig, ebpfMonitor)
	report, err := healthChecker.RunHealthCheck(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// Output results
	if diagnoseFlags.json {
		return outputDiagnosticJSON(report)
	}

	return outputHumanReadable(report)
}

func outputHumanReadable(report *diagnostics.HealthReport) error {
	// Overall status
	statusIcon := getStatusIcon(report.OverallHealth)
	fmt.Printf("%s Overall Status: %s\n\n", statusIcon, report.OverallHealth)

	// Component health table
	fmt.Println("Component Health:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tSTATUS\tMESSAGE\tRESPONSE TIME")

	for name, health := range report.Components {
		status := "✅"
		if !health.Healthy {
			status = "❌"
		}

		responseTime := ""
		if health.ResponseTime > 0 {
			responseTime = health.ResponseTime.String()
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			name, status, health.Message, responseTime)
	}
	w.Flush()

	// Diagnostics
	if len(report.Diagnostics) > 0 {
		fmt.Println("\nDiagnostics:")
		for _, diag := range report.Diagnostics {
			fmt.Printf("  %s\n", diag)
		}
	}

	// Recommended actions
	if len(report.Actions) > 0 {
		fmt.Println("\nRecommended Actions:")
		for i, action := range report.Actions {
			fmt.Printf("  %d. %s\n", i+1, action)
		}
	}

	// Detailed component information if verbose
	if diagnoseFlags.verbose {
		fmt.Println("\nDetailed Component Information:")
		for name, health := range report.Components {
			fmt.Printf("\n%s:\n", name)

			// Details
			if len(health.Details) > 0 {
				fmt.Println("  Details:")
				for k, v := range health.Details {
					fmt.Printf("    %s: %v\n", k, v)
				}
			}

			// Recommendations
			if len(health.Recommendations) > 0 {
				fmt.Println("  Recommendations:")
				for _, rec := range health.Recommendations {
					fmt.Printf("    - %s\n", rec)
				}
			}
		}
	}

	return nil
}

func outputDiagnosticJSON(report *diagnostics.HealthReport) error {
	// Convert to JSON and output
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func getStatusIcon(status string) string {
	switch status {
	case "healthy":
		return "✅"
	case "degraded":
		return "⚠️ "
	case "unhealthy":
		return "❌"
	default:
		return "❓"
	}
}
