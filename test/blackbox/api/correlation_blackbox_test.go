package blackbox_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CorrelationBlackBoxTest tests correlation API from external perspective
type CorrelationBlackBoxTest struct {
	apiURL     string
	httpClient *http.Client
}

// CorrelationRequest represents API request for correlation
type CorrelationRequest struct {
	Events    []Event            `json:"events"`
	TimeRange TimeRange          `json:"time_range"`
	Options   CorrelationOptions `json:"options"`
}

// CorrelationResponse represents API response
type CorrelationResponse struct {
	CorrelationID string               `json:"correlation_id"`
	Patterns      []CorrelationPattern `json:"patterns"`
	Insights      []Insight            `json:"insights"`
	Score         float64              `json:"score"`
	Timestamp     time.Time            `json:"timestamp"`
}

// CorrelationPattern represents detected pattern
type CorrelationPattern struct {
	Type        string   `json:"type"`
	Confidence  float64  `json:"confidence"`
	EventIDs    []string `json:"event_ids"`
	Description string   `json:"description"`
	RootCause   string   `json:"root_cause,omitempty"`
}

// Insight represents AI-generated insight
type Insight struct {
	Type            string                 `json:"type"`
	Severity        string                 `json:"severity"`
	Message         string                 `json:"message"`
	Recommendations []string               `json:"recommendations"`
	Evidence        map[string]interface{} `json:"evidence"`
}

// TimeRange for correlation window
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// CorrelationOptions for request
type CorrelationOptions struct {
	IncludeAIInsights bool     `json:"include_ai_insights"`
	MinConfidence     float64  `json:"min_confidence"`
	MaxPatterns       int      `json:"max_patterns"`
	EventTypes        []string `json:"event_types,omitempty"`
}

