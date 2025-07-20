package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventProcessorCreation(t *testing.T) {
	processor := newEventProcessor()
	if processor == nil {
		t.Fatal("Expected processor to be created")
	}
}

func TestProcessPodEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	raw := core.RawEvent{
		Type:         core.EventTypeAdded,
		Object:       pod,
		ResourceKind: "Pod",
		Namespace:    "default",
		Name:         "test-pod",
		Timestamp:    time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process pod event: %v", err)
	}

	// Verify basic event properties
	if event.Type != domain.EventTypeKubernetes {
		t.Errorf("Expected event type %s, got %s", domain.EventTypeKubernetes, event.Type)
	}

	if event.Source != domain.SourceK8s {
		t.Errorf("Expected source %s, got %s", domain.SourceK8s, event.Source)
	}

	// Verify context
	if event.Context.Service != "kubernetes" {
		t.Errorf("Expected service kubernetes, got %s", event.Context.Service)
	}
}

func TestDetermineSeverity(t *testing.T) {
	processor := newEventProcessor()

	tests := []struct {
		name     string
		raw      core.RawEvent
		expected domain.EventSeverity
	}{
		{
			name: "normal k8s event",
			raw: core.RawEvent{
				Object: &corev1.Event{Type: corev1.EventTypeNormal},
			},
			expected: domain.EventSeverityLow,
		},
		{
			name: "warning k8s event",
			raw: core.RawEvent{
				Object: &corev1.Event{Type: corev1.EventTypeWarning},
			},
			expected: domain.EventSeverityWarning,
		},
		{
			name: "error event",
			raw: core.RawEvent{
				Type: core.EventTypeError,
			},
			expected: domain.EventSeverityHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := processor.(*eventProcessor).determineSeverity(tt.raw)
			if severity != tt.expected {
				t.Errorf("Expected severity %s, got %s", tt.expected, severity)
			}
		})
	}
}
