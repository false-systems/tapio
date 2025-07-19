package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/output"
)

// checkCmd represents the check command with OTEL as default output
var checkCmd = &cobra.Command{
	Use:   "check [target]",
	Short: "Check health and detect issues (outputs OTEL traces by default)",
	Long: `Check performs intelligent health analysis of your Kubernetes resources.

By default, outputs rich OTEL traces to your configured collector with:
- Semantic event correlation and grouping
- Predictive failure analysis 
- Human-readable explanations as span attributes
- Business impact assessment
- Actionable recommendations

Examples:
  # Check current namespace (outputs to OTEL collector)
  tapio check

  # Check specific deployment with OTEL output
  tapio check my-app

  # Check with human-readable output for debugging
  tapio check --human

  # Export to specific OTEL endpoint
  tapio check --otel-endpoint remote-collector:4317

  # Check all namespaces
  tapio check --all-namespaces

Default behavior sends OTEL traces to localhost:4317.
Configure with TAPIO_OTEL_ENDPOINT environment variable.`,
	RunE: runCheckWithOTEL,
}

var (
	// Output flags
	humanOutput  bool
	otelEndpoint string
	outputFormat string

	// Check flags
	allNamespaces bool
	namespace     string
	selector      string

	// OTEL feature flags
	includePredictions     bool
	includeRecommendations bool
	includeBusinessImpact  bool
)

func init() {
	rootCmd.AddCommand(checkCmd)

	// Output format flags
	checkCmd.Flags().BoolVar(&humanOutput, "human", false, "Output human-readable text instead of OTEL traces")
	checkCmd.Flags().StringVar(&outputFormat, "output", "otel", "Output format: otel, json, yaml (default: otel)")
	checkCmd.Flags().StringVar(&otelEndpoint, "otel-endpoint", "", "OTEL collector endpoint (default: localhost:4317)")

	// Check scope flags
	checkCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Check all namespaces")
	checkCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to check")
	checkCmd.Flags().StringVarP(&selector, "selector", "l", "", "Label selector")

	// OTEL enrichment flags
	checkCmd.Flags().BoolVar(&includePredictions, "predictions", true, "Include failure predictions in OTEL traces")
	checkCmd.Flags().BoolVar(&includeRecommendations, "recommendations", true, "Include recommendations in OTEL traces")
	checkCmd.Flags().BoolVar(&includeBusinessImpact, "business-impact", true, "Include business impact assessment")
}

func runCheckWithOTEL(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine target
	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	// Initialize health checker
	checker, err := initializeHealthChecker()
	if err != nil {
		return fmt.Errorf("failed to initialize health checker: %w", err)
	}

	// Perform health analysis
	analysis, err := checker.Analyze(ctx, target, namespace, allNamespaces)
	if err != nil {
		return fmt.Errorf("health analysis failed: %w", err)
	}

	// Output results based on format
	if humanOutput || outputFormat == "human" {
		// Human-readable output for debugging
		return outputHumanReadable(analysis)
	}

	// Default: OTEL output
	return outputOTEL(ctx, analysis)
}

func outputOTEL(ctx context.Context, analysis interface{}) error {
	// Get OTEL endpoint
	endpoint := otelEndpoint
	if endpoint == "" {
		endpoint = os.Getenv("TAPIO_OTEL_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	// Create OTEL output config
	config := &output.OTELOutputConfig{
		Endpoint:                 endpoint,
		ServiceName:              "tapio-check",
		ServiceVersion:           version,
		ServiceInstance:          fmt.Sprintf("tapio-cli-%s", os.Getenv("USER")),
		IncludeHumanExplanations: true,
		IncludePredictions:       includePredictions,
		IncludeRecommendations:   includeRecommendations,
		IncludeBusinessImpact:    includeBusinessImpact,
	}

	// Create OTEL output handler
	otelOutput, err := output.NewOTELNativeOutput(config)
	if err != nil {
		return fmt.Errorf("failed to create OTEL output: %w", err)
	}
	defer otelOutput.Close()

	// Output based on analysis type
	switch a := analysis.(type) {
	case *health.Analysis:
		if err := otelOutput.OutputHealthCheck(ctx, a); err != nil {
			return fmt.Errorf("failed to output health check: %w", err)
		}

		// Print summary for user feedback
		fmt.Printf("✓ OTEL traces exported to %s\n", endpoint)
		fmt.Printf("  Status: %s\n", a.Status)
		fmt.Printf("  Issues: %d found\n", len(a.Issues))
		if len(a.Predictions) > 0 {
			fmt.Printf("  Predictions: %d future issues predicted\n", len(a.Predictions))
		}
		fmt.Printf("\nView traces in your OTEL backend (Jaeger, Grafana, etc.)\n")

	case *correlation.CorrelationResult:
		if err := otelOutput.OutputCorrelation(ctx, a); err != nil {
			return fmt.Errorf("failed to output correlation: %w", err)
		}

		fmt.Printf("✓ Correlation analysis exported to %s\n", endpoint)
		fmt.Printf("  Patterns found: %d\n", len(a.Patterns))
		fmt.Printf("  Insights generated: %d\n", len(a.Insights))
	}

	return nil
}

func outputHumanReadable(analysis interface{}) error {
	// Use existing human formatter
	formatter := output.NewHumanFormatter()

	switch a := analysis.(type) {
	case *health.Analysis:
		fmt.Println(formatter.FormatHealthAnalysis(a))
	case *correlation.CorrelationResult:
		fmt.Println(formatter.FormatCorrelation(a))
	default:
		return fmt.Errorf("unknown analysis type: %T", analysis)
	}

	return nil
}

// Environment variable documentation
const envVarHelp = `
Environment Variables:
  TAPIO_OTEL_ENDPOINT    OTEL collector endpoint (default: localhost:4317)
  TAPIO_OTEL_INSECURE    Use insecure connection (default: true)
  TAPIO_OTEL_HEADERS     Additional headers for OTEL export (key=value,key=value)
`
