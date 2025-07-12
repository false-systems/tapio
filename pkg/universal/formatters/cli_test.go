package formatters

import (
	"strings"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/universal"
)

func TestCLIFormatter_FormatMetric(t *testing.T) {
	formatter := NewCLIFormatter(&CLIConfig{
		UseColor:   false,
		Verbosity:  2,
		TimeFormat: "15:04:05",
	})

	tests := []struct {
		name     string
		metric   *universal.UniversalMetric
		contains []string
	}{
		{
			name: "Basic metric",
			metric: &universal.UniversalMetric{
				Name:      "memory_usage",
				Value:     1024.0,
				Unit:      "MB",
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Target: universal.Target{
					Type: universal.TargetTypePod,
					Name: "test-pod",
				},
				Quality: universal.DataQuality{
					Level: universal.QualityGood,
				},
			},
			contains: []string{
				"12:00:00",
				"pod/test-pod",
				"memory_usage = 1024.00 MB",
			},
		},
		{
			name: "Metric with labels",
			metric: &universal.UniversalMetric{
				Name:  "cpu_usage",
				Value: 85.5,
				Unit:  "percent",
				Target: universal.Target{
					Type:      universal.TargetTypeContainer,
					Name:      "my-pod",
					Container: "app",
					Namespace: "production",
				},
				Labels: map[string]string{
					"cpu":  "0",
					"mode": "user",
				},
			},
			contains: []string{
				"container/production/my-pod/app",
				"cpu_usage = 85.50 percent",
				"{cpu=\"0\", mode=\"user\"}",
			},
		},
		{
			name: "Degraded quality metric",
			metric: &universal.UniversalMetric{
				Name:  "disk_io",
				Value: 100.0,
				Unit:  "IOPS",
				Target: universal.Target{
					Type: universal.TargetTypeNode,
					Name: "worker-1",
				},
				Quality: universal.DataQuality{
					Level:   universal.QualityDegraded,
					Message: "Sampling rate reduced",
				},
			},
			contains: []string{
				"node/worker-1",
				"disk_io = 100.00 IOPS",
				"[degraded]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatter.FormatMetric(tt.metric)

			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

func TestCLIFormatter_FormatEvent(t *testing.T) {
	formatter := NewCLIFormatter(&CLIConfig{
		UseColor:  false,
		Verbosity: 3,
	})

	event := &universal.UniversalEvent{
		Type:      universal.EventTypeOOMKill,
		Level:     universal.EventLevelCritical,
		Message:   "Process killed due to out of memory",
		Category:  "memory",
		Source:    "kernel",
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Target: universal.Target{
			Type: universal.TargetTypeProcess,
			Name: "nginx",
			PID:  1234,
		},
		Details: map[string]interface{}{
			"memory_usage": "512MB",
			"memory_limit": "500MB",
		},
	}

	output := formatter.FormatEvent(event)

	expectedStrings := []string{
		"[CRITICAL]",
		"process/nginx[1234]",
		"oomkill",
		"Process killed due to out of memory",
		"Details:",
		"memory_usage: 512MB",
		"memory_limit: 500MB",
		"Category: memory",
		"Source: kernel",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
		}
	}
}

func TestCLIFormatter_FormatPrediction(t *testing.T) {
	formatter := NewCLIFormatter(&CLIConfig{
		UseColor:  false,
		Verbosity: 1,
	})

	prediction := &universal.UniversalPrediction{
		Type:        "oom",
		Severity:    universal.SeverityHigh,
		Confidence:  0.85,
		TimeToEvent: 5 * time.Minute,
		Description: "Memory usage is increasing rapidly",
		Target: universal.Target{
			Type:      universal.TargetTypePod,
			Name:      "webapp",
			Namespace: "default",
		},
		Factors: []string{
			"Memory usage growing at 10MB/min",
			"No memory limits configured",
			"Historical OOM events detected",
		},
		Recommendations: []string{
			"Set memory limits for the pod",
			"Investigate memory leak in application",
			"Enable horizontal pod autoscaling",
		},
	}

	output := formatter.FormatPrediction(prediction)

	expectedStrings := []string{
		"[HIGH]",
		"pod/webapp",
		"oom in 5.0 minutes (85% confidence)",
		"Memory usage is increasing rapidly",
		"Contributing factors:",
		"• Memory usage growing at 10MB/min",
		"• No memory limits configured",
		"Recommendations:",
		"→ Set memory limits for the pod",
		"→ Investigate memory leak",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
		}
	}
}

func TestCLIFormatter_FormatExplanation(t *testing.T) {
	formatter := NewCLIFormatter(&CLIConfig{
		UseColor:  false,
		Verbosity: 1,
	})

	dataset := &universal.UniversalDataset{
		Predictions: []*universal.UniversalPrediction{
			{
				Type:        "oom",
				Severity:    universal.SeverityCritical,
				Confidence:  0.95,
				TimeToEvent: 2 * time.Minute,
				Target: universal.Target{
					Type: universal.TargetTypePod,
					Name: "api-server",
				},
				Description: "Critical memory pressure detected",
			},
			{
				Type:        "cpu_throttle",
				Severity:    universal.SeverityMedium,
				Confidence:  0.70,
				TimeToEvent: 10 * time.Minute,
				Target: universal.Target{
					Type: universal.TargetTypePod,
					Name: "api-server",
				},
				Description: "CPU usage approaching limits",
			},
			{
				Type:        "disk_full",
				Severity:    universal.SeverityHigh,
				Confidence:  0.80,
				TimeToEvent: 1 * time.Hour,
				Target: universal.Target{
					Type: universal.TargetTypeNode,
					Name: "worker-1",
				},
				Description: "Disk space running low",
			},
		},
	}

	output := formatter.FormatExplanation(dataset)

	expectedStrings := []string{
		"=== pod/api-server ===",
		"=== node/worker-1 ===",
		"Summary: 3 predictions across 2 targets",
		"(1 critical, 1 high, 1 medium)",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
		}
	}
}

func TestCLIFormatter_ColorOutput(t *testing.T) {
	formatter := NewCLIFormatter(&CLIConfig{
		UseColor:  true,
		Verbosity: 0,
	})

	tests := []struct {
		name     string
		format   func() string
		contains string
	}{
		{
			name: "Critical event color",
			format: func() string {
				event := &universal.UniversalEvent{
					Type:  universal.EventTypeOOMKill,
					Level: universal.EventLevelCritical,
					Target: universal.Target{
						Type: universal.TargetTypeProcess,
						Name: "test",
					},
				}
				return formatter.FormatEvent(event)
			},
			contains: "\033[31m[CRITICAL]\033[0m", // Red
		},
		{
			name: "Warning event color",
			format: func() string {
				event := &universal.UniversalEvent{
					Type:  universal.EventTypeMemoryPressure,
					Level: universal.EventLevelWarning,
					Target: universal.Target{
						Type: universal.TargetTypeProcess,
						Name: "test",
					},
				}
				return formatter.FormatEvent(event)
			},
			contains: "\033[33m[WARNING]\033[0m", // Yellow
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := tt.format()
			if !strings.Contains(output, tt.contains) {
				t.Errorf("Expected color code %q in output, got:\n%s", tt.contains, output)
			}
		})
	}
}

