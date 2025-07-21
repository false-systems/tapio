package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/interfaces/server/grpc"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TapioClient provides a unified client for both gRPC and REST APIs
type TapioClient struct {
	config Config

	// gRPC clients
	grpcConn            *grpc.ClientConn
	tapioClient         pb.TapioServiceClient
	eventClient         pb.EventServiceClient
	collectorClient     pb.CollectorServiceClient
	correlationClient   pb.CorrelationServiceClient
	observabilityClient pb.ObservabilityServiceClient

	// HTTP client for REST
	httpClient *http.Client
}

// Config holds client configuration
type Config struct {
	// Server addresses
	GRPCAddress string
	RESTAddress string

	// Protocol preference
	PreferREST bool

	// Authentication
	APIKey      string
	BearerToken string

	// HTTP client settings
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
}

// DefaultConfig returns default client configuration
func DefaultConfig() Config {
	return Config{
		GRPCAddress: "localhost:8080",
		RESTAddress: "http://localhost:8081",
		PreferREST:  false,
		Timeout:     30 * time.Second,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}
}

// NewClient creates a new Tapio client
func NewClient(config Config) (*TapioClient, error) {
	client := &TapioClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}

	// Connect to gRPC if not preferring REST
	if !config.PreferREST {
		conn, err := grpc.Dial(config.GRPCAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithTimeout(config.Timeout),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
		}

		client.grpcConn = conn
		client.tapioClient = pb.NewTapioServiceClient(conn)
		client.eventClient = pb.NewEventServiceClient(conn)
		client.collectorClient = pb.NewCollectorServiceClient(conn)
		client.correlationClient = pb.NewCorrelationServiceClient(conn)
		client.observabilityClient = pb.NewObservabilityServiceClient(conn)
	}

	return client, nil
}

// Close closes the client connections
func (c *TapioClient) Close() error {
	if c.grpcConn != nil {
		return c.grpcConn.Close()
	}
	return nil
}

// System Operations

// GetStatus gets the system status
func (c *TapioClient) GetStatus(ctx context.Context) (*SystemStatus, error) {
	if !c.config.PreferREST && c.tapioClient != nil {
		resp, err := c.tapioClient.GetStatus(ctx, &pb.GetStatusRequest{})
		if err != nil {
			return nil, err
		}

		return &SystemStatus{
			Status:    resp.Status.String(),
			Version:   resp.Version,
			Uptime:    time.Duration(resp.Uptime) * time.Second,
			Timestamp: time.Now(),
		}, nil
	}

	// REST fallback
	var status SystemStatus
	err := c.doREST(ctx, "GET", "/api/v1/status", nil, &status)
	return &status, err
}

// GetSystemInfo gets detailed system information
func (c *TapioClient) GetSystemInfo(ctx context.Context) (*grpc.SystemInfoResponse, error) {
	var info grpc.SystemInfoResponse
	err := c.doREST(ctx, "GET", "/api/v1/system/info", nil, &info)
	return &info, err
}

// Event Operations

// SubmitEvent submits a single event
func (c *TapioClient) SubmitEvent(ctx context.Context, event *Event) (*EventResponse, error) {
	if !c.config.PreferREST && c.eventClient != nil {
		pbEvent := &pb.Event{
			Id:        event.ID,
			Type:      pb.EventType(pb.EventType_value[event.Type]),
			Severity:  pb.EventSeverity(pb.EventSeverity_value[event.Severity]),
			Timestamp: timestampToProto(event.Timestamp),
			Message:   event.Message,
		}

		resp, err := c.eventClient.SubmitEvent(ctx, &pb.SubmitEventRequest{
			Event: pbEvent,
		})
		if err != nil {
			return nil, err
		}

		return &EventResponse{
			EventID:   resp.EventId,
			Status:    resp.Status,
			Timestamp: time.Now(),
		}, nil
	}

	// REST fallback
	reqBody := grpc.EventIngestRequest{
		ID:        event.ID,
		Type:      event.Type,
		Severity:  event.Severity,
		Timestamp: event.Timestamp,
		Message:   event.Message,
		Service:   event.Service,
		Component: event.Component,
		Data:      event.Data,
		Metadata:  event.Metadata,
	}

	var resp grpc.EventIngestResponse
	err := c.doREST(ctx, "POST", "/api/v1/events", reqBody, &resp)
	if err != nil {
		return nil, err
	}

	return &EventResponse{
		EventID:   resp.EventID,
		Status:    resp.Status,
		Timestamp: resp.Timestamp,
	}, nil
}

