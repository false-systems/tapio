package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/client"
	"github.com/yairfalse/tapio/pkg/di"
)

const version = "1.0.0"

var (
	engineEndpoint string
	namespace      string
	output         string
	debug          bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tapio",
		Short: "Kubernetes Intelligence Platform CLI",
		Long: `Tapio CLI - Make Kubernetes debugging accessible to everyone.

The CLI communicates with tapio-engine to provide:
- Human-readable cluster health insights
- Root cause analysis for common issues  
- Actionable remediation suggestions
- Zero-config operation (works like kubectl)`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&engineEndpoint, "engine", "localhost:9090", "Tapio engine endpoint")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Kubernetes namespace (default: current context)")
	rootCmd.PersistentFlags().StringVar(&output, "output", "human", "Output format (human, json, yaml)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")

	// Environment variable binding
	viper.SetEnvPrefix("TAPIO")
	viper.AutomaticEnv()

	// Add subcommands
	rootCmd.AddCommand(newCheckCommand())
	rootCmd.AddCommand(newVersionCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newCheckCommand() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "check [target]",
		Short: "Check health of Kubernetes resources",
		Long: `Check the health of Kubernetes resources and provide human-readable insights.

Examples:
  tapio check                    # Check current namespace
  tapio check my-app             # Check specific deployment  
  tapio check pod/my-pod-xyz     # Check specific pod
  tapio check --all              # Check entire cluster
  tapio check --namespace prod   # Check production namespace`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(args, all)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Check entire cluster")
	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Tapio CLI v%s\n", version)
			return nil
		},
	}
}

func runCheck(targets []string, all bool) error {
	// Create CLI application with DI
	app := di.NewCLIApplication()
	
	// Start application
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("failed to start CLI application: %w", err)
	}
	defer app.Stop(ctx)

	// Get engine client from DI container
	engineClient, err := di.GetTypedService[*client.EngineClient](app, "engine-client")
	if err != nil {
		return fmt.Errorf("failed to get engine client: %w", err)
	}

	// Connect to engine
	if err := engineClient.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to tapio-engine: %w", err)
	}
	defer engineClient.Close()

	// Determine target
	target := ""
	if len(targets) > 0 {
		target = targets[0]
	}

	// Create check request
	req := &client.CheckRequest{
		Target:    target,
		Namespace: namespace,
		All:       all,
		Options: map[string]string{
			"output": output,
			"debug":  fmt.Sprintf("%v", debug),
		},
	}

	// Perform check
	response, err := engineClient.Check(ctx, req)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	// Output results
	return outputCheckResults(response)
}

func outputCheckResults(response *client.CheckResponse) error {
	switch output {
	case "json":
		// JSON output would be implemented here
		fmt.Printf("{\"status\": \"%s\", \"summary\": \"%s\"}\n", response.Status, response.Summary)
	case "yaml":
		// YAML output would be implemented here  
		fmt.Printf("status: %s\nsummary: %s\n", response.Status, response.Summary)
	default:
		// Human-readable output
		if response.Status == "healthy" {
			fmt.Printf("✅ %s\n", response.Summary)
		} else {
			fmt.Printf("⚠️  Issues detected: %s\n", response.Summary)
			
			for _, issue := range response.Issues {
				fmt.Printf("\n%s: %s\n", issue.Resource, issue.Message)
				if issue.Details != "" {
					fmt.Printf("  Details: %s\n", issue.Details)
				}
				if issue.Remediation != "" {
					fmt.Printf("  Fix: %s\n", issue.Remediation)
				}
			}

			if len(response.Suggestions) > 0 {
				fmt.Printf("\nSuggested next steps:\n")
				for i, suggestion := range response.Suggestions {
					fmt.Printf("[%d] %s\n", i+1, suggestion.Title)
					if suggestion.Command != "" {
						fmt.Printf("    %s\n", suggestion.Command)
					}
				}
			}
		}
	}

	return nil
}
