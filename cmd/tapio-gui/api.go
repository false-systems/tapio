package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/interfaces/server/api"
)

// APIClient provides the GUI backend API
type APIClient struct {
	engineClient *client.EngineClient
}

// NewAPIClient creates a new API client for the GUI
func NewAPIClient(engineEndpoint string) *APIClient {
	config := client.DefaultConfig()
	config.Endpoint = engineEndpoint

	return &APIClient{
		engineClient: client.NewEngineClient(config),
	}
}

// Connect connects to the engine
func (c *APIClient) Connect(ctx context.Context) error {
	return c.engineClient.Connect(ctx)
}

// Close closes the connection
func (c *APIClient) Close() error {
	return c.engineClient.Close()
}

// GUI API methods that the frontend can call

// GetHealthStatus returns the engine health status
func (c *APIClient) GetHealthStatus(ctx context.Context) (*api.RESTHealthResponse, error) {
	health, err := c.engineClient.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}

	return &api.RESTHealthResponse{
		Status:    health.Status,
		Message:   health.Message,
		Timestamp: time.Now(),
		Details:   health.Details,
	}, nil
}

// GetClusterStatus returns cluster analysis results
func (c *APIClient) GetClusterStatus(ctx context.Context, options map[string]string) (*api.RESTClusterAnalysisResponse, error) {
	req := &client.CheckRequest{
		All:     true,
		Options: options,
	}

	response, err := c.engineClient.Check(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert client response to REST API format
	restIssues := make([]*api.RESTIssue, len(response.Issues))
	for i, issue := range response.Issues {
		restIssues[i] = &api.RESTIssue{
			Id:          fmt.Sprintf("issue-%d", i),
			Type:        issue.Type,
			Resource:    issue.Resource,
			Severity:    issue.Severity,
			Message:     issue.Message,
			Details:     issue.Details,
			Remediation: issue.Remediation,
			Timestamp:   time.Now(),
		}
	}

	restSuggestions := make([]*api.RESTSuggestion, len(response.Suggestions))
	for i, suggestion := range response.Suggestions {
		restSuggestions[i] = &api.RESTSuggestion{
			Id:          fmt.Sprintf("suggestion-%d", i),
			Title:       suggestion.Title,
			Description: suggestion.Description,
			Command:     suggestion.Command,
			Steps:       suggestion.Steps,
			Priority:    "medium",
		}
	}

	return &api.RESTClusterAnalysisResponse{
		Status:      response.Status,
		Summary:     response.Summary,
		Issues:      restIssues,
		Suggestions: restSuggestions,
		Namespaces:  []*api.RESTNamespaceStatus{}, // TODO: Implement namespace status
		Metrics: &api.RESTClusterMetrics{
			NodeCount:      5, // Mock data
			PodCount:       50,
			NamespaceCount: 10,
			ResourceUsage:  65.5,
			HealthScore:    0.85,
		},
		Timestamp: time.Now(),
	}, nil
}

// GetNamespaceStatus returns namespace analysis results
func (c *APIClient) GetNamespaceStatus(ctx context.Context, namespace string, options map[string]string) (*api.RESTNamespaceAnalysisResponse, error) {
	req := &client.CheckRequest{
		Namespace: namespace,
		Options:   options,
	}

	response, err := c.engineClient.Check(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert client response to REST API format
	restIssues := make([]*api.RESTIssue, len(response.Issues))
	for i, issue := range response.Issues {
		restIssues[i] = &api.RESTIssue{
			Id:          fmt.Sprintf("ns-issue-%d", i),
			Type:        issue.Type,
			Resource:    issue.Resource,
			Severity:    issue.Severity,
			Message:     issue.Message,
			Details:     issue.Details,
			Remediation: issue.Remediation,
			Timestamp:   time.Now(),
		}
	}

	restSuggestions := make([]*api.RESTSuggestion, len(response.Suggestions))
	for i, suggestion := range response.Suggestions {
		restSuggestions[i] = &api.RESTSuggestion{
			Id:          fmt.Sprintf("ns-suggestion-%d", i),
			Title:       suggestion.Title,
			Description: suggestion.Description,
			Command:     suggestion.Command,
			Steps:       suggestion.Steps,
			Priority:    "medium",
		}
	}

	return &api.RESTNamespaceAnalysisResponse{
		Namespace:   namespace,
		Status:      response.Status,
		Summary:     response.Summary,
		Issues:      restIssues,
		Suggestions: restSuggestions,
		Resources:   []*api.RESTResourceStatus{}, // TODO: Implement resource status
		Timestamp:   time.Now(),
	}, nil
}

// GetResourceStatus returns resource analysis results
func (c *APIClient) GetResourceStatus(ctx context.Context, resource, namespace string, options map[string]string) (*api.RESTResourceAnalysisResponse, error) {
	req := &client.CheckRequest{
		Target:    resource,
		Namespace: namespace,
		Options:   options,
	}

	response, err := c.engineClient.Check(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert client response to REST API format
	restIssues := make([]*api.RESTIssue, len(response.Issues))
	for i, issue := range response.Issues {
		restIssues[i] = &api.RESTIssue{
			Id:          fmt.Sprintf("res-issue-%d", i),
			Type:        issue.Type,
			Resource:    issue.Resource,
			Severity:    issue.Severity,
			Message:     issue.Message,
			Details:     issue.Details,
			Remediation: issue.Remediation,
			Timestamp:   time.Now(),
		}
	}

	restSuggestions := make([]*api.RESTSuggestion, len(response.Suggestions))
	for i, suggestion := range response.Suggestions {
		restSuggestions[i] = &api.RESTSuggestion{
			Id:          fmt.Sprintf("res-suggestion-%d", i),
			Title:       suggestion.Title,
			Description: suggestion.Description,
			Command:     suggestion.Command,
			Steps:       suggestion.Steps,
			Priority:    "medium",
		}
	}

	return &api.RESTResourceAnalysisResponse{
		Resource:    resource,
		Namespace:   namespace,
		Status:      response.Status,
		Summary:     response.Summary,
		Issues:      restIssues,
		Suggestions: restSuggestions,
		Details:     response.Metadata,
		Timestamp:   time.Now(),
	}, nil
}

// GetPatterns returns available patterns
func (c *APIClient) GetPatterns(ctx context.Context, category string, enabled bool) (*api.RESTGetPatternsResponse, error) {
	// Mock pattern data - in real implementation, this would call engine
	patterns := []*api.RESTPatternInfo{
		{
			Id:          "memory-leak",
			Name:        "Memory Leak Detection",
			Description: "Detects memory leaks in containers",
			Category:    "performance",
			EventTypes:  []string{"memory", "oom"},
			Enabled:     true,
		},
		{
			Id:          "cpu-spike",
			Name:        "CPU Spike Detection",
			Description: "Detects CPU usage spikes",
			Category:    "performance",
			EventTypes:  []string{"cpu"},
			Enabled:     true,
		},
		{
			Id:          "network-anomaly",
			Name:        "Network Anomaly Detection",
			Description: "Detects network anomalies",
			Category:    "network",
			EventTypes:  []string{"network"},
			Enabled:     false,
		},
	}

	// Filter by category if specified
	if category != "" {
		filteredPatterns := []*api.RESTPatternInfo{}
		for _, pattern := range patterns {
			if pattern.Category == category {
				filteredPatterns = append(filteredPatterns, pattern)
			}
		}
		patterns = filteredPatterns
	}

	// Filter by enabled status
	filteredPatterns := []*api.RESTPatternInfo{}
	for _, pattern := range patterns {
		if pattern.Enabled == enabled {
			filteredPatterns = append(filteredPatterns, pattern)
		}
	}

	return &api.RESTGetPatternsResponse{
		Patterns: filteredPatterns,
	}, nil
}

// GetMetrics returns system metrics
func (c *APIClient) GetMetrics(ctx context.Context, metricNames []string, timeRange *api.RESTTimeRange) (*api.RESTMetricsResponse, error) {
	// Mock metrics data - in real implementation, this would call engine
	metrics := []*api.RESTMetricData{
		{
			Name:      "cpu_usage",
			Type:      "gauge",
			Value:     45.5,
			Labels:    map[string]string{"node": "node-1"},
			Timestamp: time.Now(),
		},
		{
			Name:      "memory_usage",
			Type:      "gauge",
			Value:     2048.0,
			Labels:    map[string]string{"node": "node-1"},
			Timestamp: time.Now(),
		},
		{
			Name:      "events_per_second",
			Type:      "gauge",
			Value:     150.0,
			Labels:    map[string]string{"component": "engine"},
			Timestamp: time.Now(),
		},
	}

	// Filter by metric names if specified
	if len(metricNames) > 0 {
		filteredMetrics := []*api.RESTMetricData{}
		for _, metric := range metrics {
			for _, name := range metricNames {
				if metric.Name == name {
					filteredMetrics = append(filteredMetrics, metric)
					break
				}
			}
		}
		metrics = filteredMetrics
	}

	return &api.RESTMetricsResponse{
		Metrics:   metrics,
		Timestamp: time.Now(),
	}, nil
}

// GetRealtimeEvents returns real-time event stream (mock implementation)
func (c *APIClient) GetRealtimeEvents(ctx context.Context) (<-chan *api.RESTEvent, error) {
	events := make(chan *api.RESTEvent, 100)

	go func() {
		defer close(events)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		eventCount := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				eventCount++
				event := &api.RESTEvent{
					Id:        fmt.Sprintf("event-%d", eventCount),
					Type:      "system",
					Source:    "mock-collector",
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"cpu_usage":    45.5 + float64(eventCount%20),
						"memory_usage": 2048.0 + float64(eventCount*10),
					},
					Labels: map[string]string{
						"node":      "node-1",
						"component": "system",
					},
					Severity: "info",
				}

				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, nil
}

// GetDashboardData returns dashboard data
func (c *APIClient) GetDashboardData(ctx context.Context) (*DashboardData, error) {
	// Fetch various data in parallel
	healthCtx, healthCancel := context.WithTimeout(ctx, 5*time.Second)
	defer healthCancel()

	clusterCtx, clusterCancel := context.WithTimeout(ctx, 10*time.Second)
	defer clusterCancel()

	metricsCtx, metricsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer metricsCancel()

	patternsCtx, patternsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer patternsCancel()

	// Fetch data concurrently
	healthChan := make(chan *api.RESTHealthResponse, 1)
	clusterChan := make(chan *api.RESTClusterAnalysisResponse, 1)
	metricsChan := make(chan *api.RESTMetricsResponse, 1)
	patternsChan := make(chan *api.RESTGetPatternsResponse, 1)

	go func() {
		if health, err := c.GetHealthStatus(healthCtx); err == nil {
			healthChan <- health
		}
		close(healthChan)
	}()

	go func() {
		if cluster, err := c.GetClusterStatus(clusterCtx, nil); err == nil {
			clusterChan <- cluster
		}
		close(clusterChan)
	}()

	go func() {
		if metrics, err := c.GetMetrics(metricsCtx, nil, nil); err == nil {
			metricsChan <- metrics
		}
		close(metricsChan)
	}()

	go func() {
		if patterns, err := c.GetPatterns(patternsCtx, "", true); err == nil {
			patternsChan <- patterns
		}
		close(patternsChan)
	}()

	// Collect results
	dashboard := &DashboardData{
		Timestamp: time.Now(),
	}

	select {
	case health := <-healthChan:
		dashboard.Health = health
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case cluster := <-clusterChan:
		dashboard.Cluster = cluster
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case metrics := <-metricsChan:
		dashboard.Metrics = metrics
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case patterns := <-patternsChan:
		dashboard.Patterns = patterns
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return dashboard, nil
}

// DashboardData represents the main dashboard data
type DashboardData struct {
	Health    *api.RESTHealthResponse          `json:"health"`
	Cluster   *api.RESTClusterAnalysisResponse `json:"cluster"`
	Metrics   *api.RESTMetricsResponse         `json:"metrics"`
	Patterns  *api.RESTGetPatternsResponse     `json:"patterns"`
	Timestamp time.Time                        `json:"timestamp"`
}
