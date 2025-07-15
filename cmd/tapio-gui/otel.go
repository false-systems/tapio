package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OTELTrace represents a trace from OTEL backend
type OTELTrace struct {
	TraceID       string         `json:"traceId"`
	SpanCount     int            `json:"spanCount"`
	ServiceName   string         `json:"serviceName"`
	OperationName string         `json:"operationName"`
	Duration      int64          `json:"duration"` // microseconds
	StartTime     time.Time      `json:"startTime"`
	Spans         []OTELSpan     `json:"spans"`
	Tags          map[string]any `json:"tags"`
	Warnings      []string       `json:"warnings,omitempty"`
}

// OTELSpan represents a span within a trace
type OTELSpan struct {
	SpanID        string         `json:"spanId"`
	TraceID       string         `json:"traceId"`
	OperationName string         `json:"operationName"`
	ServiceName   string         `json:"serviceName"`
	StartTime     int64          `json:"startTime"` // microseconds
	Duration      int64          `json:"duration"`  // microseconds
	Tags          map[string]any `json:"tags"`
	Logs          []SpanLog      `json:"logs"`
	Process       SpanProcess    `json:"process"`
	References    []SpanRef      `json:"references,omitempty"`
	
	// Tapio-specific fields
	StoryID       string  `json:"storyId,omitempty"`
	CorrelationID string  `json:"correlationId,omitempty"`
	Severity      string  `json:"severity,omitempty"`
	Pattern       string  `json:"pattern,omitempty"`
}

// SpanLog represents a log entry in a span
type SpanLog struct {
	Timestamp int64          `json:"timestamp"`
	Fields    []LogField     `json:"fields"`
}

// LogField represents a field in a span log
type LogField struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// SpanProcess represents process information
type SpanProcess struct {
	ServiceName string         `json:"serviceName"`
	Tags        map[string]any `json:"tags"`
}