func TestTableFormatter_FormatMetricsTable(t *testing.T) {
	formatter := NewTableFormatter(&CLIConfig{
		UseColor: false,
	})

	metrics := []*universal.UniversalMetric{
		{
			Name:  "memory_usage",
			Value: 1024.50,
			Unit:  "MB",
			Target: universal.Target{
				Type: universal.TargetTypePod,
				Name: "webapp-deployment-abc123",
			},
			Quality: universal.DataQuality{
				Level: universal.QualityGood,
			},
		},
		{
			Name:  "cpu_usage_percentage",
			Value: 85.25,
			Unit:  "%",
			Target: universal.Target{
				Type:      universal.TargetTypeContainer,
				Name:      "webapp",
				Container: "nginx",
			},
			Quality: universal.DataQuality{
				Level: universal.QualityDegraded,
			},
		},
	}

	output := formatter.FormatMetricsTable(metrics)

	// Check table structure
	lines := strings.Split(output, "\n")
	if len(lines) < 4 {
		t.Fatalf("Expected at least 4 lines, got %d", len(lines))
	}

	// Check header
	if !strings.Contains(lines[0], "Target") || !strings.Contains(lines[0], "Metric") {
		t.Error("Table header missing expected columns")
	}

	// Check separator
	if !strings.Contains(lines[1], "---") {
		t.Error("Table separator missing")
	}

	// Check data rows
	if !strings.Contains(output, "1024.50") {
		t.Error("Expected metric value not found")
	}
	if !strings.Contains(output, "degraded") {
		t.Error("Expected quality level not found")
	}
}

func TestCLIFormatter_FormatDuration(t *testing.T) {
	formatter := NewCLIFormatter(nil)

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30 seconds"},
		{5 * time.Minute, "5.0 minutes"},
		{2 * time.Hour, "2.0 hours"},
		{36 * time.Hour, "1.5 days"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatter.formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
