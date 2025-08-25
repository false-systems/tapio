package domain

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHealthStatusValue_String(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatusValue
		expected string
	}{
		{
			name:     "healthy_status",
			status:   HealthHealthy,
			expected: "healthy",
		},
		{
			name:     "degraded_status",
			status:   HealthDegraded,
			expected: "degraded",
		},
		{
			name:     "unhealthy_status",
			status:   HealthUnhealthy,
			expected: "unhealthy",
		},
		{
			name:     "unknown_status",
			status:   HealthUnknown,
			expected: "unknown",
		},
		{
			name:     "custom_status",
			status:   HealthStatusValue("custom"),
			expected: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHealthStatusValue_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatusValue
		expected bool
	}{
		{
			name:     "healthy_is_healthy",
			status:   HealthHealthy,
			expected: true,
		},
		{
			name:     "degraded_not_healthy",
			status:   HealthDegraded,
			expected: false,
		},
		{
			name:     "unhealthy_not_healthy",
			status:   HealthUnhealthy,
			expected: false,
		},
		{
			name:     "unknown_not_healthy",
			status:   HealthUnknown,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsHealthy()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewHealthStatus(t *testing.T) {
	before := time.Now()
	status := NewHealthStatus(HealthHealthy, "test message")
	after := time.Now()

	assert.NotNil(t, status)
	assert.Equal(t, HealthHealthy, status.Status)
	assert.Equal(t, "test message", status.Message)
	assert.True(t, status.Timestamp.After(before) || status.Timestamp.Equal(before))
	assert.True(t, status.Timestamp.Before(after) || status.Timestamp.Equal(after))
	assert.NotNil(t, status.Details)
}

func TestNewHealthyStatus(t *testing.T) {
	status := NewHealthyStatus("everything is good")

	assert.NotNil(t, status)
	assert.Equal(t, HealthHealthy, status.Status)
	assert.Equal(t, "everything is good", status.Message)
	assert.NotZero(t, status.Timestamp)
	assert.NotNil(t, status.Details)
}

func TestNewUnhealthyStatus(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		err      error
		validate func(t *testing.T, status *HealthStatus)
	}{
		{
			name:    "unhealthy_with_error",
			message: "connection failed",
			err:     errors.New("timeout connecting to database"),
			validate: func(t *testing.T, status *HealthStatus) {
				assert.Equal(t, HealthUnhealthy, status.Status)
				assert.Equal(t, "connection failed", status.Message)
				assert.Equal(t, errors.New("timeout connecting to database"), status.LastError)
				assert.Equal(t, "timeout connecting to database", status.LastErrorText)
				assert.Equal(t, int64(1), status.ErrorCount)
			},
		},
		{
			name:    "unhealthy_without_error",
			message: "service degraded",
			err:     nil,
			validate: func(t *testing.T, status *HealthStatus) {
				assert.Equal(t, HealthUnhealthy, status.Status)
				assert.Equal(t, "service degraded", status.Message)
				assert.Nil(t, status.LastError)
				assert.Equal(t, "", status.LastErrorText)
				assert.Equal(t, int64(0), status.ErrorCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := NewUnhealthyStatus(tt.message, tt.err)
			assert.NotNil(t, status)
			tt.validate(t, status)
		})
	}
}

func TestHealthStatus_SetError(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  *HealthStatus
		errors         []error
		expectedCount  int64
		expectedStatus HealthStatusValue
	}{
		{
			name:          "set_single_error",
			initialStatus: NewHealthyStatus("initially healthy"),
			errors: []error{
				errors.New("first error"),
			},
			expectedCount:  1,
			expectedStatus: HealthUnhealthy,
		},
		{
			name:          "set_multiple_errors",
			initialStatus: NewHealthyStatus("initially healthy"),
			errors: []error{
				errors.New("first error"),
				errors.New("second error"),
				errors.New("third error"),
			},
			expectedCount:  3,
			expectedStatus: HealthUnhealthy,
		},
		{
			name:          "set_nil_error",
			initialStatus: NewHealthyStatus("healthy"),
			errors: []error{
				nil,
			},
			expectedCount:  0,
			expectedStatus: HealthHealthy,
		},
		{
			name: "add_error_to_already_unhealthy",
			initialStatus: NewUnhealthyStatus("already unhealthy",
				errors.New("initial error")),
			errors: []error{
				errors.New("additional error"),
			},
			expectedCount:  2,
			expectedStatus: HealthUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.initialStatus
			for _, err := range tt.errors {
				status.SetError(err)
			}

			assert.Equal(t, tt.expectedCount, status.ErrorCount)
			assert.Equal(t, tt.expectedStatus, status.Status)
			if tt.expectedCount > 0 {
				assert.NotNil(t, status.LastError)
				assert.NotEmpty(t, status.LastErrorText)
			}
		})
	}
}

func TestHealthStatus_SetDetail(t *testing.T) {
	tests := []struct {
		name    string
		status  *HealthStatus
		details []struct {
			key   string
			value interface{}
		}
		validate func(t *testing.T, status *HealthStatus)
	}{
		{
			name:   "set_components_detail",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{
					key: "components",
					value: map[string]*HealthStatus{
						"component1": NewHealthyStatus("comp1 healthy"),
						"component2": NewUnhealthyStatus("comp2 unhealthy", nil),
					},
				},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.NotNil(t, status.Details)
				assert.NotNil(t, status.Details.Components)
				assert.Len(t, status.Details.Components, 2)
				assert.Equal(t, HealthHealthy, status.Details.Components["component1"].Status)
				assert.Equal(t, HealthUnhealthy, status.Details.Components["component2"].Status)
			},
		},
		{
			name:   "set_unhealthy_count",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "unhealthy_count", value: 3},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.Equal(t, 3, status.Details.ChecksFailed)
			},
		},
		{
			name:   "set_degraded_count",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "degraded_count", value: 2},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.Equal(t, 2, status.Details.ChecksFailed)
			},
		},
		{
			name:   "set_total_components",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "total_components", value: 10},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.Equal(t, 10, status.Details.ChecksTotal)
			},
		},
		{
			name:   "set_custom_string_detail",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "custom_key", value: "custom_value"},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.NotNil(t, status.Details.Labels)
				assert.Equal(t, "custom_value", status.Details.Labels["custom_key"])
			},
		},
		{
			name:   "set_custom_non_string_detail",
			status: NewHealthyStatus("test"),
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "number", value: 42},
				{key: "float", value: 3.14},
				{key: "bool", value: true},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.NotNil(t, status.Details.Labels)
				assert.Equal(t, "42", status.Details.Labels["number"])
				assert.Equal(t, "3.14", status.Details.Labels["float"])
				assert.Equal(t, "true", status.Details.Labels["bool"])
			},
		},
		{
			name:   "set_detail_on_nil_details",
			status: &HealthStatus{Status: HealthHealthy},
			details: []struct {
				key   string
				value interface{}
			}{
				{key: "test", value: "value"},
			},
			validate: func(t *testing.T, status *HealthStatus) {
				assert.NotNil(t, status.Details)
				assert.NotNil(t, status.Details.Labels)
				assert.Equal(t, "value", status.Details.Labels["test"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, detail := range tt.details {
				tt.status.SetDetail(detail.key, detail.value)
			}
			tt.validate(t, tt.status)
		})
	}
}

