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

// TestSyncloopCollector_Collect_Healthy tests successful syncloop health check
func TestSyncloopCollector_Collect_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/healthz/syncloop", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	collector := NewSyncloopCollector(
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
	assert.Empty(t, events) // No events when healthy
	assert.True(t, collector.IsHealthy())
}

// TestSyncloopCollector_Collect_Unhealthy tests unhealthy syncloop detection
func TestSyncloopCollector_Collect_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("syncloop is not healthy: PLEG is not healthy"))
	}))
	defer server.Close()

	collector := NewSyncloopCollector(
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
	assert.Len(t, events, 1)
	assert.False(t, collector.IsHealthy())

	event := events[0]
	assert.Equal(t, domain.EventTypeKubeletSyncloopUnhealthy, event.Type)
	assert.Equal(t, domain.EventSeverityCritical, event.Severity)
	assert.NotNil(t, event.EventData.Kubelet)
	assert.NotNil(t, event.EventData.Kubelet.SyncloopHealth)
	assert.False(t, event.EventData.Kubelet.SyncloopHealth.Healthy)
	assert.Equal(t, http.StatusInternalServerError, event.EventData.Kubelet.SyncloopHealth.StatusCode)
	assert.Contains(t, event.EventData.Kubelet.SyncloopHealth.Message, "PLEG is not healthy")
}

// TestSyncloopCollector_Collect_NetworkError tests network error handling
func TestSyncloopCollector_Collect_NetworkError(t *testing.T) {
	collector := NewSyncloopCollector(
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
	assert.Contains(t, err.Error(), "failed to fetch syncloop health")
	assert.False(t, collector.IsHealthy())
}

// TestSyncloopCollector_Collect_ServiceUnavailable tests 503 status
func TestSyncloopCollector_Collect_ServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service temporarily unavailable"))
	}))
	defer server.Close()

	collector := NewSyncloopCollector(
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
	assert.Len(t, events, 1)
	assert.False(t, collector.IsHealthy())

	event := events[0]
	assert.Equal(t, domain.EventTypeKubeletSyncloopUnhealthy, event.Type)
	assert.Equal(t, domain.EventSeverityCritical, event.Severity)
	assert.Equal(t, http.StatusServiceUnavailable, event.EventData.Kubelet.SyncloopHealth.StatusCode)
}

// TestSyncloopCollector_Collect_RecoveryAfterFailure tests health recovery
func TestSyncloopCollector_Collect_RecoveryAfterFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: unhealthy
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("unhealthy"))
		} else {
			// Second call: healthy
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}
	}))
	defer server.Close()

	collector := NewSyncloopCollector(
		"test-observer",
		strings.TrimPrefix(server.URL, "http://"),
		true,
		server.Client(),
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	ctx := context.Background()

	// First call - unhealthy
	events1, err1 := collector.Collect(ctx)
	require.NoError(t, err1)
	assert.Len(t, events1, 1)
	assert.False(t, collector.IsHealthy())

	// Second call - recovered
	events2, err2 := collector.Collect(ctx)
	require.NoError(t, err2)
	assert.Empty(t, events2) // No events when healthy
	assert.True(t, collector.IsHealthy())
}

// TestSyncloopCollector_BaseCollectorIntegration tests BaseCollector integration
func TestSyncloopCollector_BaseCollectorIntegration(t *testing.T) {
	collector := NewSyncloopCollector(
		"test-observer",
		"localhost:10250",
		false,
		&http.Client{},
		trace.NewNoopTracerProvider().Tracer("test"),
		nil,
		func(ctx context.Context) (string, string) { return "trace123", "span456" },
	)

	// Check BaseCollector fields
	assert.Equal(t, "syncloop", collector.Name())
	assert.Equal(t, "/healthz/syncloop", collector.Endpoint())
	assert.True(t, collector.IsHealthy()) // Should start healthy
}

// TestSyncloopCollector_EventMetadata tests event metadata correctness
func TestSyncloopCollector_EventMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unhealthy"))
	}))
	defer server.Close()

	collector := NewSyncloopCollector(
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
	require.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, "test-observer", event.Source)
	assert.Equal(t, "trace123", event.Metadata.TraceID)
	assert.Equal(t, "span456", event.Metadata.SpanID)
	assert.Equal(t, "test-observer", event.Metadata.Labels["observer"])
	assert.Equal(t, "1.0.0", event.Metadata.Labels["version"])
	assert.NotEmpty(t, event.EventID)
	assert.NotZero(t, event.Timestamp)
}
