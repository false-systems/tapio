package context

import (
	"math"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestConfidenceScorer_CalculateConfidence(t *testing.T) {
	scorer := NewConfidenceScorer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		wantMin  float64
		wantMax  float64
		wantDesc string
	}{
		{
			name:     "nil event",
			event:    nil,
			wantMin:  0.0,
			wantMax:  0.0,
			wantDesc: "nil event should have 0 confidence",
		},
		{
			name: "perfect event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "kubernetes",
				TraceContext: &domain.TraceContext{
					TraceID: "trace-123",
					SpanID:  "span-456",
				},
				Entity: &domain.EntityContext{
					Type:      "Pod",
					Name:      "nginx-123",
					Namespace: "default",
					UID:       "uid-789",
				},
				Kubernetes: &domain.KubernetesData{
					EventType: "Normal",
					Object:    "Pod/nginx-123",
					Reason:    "Started",
				},
			},
			wantMin:  0.95,
			wantMax:  1.0,
			wantDesc: "perfect event should have maximum confidence",
		},
		{
			name: "minimal valid event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now().Add(-10 * time.Minute), // Old timestamp
				Type:      domain.EventTypeSystem,
				Source:    "unknown-source",
			},
			wantMin:  0.0,
			wantMax:  0.3,
			wantDesc: "minimal event should have low confidence",
		},
		{
			name: "event with trace context only",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "unknown",
				TraceContext: &domain.TraceContext{
					TraceID: "trace-123",
					SpanID:  "span-456",
				},
			},
			wantMin:  0.3,
			wantMax:  0.4,
			wantDesc: "event with trace context should get trace bonus",
		},
		{
			name: "event with known source",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "ebpf",
			},
			wantMin:  0.25,
			wantMax:  0.35,
			wantDesc: "event with known source should get source bonus",
		},
		{
			name: "complete kernel event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					Syscall: "open",
					PID:     1234,
					TID:     1234,
					Comm:    "nginx",
					UID:     1000,
					GID:     1000,
				},
			},
			wantMin:  0.55,
			wantMax:  0.65,
			wantDesc: "complete kernel event should have good confidence",
		},
		{
			name: "incomplete network event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol: "TCP",
					// Missing IPs and ports
				},
			},
			wantMin:  0.25,
			wantMax:  0.35,
			wantDesc: "incomplete network event should have lower confidence",
		},
		{
			name: "complete network event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourceIP:   "192.168.1.1",
					SourcePort: 8080,
					DestIP:     "10.0.0.1",
					DestPort:   443,
				},
			},
			wantMin:  0.55,
			wantMax:  0.65,
			wantDesc: "complete network event should have good confidence",
		},
		{
			name: "future timestamp event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now().Add(2 * time.Minute), // Future
				Type:      domain.EventTypeSystem,
				Source:    "system",
			},
			wantMin:  0.1,
			wantMax:  0.2,
			wantDesc: "future timestamp should reduce confidence",
		},
		{
			name: "application event with partial data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeLog,
				Source:    "application",
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "Something went wrong",
					// Missing logger
				},
			},
			wantMin:  0.55,
			wantMax:  0.65,
			wantDesc: "application event with 2/3 fields should have good confidence",
		},
		{
			name: "process event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:  1234,
					Comm: "nginx",
				},
			},
			wantMin:  0.55,
			wantMax:  0.65,
			wantDesc: "process event with PID and comm should have good confidence",
		},
		{
			name: "service event with entity",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeService,
				Source:    "kubernetes",
				Entity: &domain.EntityContext{
					Type: "Service",
					Name: "nginx-svc",
				},
			},
			wantMin:  0.55,
			wantMax:  0.65,
			wantDesc: "service event with basic entity should have medium confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scorer.CalculateConfidence(tt.event)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CalculateConfidence() = %v, want between %v and %v (%s)",
					got, tt.wantMin, tt.wantMax, tt.wantDesc)
			}
			// Ensure result is between 0 and 1
			if got < 0.0 || got > 1.0 {
				t.Errorf("CalculateConfidence() = %v, must be between 0.0 and 1.0", got)
			}
		})
	}
}

