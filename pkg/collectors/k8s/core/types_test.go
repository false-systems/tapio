package core

import (
	"errors"
	"testing"
	"time"
)

func TestCollectorError(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name        string
		err         CollectorError
		wantMessage string
		wantCause   error
	}{
		{
			name: "error with cause",
			err: NewCollectorError(
				ErrorTypeConnection,
				"failed to connect",
				baseErr,
			),
			wantMessage: "connection error: failed to connect (caused by: base error)",
			wantCause:   baseErr,
		},
		{
			name: "error without cause",
			err: NewCollectorError(
				ErrorTypeAuthentication,
				"invalid credentials",
				nil,
			),
			wantMessage: "authentication error: invalid credentials",
			wantCause:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMessage {
				t.Errorf("Error() = %v, want %v", got, tt.wantMessage)
			}

			if got := tt.err.Unwrap(); got != tt.wantCause {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantCause)
			}

			if tt.err.Type == "" {
				t.Error("Expected error type to be set")
			}

			if tt.err.Timestamp.IsZero() {
				t.Error("Expected timestamp to be set")
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		checks  func(t *testing.T, c *Config)
	}{
		{
			name: "valid config",
			config: Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 100,
				ResyncPeriod:    5 * time.Minute,
				WatchPods:       true,
			},
			wantErr: false,
		},
		{
			name: "zero buffer size gets default",
			config: Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 0,
			},
			wantErr: false,
			checks: func(t *testing.T, c *Config) {
				if c.EventBufferSize != 1000 {
					t.Errorf("Expected default buffer size 1000, got %d", c.EventBufferSize)
				}
			},
		},
		{
			name: "zero resync period gets default",
			config: Config{
				Name:         "test",
				Enabled:      true,
				ResyncPeriod: 0,
			},
			wantErr: false,
			checks: func(t *testing.T, c *Config) {
				if c.ResyncPeriod != 30*time.Minute {
					t.Errorf("Expected default resync period 30m, got %v", c.ResyncPeriod)
				}
			},
		},
		{
			name: "no resources watched gets defaults",
			config: Config{
				Name:    "test",
				Enabled: true,
			},
			wantErr: false,
			checks: func(t *testing.T, c *Config) {
				if !c.WatchPods {
					t.Error("Expected WatchPods to be true by default")
				}
				if !c.WatchEvents {
					t.Error("Expected WatchEvents to be true by default")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying test data
			config := tt.config
			err := config.Validate()

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.checks != nil {
				tt.checks(t, &config)
			}
		})
	}
}

func TestHealthStatus(t *testing.T) {
	// Just verify constants are defined
	statuses := []HealthStatus{
		HealthStatusHealthy,
		HealthStatusDegraded,
		HealthStatusUnhealthy,
		HealthStatusUnknown,
	}

	for _, status := range statuses {
		if status == "" {
			t.Error("Health status constant should not be empty")
		}
	}
}

func TestEventType(t *testing.T) {
	// Verify event type constants
	types := []EventType{
		EventTypeAdded,
		EventTypeModified,
		EventTypeDeleted,
		EventTypeError,
	}

	for _, et := range types {
		if et == "" {
			t.Error("Event type constant should not be empty")
		}
	}
}

func TestResourceFilter(t *testing.T) {
	filter := ResourceFilter{
		Namespaces:         []string{"default", "kube-system"},
		ExcludeNamespaces:  []string{"kube-public"},
		Labels:             map[string]string{"app": "test"},
		LabelSelector:      "app=test,env=prod",
		FieldSelector:      "status.phase=Running",
		Names:              []string{"pod1", "pod2"},
		NamePrefix:         "test-",
		NameSuffix:         "-prod",
		EventTypes:         []string{"Warning"},
		EventReasons:       []string{"BackOff", "Failed"},
		MaxEventsPerSecond: 100,
	}

	// Just verify the struct can be created and fields are accessible
	if len(filter.Namespaces) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(filter.Namespaces))
	}

	if filter.MaxEventsPerSecond != 100 {
		t.Errorf("Expected max events per second 100, got %d", filter.MaxEventsPerSecond)
	}
}

