package base

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestFilterManager_LoadFromFile(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	// Load test filter config
	err := fm.LoadFromFile("testdata/filters.yaml")
	require.NoError(t, err)

	stats := fm.GetStatistics()
	assert.Equal(t, 3, stats.DenyFilters)  // 3 enabled deny filters
	assert.Equal(t, 0, stats.AllowFilters) // Allow filters are disabled
}

func TestFilterManager_NetworkFilter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	err := fm.LoadFromFile("testdata/filters.yaml")
	require.NoError(t, err)

	// Test localhost traffic is filtered
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "test",
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				SrcIP: "127.0.0.1",
				DstIP: "192.168.1.1",
			},
		},
	}

	// Should be denied (localhost source)
	assert.False(t, fm.ShouldAllow(event))
	assert.Equal(t, int64(1), fm.eventsDenied.Load())

	// Non-localhost traffic should pass
	event.EventData.Network.SrcIP = "192.168.1.1"
	assert.True(t, fm.ShouldAllow(event))
}

func TestFilterManager_HTTPFilter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	err := fm.LoadFromFile("testdata/filters.yaml")
	require.NoError(t, err)

	// Test healthcheck endpoints are filtered
	event := &domain.CollectorEvent{
		EventID:   "test-2",
		Timestamp: time.Now(),
		Type:      domain.EventTypeHTTP,
		Source:    "test",
		EventData: domain.EventDataContainer{
			HTTP: &domain.HTTPData{
				Method: "GET",
				URL:    "/health",
			},
		},
	}

	// Should be denied (healthcheck endpoint)
	assert.False(t, fm.ShouldAllow(event))

	// Normal endpoint should pass
	event.EventData.HTTP.URL = "/api/users"
	assert.True(t, fm.ShouldAllow(event))
}

func TestFilterManager_DNSFilter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	err := fm.LoadFromFile("testdata/filters.yaml")
	require.NoError(t, err)

	// Test noisy DNS queries are filtered
	event := &domain.CollectorEvent{
		EventID:   "test-3",
		Timestamp: time.Now(),
		Type:      domain.EventTypeDNS,
		Source:    "test",
		EventData: domain.EventDataContainer{
			DNS: &domain.DNSData{
				QueryName: "service.cluster.local",
				QueryType: "A",
			},
		},
	}

	// Should be denied (cluster.local domain)
	assert.False(t, fm.ShouldAllow(event))

	// External domain should pass
	event.EventData.DNS.QueryName = "google.com"
	assert.True(t, fm.ShouldAllow(event))

	// PTR queries should be denied
	event.EventData.DNS.QueryType = "PTR"
	assert.False(t, fm.ShouldAllow(event))
}

func TestFilterManager_RuntimeFilters(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	// Add runtime filter
	fm.AddDenyFilter("test_runtime", func(event *domain.CollectorEvent) bool {
		return event.Source == "blocked_source"
	})

	event := &domain.CollectorEvent{
		EventID:   "test-4",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "blocked_source",
	}

	// Should be denied
	assert.False(t, fm.ShouldAllow(event))

	// Remove filter
	fm.RemoveFilter("test_runtime")

	// Should now pass
	assert.True(t, fm.ShouldAllow(event))
}

func TestFilterManager_ConfigReload(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "filters.yaml")

	initialConfig := `
version: "1.0"
deny:
  - name: "test_filter"
    type: "severity"
    condition:
      min_severity: "warning"
`
	err := os.WriteFile(configPath, []byte(initialConfig), 0644)
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	fm := NewFilterManager("test", logger)

	// Start watching
	err = fm.WatchConfigFile(configPath)
	require.NoError(t, err)
	defer fm.Stop()

	// Initial state
	stats := fm.GetStatistics()
	assert.Equal(t, 1, stats.DenyFilters)

	// Update config
	updatedConfig := `
version: "2.0"
deny:
  - name: "test_filter"
    type: "severity"
    condition:
      min_severity: "error"
  - name: "another_filter"
    type: "event_type"
    condition:
      types: ["test.event"]
`
	err = os.WriteFile(configPath, []byte(updatedConfig), 0644)
	require.NoError(t, err)

	// Wait for reload
	time.Sleep(200 * time.Millisecond)

	// Check updated state
	stats = fm.GetStatistics()
	assert.Equal(t, 2, stats.DenyFilters)
}

func TestBaseObserver_WithFilters(t *testing.T) {
	logger := zaptest.NewLogger(t)

	bc := NewBaseObserverWithConfig(BaseObserverConfig{
		Name:             "test-observer",
		EnableFilters:    true,
		FilterConfigPath: "testdata/filters.yaml",
		Logger:           logger,
	})

	// Test network event filtering
	networkEvent := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "test",
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				SourceIP: "127.0.0.1", // Localhost
				DestIP:   "8.8.8.8",
			},
		},
	}

	// Should be filtered (localhost)
	assert.False(t, bc.ShouldProcess(networkEvent))
	assert.Equal(t, int64(1), bc.eventsFiltered.Load())

	// Non-localhost should pass
	networkEvent.EventData.Network.SourceIP = "192.168.1.1"
	assert.True(t, bc.ShouldProcess(networkEvent))

	// Add runtime filter
	bc.AddDenyFilter("high_ports", func(event *domain.CollectorEvent) bool {
		if netData, ok := event.GetNetworkData(); ok {
			return netData.SourcePort > 10000 || netData.DestPort > 10000
		}
		return false
	})

	// Test runtime filter
	networkEvent.EventData.Network.SourcePort = 50000
	assert.False(t, bc.ShouldProcess(networkEvent))

	// Get statistics
	stats := bc.Statistics()
	assert.Contains(t, stats.CustomMetrics, "events_filtered")
	assert.Contains(t, stats.CustomMetrics, "filter_deny_count")
}

func TestFilterCompiler_SeverityFilter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fc := NewFilterCompiler(logger)

	rule := &FilterRule{
		Name: "test_severity",
		Type: "severity",
		Conditions: FilterConditions{
			MinSeverity: "warning",
		},
	}

	filter, err := fc.CompileRule(rule)
	require.NoError(t, err)

	// Debug event should be filtered (below minimum severity)
	debugEvent := &domain.CollectorEvent{
		EventID:  "test-1",
		Severity: domain.EventSeverityDebug,
	}
	assert.False(t, filter(debugEvent)) // Returns false - doesn't meet min severity

	// Warning event should pass (meets minimum severity)
	warningEvent := &domain.CollectorEvent{
		EventID:  "test-2",
		Severity: domain.EventSeverityWarning,
	}
	assert.True(t, filter(warningEvent)) // Returns true - meets min severity
}

func BenchmarkFilterManager_ShouldAllow(b *testing.B) {
	logger := zaptest.NewLogger(b)
	fm := NewFilterManager("bench", logger)

	// Add some filters
	fm.AddDenyFilter("filter1", func(e *domain.CollectorEvent) bool {
		return e.Source == "blocked"
	})
	fm.AddDenyFilter("filter2", func(e *domain.CollectorEvent) bool {
		return e.Severity < domain.EventSeverityInfo
	})

	event := &domain.CollectorEvent{
		EventID:   "bench-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.ShouldAllow(event)
	}
}
