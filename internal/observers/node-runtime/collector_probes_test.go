package noderuntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/trace"
)

// TestProbesCollector_Collect_Success tests successful probe metrics collection
func TestProbesCollector_Collect_Success(t *testing.T) {
	// Mock Prometheus metrics response
	metricsData := `# HELP prober_probe_total Total number of probe attempts
# TYPE prober_probe_total counter
prober_probe_total{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc123",probe_type="Liveness",result="successful"} 10
prober_probe_total{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc123",probe_type="Readiness",result="successful"} 15
# HELP prober_probe_duration_seconds Duration of probe execution
# TYPE prober_probe_duration_seconds histogram
prober_probe_duration_seconds{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc123",probe_type="Liveness"} 0.05
prober_probe_duration_seconds{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc123",probe_type="Readiness"} 0.03
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/metrics/probes", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metricsData))
	}))
	defer server.Close()

	collector := NewProbesCollector(
		"test-observer",
		strings.TrimPrefix(server.URL, "http://"),
		true,
		server.Client(),
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()
	events, err := collector.Collect(ctx)

	require.NoError(t, err)
	assert.NotNil(t, events)
	assert.True(t, collector.IsHealthy())
}

// TestProbesCollector_Collect_ProbeFailure tests probe failure detection
func TestProbesCollector_Collect_ProbeFailure(t *testing.T) {
	metricsData := `prober_probe_total{container="app",namespace="prod",pod="app-pod",pod_uid="xyz789",probe_type="Liveness",result="failed"} 1
prober_probe_duration_seconds{container="app",namespace="prod",pod="app-pod",pod_uid="xyz789",probe_type="Liveness"} 0.5
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metricsData))
	}))
	defer server.Close()

	collector := NewProbesCollector(
		"test-observer",
		strings.TrimPrefix(server.URL, "http://"),
		true,
		server.Client(),
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()
	events, err := collector.Collect(ctx)

	require.NoError(t, err)
	assert.NotEmpty(t, events)

	// Should have probe failure event
	foundFailure := false
	for _, event := range events {
		if event.Type == domain.EventTypeKubeletProbeFailure {
			foundFailure = true
			assert.Equal(t, domain.EventSeverityCritical, event.Severity)
			assert.NotNil(t, event.EventData.Kubelet)
			assert.NotNil(t, event.EventData.Kubelet.ProbeResult)
			assert.Equal(t, "Liveness", event.EventData.Kubelet.ProbeResult.ProbeType)
			assert.Equal(t, "failed", event.EventData.Kubelet.ProbeResult.Result)
		}
	}
	assert.True(t, foundFailure, "Expected probe failure event")
}