func TestHealthStatus_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   *HealthStatus
		expected bool
	}{
		{
			name:     "healthy_status",
			status:   NewHealthyStatus("all good"),
			expected: true,
		},
		{
			name:     "unhealthy_status",
			status:   NewUnhealthyStatus("not good", nil),
			expected: false,
		},
		{
			name:     "degraded_status",
			status:   NewHealthStatus(HealthDegraded, "partially working"),
			expected: false,
		},
		{
			name:     "unknown_status",
			status:   NewHealthStatus(HealthUnknown, "status unknown"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsHealthy()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Mock HealthChecker for testing
type mockHealthChecker struct {
	status *HealthStatus
}

func (m *mockHealthChecker) Health() *HealthStatus {
	return m.status
}

func TestNewHealthAggregator(t *testing.T) {
	aggregator := NewHealthAggregator()

	assert.NotNil(t, aggregator)
	assert.NotNil(t, aggregator.components)
	assert.Empty(t, aggregator.components)
}

func TestHealthAggregator_Register(t *testing.T) {
	aggregator := NewHealthAggregator()

	checker1 := &mockHealthChecker{status: NewHealthyStatus("comp1")}
	checker2 := &mockHealthChecker{status: NewHealthyStatus("comp2")}

	aggregator.Register("component1", checker1)
	aggregator.Register("component2", checker2)

	aggregator.mu.RLock()
	defer aggregator.mu.RUnlock()

	assert.Len(t, aggregator.components, 2)
	assert.Equal(t, checker1, aggregator.components["component1"])
	assert.Equal(t, checker2, aggregator.components["component2"])
}

func TestHealthAggregator_Unregister(t *testing.T) {
	aggregator := NewHealthAggregator()

	checker1 := &mockHealthChecker{status: NewHealthyStatus("comp1")}
	checker2 := &mockHealthChecker{status: NewHealthyStatus("comp2")}

	aggregator.Register("component1", checker1)
	aggregator.Register("component2", checker2)

	aggregator.Unregister("component1")

	aggregator.mu.RLock()
	defer aggregator.mu.RUnlock()

	assert.Len(t, aggregator.components, 1)
	assert.Nil(t, aggregator.components["component1"])
	assert.Equal(t, checker2, aggregator.components["component2"])
}

func TestHealthAggregator_AggregateHealth(t *testing.T) {
	tests := []struct {
		name       string
		components map[string]*mockHealthChecker
		expected   struct {
			status  HealthStatusValue
			message string
		}
	}{
		{
			name: "all_healthy",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewHealthyStatus("comp1 healthy")},
				"comp2": {status: NewHealthyStatus("comp2 healthy")},
				"comp3": {status: NewHealthyStatus("comp3 healthy")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthHealthy,
				message: "All components healthy",
			},
		},
		{
			name: "one_unhealthy",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewHealthyStatus("comp1 healthy")},
				"comp2": {status: NewUnhealthyStatus("comp2 unhealthy", nil)},
				"comp3": {status: NewHealthyStatus("comp3 healthy")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthUnhealthy,
				message: "1 components unhealthy",
			},
		},
		{
			name: "multiple_unhealthy",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewUnhealthyStatus("comp1 unhealthy", nil)},
				"comp2": {status: NewUnhealthyStatus("comp2 unhealthy", nil)},
				"comp3": {status: NewHealthyStatus("comp3 healthy")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthUnhealthy,
				message: "2 components unhealthy",
			},
		},
		{
			name: "degraded_components",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewHealthyStatus("comp1 healthy")},
				"comp2": {status: NewHealthStatus(HealthDegraded, "comp2 degraded")},
				"comp3": {status: NewHealthStatus(HealthDegraded, "comp3 degraded")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthDegraded,
				message: "2 components degraded",
			},
		},
		{
			name: "mixed_unhealthy_and_degraded",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewHealthyStatus("comp1 healthy")},
				"comp2": {status: NewUnhealthyStatus("comp2 unhealthy", nil)},
				"comp3": {status: NewHealthStatus(HealthDegraded, "comp3 degraded")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthUnhealthy,
				message: "1 components unhealthy",
			},
		},
		{
			name: "with_unknown_status",
			components: map[string]*mockHealthChecker{
				"comp1": {status: NewHealthyStatus("comp1 healthy")},
				"comp2": {status: NewHealthStatus(HealthUnknown, "comp2 unknown")},
				"comp3": {status: NewHealthyStatus("comp3 healthy")},
			},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthHealthy,
				message: "All components healthy",
			},
		},
		{
			name:       "no_components",
			components: map[string]*mockHealthChecker{},
			expected: struct {
				status  HealthStatusValue
				message string
			}{
				status:  HealthHealthy,
				message: "All components healthy",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregator := NewHealthAggregator()

			for name, checker := range tt.components {
				aggregator.Register(name, checker)
			}

			result := aggregator.AggregateHealth()

			assert.Equal(t, tt.expected.status, result.Status)
			assert.Equal(t, tt.expected.message, result.Message)
			assert.NotNil(t, result.Details)
			assert.NotNil(t, result.Details.Components)
			assert.Equal(t, len(tt.components), result.Details.ChecksTotal)
		})
	}
}