// SpanRef represents span references (parent/follows)
type SpanRef struct {
	RefType string `json:"refType"`
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// OTELBackend represents connection to OTEL backend (Jaeger/Tempo)
type OTELBackend struct {
	// Jaeger Query API endpoint
	jaegerEndpoint string
	httpClient     *http.Client
}

// NewOTELBackend creates a new OTEL backend connection
func NewOTELBackend(endpoint string) *OTELBackend {
	if endpoint == "" {
		endpoint = "http://localhost:16686" // Default Jaeger UI
	}
	
	return &OTELBackend{
		jaegerEndpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetTraces fetches recent traces related to Tapio
func (a *App) GetTraces(service string, limit int) ([]OTELTrace, error) {
	if a.otelBackend == nil {
		a.otelBackend = NewOTELBackend("")
	}
	
	// Default to tapio services
	if service == "" {
		service = "tapio"
	}
	
	// Query Jaeger for traces
	traces, err := a.otelBackend.SearchTraces(service, limit)
	if err != nil {
		// Return mock traces for development
		return a.getMockTraces(), nil
	}
	
	return traces, nil
}

// GetTraceByID fetches a specific trace by ID
func (a *App) GetTraceByID(traceID string) (*OTELTrace, error) {
	if a.otelBackend == nil {
		a.otelBackend = NewOTELBackend("")
	}
	
	trace, err := a.otelBackend.GetTrace(traceID)
	if err != nil {
		// Return mock trace for development
		mockTraces := a.getMockTraces()
		if len(mockTraces) > 0 {
			return &mockTraces[0], nil
		}
		return nil, err
	}
	
	return trace, nil
}

// GetTracesForStory fetches OTEL traces related to a story
func (a *App) GetTracesForStory(storyID string) ([]OTELTrace, error) {
	// In a real implementation, we'd query by story correlation ID
	// For now, return mock traces that match the story
	
	mockTraces := a.getMockTraces()
	relatedTraces := []OTELTrace{}
	
	for _, trace := range mockTraces {
		// Check if any span references this story
		for _, span := range trace.Spans {
			if span.StoryID == storyID {
				relatedTraces = append(relatedTraces, trace)
				break
			}
		}
	}
	
	return relatedTraces, nil
}

// SearchTraces queries Jaeger for traces
func (ob *OTELBackend) SearchTraces(service string, limit int) ([]OTELTrace, error) {
	// Jaeger Query API: /api/traces?service=SERVICE&limit=N
	url := fmt.Sprintf("%s/api/traces?service=%s&limit=%d&lookback=1h", 
		ob.jaegerEndpoint, service, limit)
	
	resp, err := ob.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query Jaeger: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jaeger returned %d: %s", resp.StatusCode, body)
	}
	
	var result struct {
		Data []json.RawMessage `json:"data"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode Jaeger response: %w", err)
	}
	
	traces := make([]OTELTrace, 0, len(result.Data))
	for _, rawTrace := range result.Data {
		var trace OTELTrace
		if err := json.Unmarshal(rawTrace, &trace); err != nil {
			continue
		}
		traces = append(traces, trace)
	}
	
	return traces, nil
}

// GetTrace fetches a specific trace by ID
func (ob *OTELBackend) GetTrace(traceID string) (*OTELTrace, error) {
	url := fmt.Sprintf("%s/api/traces/%s", ob.jaegerEndpoint, traceID)
	
	resp, err := ob.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get trace: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trace not found")
	}
	
	var result struct {
		Data []OTELTrace `json:"data"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	if len(result.Data) > 0 {
		return &result.Data[0], nil
	}
	
	return nil, fmt.Errorf("trace not found")
}

// getMockTraces returns mock OTEL traces for development
func (a *App) getMockTraces() []OTELTrace {
	now := time.Now()
	
	return []OTELTrace{
		{
			TraceID:       "1234567890abcdef",
			SpanCount:     5,
			ServiceName:   "tapio-relay",
			OperationName: "correlation.memory-pressure",
			Duration:      2500000, // 2.5 seconds
			StartTime:     now.Add(-5 * time.Minute),
			Tags: map[string]any{
				"correlation.id":   "corr-memory-pressure",
				"cluster":          "production",
				"severity":         "high",
			},
			Spans: []OTELSpan{
				{
					SpanID:        "span1",
					TraceID:       "1234567890abcdef",
					OperationName: "issue.OOMKiller",
					ServiceName:   "tapio-relay",
					StartTime:     now.Add(-5 * time.Minute).UnixMicro(),
					Duration:      2300,
					Tags: map[string]any{
						"k8s.namespace":     "production",
						"k8s.pod":          "api-service-abc123",
						"memory.limit":     "256Mi",
						"memory.used":      "256Mi",
						"pattern":          "memory_pressure",
					},
					StoryID:       "story-001",
					CorrelationID: "corr-memory-pressure",
					Severity:      "critical",
					Pattern:       "memory_pressure",
					Logs: []SpanLog{
						{
							Timestamp: now.Add(-5 * time.Minute).UnixMicro(),
							Fields: []LogField{
								{Key: "event", Value: "OOM Kill"},
								{Key: "reason", Value: "Memory limit exceeded"},
							},
						},
					},
				},
				{
					SpanID:        "span2",
					TraceID:       "1234567890abcdef",
					OperationName: "prediction.ServiceOutage",
					ServiceName:   "tapio-relay",
					StartTime:     now.Add(-4 * time.Minute).UnixMicro(),
					Duration:      1500,
					Tags: map[string]any{
						"prediction.type":        "service_outage",
						"prediction.probability": 0.78,
						"prediction.tte":         "45m",
					},
					References: []SpanRef{
						{RefType: "CHILD_OF", TraceID: "1234567890abcdef", SpanID: "span1"},
					},
				},
			},
		},
		{
			TraceID:       "fedcba0987654321",
			SpanCount:     3,
			ServiceName:   "tapio-engine",
			OperationName: "correlation.network-issue",
			Duration:      1200000, // 1.2 seconds
			StartTime:     now.Add(-2 * time.Minute),
			Tags: map[string]any{
				"correlation.id": "corr-network-issue",
				"pattern.type":   "connection_timeout",
			},
			Spans: []OTELSpan{
				{
					SpanID:        "span3",
					TraceID:       "fedcba0987654321",
					OperationName: "pattern.connection_timeouts",
					ServiceName:   "tapio-engine",
					StartTime:     now.Add(-2 * time.Minute).UnixMicro(),
					Duration:      1000,
					Tags: map[string]any{
						"service.from": "api-service",
						"service.to":   "database",
						"timeout.count": 47,
					},
					StoryID: "story-002",
				},
			},
		},
	}
}

// LinkStoryToTrace creates a link between a story and its OTEL traces
func (a *App) LinkStoryToTrace(storyID string, traceID string) error {
	// In a real implementation, this would update the correlation engine
	// to maintain story-trace relationships
	
	fmt.Printf("Linking story %s to trace %s\n", storyID, traceID)
	return nil
}

// GetTraceTimeline returns a timeline view of traces
func (a *App) GetTraceTimeline(startTime, endTime time.Time) ([]OTELTrace, error) {
	// This would query traces within a time range
	// For now, return all mock traces
	return a.getMockTraces(), nil
}