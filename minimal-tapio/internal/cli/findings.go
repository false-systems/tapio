package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/client"
)

var findingsCmd = &cobra.Command{
	Use:   "findings",
	Short: "Get correlation findings",
	RunE:  runFindings,
}

func init() {
	rootCmd.AddCommand(findingsCmd)
}

func runFindings(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c := client.NewClient(serverURL)

	// Get findings
	findings, err := c.GetFindings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get findings: %w", err)
	}

	// Display findings
	if len(findings) == 0 {
		fmt.Println("No findings found")
		return nil
	}

	fmt.Printf("Found %d findings:\n\n", len(findings))
	for _, finding := range findings {
		fmt.Printf("ID:          %s\n", finding.ID)
		fmt.Printf("Type:        %s\n", finding.Type)
		fmt.Printf("Severity:    %s\n", finding.Severity)
		fmt.Printf("Title:       %s\n", finding.Title)
		fmt.Printf("Description: %s\n", finding.Description)
		fmt.Printf("Time:        %s\n", finding.Timestamp.Format("2006-01-02 15:04:05"))

		if len(finding.Insights) > 0 {
			fmt.Println("Insights:")
			for _, insight := range finding.Insights {
				fmt.Printf("  - %s\n", insight)
			}
		}

		fmt.Println("---")
	}

	return nil
}