func TestHealthAggregator_Concurrent(t *testing.T) {
	aggregator := NewHealthAggregator()

	// Concurrent registration
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("component_%d", id)
			checker := &mockHealthChecker{
				status: NewHealthyStatus(fmt.Sprintf("%s healthy", name)),
			}
			aggregator.Register(name, checker)
		}(i)
	}
	wg.Wait()

	// Verify all components registered
	aggregator.mu.RLock()
	assert.Len(t, aggregator.components, 100)
	aggregator.mu.RUnlock()

	// Concurrent health checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := aggregator.AggregateHealth()
			assert.NotNil(t, status)
			assert.Equal(t, HealthHealthy, status.Status)
		}()
	}

	// Concurrent unregistration
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("component_%d", id)
			aggregator.Unregister(name)
		}(i)
	}
	wg.Wait()

	// Verify half components remain
	aggregator.mu.RLock()
	assert.Len(t, aggregator.components, 50)
	aggregator.mu.RUnlock()
}

func TestHealthDetails(t *testing.T) {
	// Test HealthDetails struct initialization
	details := &HealthDetails{
		CPUUsage:    75.5,
		MemoryUsage: 1024 * 1024 * 512,  // 512MB
		DiskUsage:   1024 * 1024 * 1024, // 1GB
		NetworkIO:   1024 * 100,         // 100KB

		Latency:     100 * time.Millisecond,
		Throughput:  1000.0,
		QueueSize:   50,
		ActiveConns: 25,

		EventRate:   100.5,
		ErrorRate:   0.01,
		SuccessRate: 99.99,

		Dependencies: map[string]string{
			"database": "connected",
			"cache":    "connected",
		},
		ConfigHash:   "abc123def456",
		FeatureFlags: []string{"feature1", "feature2"},

		StartTime:    time.Now().Add(-1 * time.Hour),
		RestartCount: 2,
		PID:          12345,

		DBConnections: 10,
		CacheHitRate:  0.95,
		StorageHealth: "healthy",

		Components: map[string]*HealthStatus{
			"subcomp1": NewHealthyStatus("healthy"),
		},
		ChecksTotal:  10,
		ChecksPassed: 9,
		ChecksFailed: 1,

		Labels: map[string]string{
			"env": "production",
		},
		Annotations: map[string]string{
			"note": "test annotation",
		},
	}

	// Verify all fields are set correctly
	assert.Equal(t, 75.5, details.CPUUsage)
	assert.Equal(t, int64(1024*1024*512), details.MemoryUsage)
	assert.Equal(t, int64(1024*1024*1024), details.DiskUsage)
	assert.Equal(t, int64(1024*100), details.NetworkIO)

	assert.Equal(t, 100*time.Millisecond, details.Latency)
	assert.Equal(t, 1000.0, details.Throughput)
	assert.Equal(t, 50, details.QueueSize)
	assert.Equal(t, 25, details.ActiveConns)

	assert.Equal(t, 100.5, details.EventRate)
	assert.Equal(t, 0.01, details.ErrorRate)
	assert.Equal(t, 99.99, details.SuccessRate)

	assert.Len(t, details.Dependencies, 2)
	assert.Equal(t, "abc123def456", details.ConfigHash)
	assert.Len(t, details.FeatureFlags, 2)

	assert.Equal(t, int32(2), details.RestartCount)
	assert.Equal(t, int32(12345), details.PID)

	assert.Equal(t, 10, details.DBConnections)
	assert.Equal(t, 0.95, details.CacheHitRate)
	assert.Equal(t, "healthy", details.StorageHealth)

	assert.Len(t, details.Components, 1)
	assert.Equal(t, 10, details.ChecksTotal)
	assert.Equal(t, 9, details.ChecksPassed)
	assert.Equal(t, 1, details.ChecksFailed)

	assert.Equal(t, "production", details.Labels["env"])
	assert.Equal(t, "test annotation", details.Annotations["note"])
}

func BenchmarkHealthStatus_SetError(b *testing.B) {
	status := NewHealthyStatus("benchmark")
	err := errors.New("benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status.SetError(err)
	}
}

func BenchmarkHealthAggregator_AggregateHealth(b *testing.B) {
	aggregator := NewHealthAggregator()

	// Register 10 components
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("component_%d", i)
		var checker HealthChecker
		if i%3 == 0 {
			checker = &mockHealthChecker{status: NewUnhealthyStatus(name, nil)}
		} else if i%3 == 1 {
			checker = &mockHealthChecker{status: NewHealthStatus(HealthDegraded, name)}
		} else {
			checker = &mockHealthChecker{status: NewHealthyStatus(name)}
		}
		aggregator.Register(name, checker)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = aggregator.AggregateHealth()
	}
}

func BenchmarkHealthStatus_SetDetail(b *testing.B) {
	status := NewHealthyStatus("benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		status.Details = &HealthDetails{} // Reset details
		b.StartTimer()

		status.SetDetail("key1", "value1")
		status.SetDetail("key2", 42)
		status.SetDetail("key3", true)
		status.SetDetail("components", map[string]*HealthStatus{
			"comp1": NewHealthyStatus("healthy"),
		})
	}
}
