package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Example CLI integration with Tapio REST API using correlation adapter

const baseURL = "http://localhost:8888"

// Event represents an event to be processed
type Event struct {
	ID        string                 `json:"id,omitempty"`
	Type      string                 `json:"type"`
	Severity  string                 `json:"severity"`
	Source    string                 `json:"source"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Entity    Entity                 `json:"entity"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Entity represents the entity associated with an event
type Entity struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "insights":
		if len(os.Args) < 4 {
			fmt.Println("Usage: cli insights <namespace> <resource>")
			os.Exit(1)
		}
		getInsights(os.Args[2], os.Args[3])

	case "predictions":
		if len(os.Args) < 4 {
			fmt.Println("Usage: cli predictions <namespace> <resource>")
			os.Exit(1)
		}
		getPredictions(os.Args[2], os.Args[3])

	case "event":
		if len(os.Args) < 3 {
			fmt.Println("Usage: cli event <event-type>")
			os.Exit(1)
		}
		sendEvent(os.Args[2])

	case "correlate":
		correlateRecentEvents()

	case "patterns":
		listPatterns()

	case "stats":
		getStats()

	case "health":
		checkHealth()

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Tapio CLI - Correlation Adapter Integration")
	fmt.Println("\nUsage:")
	fmt.Println("  cli insights <namespace> <resource>  - Get insights for a resource")
	fmt.Println("  cli predictions <namespace> <resource> - Get predictions for a resource")
	fmt.Println("  cli event <event-type>               - Send an event to be processed")
	fmt.Println("  cli correlate                        - Correlate recent events")
	fmt.Println("  cli patterns                         - List correlation patterns")
	fmt.Println("  cli stats                           - Get correlation statistics")
	fmt.Println("  cli health                          - Check API health")
}

func getInsights(namespace, resource string) {
	url := fmt.Sprintf("%s/api/v1/insights/%s/%s", baseURL, namespace, resource)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error getting insights: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	insights, _ := result["insights"].([]interface{})
	fmt.Printf("Found %d insights for %s/%s:\n\n", len(insights), namespace, resource)

	for _, insight := range insights {
		i := insight.(map[string]interface{})
		fmt.Printf("ID: %s\n", i["id"])
		fmt.Printf("Title: %s\n", i["title"])
		fmt.Printf("Severity: %s\n", i["severity"])
		fmt.Printf("Description: %s\n", i["description"])

		if pred, ok := i["prediction"].(map[string]interface{}); ok {
			fmt.Printf("Prediction: %s in %s (probability: %.2f)\n",
				pred["type"], pred["time_to_event"], pred["probability"])
		}

		fmt.Println("---")
	}
}

func getPredictions(namespace, resource string) {
	url := fmt.Sprintf("%s/api/v1/predictions/%s/%s", baseURL, namespace, resource)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error getting predictions: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	predictions, _ := result["predictions"].([]interface{})
	fmt.Printf("Found %d predictions for %s/%s:\n\n", len(predictions), namespace, resource)

	for _, prediction := range predictions {
		p := prediction.(map[string]interface{})
		fmt.Printf("Type: %s\n", p["type"])
		fmt.Printf("Time to event: %s\n", p["time_to_event"])
		fmt.Printf("Probability: %.2f\n", p["probability"])
		fmt.Printf("Confidence: %.2f\n", p["confidence"])
		fmt.Println("---")
	}
}

func sendEvent(eventType string) {
	event := Event{
		Type:      eventType,
		Severity:  "warning",
		Source:    "cli-test",
		Message:   fmt.Sprintf("Test %s event from CLI", eventType),
		Timestamp: time.Now(),
		Entity: Entity{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
		},
		Metadata: map[string]interface{}{
			"source": "cli",
			"test":   true,
		},
	}

	jsonData, _ := json.Marshal(event)

	resp, err := http.Post(
		fmt.Sprintf("%s/api/v1/events", baseURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		fmt.Printf("Error sending event: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	fmt.Printf("Event sent successfully!\n")
	fmt.Printf("Event ID: %s\n", result["event_id"])
	fmt.Printf("Status: %s\n", result["status"])
}

func correlateRecentEvents() {
	// Create sample events to correlate
	events := struct {
		Events []Event `json:"events"`
	}{
		Events: []Event{
			{
				ID:        "event-1",
				Type:      "memory_warning",
				Severity:  "warning",
				Source:    "kubelet",
				Message:   "High memory usage detected",
				Timestamp: time.Now().Add(-5 * time.Minute),
			},
			{
				ID:        "event-2",
				Type:      "pod_restart",
				Severity:  "error",
				Source:    "kubelet",
				Message:   "Pod restarted due to OOM",
				Timestamp: time.Now(),
			},
		},
	}

	jsonData, _ := json.Marshal(events)

	resp, err := http.Post(
		fmt.Sprintf("%s/api/v1/correlate", baseURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		fmt.Printf("Error correlating events: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	fmt.Printf("Correlation Result:\n")
	fmt.Printf("Correlation ID: %s\n", result["correlation_id"])
	fmt.Printf("Event Count: %.0f\n", result["event_count"])

	if correlations, ok := result["correlations"].([]interface{}); ok {
		fmt.Printf("Found %d correlations\n", len(correlations))
		for _, corr := range correlations {
			c := corr.(map[string]interface{})
			fmt.Printf("  - %s (confidence: %.2f)\n", c["description"], c["confidence"])
		}
	}
}

func listPatterns() {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/patterns", baseURL))
	if err != nil {
		fmt.Printf("Error getting patterns: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	patterns, _ := result["patterns"].([]interface{})
	fmt.Printf("Available Patterns (%d):\n\n", len(patterns))

	for _, pattern := range patterns {
		p := pattern.(map[string]interface{})
		fmt.Printf("ID: %s\n", p["id"])
		fmt.Printf("Name: %s\n", p["name"])
		fmt.Printf("Type: %s\n", p["type"])
		fmt.Printf("Description: %s\n", p["description"])
		fmt.Printf("Enabled: %v\n", p["enabled"])
		fmt.Println("---")
	}
}

func getStats() {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/stats", baseURL))
	if err != nil {
		fmt.Printf("Error getting stats: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	fmt.Println("Correlation Engine Statistics:")
	fmt.Printf("  Enabled: %v\n", result["enabled"])
	fmt.Printf("  Events Processed: %.0f\n", result["events_processed"])
	fmt.Printf("  Insights Generated: %.0f\n", result["insights_generated"])
	fmt.Printf("  Predictions Generated: %.0f\n", result["predictions_generated"])
	fmt.Printf("  Correlations Found: %.0f\n", result["correlations_found"])
	fmt.Printf("  Last Processed: %s\n", result["last_processed_at"])
}

func checkHealth() {
	resp, err := http.Get(fmt.Sprintf("%s/health", baseURL))
	if err != nil {
		fmt.Printf("Error checking health: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	fmt.Printf("API Health: %s\n", result["status"])

	// Check readiness
	resp, err = http.Get(fmt.Sprintf("%s/ready", baseURL))
	if err != nil {
		fmt.Printf("Error checking readiness: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	fmt.Printf("API Ready: %s\n", result["status"])
	fmt.Printf("Adapter Enabled: %v\n", result["adapter"])
}