func TestWatchOptions(t *testing.T) {
	opts := WatchOptions{
		ResourceVersion: "12345",
		ListFirst:       true,
		WatchTimeout:    5 * time.Minute,
		MaxRetries:      3,
		RetryBackoff:    time.Second,
	}

	if opts.ResourceVersion != "12345" {
		t.Errorf("Expected resource version 12345, got %s", opts.ResourceVersion)
	}

	if !opts.ListFirst {
		t.Error("Expected ListFirst to be true")
	}
}

func TestMetricType(t *testing.T) {
	types := []MetricType{
		MetricTypeCounter,
		MetricTypeGauge,
		MetricTypeHistogram,
	}

	for _, mt := range types {
		if mt == "" {
			t.Error("Metric type constant should not be empty")
		}
	}
}

func TestMetric(t *testing.T) {
	metric := Metric{
		Name:  "k8s_events_total",
		Type:  MetricTypeCounter,
		Value: 42.0,
		Labels: map[string]string{
			"namespace": "default",
			"type":      "pod",
		},
		Timestamp: time.Now(),
		Unit:      "count",
		Help:      "Total number of K8s events",
	}

	if metric.Name != "k8s_events_total" {
		t.Errorf("Expected metric name k8s_events_total, got %s", metric.Name)
	}

	if metric.Value != 42.0 {
		t.Errorf("Expected value 42.0, got %f", metric.Value)
	}
}

func TestResourceMetrics(t *testing.T) {
	metrics := ResourceMetrics{
		ResourceType:   "Pod",
		TotalCount:     10,
		EventsReceived: 100,
		LastEventTime:  time.Now(),
		ErrorCount:     2,
	}

	if metrics.ResourceType != "Pod" {
		t.Errorf("Expected resource type Pod, got %s", metrics.ResourceType)
	}

	if metrics.TotalCount != 10 {
		t.Errorf("Expected total count 10, got %d", metrics.TotalCount)
	}
}

func TestConnectionState(t *testing.T) {
	state := ConnectionState{
		Connected:      true,
		LastConnected:  time.Now(),
		LastError:      nil,
		ReconnectCount: 3,
		APIVersion:     "v1.24.0",
	}

	if !state.Connected {
		t.Error("Expected connected to be true")
	}

	if state.APIVersion != "v1.24.0" {
		t.Errorf("Expected API version v1.24.0, got %s", state.APIVersion)
	}
}

func TestRawEvent(t *testing.T) {
	now := time.Now()
	event := RawEvent{
		Type:         EventTypeAdded,
		Object:       struct{ Name string }{Name: "test"},
		OldObject:    nil,
		ResourceKind: "Pod",
		Namespace:    "default",
		Name:         "test-pod",
		Timestamp:    now,
		Raw: map[string]interface{}{
			"additional": "data",
		},
	}

	if event.Type != EventTypeAdded {
		t.Errorf("Expected type ADDED, got %s", event.Type)
	}

	if event.ResourceKind != "Pod" {
		t.Errorf("Expected resource kind Pod, got %s", event.ResourceKind)
	}

	if !event.Timestamp.Equal(now) {
		t.Errorf("Expected timestamp %v, got %v", now, event.Timestamp)
	}
}

func TestClusterInfo(t *testing.T) {
	info := ClusterInfo{
		Name:         "test-cluster",
		Version:      "v1.24.0",
		Platform:     "GKE",
		ConnectedAt:  time.Now(),
		APIServerURL: "https://k8s.example.com",
	}

	if info.Name != "test-cluster" {
		t.Errorf("Expected cluster name test-cluster, got %s", info.Name)
	}

	if info.Platform != "GKE" {
		t.Errorf("Expected platform GKE, got %s", info.Platform)
	}
}
