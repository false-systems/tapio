package main

import (
	"context"
	"fmt"
	"time"
)

// App struct - Tapio GUI client that connects to tapio-engine
type App struct {
	ctx         context.Context
	apiClient   *APIClient
	otelBackend *OTELBackend // Connection to OTEL backend (Jaeger/Tempo)
}

// NewApp creates a new Tapio GUI application
func NewApp() *App {
	// Use HTTP endpoint for simplified demo
	apiClient := NewAPIClient("http://localhost:8080")
	otelBackend := NewOTELBackend("http://localhost:16686")

	return &App{
		apiClient:   apiClient,
		otelBackend: otelBackend,
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	fmt.Printf("🚀 Tapio GUI starting up...\n")
	fmt.Printf("Using mock data for demonstration\n")
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
	Resource  string     `json:"resource"`
	Namespace string     `json:"namespace"`
	Insights  []*Insight `json:"insights"`
	Count     int        `json:"count"`
	Timestamp time.Time  `json:"timestamp"`
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

// GetStories fetches current stories from tapio-engine
func (a *App) GetStories() []Story {
	// Return mock data for demonstration
	return a.getMockStories()
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
func (a *App) GetClusterStatus() ClusterStatus {
	// Return mock data for demonstration
	return a.getMockClusterStatus()
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
func (a *App) ApplyFix(storyID string, actionID string) bool {
	// Simulate fix application for demonstration
	fmt.Printf("✅ Applied fix - Story: %s, Action: %s\n", storyID, actionID)
	time.Sleep(500 * time.Millisecond) // Simulate processing time
	return true
}

// RefreshStories forces a refresh of stories from correlation engine
func (a *App) RefreshStories() bool {
	fmt.Println("🔄 Refreshing stories from correlation engine...")
	return true
}

// ConnectToServer attempts to connect to tapio-server
func (a *App) ConnectToServer(serverURL string) bool {
	fmt.Printf("🌐 Connecting to server: %s\n", serverURL)
	time.Sleep(200 * time.Millisecond) // Simulate connection time
	return true
}

// GetServerInfo returns information about the connected tapio-server
func (a *App) GetServerInfo() map[string]interface{} {
	return map[string]interface{}{
		"server_url":         "http://localhost:8080",
		"version":            "1.0.0",
		"connected":          true,
		"correlation_engine": "active",
		"last_ping":          time.Now(),
	}
}

// GetHealth returns current health status
func (a *App) GetHealth() *HealthResponse {
	return &HealthResponse{
		Status:    "healthy",
		Message:   "Tapio GUI connected successfully",
		Timestamp: time.Now(),
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
