package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio-minimal/pkg/client"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get server status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	
	// Create client
	c := client.NewClient(serverURL)
	
	// Get status
	status, err := c.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}
	
	// Display status
	fmt.Println("Server Status:")
	for k, v := range status {
		fmt.Printf("  %s: %v\n", k, v)
	}
	
	return nil
}