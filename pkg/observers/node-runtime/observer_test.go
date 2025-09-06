package noderuntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
)

func TestNewObserver(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "default config",
			config:      nil,
			expectError: false,
		},
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid config with invalid cert paths",
			config: &Config{
				Address:         "localhost:10250",
				ClientCert:      "/non/existent/cert.pem",
				ClientKey:       "/non/existent/key.pem",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
				RequestTimeout:  10 * time.Second,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test-node-runtime", tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				assert.Equal(t, "test-node-runtime", observer.Name())
				assert.True(t, observer.IsHealthy())

				// Test OTEL instrumentation is initialized
				assert.NotNil(t, observer.tracer)

				// Test base components are initialized
				assert.NotNil(t, observer.BaseObserver)
				assert.NotNil(t, observer.EventChannelManager)
				assert.NotNil(t, observer.LifecycleManager)
			}
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	// Create mock node-runtime server
	mockKubelet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		case "/stats/summary":
			// Mock stats response
			statsResponse := `{
				"node": {
					"nodeName": "test-node",
					"cpu": {
						"time": "2024-01-01T00:00:00Z",
						"usageNanoCores": 1000000000,
						"usageCoreNanoSeconds": 5000000000
					},
					"memory": {
						"time": "2024-01-01T00:00:00Z",
						"availableBytes": 4000000000,
						"usageBytes": 2000000000,
						"workingSetBytes": 1500000000
					}
				},
				"pods": []
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(statsResponse))
		case "/pods":
			// Mock pods response
			podsResponse := `{
				"kind": "PodList",
				"apiVersion": "v1",
				"items": []
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(podsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockKubelet.Close()

	// Extract host:port from test server URL
	serverAddr := mockKubelet.Listener.Addr().String()

	config := &Config{
		Address:         serverAddr,
		Insecure:        true,
		MetricsInterval: 100 * time.Millisecond,
		StatsInterval:   100 * time.Millisecond,
		RequestTimeout:  5 * time.Second,
	}

	logger, _ := zap.NewDevelopment()
	config.Logger = logger

	observer, err := NewObserver("test-node-runtime", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Test Start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	// Let it collect some events
	time.Sleep(200 * time.Millisecond)

	// Check events channel is available
	events := observer.Events()
	assert.NotNil(t, events)

	// Test Stop
	err = observer.Stop()
	assert.NoError(t, err)

	// After stop, should not be healthy
	assert.False(t, observer.IsHealthy())
}

func TestObserverStatistics(t *testing.T) {
	observer, err := NewObserver("test-node-runtime", nil)
	require.NoError(t, err)

	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Check it returns domain.CollectorStats
	observerStats, ok := stats.(*domain.CollectorStats)
	assert.True(t, ok)
	assert.Equal(t, int64(0), observerStats.EventsProcessed)
	assert.Equal(t, int64(0), observerStats.ErrorCount)
}

func TestObserverHealth(t *testing.T) {
	observer, err := NewObserver("test-node-runtime", nil)
	require.NoError(t, err)

	health := observer.Health()
	assert.NotNil(t, health)
	assert.Equal(t, domain.HealthHealthy, health.Status)
	assert.Equal(t, "test-node-runtime", health.Component)

	// Record an error and check health
	observer.BaseObserver.RecordError(assert.AnError)
	health = observer.Health()
	assert.Equal(t, int64(1), health.ErrorCount)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
				RequestTimeout:  5 * time.Second,
				MaxRetries:      3,
			},
			expectError: false,
		},
		{
			name: "empty address",
			config: &Config{
				Address:         "",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
			},
			expectError: true,
			errorMsg:    "node-runtime address cannot be empty",
		},
		{
			name: "metrics interval too short",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 2 * time.Second,
				StatsInterval:   10 * time.Second,
			},
			expectError: true,
			errorMsg:    "metrics interval must be at least 5 seconds",
		},
		{
			name: "stats interval too short",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   2 * time.Second,
			},
			expectError: true,
			errorMsg:    "stats interval must be at least 5 seconds",
		},
		{
			name: "negative max retries",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
				MaxRetries:      -1,
			},
			expectError: true,
			errorMsg:    "max retries cannot be negative",
		},
		{
			name: "too many retries",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
				MaxRetries:      11,
			},
			expectError: true,
			errorMsg:    "max retries must not exceed 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPodTraceManager(t *testing.T) {
	manager := NewPodTraceManager()
	defer manager.Stop()

	// Test GetOrGenerate
	uid1 := types.UID("pod-uid-1")
	trace1 := manager.GetOrGenerate(uid1)
	assert.NotEmpty(t, trace1)

	// Getting same UID should return same trace
	trace1Again := manager.GetOrGenerate(uid1)
	assert.Equal(t, trace1, trace1Again)

	// Different UID should get different trace
	uid2 := types.UID("pod-uid-2")
	trace2 := manager.GetOrGenerate(uid2)
	assert.NotEmpty(t, trace2)
	assert.NotEqual(t, trace1, trace2)

	// Test Count
	assert.Equal(t, 2, manager.Count())

	// Test cleanup (manual trigger)
	manager.cleanupExpired()
	// Should still have 2 since they're not expired yet
	assert.Equal(t, 2, manager.Count())
}

func TestProductionConfig(t *testing.T) {
	config := ProductionConfig()
	assert.False(t, config.Insecure)
	assert.Equal(t, 5*time.Second, config.RequestTimeout)
	assert.Equal(t, 2, config.MaxRetries)
	assert.Equal(t, 60*time.Second, config.MetricsInterval)
}

func TestDevelopmentConfig(t *testing.T) {
	config := DevelopmentConfig()
	assert.True(t, config.Insecure)
	assert.Equal(t, 30*time.Second, config.RequestTimeout)
	assert.Equal(t, 10*time.Second, config.MetricsInterval)
	assert.Equal(t, 5*time.Second, config.StatsInterval)
}