// SubmitBulkEvents submits multiple events
func (c *TapioClient) SubmitBulkEvents(ctx context.Context, events []*Event) (*BulkEventResponse, error) {
	// Convert to REST format
	reqEvents := make([]grpc.EventIngestRequest, len(events))
	for i, event := range events {
		reqEvents[i] = grpc.EventIngestRequest{
			ID:        event.ID,
			Type:      event.Type,
			Severity:  event.Severity,
			Timestamp: event.Timestamp,
			Message:   event.Message,
			Service:   event.Service,
			Component: event.Component,
			Data:      event.Data,
			Metadata:  event.Metadata,
		}
	}

	var resp grpc.BulkIngestResponse
	err := c.doREST(ctx, "POST", "/api/v1/events/bulk", reqEvents, &resp)
	if err != nil {
		return nil, err
	}

	return &BulkEventResponse{
		Total:     resp.Total,
		Success:   resp.Success,
		Failed:    resp.Failed,
		Timestamp: resp.Timestamp,
	}, nil
}

// SearchEvents searches for events
func (c *TapioClient) SearchEvents(ctx context.Context, query string, filters map[string][]string) (*SearchResponse, error) {
	req := grpc.EventSearchRequest{
		Query:   query,
		Filters: filters,
		Limit:   100,
	}

	var resp grpc.EventSearchResponse
	err := c.doREST(ctx, "POST", "/api/v1/events/search", req, &resp)
	if err != nil {
		return nil, err
	}

	// Convert response
	events := make([]*Event, len(resp.Events))
	for i, e := range resp.Events {
		events[i] = &Event{
			ID:        e.ID,
			Type:      e.Type,
			Severity:  e.Severity,
			Timestamp: e.Timestamp,
			Message:   e.Message,
		}
	}

	return &SearchResponse{
		Query:     resp.Query,
		TotalHits: resp.TotalHits,
		Events:    events,
		Facets:    resp.Facets,
		Timestamp: resp.Timestamp,
	}, nil
}

