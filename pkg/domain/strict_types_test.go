package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLogFields(t *testing.T) {
	lf := NewLogFields()

	assert.NotNil(t, lf)
	assert.NotNil(t, lf.StringFields)
	assert.NotNil(t, lf.IntFields)
	assert.NotNil(t, lf.FloatFields)
	assert.NotNil(t, lf.BoolFields)
	assert.NotNil(t, lf.TimeFields)
	assert.Empty(t, lf.StringFields)
	assert.Empty(t, lf.IntFields)
	assert.Empty(t, lf.FloatFields)
	assert.Empty(t, lf.BoolFields)
	assert.Empty(t, lf.TimeFields)
}

func TestLogFields_AddString(t *testing.T) {
	tests := []struct {
		name     string
		initial  *LogFields
		adds     []struct{ key, value string }
		expected map[string]string
	}{
		{
			name:    "add_to_new_fields",
			initial: NewLogFields(),
			adds: []struct{ key, value string }{
				{"key1", "value1"},
				{"key2", "value2"},
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:    "add_to_nil_map",
			initial: &LogFields{},
			adds: []struct{ key, value string }{
				{"key", "value"},
			},
			expected: map[string]string{
				"key": "value",
			},
		},
		{
			name: "overwrite_existing",
			initial: &LogFields{
				StringFields: map[string]string{
					"key": "old",
				},
			},
			adds: []struct{ key, value string }{
				{"key", "new"},
			},
			expected: map[string]string{
				"key": "new",
			},
		},
		{
			name:    "method_chaining",
			initial: NewLogFields(),
			adds: []struct{ key, value string }{
				{"first", "1"},
				{"second", "2"},
				{"third", "3"},
			},
			expected: map[string]string{
				"first":  "1",
				"second": "2",
				"third":  "3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := tt.initial
			for _, add := range tt.adds {
				result := lf.AddString(add.key, add.value)
				assert.Equal(t, lf, result, "should return self for chaining")
			}
			assert.Equal(t, tt.expected, lf.StringFields)
		})
	}
}

func TestLogFields_AddInt(t *testing.T) {
	tests := []struct {
		name    string
		initial *LogFields
		adds    []struct {
			key   string
			value int64
		}
		expected map[string]int64
	}{
		{
			name:    "add_integers",
			initial: NewLogFields(),
			adds: []struct {
				key   string
				value int64
			}{
				{"count", 100},
				{"size", 1024},
				{"negative", -50},
			},
			expected: map[string]int64{
				"count":    100,
				"size":     1024,
				"negative": -50,
			},
		},
		{
			name:    "add_to_nil_map",
			initial: &LogFields{},
			adds: []struct {
				key   string
				value int64
			}{
				{"key", 42},
			},
			expected: map[string]int64{
				"key": 42,
			},
		},
		{
			name: "overwrite_existing",
			initial: &LogFields{
				IntFields: map[string]int64{
					"key": 10,
				},
			},
			adds: []struct {
				key   string
				value int64
			}{
				{"key", 20},
			},
			expected: map[string]int64{
				"key": 20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := tt.initial
			for _, add := range tt.adds {
				result := lf.AddInt(add.key, add.value)
				assert.Equal(t, lf, result, "should return self for chaining")
			}
			assert.Equal(t, tt.expected, lf.IntFields)
		})
	}
}

func TestLogFields_AddFloat(t *testing.T) {
	tests := []struct {
		name    string
		initial *LogFields
		adds    []struct {
			key   string
			value float64
		}
		expected map[string]float64
	}{
		{
			name:    "add_floats",
			initial: NewLogFields(),
			adds: []struct {
				key   string
				value float64
			}{
				{"percentage", 99.9},
				{"rate", 0.05},
				{"pi", 3.14159},
			},
			expected: map[string]float64{
				"percentage": 99.9,
				"rate":       0.05,
				"pi":         3.14159,
			},
		},
		{
			name:    "add_to_nil_map",
			initial: &LogFields{},
			adds: []struct {
				key   string
				value float64
			}{
				{"key", 1.5},
			},
			expected: map[string]float64{
				"key": 1.5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := tt.initial
			for _, add := range tt.adds {
				result := lf.AddFloat(add.key, add.value)
				assert.Equal(t, lf, result, "should return self for chaining")
			}
			assert.Equal(t, tt.expected, lf.FloatFields)
		})
	}
}

func TestLogFields_AddBool(t *testing.T) {
	tests := []struct {
		name    string
		initial *LogFields
		adds    []struct {
			key   string
			value bool
		}
		expected map[string]bool
	}{
		{
			name:    "add_booleans",
			initial: NewLogFields(),
			adds: []struct {
				key   string
				value bool
			}{
				{"enabled", true},
				{"debug", false},
				{"valid", true},
			},
			expected: map[string]bool{
				"enabled": true,
				"debug":   false,
				"valid":   true,
			},
		},
		{
			name:    "add_to_nil_map",
			initial: &LogFields{},
			adds: []struct {
				key   string
				value bool
			}{
				{"key", true},
			},
			expected: map[string]bool{
				"key": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := tt.initial
			for _, add := range tt.adds {
				result := lf.AddBool(add.key, add.value)
				assert.Equal(t, lf, result, "should return self for chaining")
			}
			assert.Equal(t, tt.expected, lf.BoolFields)
		})
	}
}

func TestLogFields_AddTime(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	tests := []struct {
		name    string
		initial *LogFields
		adds    []struct {
			key   string
			value time.Time
		}
		expected map[string]time.Time
	}{
		{
			name:    "add_times",
			initial: NewLogFields(),
			adds: []struct {
				key   string
				value time.Time
			}{
				{"now", now},
				{"yesterday", yesterday},
				{"tomorrow", tomorrow},
			},
			expected: map[string]time.Time{
				"now":       now,
				"yesterday": yesterday,
				"tomorrow":  tomorrow,
			},
		},
		{
			name:    "add_to_nil_map",
			initial: &LogFields{},
			adds: []struct {
				key   string
				value time.Time
			}{
				{"key", now},
			},
			expected: map[string]time.Time{
				"key": now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := tt.initial
			for _, add := range tt.adds {
				result := lf.AddTime(add.key, add.value)
				assert.Equal(t, lf, result, "should return self for chaining")
			}
			assert.Equal(t, tt.expected, lf.TimeFields)
		})
	}
}

func TestLogFields_MethodChaining(t *testing.T) {
	now := time.Now()

	lf := NewLogFields().
		AddString("name", "test").
		AddInt("count", 42).
		AddFloat("rate", 99.9).
		AddBool("enabled", true).
		AddTime("timestamp", now)

	assert.Equal(t, "test", lf.StringFields["name"])
	assert.Equal(t, int64(42), lf.IntFields["count"])
	assert.Equal(t, 99.9, lf.FloatFields["rate"])
	assert.Equal(t, true, lf.BoolFields["enabled"])
	assert.Equal(t, now, lf.TimeFields["timestamp"])
}

func TestCollectorConfig(t *testing.T) {
	// Test various collector configurations
	tests := []struct {
		name   string
		config CollectorConfig
		check  func(t *testing.T, cfg CollectorConfig)
	}{
		{
			name: "basic_config",
			config: CollectorConfig{
				Name:      "test-collector",
				Type:      "kernel",
				Enabled:   true,
				Interval:  5 * time.Second,
				BatchSize: 100,
				Labels: map[string]string{
					"env": "test",
				},
			},
			check: func(t *testing.T, cfg CollectorConfig) {
				assert.Equal(t, "test-collector", cfg.Name)
				assert.Equal(t, "kernel", cfg.Type)
				assert.True(t, cfg.Enabled)
				assert.Equal(t, 5*time.Second, cfg.Interval)
				assert.Equal(t, 100, cfg.BatchSize)
				assert.Equal(t, "test", cfg.Labels["env"])
			},
		},
		{
			name: "kernel_specific_config",
			config: CollectorConfig{
				Name: "kernel-collector",
				Type: "kernel",
				Kernel: &KernelConfig{
					BufferSize:    1024,
					PerfEventSize: 4096,
					BPFPath:       "/path/to/bpf",
				},
			},
			check: func(t *testing.T, cfg CollectorConfig) {
				assert.NotNil(t, cfg.Kernel)
				assert.Equal(t, 1024, cfg.Kernel.BufferSize)
				assert.Equal(t, 4096, cfg.Kernel.PerfEventSize)
				assert.Equal(t, "/path/to/bpf", cfg.Kernel.BPFPath)
			},
		},
		{
			name: "etcd_config_with_tls",
			config: CollectorConfig{
				Name: "etcd-collector",
				Type: "etcd",
				ETCD: &ETCDConfig{
					Endpoints: []string{"localhost:2379", "localhost:2380"},
					Timeout:   10 * time.Second,
					TLS: &TLSConfig{
						CertFile: "/cert.pem",
						KeyFile:  "/key.pem",
						CAFile:   "/ca.pem",
					},
				},
			},
			check: func(t *testing.T, cfg CollectorConfig) {
				assert.NotNil(t, cfg.ETCD)
				assert.Len(t, cfg.ETCD.Endpoints, 2)
				assert.Equal(t, 10*time.Second, cfg.ETCD.Timeout)
				assert.NotNil(t, cfg.ETCD.TLS)
				assert.Equal(t, "/cert.pem", cfg.ETCD.TLS.CertFile)
			},
		},
		{
			name: "dns_config",
			config: CollectorConfig{
				Name: "dns-collector",
				Type: "dns",
				DNS: &DNSConfig{
					ServerAddr: "8.8.8.8:53",
					Timeout:    3 * time.Second,
					MaxRetries: 3,
				},
			},
			check: func(t *testing.T, cfg CollectorConfig) {
				assert.NotNil(t, cfg.DNS)
				assert.Equal(t, "8.8.8.8:53", cfg.DNS.ServerAddr)
				assert.Equal(t, 3*time.Second, cfg.DNS.Timeout)
				assert.Equal(t, 3, cfg.DNS.MaxRetries)
			},
		},
		{
			name: "cri_config",
			config: CollectorConfig{
				Name: "cri-collector",
				Type: "cri",
				CRI: &CRIConfig{
					SocketPath: "/var/run/dockershim.sock",
					Timeout:    5 * time.Second,
				},
			},
			check: func(t *testing.T, cfg CollectorConfig) {
				assert.NotNil(t, cfg.CRI)
				assert.Equal(t, "/var/run/dockershim.sock", cfg.CRI.SocketPath)
				assert.Equal(t, 5*time.Second, cfg.CRI.Timeout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

func TestCollectorStats(t *testing.T) {
	now := time.Now()
	stats := CollectorStats{
		EventsProcessed: 10000,
		ErrorCount:      5,
		LastEventTime:   now,
		Uptime:          24 * time.Hour,
		CustomMetrics: map[string]string{
			"cache_hit_rate": "95.5",
			"queue_depth":    "100",
		},
	}

	assert.Equal(t, int64(10000), stats.EventsProcessed)
	assert.Equal(t, int64(5), stats.ErrorCount)
	assert.Equal(t, now, stats.LastEventTime)
	assert.Equal(t, 24*time.Hour, stats.Uptime)
	assert.Equal(t, "95.5", stats.CustomMetrics["cache_hit_rate"])
	assert.Equal(t, "100", stats.CustomMetrics["queue_depth"])
}

func TestStrictEventAttributes(t *testing.T) {
	attrs := StrictEventAttributes{
		StringAttrs: map[string]string{
			"name": "test-event",
			"type": "kernel",
		},
		IntAttrs: map[string]int64{
			"pid":  12345,
			"size": 1024,
		},
		FloatAttrs: map[string]float64{
			"cpu_usage": 75.5,
			"memory":    512.25,
		},
		BoolAttrs: map[string]bool{
			"success": true,
			"retry":   false,
		},
	}

	assert.Equal(t, "test-event", attrs.StringAttrs["name"])
	assert.Equal(t, int64(12345), attrs.IntAttrs["pid"])
	assert.Equal(t, 75.5, attrs.FloatAttrs["cpu_usage"])
	assert.True(t, attrs.BoolAttrs["success"])
}

func TestServiceInfo(t *testing.T) {
	svc := ServiceInfo{
		Name:      "api-service",
		Namespace: "production",
		Labels: map[string]string{
			"app":     "api",
			"version": "v1",
		},
		Selector: map[string]string{
			"app": "api",
		},
		Ports: []PortInfo{
			{Name: "http", Port: 80, Protocol: "TCP"},
			{Name: "https", Port: 443, Protocol: "TCP"},
		},
	}

	assert.Equal(t, "api-service", svc.Name)
	assert.Equal(t, "production", svc.Namespace)
	assert.Equal(t, "v1", svc.Labels["version"])
	assert.Len(t, svc.Ports, 2)
	assert.Equal(t, int32(80), svc.Ports[0].Port)
}

func TestPipelineConfig(t *testing.T) {
	cfg := PipelineConfig{
		BatchSize:     100,
		FlushInterval: 10 * time.Second,
		Processors: []ProcessorConfig{
			{
				Type: "filter",
				Config: map[string]string{
					"field": "level",
					"value": "error",
				},
			},
			{
				Type: "transform",
				Config: map[string]string{
					"format": "json",
				},
			},
		},
	}

	assert.Equal(t, 100, cfg.BatchSize)
	assert.Equal(t, 10*time.Second, cfg.FlushInterval)
	assert.Len(t, cfg.Processors, 2)
	assert.Equal(t, "filter", cfg.Processors[0].Type)
	assert.Equal(t, "error", cfg.Processors[0].Config["value"])
}

func TestBatchMetadata(t *testing.T) {
	metadata := BatchMetadata{
		BatchID:        "batch-001",
		ProcessingTime: 100 * time.Millisecond,
		EventCount:     50,
		Source:         "kernel",
		Labels: map[string]string{
			"host": "node-1",
		},
		Metrics: &BatchMetrics{
			BytesProcessed:   10240,
			EventsDropped:    2,
			ErrorCount:       1,
			AverageLatency:   5 * time.Millisecond,
			ThroughputPerSec: 500.5,
		},
	}

	assert.Equal(t, "batch-001", metadata.BatchID)
	assert.Equal(t, 100*time.Millisecond, metadata.ProcessingTime)
	assert.Equal(t, 50, metadata.EventCount)
	assert.NotNil(t, metadata.Metrics)
	assert.Equal(t, int64(10240), metadata.Metrics.BytesProcessed)
	assert.Equal(t, 500.5, metadata.Metrics.ThroughputPerSec)
}

func TestLoaderProperties(t *testing.T) {
	props := LoaderProperties{
		BatchSize:     100,
		Timeout:       30 * time.Second,
		RetryAttempts: 3,
		Endpoints:     []string{"localhost:9200"},
		Username:      "admin",
		Password:      "secret",
		TLS: &TLSConfig{
			CertFile: "/cert.pem",
		},
		PoolSize:      10,
		QueueSize:     1000,
		FlushInterval: 5 * time.Second,
		EnableMetrics: true,
		EnableTracing: false,
		Labels: map[string]string{
			"env": "prod",
		},
		Tags: []string{"monitoring", "production"},
	}

	assert.Equal(t, 100, props.BatchSize)
	assert.Equal(t, 30*time.Second, props.Timeout)
	assert.Equal(t, "admin", props.Username)
	assert.NotNil(t, props.TLS)
	assert.True(t, props.EnableMetrics)
	assert.False(t, props.EnableTracing)
	assert.Len(t, props.Tags, 2)
}

func TestK8sEventData(t *testing.T) {
	now := time.Now()
	event := K8sEventData{
		Type:      "ADDED",
		Timestamp: now,
		Source:    "kube-apiserver",
		Labels: map[string]string{
			"component": "scheduler",
		},
		Object: &ObjectData{
			Kind:       "Pod",
			APIVersion: "v1",
			Name:       "test-pod",
			Namespace:  "default",
			UID:        "12345-67890",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"note": "test annotation",
			},
		},
	}

	assert.Equal(t, "ADDED", event.Type)
	assert.Equal(t, now, event.Timestamp)
	assert.NotNil(t, event.Object)
	assert.Equal(t, "Pod", event.Object.Kind)
	assert.Equal(t, "test-pod", event.Object.Name)
	assert.Equal(t, "test", event.Object.Labels["app"])
}

func TestMonitoringStatus(t *testing.T) {
	now := time.Now()
	status := MonitoringStatus{
		Component:       "collector",
		Status:          "healthy",
		Timestamp:       now,
		Uptime:          48 * time.Hour,
		CPUUsage:        65.5,
		MemoryUsage:     1024 * 1024 * 512, // 512MB
		EventsProcessed: 1000000,
		ErrorCount:      10,
		LastError:       "connection timeout",
		Details: map[string]string{
			"version": "1.0.0",
			"node":    "node-1",
		},
	}

	assert.Equal(t, "collector", status.Component)
	assert.Equal(t, "healthy", status.Status)
	assert.Equal(t, now, status.Timestamp)
	assert.Equal(t, 48*time.Hour, status.Uptime)
	assert.Equal(t, 65.5, status.CPUUsage)
	assert.Equal(t, int64(1024*1024*512), status.MemoryUsage)
	assert.Equal(t, int64(1000000), status.EventsProcessed)
	assert.Equal(t, "connection timeout", status.LastError)
	assert.Equal(t, "1.0.0", status.Details["version"])
}

func BenchmarkNewLogFields(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewLogFields()
	}
}

func BenchmarkLogFields_ChainedAdds(b *testing.B) {
	now := time.Now()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		lf := NewLogFields().
			AddString("str1", "value1").
			AddString("str2", "value2").
			AddInt("int1", 100).
			AddInt("int2", 200).
			AddFloat("float1", 1.5).
			AddFloat("float2", 2.5).
			AddBool("bool1", true).
			AddBool("bool2", false).
			AddTime("time1", now).
			AddTime("time2", now)
		_ = lf
	}
}

func BenchmarkLogFields_AddOperations(b *testing.B) {
	lf := NewLogFields()
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lf.AddString("key", "value")
		lf.AddInt("count", int64(i))
		lf.AddFloat("rate", float64(i)*0.1)
		lf.AddBool("flag", i%2 == 0)
		lf.AddTime("timestamp", now)
	}
}
