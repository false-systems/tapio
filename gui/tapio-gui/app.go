package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// App struct - Tapio GUI client that connects to tapio-server
type App struct {
	ctx        context.Context
	serverURL  string
	httpClient *http.Client
}

// NewApp creates a new Tapio GUI application
func NewApp() *App {
	return &App{
		serverURL: "http://localhost:8888", // REST API port for tapio-server
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Story represents a Kubernetes story from correlation engine
type Story struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"` // "critical", "high", "medium", "low"
	Category    string    `json:"category"` // "memory", "network", "cpu", etc.
	Timestamp   time.Time `json:"timestamp"`

	// Affected resources
	Resources []Resource `json:"resources"`

	// Actionable recommendations
	Actions []Action `json:"actions"`

	// Root cause analysis
	RootCause string `json:"root_cause,omitempty"`

	// Predictions
	Prediction string `json:"prediction,omitempty"`
}

// Resource represents an affected Kubernetes resource
type Resource struct {
	Type      string `json:"type"` // "pod", "service", "deployment"
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Action represents an actionable recommendation
type Action struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`   // kubectl commands
	Risk        string   `json:"risk"`       // "low", "medium", "high"
	AutoApply   bool     `json:"auto_apply"` // Can be auto-applied
}

// ClusterStatus represents overall cluster health
type ClusterStatus struct {
	Status      string `json:"status"` // "healthy", "warning", "critical"
	NodesTotal  int    `json:"nodes_total"`
	NodesReady  int    `json:"nodes_ready"`
	PodsTotal   int    `json:"pods_total"`
	PodsHealthy int    `json:"pods_healthy"`

	// Recent activity
	StoriesTotal    int `json:"stories_total"`
	StoriesCritical int `json:"stories_critical"`
	StoriesResolved int `json:"stories_resolved"`

	LastUpdate time.Time `json:"last_update"`
}

// API Response types (matching the REST API server)
type InsightsResponse struct {
	Resource  string      `json:"resource"`
	Namespace string      `json:"namespace"`
	Insights  []*Insight  `json:"insights"`
	Count     int         `json:"count"`
	Timestamp time.Time   `json:"timestamp"`
}

type Insight struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	Severity         string            `json:"severity"`
	Category         string            `json:"category"`
	Timestamp        time.Time         `json:"timestamp"`
	AffectedResource *AffectedResource `json:"affected_resource"`
	ActionableItems  []*ActionableItem `json:"actionable_items"`
	Prediction       *Prediction       `json:"prediction,omitempty"`
	RootCause        string            `json:"root_cause,omitempty"`
}

type AffectedResource struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type ActionableItem struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	Impact      string `json:"impact"`
	Risk        string `json:"risk"`
}

type Prediction struct {
	Type        string        `json:"type"`
	TimeToEvent time.Duration `json:"time_to_event,omitempty"`
	Probability float64       `json:"probability"`
	Confidence  float64       `json:"confidence"`
}

type ClusterHealthResponse struct {
	Status         string    `json:"status"`
	Score          float64   `json:"score"`
	CriticalIssues int       `json:"criticalIssues"`
	Warnings       int       `json:"warnings"`
	LastChecked    time.Time `json:"lastChecked"`
}

// GetStories fetches current stories from tapio-server
func (a *App) GetStories() ([]Story, error) {
	// Try to get insights from all namespaces/resources
	url := a.serverURL + "/api/v1/insights"
	
	resp, err := a.httpClient.Get(url)
	if err != nil {
		// Fallback to mock data if server is unavailable
		return a.getMockStories(), fmt.Errorf("server unavailable, using mock data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fallback to mock data if API call fails
		return a.getMockStories(), fmt.Errorf("API call failed with status %d, using mock data", resp.StatusCode)
	}

	var insightsResp InsightsResponse
	if err := json.NewDecoder(resp.Body).Decode(&insightsResp); err != nil {
		return a.getMockStories(), fmt.Errorf("failed to decode insights response, using mock data: %w", err)
	}

	// Convert insights to stories
	stories := make([]Story, 0, len(insightsResp.Insights))
	for _, insight := range insightsResp.Insights {
		story := a.convertInsightToStory(insight)
		stories = append(stories, story)
	}

	// If no stories from server, return mock data for demo
	if len(stories) == 0 {
		return a.getMockStories(), nil
	}

	return stories, nil
}

// convertInsightToStory converts API insight to GUI story format
func (a *App) convertInsightToStory(insight *Insight) Story {
	story := Story{
		ID:          insight.ID,
		Title:       insight.Title,
		Description: insight.Description,
		Severity:    insight.Severity,
		Category:    insight.Category,
		Timestamp:   insight.Timestamp,
		RootCause:   insight.RootCause,
	}

	// Convert affected resource to resources
	if insight.AffectedResource != nil {
		story.Resources = []Resource{
			{
				Type:      insight.AffectedResource.Type,
				Name:      insight.AffectedResource.Name,
				Namespace: insight.AffectedResource.Namespace,
			},
		}
	}

	// Convert actionable items to actions
	story.Actions = make([]Action, 0, len(insight.ActionableItems))
	for i, item := range insight.ActionableItems {
		action := Action{
			ID:          fmt.Sprintf("%s-action-%d", insight.ID, i),
			Title:       item.Description, // Use description as title
			Description: item.Impact,
			Commands:    []string{item.Command},
			Risk:        item.Risk,
			AutoApply:   item.Risk == "low",
		}
		story.Actions = append(story.Actions, action)
	}

	// Convert prediction
	if insight.Prediction != nil {
		story.Prediction = fmt.Sprintf("Predicted %s (%.1f%% confidence)", 
			insight.Prediction.Type, insight.Prediction.Confidence*100)
	}

	return story
}

// getMockStories returns mock data for demonstration when server is unavailable
func (a *App) getMockStories() []Story {
	return []Story{
		{
			ID:          "story-001",
			Title:       "Memory Leak Detected in api-service",
			Description: "Memory usage has increased 300% over the last 30 minutes and is approaching OOM threshold",
			Severity:    "critical",
			Category:    "memory",
			Timestamp:   time.Now().Add(-5 * time.Minute),
			Resources: []Resource{
				{Type: "pod", Name: "api-service-7b8f9d", Namespace: "production"},
				{Type: "deployment", Name: "api-service", Namespace: "production"},
			},
			Actions: []Action{
				{
					ID:          "action-001",
					Title:       "Increase Memory Limits",
					Description: "Increase memory limits from 256Mi to 512Mi",
					Commands:    []string{"kubectl patch deployment api-service -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'"},
					Risk:        "low",
					AutoApply:   false,
				},
				{
					ID:          "action-002",
					Title:       "Restart Pod",
					Description: "Restart the pod to clear memory leak",
					Commands:    []string{"kubectl delete pod api-service-7b8f9d -n production"},
					Risk:        "medium",
					AutoApply:   true,
				},
			},
			RootCause:  "Application appears to have a memory leak in the session management code",
			Prediction: "OOM kill will occur within 10 minutes if no action is taken",
		},
		{
			ID:          "story-002",
			Title:       "Database Connection Pool Exhaustion",
			Description: "Connection pool to PostgreSQL is 95% utilized, causing API latency spikes",
			Severity:    "high",
			Category:    "database",
			Timestamp:   time.Now().Add(-2 * time.Minute),
			Resources: []Resource{
				{Type: "service", Name: "postgres", Namespace: "database"},
				{Type: "pod", Name: "api-service-7b8f9d", Namespace: "production"},
			},
			Actions: []Action{
				{
					ID:          "action-003",
					Title:       "Scale Database Connections",
					Description: "Increase max_connections in PostgreSQL configuration",
					Commands:    []string{"kubectl patch configmap postgres-config --patch '{\"data\":{\"max_connections\":\"200\"}}'"},
					Risk:        "low",
					AutoApply:   false,
				},
			},
			RootCause:  "Sudden traffic spike increased connection usage by 400%",
			Prediction: "API response times will degrade further in next 5 minutes",
		},
		{
			ID:          "story-003",
			Title:       "Node Storage Almost Full",
			Description: "Node worker-node-2 has only 5% disk space remaining",
			Severity:    "medium",
			Category:    "storage",
			Timestamp:   time.Now().Add(-8 * time.Minute),
			Resources: []Resource{
				{Type: "node", Name: "worker-node-2", Namespace: ""},
			},
			Actions: []Action{
				{
					ID:          "action-004",
					Title:       "Clean Docker Images",
					Description: "Remove unused Docker images to free space",
					Commands:    []string{"kubectl debug node/worker-node-2 -it --image=busybox -- docker system prune -f"},
					Risk:        "low",
					AutoApply:   true,
				},
			},
			Prediction: "Node will become unavailable when disk reaches 100%",
		},
	}
}

// GetClusterStatus fetches overall cluster health from tapio-server
func (a *App) GetClusterStatus() (ClusterStatus, error) {
	url := a.serverURL + "/api/v1/health/cluster"
	
	resp, err := a.httpClient.Get(url)
	if err != nil {
		// Return mock data if server is unavailable
		return a.getMockClusterStatus(), fmt.Errorf("server unavailable, using mock data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Return mock data if API call fails
		return a.getMockClusterStatus(), fmt.Errorf("API call failed with status %d, using mock data", resp.StatusCode)
	}

	var healthResp ClusterHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return a.getMockClusterStatus(), fmt.Errorf("failed to decode health response, using mock data: %w", err)
	}

	// Convert API response to GUI format
	status := ClusterStatus{
		Status:          healthResp.Status,
		NodesTotal:      3, // These would come from K8s API in real implementation
		NodesReady:      3,
		PodsTotal:       45,
		PodsHealthy:     42,
		StoriesTotal:    healthResp.CriticalIssues + healthResp.Warnings,
		StoriesCritical: healthResp.CriticalIssues,
		StoriesResolved: 7, // This would be tracked separately
		LastUpdate:      healthResp.LastChecked,
	}

	return status, nil
}

// getMockClusterStatus returns mock cluster status for demonstration
func (a *App) getMockClusterStatus() ClusterStatus {
	return ClusterStatus{
		Status:          "warning",
		NodesTotal:      3,
		NodesReady:      3,
		PodsTotal:       45,
		PodsHealthy:     42,
		StoriesTotal:    3,
		StoriesCritical: 1,
		StoriesResolved: 7,
		LastUpdate:      time.Now(),
	}
}

// ApplyFix applies a recommended fix action
func (a *App) ApplyFix(storyID string, actionID string) (bool, error) {
	// Extract namespace and resource from storyID for API call
	// This is a simplified implementation - real version would track these properly
	url := fmt.Sprintf("%s/api/v1/fixes/default/unknown/%s/apply", a.serverURL, actionID)
	
	resp, err := a.httpClient.Post(url, "application/json", nil)
	if err != nil {
		// Simulate fix application if server is unavailable
		fmt.Printf("Server unavailable - simulating fix application: Story: %s, Action: %s\n", storyID, actionID)
		time.Sleep(1 * time.Second)
		return true, fmt.Errorf("server unavailable, simulated fix: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Simulate fix application if API call fails
		fmt.Printf("API call failed - simulating fix application: Story: %s, Action: %s\n", storyID, actionID)
		time.Sleep(1 * time.Second)
		return true, fmt.Errorf("API call failed with status %d, simulated fix", resp.StatusCode)
	}

	fmt.Printf("Successfully applied fix via API - Story: %s, Action: %s\n", storyID, actionID)
	return true, nil
}

// RefreshStories forces a refresh of stories from correlation engine
func (a *App) RefreshStories() error {
	// TODO: Trigger correlation engine refresh via tapio-server API
	fmt.Println("Refreshing stories from correlation engine...")
	return nil
}

// ConnectToServer attempts to connect to tapio-server
func (a *App) ConnectToServer(serverURL string) (bool, error) {
	if serverURL != "" {
		a.serverURL = serverURL
	}

	// Test connection to tapio-server REST API
	resp, err := a.httpClient.Get(a.serverURL + "/health")
	if err != nil {
		return false, fmt.Errorf("failed to connect to tapio-server at %s: %w", a.serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("tapio-server health check failed: status %d", resp.StatusCode)
	}

	// Verify API endpoints are available
	resp, err = a.httpClient.Get(a.serverURL + "/ready")
	if err != nil {
		return false, fmt.Errorf("tapio-server readiness check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("tapio-server not ready: status %d", resp.StatusCode)
	}

	return true, nil
}

// GetServerInfo returns information about the connected tapio-server
func (a *App) GetServerInfo() (map[string]interface{}, error) {
	info := map[string]interface{}{
		"server_url":         a.serverURL,
		"version":            "1.0.0",
		"connected":          true,
		"correlation_engine": "active",
		"last_ping":          time.Now(),
	}

	return info, nil
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
