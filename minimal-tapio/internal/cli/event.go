package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/client"
	"github.com/yairfalse/tapio/pkg/domain"
)

var (
	eventCmd = &cobra.Command{
		Use:   "event",
		Short: "Manage events",
	}

	submitCmd = &cobra.Command{
		Use:   "submit",
		Short: "Submit an event",
		RunE:  runSubmitEvent,
	}

	listCmd = &cobra.Command{
		Use:   "list",
		Short: "List events",
		RunE:  runListEvents,
	}

	// Flags
	eventType     string
	eventSource   string
	eventSeverity string
	eventMessage  string
	eventLimit    int
)

func init() {
	rootCmd.AddCommand(eventCmd)
	eventCmd.AddCommand(submitCmd)
	eventCmd.AddCommand(listCmd)

	// Submit flags
	submitCmd.Flags().StringVar(&eventType, "type", "system", "Event type")
	submitCmd.Flags().StringVar(&eventSource, "source", "cli", "Event source")
	submitCmd.Flags().StringVar(&eventSeverity, "severity", "info", "Event severity (info, warning, error, critical)")
	submitCmd.Flags().StringVar(&eventMessage, "message", "", "Event message")
	submitCmd.MarkFlagRequired("message")

	// List flags
	listCmd.Flags().IntVar(&eventLimit, "limit", 10, "Number of events to retrieve")
}

func runSubmitEvent(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c := client.NewClient(serverURL)

	// Create event
	event := domain.Event{
		Type:      domain.EventType(eventType),
		Source:    domain.SourceType(eventSource),
		Severity:  domain.Severity(eventSeverity),
		Message:   eventMessage,
		Timestamp: time.Now(),
	}

	// Submit event
	if err := c.SubmitEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to submit event: %w", err)
	}

	fmt.Println("Event submitted successfully")
	return nil
}

func runListEvents(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c := client.NewClient(serverURL)

	// Get events
	events, err := c.GetEvents(ctx, eventLimit)
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	// Display events
	if len(events) == 0 {
		fmt.Println("No events found")
		return nil
	}

	fmt.Printf("Found %d events:\n\n", len(events))
	for _, event := range events {
		fmt.Printf("ID:       %s\n", event.ID)
		fmt.Printf("Type:     %s\n", event.Type)
		fmt.Printf("Source:   %s\n", event.Source)
		fmt.Printf("Severity: %s\n", event.Severity)
		fmt.Printf("Time:     %s\n", event.Timestamp.Format("2006-01-02 15:04:05"))
		fmt.Printf("Message:  %s\n", event.Message)
		fmt.Println("---")
	}

	return nil
}
