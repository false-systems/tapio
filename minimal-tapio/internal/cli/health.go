package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/client"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check server health",
	RunE:  runHealth,
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

func runHealth(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c := client.NewClient(serverURL)

	// Check health
	health, err := c.Health(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// Display results
	fmt.Printf("Server Status: %s\n", health.Status)
	fmt.Printf("Timestamp: %s\n", health.Timestamp.Format("2006-01-02 15:04:05"))

	if len(health.Details) > 0 {
		fmt.Println("Details:")
		for k, v := range health.Details {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}

	return nil
}