// TestProbesCollector_Collect_SlowProbe tests slow probe detection
func TestProbesCollector_Collect_SlowProbe(t *testing.T) {
	metricsData := `prober_probe_total{container="slow",namespace="test",pod="slow-pod",pod_uid="slow123",probe_type="Readiness",result="successful"} 1
prober_probe_duration_seconds{container="slow",namespace="test",pod="slow-pod",pod_uid="slow123",probe_type="Readiness"} 2.5
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metricsData))
	}))
	defer server.Close()

	collector := NewProbesCollector(
		"test-observer",
		strings.TrimPrefix(server.URL, "http://"),
		true,
		server.Client(),
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()
	events, err := collector.Collect(ctx)

	require.NoError(t, err)
	assert.NotEmpty(t, events)

	// Should have slow probe event
	foundSlow := false
	for _, event := range events {
		if event.Type == domain.EventTypeKubeletProbeSlow {
			foundSlow = true
			assert.Equal(t, domain.EventSeverityWarning, event.Severity)
			assert.NotNil(t, event.EventData.Kubelet)
			assert.NotNil(t, event.EventData.Kubelet.ProbeResult)
			assert.Equal(t, "Readiness", event.EventData.Kubelet.ProbeResult.ProbeType)
			assert.True(t, event.EventData.Kubelet.ProbeResult.DurationSec > 1.0)
		}
	}
	assert.True(t, foundSlow, "Expected slow probe event")
}

// TestProbesCollector_Collect_HTTPError tests HTTP error handling
func TestProbesCollector_Collect_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	collector := NewProbesCollector(
		"test-observer",
		strings.TrimPrefix(server.URL, "http://"),
		true,
		server.Client(),
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()
	events, err := collector.Collect(ctx)

	assert.Error(t, err)
	assert.Nil(t, events)
	assert.Contains(t, err.Error(), "kubelet returned status 500")
	assert.False(t, collector.IsHealthy())
}

// TestProbesCollector_Collect_NetworkError tests network error handling
func TestProbesCollector_Collect_NetworkError(t *testing.T) {
	collector := NewProbesCollector(
		"test-observer",
		"nonexistent.invalid:10250",
		false,
		&http.Client{Timeout: 1 * time.Second},
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()
	events, err := collector.Collect(ctx)

	assert.Error(t, err)
	assert.Nil(t, events)
	assert.Contains(t, err.Error(), "failed to fetch probes metrics")
	assert.False(t, collector.IsHealthy())
}

// TestProbesCollector_parsePrometheusMetrics tests Prometheus parser
func TestProbesCollector_parsePrometheusMetrics(t *testing.T) {
	collector := NewProbesCollector(
		"test",
		"localhost:10250",
		false,
		&http.Client{},
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	tests := []struct {
		name        string
		input       string
		expectCount int
		validate    func(*testing.T, []ProbeMetric)
	}{
		{
			name: "valid_metrics",
			input: `prober_probe_total{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc",probe_type="Liveness",result="successful"} 10
prober_probe_duration_seconds{container="nginx",namespace="default",pod="nginx-pod",pod_uid="abc",probe_type="Liveness"} 0.5`,
			expectCount: 1,
			validate: func(t *testing.T, metrics []ProbeMetric) {
				assert.Equal(t, "Liveness", metrics[0].ProbeType)
				assert.Equal(t, "nginx", metrics[0].Container)
				assert.Equal(t, 0.5, metrics[0].DurationSec)
			},
		},
		{
			name:        "empty_input",
			input:       "",
			expectCount: 0,
		},
		{
			name: "comments_only",
			input: `# HELP prober_probe_total
# TYPE prober_probe_total counter`,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			metrics, err := collector.parsePrometheusMetrics(reader)

			require.NoError(t, err)
			assert.Len(t, metrics, tt.expectCount)
			if tt.validate != nil && len(metrics) > 0 {
				tt.validate(t, metrics)
			}
		})
	}
}

// TestProbesCollector_extractLabels tests label extraction
func TestProbesCollector_extractLabels(t *testing.T) {
	collector := &ProbesCollector{}

	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "valid_labels",
			input: `prober_probe_total{container="nginx",namespace="default",pod="nginx-pod"}`,
			expected: map[string]string{
				"container": "nginx",
				"namespace": "default",
				"pod":       "nginx-pod",
			},
		},
		{
			name:     "no_labels",
			input:    `prober_probe_total`,
			expected: nil,
		},
		{
			name:  "single_label",
			input: `prober_probe_total{probe_type="Liveness"}`,
			expected: map[string]string{
				"probe_type": "Liveness",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := collector.extractLabels(tt.input)
			if tt.expected == nil {
				assert.Nil(t, labels)
			} else {
				assert.Equal(t, tt.expected, labels)
			}
		})
	}
}

// TestProbesCollector_BaseCollectorIntegration tests BaseCollector integration
func TestProbesCollector_BaseCollectorIntegration(t *testing.T) {
	collector := NewProbesCollector(
		"test-observer",
		"localhost:10250",
		false,
		&http.Client{},
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	// Check BaseCollector fields
	assert.Equal(t, "probes", collector.Name())
	assert.Equal(t, "/metrics/probes", collector.Endpoint())
	assert.True(t, collector.IsHealthy()) // Should start healthy
}