func TestConfidenceScorer_HasCompleteData(t *testing.T) {
	scorer := NewConfidenceScorer()

	tests := []struct {
		name  string
		event *domain.UnifiedEvent
		want  bool
	}{
		{
			name: "missing base fields",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "test",
			},
			want: false,
		},
		{
			name: "kernel event without kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeCPU,
				Source:    "ebpf",
			},
			want: false,
		},
		{
			name: "kernel event with complete data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeCPU,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					Syscall: "sched_yield",
					PID:     1234,
					TID:     1234,
					Comm:    "test",
					UID:     1000,
					GID:     1000,
				},
			},
			want: true,
		},
		{
			name: "memory event with kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemory,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					Syscall: "mmap",
					PID:     1234,
					TID:     1234,
					Comm:    "test",
					UID:     1000,
				},
			},
			want: true,
		},
		{
			name: "memory event with application data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemory,
				Source:    "app",
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "out of memory",
				},
			},
			want: true,
		},
		{
			name: "network event with minimal TCP data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol: "TCP",
				},
			},
			want: false,
		},
		{
			name: "network event with complete TCP data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourcePort: 8080,
					DestIP:     "10.0.0.1",
				},
			},
			want: true,
		},
		{
			name: "network event with non-TCP protocol",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol: "ICMP",
				},
			},
			want: true,
		},
		{
			name: "kubernetes event with complete data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "k8s",
				Kubernetes: &domain.KubernetesData{
					EventType: "Normal",
					Object:    "Pod/test",
				},
			},
			want: true,
		},
		{
			name: "unknown event type with data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      "custom-type",
				Source:    "custom",
				Kernel: &domain.KernelData{
					PID: 1234,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scorer.hasCompleteData(tt.event); got != tt.want {
				t.Errorf("hasCompleteData() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfidenceScorer_IsKnownSource(t *testing.T) {
	scorer := NewConfidenceScorer()

	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{"empty source", "", false},
		{"known source exact", "ebpf", true},
		{"known source uppercase", "EBPF", true},
		{"known source with spaces", " ebpf ", true},
		{"unknown source", "custom-collector", false},
		{"source with known prefix", "ebpf-collector", true},
		{"source with known suffix", "collector-ebpf", true},
		{"source containing known", "my-kubernetes-collector", true},
		{"partial match", "eb", false},
		{"k8s alias", "k8s", true},
		{"kubernetes full", "kubernetes", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scorer.isKnownSource(tt.source); got != tt.want {
				t.Errorf("isKnownSource(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestConfidenceScorer_HasAccurateTimestamp(t *testing.T) {
	scorer := NewConfidenceScorer()

	tests := []struct {
		name      string
		timestamp time.Time
		want      bool
	}{
		{"current time", time.Now(), true},
		{"1 minute ago", time.Now().Add(-1 * time.Minute), true},
		{"4 minutes ago", time.Now().Add(-4 * time.Minute), true},
		{"6 minutes ago", time.Now().Add(-6 * time.Minute), false},
		{"30 seconds future", time.Now().Add(30 * time.Second), true},
		{"2 minutes future", time.Now().Add(2 * time.Minute), false},
		{"1 hour ago", time.Now().Add(-1 * time.Hour), false},
		{"exactly 5 minutes ago", time.Now().Add(-5 * time.Minute).Add(time.Second), true}, // Add 1s buffer for test timing
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				Timestamp: tt.timestamp,
			}
			if got := scorer.hasAccurateTimestamp(event); got != tt.want {
				t.Errorf("hasAccurateTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfidenceScorer_CustomWeights(t *testing.T) {
	customWeights := map[string]float64{
		"complete_data":      0.5,
		"trace_context":      0.1,
		"entity_context":     0.1,
		"timestamp_accuracy": 0.2,
		"known_source":       0.1,
	}
	knownSources := []string{"custom1", "custom2"}

	scorer := NewConfidenceScorerWithConfig(customWeights, knownSources)

	// Test that weights are normalized
	weights := scorer.GetWeights()
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("Weights don't sum to 1.0: %v", total)
	}

	// Test custom source recognition
	if !scorer.isKnownSource("custom1") {
		t.Error("Custom source 'custom1' should be recognized")
	}
	if scorer.isKnownSource("ebpf") {
		t.Error("Default source 'ebpf' should not be recognized with custom config")
	}

	// Test that complete data has higher weight
	event := &domain.UnifiedEvent{
		ID:        "test",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		Source:    "unknown",
		Kubernetes: &domain.KubernetesData{
			EventType: "Normal",
			Object:    "Pod/test",
			Reason:    "Started",
		},
	}

	confidence := scorer.CalculateConfidence(event)
	// With complete data (0.5 weight) and timestamp accuracy (0.2 weight)
	if confidence < 0.6 || confidence > 0.8 {
		t.Errorf("Expected confidence between 0.6 and 0.8, got %v", confidence)
	}
}

func TestConfidenceScorer_WeightManipulation(t *testing.T) {
	scorer := NewConfidenceScorer()

	// Test SetWeight
	scorer.SetWeight("trace_context", 0.5)
	weights := scorer.GetWeights()

	// Verify weights still sum to 1.0
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("Weights don't sum to 1.0 after SetWeight: %v", total)
	}

	// Test setting invalid weight
	scorer.SetWeight("unknown_weight", 0.5)
	// Should not crash, just ignore

	// Test boundary values
	scorer.SetWeight("known_source", -1.0)
	if scorer.GetWeights()["known_source"] < 0 {
		t.Error("Weight should not be negative")
	}

	scorer.SetWeight("known_source", 2.0)
	// After normalization, no weight should exceed 1.0
	for key, weight := range scorer.GetWeights() {
		if weight > 1.0 {
			t.Errorf("Weight %s exceeds 1.0: %v", key, weight)
		}
	}
}

func TestConfidenceScorer_SourceManagement(t *testing.T) {
	scorer := NewConfidenceScorer()

	// Add new source
	scorer.AddKnownSource("custom-source")
	if !scorer.isKnownSource("custom-source") {
		t.Error("Added source should be recognized")
	}

	// Add source with different casing
	scorer.AddKnownSource("  ANOTHER-Source  ")
	if !scorer.isKnownSource("another-source") {
		t.Error("Source should be normalized to lowercase")
	}

	// Remove source
	scorer.RemoveKnownSource("EBPF")
	if scorer.isKnownSource("ebpf") {
		t.Error("Removed source should not be recognized")
	}

	// Get sources
	sources := scorer.GetKnownSources()
	if len(sources) == 0 {
		t.Error("Should have at least some known sources")
	}

	found := false
	for _, s := range sources {
		if s == "custom-source" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom-source should be in known sources list")
	}
}

func TestConfidenceScorer_EdgeCases(t *testing.T) {
	scorer := NewConfidenceScorer()

	// Test with all weights set to zero
	zeroWeights := map[string]float64{
		"complete_data":      0,
		"trace_context":      0,
		"entity_context":     0,
		"timestamp_accuracy": 0,
		"known_source":       0,
	}
	zeroScorer := NewConfidenceScorerWithConfig(zeroWeights, []string{})

	event := &domain.UnifiedEvent{
		ID:        "test",
		Timestamp: time.Now(),
		Type:      domain.EventTypeSystem,
		Source:    "test",
	}

	confidence := zeroScorer.CalculateConfidence(event)
	if confidence != 0.0 {
		t.Errorf("With zero weights, confidence should be 0.0, got %v", confidence)
	}

	// Test minimum field ratio boundary
	scorer.minFieldRatio = 1.0 // Require all fields
	kernelEvent := &domain.UnifiedEvent{
		ID:        "test",
		Timestamp: time.Now(),
		Type:      domain.EventTypeSystem,
		Source:    "ebpf",
		Kernel: &domain.KernelData{
			Syscall: "open",
			PID:     1234,
			// Missing other fields
		},
	}
	if scorer.hasCompleteKernelData(kernelEvent) {
		t.Error("With minFieldRatio=1.0, partial kernel data should be incomplete")
	}

	// Reset to default
	scorer.minFieldRatio = 0.7
}

func TestConfidenceScorer_ProcessEventType(t *testing.T) {
	scorer := NewConfidenceScorer()

	tests := []struct {
		name  string
		event *domain.UnifiedEvent
		want  bool
	}{
		{
			name: "process with PID and comm",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:  1234,
					Comm: "nginx",
				},
			},
			want: true,
		},
		{
			name: "process with PID and syscall",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:     1234,
					Syscall: "execve",
				},
			},
			want: true,
		},
		{
			name: "process with only PID",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID: 1234,
				},
			},
			want: false,
		},
		{
			name: "process without kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scorer.hasCompleteData(tt.event); got != tt.want {
				t.Errorf("hasCompleteData() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfidenceScorer_AllEventTypes(t *testing.T) {
	scorer := NewConfidenceScorer()

	eventTypes := []domain.EventType{
		domain.EventTypeSystem,
		domain.EventTypeCPU,
		domain.EventTypeDisk,
		domain.EventTypeMemory,
		domain.EventTypeNetwork,
		domain.EventTypeLog,
		domain.EventTypeKubernetes,
		domain.EventTypeProcess,
		domain.EventTypeService,
		"unknown-type",
	}

	for _, eventType := range eventTypes {
		t.Run(string(eventType), func(t *testing.T) {
			event := &domain.UnifiedEvent{
				ID:        "test",
				Timestamp: time.Now(),
				Type:      eventType,
				Source:    "test",
			}

			// Should not panic
			confidence := scorer.CalculateConfidence(event)
			if confidence < 0 || confidence > 1 {
				t.Errorf("Confidence for %s should be between 0 and 1, got %v", eventType, confidence)
			}
		})
	}
}