func TestCorrelationAPIBlackBox(t *testing.T) {
	test := &CorrelationBlackBoxTest{
		apiURL: getEnvOrDefault("TAPIO_API_URL", "http://localhost:8080"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	t.Run("BasicCorrelation", func(t *testing.T) {
		test.testBasicCorrelation(t)
	})

	t.Run("CascadingFailureDetection", func(t *testing.T) {
		test.testCascadingFailureDetection(t)
	})

	t.Run("PerformanceDegradationCorrelation", func(t *testing.T) {
		test.testPerformanceDegradationCorrelation(t)
	})

	t.Run("SecurityIncidentCorrelation", func(t *testing.T) {
		test.testSecurityIncidentCorrelation(t)
	})

	t.Run("RealTimeStreaming", func(t *testing.T) {
		test.testRealTimeStreaming(t)
	})

	t.Run("HighVolumeCorrelation", func(t *testing.T) {
		test.testHighVolumeCorrelation(t)
	})
}

func (ct *CorrelationBlackBoxTest) testBasicCorrelation(t *testing.T) {
	// Create related events
	now := time.Now()
	events := []Event{
		{
			ID:        "evt-1",
			Type:      "pod_oom_killed",
			Source:    "k8s-collector",
			Timestamp: now,
			Data: map[string]interface{}{
				"pod_name":     "api-server-xyz",
				"namespace":    "production",
				"node":         "node-1",
				"memory_limit": "1Gi",
				"memory_used":  "1.2Gi",
			},
		},
		{
			ID:        "evt-2",
			Type:      "service_unhealthy",
			Source:    "k8s-collector",
			Timestamp: now.Add(10 * time.Second),
			Data: map[string]interface{}{
				"service_name": "api-service",
				"namespace":    "production",
				"endpoints":    2,
				"healthy":      1,
			},
		},
		{
			ID:        "evt-3",
			Type:      "http_5xx_spike",
			Source:    "prometheus-collector",
			Timestamp: now.Add(15 * time.Second),
			Data: map[string]interface{}{
				"service":     "api-service",
				"error_rate":  0.35,
				"status_code": 503,
			},
		},
	}

	// Send correlation request
	req := CorrelationRequest{
		Events: events,
		TimeRange: TimeRange{
			Start: now.Add(-1 * time.Minute),
			End:   now.Add(1 * time.Minute),
		},
		Options: CorrelationOptions{
			IncludeAIInsights: true,
			MinConfidence:     0.7,
		},
	}

	resp := ct.sendCorrelationRequest(t, req)

	// Validate response
	assert.NotEmpty(t, resp.CorrelationID)
	assert.NotEmpty(t, resp.Patterns)
	assert.Greater(t, resp.Score, 0.7)

	// Should detect OOM cascade pattern
	var cascadePattern *CorrelationPattern
	for _, p := range resp.Patterns {
		if p.Type == "cascade_failure" || p.Type == "memory_exhaustion_cascade" {
			cascadePattern = &p
			break
		}
	}

	require.NotNil(t, cascadePattern, "Should detect cascade failure pattern")
	assert.GreaterOrEqual(t, cascadePattern.Confidence, 0.8)
	assert.Equal(t, 3, len(cascadePattern.EventIDs))
	assert.Contains(t, cascadePattern.RootCause, "memory")

	// Validate insights
	if len(resp.Insights) > 0 {
		insight := resp.Insights[0]
		assert.Equal(t, "high", insight.Severity)
		assert.NotEmpty(t, insight.Recommendations)
		assert.Contains(t, insight.Message, "memory")
	}
}

func (ct *CorrelationBlackBoxTest) testCascadingFailureDetection(t *testing.T) {
	// Simulate cascading failure scenario
	baseTime := time.Now()

	// Database failure leads to service failures
	events := []Event{
		// Initial database connection failure
		{
			ID:        "db-fail-1",
			Type:      "database_connection_failed",
			Source:    "app-collector",
			Timestamp: baseTime,
			Data: map[string]interface{}{
				"service":  "user-service",
				"database": "users-db",
				"error":    "connection timeout",
			},
		},
		// Service starts failing
		{
			ID:        "svc-fail-1",
			Type:      "service_error_rate_high",
			Source:    "prometheus-collector",
			Timestamp: baseTime.Add(5 * time.Second),
			Data: map[string]interface{}{
				"service":    "user-service",
				"error_rate": 0.6,
			},
		},
		// Dependent service affected
		{
			ID:        "svc-fail-2",
			Type:      "service_error_rate_high",
			Source:    "prometheus-collector",
			Timestamp: baseTime.Add(10 * time.Second),
			Data: map[string]interface{}{
				"service":    "order-service",
				"error_rate": 0.4,
				"dependency": "user-service",
			},
		},
		// Frontend affected
		{
			ID:        "frontend-fail",
			Type:      "http_5xx_spike",
			Source:    "nginx-collector",
			Timestamp: baseTime.Add(15 * time.Second),
			Data: map[string]interface{}{
				"service":     "frontend",
				"error_rate":  0.3,
				"status_code": 502,
			},
		},
		// Customer complaints spike
		{
			ID:        "support-spike",
			Type:      "support_ticket_spike",
			Source:    "external-integration",
			Timestamp: baseTime.Add(20 * time.Second),
			Data: map[string]interface{}{
				"category": "service_unavailable",
				"rate":     15.0, // tickets per minute
			},
		},
	}

	req := CorrelationRequest{
		Events: events,
		TimeRange: TimeRange{
			Start: baseTime.Add(-30 * time.Second),
			End:   baseTime.Add(1 * time.Minute),
		},
		Options: CorrelationOptions{
			IncludeAIInsights: true,
			MinConfidence:     0.6,
		},
	}

	resp := ct.sendCorrelationRequest(t, req)

	// Should identify cascading failure
	var cascadePattern *CorrelationPattern
	for _, p := range resp.Patterns {
		if p.Type == "cascading_failure" {
			cascadePattern = &p
			break
		}
	}

	require.NotNil(t, cascadePattern, "Should detect cascading failure")
	assert.GreaterOrEqual(t, cascadePattern.Confidence, 0.85)
	assert.Equal(t, 5, len(cascadePattern.EventIDs))
	assert.Contains(t, cascadePattern.RootCause, "database")

	// Should provide actionable insights
	require.NotEmpty(t, resp.Insights)
	highSeverityInsight := false
	for _, insight := range resp.Insights {
		if insight.Severity == "critical" || insight.Severity == "high" {
			highSeverityInsight = true
			assert.NotEmpty(t, insight.Recommendations)
			// Should recommend database recovery
			recommendsDBFix := false
			for _, rec := range insight.Recommendations {
				if contains(rec, "database") {
					recommendsDBFix = true
					break
				}
			}
			assert.True(t, recommendsDBFix, "Should recommend database fix")
		}
	}
	assert.True(t, highSeverityInsight, "Should have high severity insight")
}

func (ct *CorrelationBlackBoxTest) testPerformanceDegradationCorrelation(t *testing.T) {
	// Simulate gradual performance degradation
	baseTime := time.Now()
	events := make([]Event, 0)

	// Generate gradual degradation events
	for i := 0; i < 10; i++ {
		latency := 100.0 + float64(i*50)  // Increasing latency
		cpuUsage := 0.3 + float64(i)*0.07 // Increasing CPU

		events = append(events, Event{
			ID:        fmt.Sprintf("perf-%d", i),
			Type:      "service_latency_high",
			Source:    "prometheus-collector",
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
			Data: map[string]interface{}{
				"service":        "api-service",
				"p99_latency_ms": latency,
				"p95_latency_ms": latency * 0.8,
				"p50_latency_ms": latency * 0.5,
			},
		})

		if i%2 == 0 {
			events = append(events, Event{
				ID:        fmt.Sprintf("cpu-%d", i),
				Type:      "high_cpu_usage",
				Source:    "node-exporter",
				Timestamp: baseTime.Add(time.Duration(i) * time.Minute).Add(30 * time.Second),
				Data: map[string]interface{}{
					"node":       "node-1",
					"cpu_usage":  cpuUsage,
					"containers": []string{"api-service"},
				},
			})
		}
	}

	req := CorrelationRequest{
		Events: events,
		TimeRange: TimeRange{
			Start: baseTime.Add(-1 * time.Minute),
			End:   baseTime.Add(11 * time.Minute),
		},
		Options: CorrelationOptions{
			IncludeAIInsights: true,
			MinConfidence:     0.6,
		},
	}

	resp := ct.sendCorrelationRequest(t, req)

	// Should detect performance degradation pattern
	var perfPattern *CorrelationPattern
	for _, p := range resp.Patterns {
		if p.Type == "performance_degradation" || p.Type == "gradual_degradation" {
			perfPattern = &p
			break
		}
	}

	require.NotNil(t, perfPattern, "Should detect performance degradation")
	assert.GreaterOrEqual(t, perfPattern.Confidence, 0.75)

	// Should identify trend
	assert.Contains(t, perfPattern.Description, "gradual")

	// Should provide scaling recommendations
	scalingRecommended := false
	for _, insight := range resp.Insights {
		for _, rec := range insight.Recommendations {
			if contains(rec, "scale") || contains(rec, "resources") {
				scalingRecommended = true
				break
			}
		}
	}
	assert.True(t, scalingRecommended, "Should recommend scaling")
}

func (ct *CorrelationBlackBoxTest) testSecurityIncidentCorrelation(t *testing.T) {
	// Simulate security incident
	baseTime := time.Now()
	attackerIP := "192.168.100.50"

	events := []Event{
		// Initial reconnaissance
		{
			ID:        "sec-1",
			Type:      "port_scan_detected",
			Source:    "ebpf-collector",
			Timestamp: baseTime,
			Data: map[string]interface{}{
				"source_ip":    attackerIP,
				"target_ports": []int{22, 80, 443, 3306, 5432},
				"scan_type":    "SYN",
			},
		},
		// Failed login attempts
		{
			ID:        "sec-2",
			Type:      "auth_failure_spike",
			Source:    "auth-service",
			Timestamp: baseTime.Add(2 * time.Minute),
			Data: map[string]interface{}{
				"source_ip": attackerIP,
				"attempts":  50,
				"service":   "ssh",
			},
		},
		// Successful breach
		{
			ID:        "sec-3",
			Type:      "privilege_escalation",
			Source:    "ebpf-collector",
			Timestamp: baseTime.Add(5 * time.Minute),
			Data: map[string]interface{}{
				"user":     "www-data",
				"new_user": "root",
				"process":  "/bin/bash",
				"node":     "node-2",
			},
		},
		// Data exfiltration
		{
			ID:        "sec-4",
			Type:      "unusual_network_traffic",
			Source:    "ebpf-collector",
			Timestamp: baseTime.Add(7 * time.Minute),
			Data: map[string]interface{}{
				"source_node": "node-2",
				"destination": attackerIP,
				"bytes_sent":  5368709120, // 5GB
				"protocol":    "TCP",
				"port":        443,
			},
		},
		// Malware execution
		{
			ID:        "sec-5",
			Type:      "suspicious_process",
			Source:    "ebpf-collector",
			Timestamp: baseTime.Add(10 * time.Minute),
			Data: map[string]interface{}{
				"process":   "/tmp/.hidden/cryptominer",
				"cpu_usage": 0.95,
				"node":      "node-2",
			},
		},
	}

	req := CorrelationRequest{
		Events: events,
		TimeRange: TimeRange{
			Start: baseTime.Add(-5 * time.Minute),
			End:   baseTime.Add(15 * time.Minute),
		},
		Options: CorrelationOptions{
			IncludeAIInsights: true,
			MinConfidence:     0.7,
		},
	}

	resp := ct.sendCorrelationRequest(t, req)

	// Should detect security incident
	var secPattern *CorrelationPattern
	for _, p := range resp.Patterns {
		if p.Type == "security_incident" || p.Type == "attack_chain" {
			secPattern = &p
			break
		}
	}

	require.NotNil(t, secPattern, "Should detect security incident")
	assert.GreaterOrEqual(t, secPattern.Confidence, 0.9)
	assert.Equal(t, 5, len(secPattern.EventIDs))

	// Should identify attack progression
	assert.Contains(t, secPattern.Description, "attack")

	// Should have critical severity insight
	criticalInsight := false
	for _, insight := range resp.Insights {
		if insight.Severity == "critical" {
			criticalInsight = true

			// Should recommend immediate actions
			immediateAction := false
			for _, rec := range insight.Recommendations {
				if contains(rec, "immediately") || contains(rec, "isolate") || contains(rec, "block") {
					immediateAction = true
					break
				}
			}
			assert.True(t, immediateAction, "Should recommend immediate action")
		}
	}
	assert.True(t, criticalInsight, "Should have critical severity insight")
}

func (ct *CorrelationBlackBoxTest) testRealTimeStreaming(t *testing.T) {
	// Test SSE streaming endpoint
	streamURL := fmt.Sprintf("%s/api/v1/correlations/stream", ct.apiURL)

	// Create streaming request
	req, err := http.NewRequest("GET", streamURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	// Start streaming
	resp, err := ct.httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Send some events to trigger correlations
	go func() {
		time.Sleep(1 * time.Second)

		// Send related events
		events := []Event{
			{
				ID:        "stream-1",
				Type:      "pod_created",
				Source:    "k8s-collector",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"pod_name": "test-stream-pod",
				},
			},
			{
				ID:        "stream-2",
				Type:      "pod_failed",
				Source:    "k8s-collector",
				Timestamp: time.Now().Add(5 * time.Second),
				Data: map[string]interface{}{
					"pod_name": "test-stream-pod",
					"reason":   "CrashLoopBackOff",
				},
			},
		}

		ct.sendEvents(t, events)
	}()

	// Read streaming response
	receivedEvents := 0
	timeout := time.After(10 * time.Second)
	eventChan := make(chan string)

	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buffer)
			if err != nil {
				return
			}
			if n > 0 {
				eventChan <- string(buffer[:n])
			}
		}
	}()

	for {
		select {
		case event := <-eventChan:
			if contains(event, "data:") {
				receivedEvents++
				// Validate SSE format
				assert.Contains(t, event, "data:")

				// Extract and validate correlation data
				if contains(event, "correlation_id") {
					var correlation CorrelationResponse
					// Parse SSE data
					dataStart := strings.Index(event, "data:") + 5
					dataEnd := strings.Index(event[dataStart:], "\n")
					if dataEnd > 0 {
						jsonData := event[dataStart : dataStart+dataEnd]
						err := json.Unmarshal([]byte(jsonData), &correlation)
						assert.NoError(t, err)
						assert.NotEmpty(t, correlation.CorrelationID)
					}
				}
			}
		case <-timeout:
			assert.Greater(t, receivedEvents, 0, "Should receive streaming events")
			return
		}

		if receivedEvents >= 2 {
			return
		}
	}
}