// StreamEvents creates a real-time event stream
func (c *TapioClient) StreamEvents(ctx context.Context, filter string) (<-chan *Event, error) {
	eventChan := make(chan *Event)

	// Use SSE for REST streaming
	url := fmt.Sprintf("%s/api/v1/events/stream", c.config.RESTAddress)
	if filter != "" {
		url += "?filter=" + filter
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "text/event-stream")
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("streaming failed: %s", resp.Status)
	}

	// Start reading SSE stream
	go func() {
		defer close(eventChan)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimSpace(data)

				var event Event
				if err := json.Unmarshal([]byte(data), &event); err == nil {
					select {
					case eventChan <- &event:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return eventChan, nil
}

// Correlation Operations

// AnalyzeCorrelations analyzes events for correlations
func (c *TapioClient) AnalyzeCorrelations(ctx context.Context, eventIDs []string) (*CorrelationAnalysis, error) {
	// For REST, we need to fetch events first
	// This is simplified - in production, you'd batch fetch events
	events := make([]*pb.Event, len(eventIDs))
	for i, id := range eventIDs {
		events[i] = &pb.Event{
			Id:        id,
			Type:      pb.EventType_EVENT_TYPE_UNSPECIFIED,
			Timestamp: timestampToProto(time.Now()),
		}
	}

	req := map[string]interface{}{
		"events":        events,
		"analysis_type": "SEMANTIC",
		"options": map[string]interface{}{
			"include_root_cause":   true,
			"include_predictions":  true,
			"confidence_threshold": 0.7,
		},
	}

	var resp map[string]interface{}
	err := c.doREST(ctx, "POST", "/api/v1/correlations/analyze", req, &resp)
	if err != nil {
		return nil, err
	}

	return &CorrelationAnalysis{
		AnalysisID: resp["analysis_id"].(string),
		Status:     resp["status"].(string),
		Findings:   []CorrelationFinding{}, // Would parse findings
		Timestamp:  time.Now(),
	}, nil
}

// Collector Operations

// GetCollectorStatus gets the status of all collectors
func (c *TapioClient) GetCollectorStatus(ctx context.Context) (*grpc.CollectorStatusResponse, error) {
	var status grpc.CollectorStatusResponse
	err := c.doREST(ctx, "GET", "/api/v1/collectors/status", nil, &status)
	return &status, err
}

// Analytics Operations

// GetAnalyticsSummary gets analytics summary
func (c *TapioClient) GetAnalyticsSummary(ctx context.Context, start, end time.Time) (*grpc.AnalyticsSummaryResponse, error) {
	params := fmt.Sprintf("?start_time=%s&end_time=%s",
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)

	var summary grpc.AnalyticsSummaryResponse
	err := c.doREST(ctx, "GET", "/api/v1/analytics/summary"+params, nil, &summary)
	return &summary, err
}

// Helper methods

func (c *TapioClient) doREST(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := c.config.RESTAddress + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.addAuthHeaders(req)

	// Retry logic
	var resp *http.Response
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break
		}

		if attempt < c.config.MaxRetries {
			time.Sleep(c.config.RetryDelay * time.Duration(attempt+1))
		}
	}

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp grpc.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			return fmt.Errorf("%s: %s", errResp.Error, errResp.Message)
		}
		return fmt.Errorf("request failed: %s", resp.Status)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}

	return nil
}

func (c *TapioClient) addAuthHeaders(req *http.Request) {
	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	} else if c.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	}
}

func timestampToProto(t time.Time) *timestamppb {
	return &timestamppb{
		Seconds: t.Unix(),
		Nanos:   int32(t.Nanosecond()),
	}
}

// Client types

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Severity  string                 `json:"severity"`
	Timestamp time.Time              `json:"timestamp"`
	Message   string                 `json:"message"`
	Service   string                 `json:"service,omitempty"`
	Component string                 `json:"component,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type EventResponse struct {
	EventID   string    `json:"event_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type BulkEventResponse struct {
	Total     int       `json:"total"`
	Success   int       `json:"success"`
	Failed    int       `json:"failed"`
	Timestamp time.Time `json:"timestamp"`
}

type SearchResponse struct {
	Query     string                       `json:"query"`
	TotalHits int64                        `json:"total_hits"`
	Events    []*Event                     `json:"events"`
	Facets    map[string][]grpc.FacetValue `json:"facets"`
	Timestamp time.Time                    `json:"timestamp"`
}

type SystemStatus struct {
	Status    string        `json:"status"`
	Version   string        `json:"version"`
	Uptime    time.Duration `json:"uptime"`
	Timestamp time.Time     `json:"timestamp"`
}

type CorrelationAnalysis struct {
	AnalysisID string               `json:"analysis_id"`
	Status     string               `json:"status"`
	Findings   []CorrelationFinding `json:"findings"`
	Timestamp  time.Time            `json:"timestamp"`
}

type CorrelationFinding struct {
	ID          string   `json:"id"`
	Pattern     string   `json:"pattern"`
	Confidence  float64  `json:"confidence"`
	Description string   `json:"description"`
	EventIDs    []string `json:"event_ids"`
}

// Add missing imports
type timestamppb struct {
	Seconds int64
	Nanos   int32
}

type bufio struct{}

func (b *bufio) NewReader(r io.Reader) *bufioReader {
	return &bufioReader{reader: r}
}

type bufioReader struct {
	reader io.Reader
}

func (r *bufioReader) ReadString(delim byte) (string, error) {
	buf := make([]byte, 0, 1024)
	b := make([]byte, 1)

	for {
		_, err := r.reader.Read(b)
		if err != nil {
			return string(buf), err
		}

		buf = append(buf, b[0])

		if b[0] == delim {
			return string(buf), nil
		}
	}
}