func (ct *CorrelationBlackBoxTest) testHighVolumeCorrelation(t *testing.T) {
	// Test correlation performance with high volume
	eventCount := 1000
	events := make([]Event, eventCount)
	baseTime := time.Now()

	// Generate diverse events that should correlate
	for i := 0; i < eventCount; i++ {
		eventType := ""
		switch i % 5 {
		case 0:
			eventType = "pod_created"
		case 1:
			eventType = "service_latency_high"
		case 2:
			eventType = "node_cpu_high"
		case 3:
			eventType = "network_packet_loss"
		case 4:
			eventType = "disk_io_high"
		}

		// Create clusters of related events
		clusterID := i / 10
		serviceName := fmt.Sprintf("service-%d", clusterID%10)

		events[i] = Event{
			ID:        fmt.Sprintf("high-vol-%d", i),
			Type:      eventType,
			Source:    "load-test-collector",
			Timestamp: baseTime.Add(time.Duration(i) * time.Second),
			Data: map[string]interface{}{
				"service":    serviceName,
				"cluster_id": clusterID,
				"metric":     float64(i % 100),
			},
		}
	}

	startTime := time.Now()

	req := CorrelationRequest{
		Events: events,
		TimeRange: TimeRange{
			Start: baseTime.Add(-1 * time.Minute),
			End:   baseTime.Add(time.Duration(eventCount) * time.Second),
		},
		Options: CorrelationOptions{
			IncludeAIInsights: false, // Disable for performance test
			MinConfidence:     0.6,
			MaxPatterns:       100,
		},
	}

	resp := ct.sendCorrelationRequest(t, req)
	responseTime := time.Since(startTime)

	// Performance assertions
	assert.Less(t, responseTime, 5*time.Second,
		"Should process %d events in less than 5 seconds", eventCount)

	// Should find patterns
	assert.NotEmpty(t, resp.Patterns)
	assert.LessOrEqual(t, len(resp.Patterns), 100, "Should respect max patterns limit")

	// Validate pattern quality despite high volume
	highConfidencePatterns := 0
	for _, p := range resp.Patterns {
		if p.Confidence >= 0.8 {
			highConfidencePatterns++
		}
	}
	assert.Greater(t, highConfidencePatterns, 0,
		"Should find high confidence patterns even in high volume")
}

// Helper methods

func (ct *CorrelationBlackBoxTest) sendCorrelationRequest(t *testing.T, req CorrelationRequest) CorrelationResponse {
	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v1/correlations", ct.apiURL),
		bytes.NewBuffer(body))
	require.NoError(t, err)

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := ct.httpClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var corrResp CorrelationResponse
	err = json.NewDecoder(resp.Body).Decode(&corrResp)
	require.NoError(t, err)

	return corrResp
}

func (ct *CorrelationBlackBoxTest) sendEvents(t *testing.T, events []Event) {
	for _, event := range events {
		body, err := json.Marshal(event)
		require.NoError(t, err)

		resp, err := ct.httpClient.Post(
			fmt.Sprintf("%s/api/v1/events", ct.apiURL),
			"application/json",
			bytes.NewBuffer(body))
		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
