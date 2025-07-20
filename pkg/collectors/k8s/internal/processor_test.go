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
				"app":     "test",
				"version": "v1",
			},
			ResourceVersion: "12345",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "container-1",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
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

	// Verify resource data
	resource, ok := event.Data["resource"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected resource data in event")
	}

	if resource["kind"] != "Pod" {
		t.Errorf("Expected resource kind Pod, got %v", resource["kind"])
	}

	if resource["name"] != "test-pod" {
		t.Errorf("Expected resource name test-pod, got %v", resource["name"])
	}

	// Verify pod-specific data
	if phase, ok := event.Data["phase"].(string); !ok || phase != "Running" {
		t.Errorf("Expected phase Running, got %v", event.Data["phase"])
	}

	// Verify context
	if event.Context.Service != "kubernetes" {
		t.Errorf("Expected service kubernetes, got %s", event.Context.Service)
	}

	if event.Context.Node != "test-node" {
		t.Errorf("Expected node test-node, got %s", event.Context.Node)
	}

	// Verify labels are copied
	if event.Context.Labels["app"] != "test" {
		t.Errorf("Expected label app=test, got %v", event.Context.Labels["app"])
	}
}

func TestProcessNodeEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-node",
			ResourceVersion: "67890",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionTrue,
					Message: "kubelet is posting ready status",
				},
				{
					Type:   corev1.NodeMemoryPressure,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	raw := core.RawEvent{
		Type:         core.EventTypeModified,
		Object:       node,
		ResourceKind: "Node",
		Name:         "test-node",
		Timestamp:    time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process node event: %v", err)
	}

	// Verify node-specific data
	if ready, ok := event.Data["ready"].(bool); !ok || !ready {
		t.Errorf("Expected node to be ready, got %v", event.Data["ready"])
	}

	conditions, ok := event.Data["conditions"].([]string)
	if !ok {
		t.Fatal("Expected conditions in node data")
	}

	if len(conditions) == 0 || conditions[0] != "Ready" {
		t.Errorf("Expected Ready condition, got %v", conditions)
	}
}

func TestProcessK8sEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	k8sEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Pod",
			Name:       "test-pod",
			Namespace:  "default",
			APIVersion: "v1",
		},
		Type:    corev1.EventTypeWarning,
		Reason:  "BackOff",
		Message: "Back-off pulling image",
		Count:   5,
	}

	raw := core.RawEvent{
		Type:         core.EventTypeAdded,
		Object:       k8sEvent,
		ResourceKind: "Event",
		Namespace:    "default",
		Name:         "test-event",
		Timestamp:    time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process k8s event: %v", err)
	}

	// Verify severity for warning event
	if event.Severity != domain.EventSeverityWarning {
		t.Errorf("Expected severity warning, got %s", event.Severity)
	}

	// Verify event-specific data
	if reason, ok := event.Data["reason"].(string); !ok || reason != "BackOff" {
		t.Errorf("Expected reason BackOff, got %v", event.Data["reason"])
	}

	if count, ok := event.Data["count"].(int32); !ok || count != 5 {
		t.Errorf("Expected count 5, got %v", event.Data["count"])
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
			name: "failed event",
			raw: core.RawEvent{
				Object: &corev1.Event{Reason: "Failed"},
			},
			expected: domain.EventSeverityHigh,
		},
		{
			name: "evicted event",
			raw: core.RawEvent{
				Object: &corev1.Event{Reason: "Evicted"},
			},
			expected: domain.EventSeverityCritical,
		},
		{
			name: "deleted pod",
			raw: core.RawEvent{
				Type:         core.EventTypeDeleted,
				ResourceKind: "Pod",
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
		{
			name: "default case",
			raw: core.RawEvent{
				Type: core.EventTypeAdded,
			},
			expected: domain.EventSeverityLow,
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

func TestProcessServiceEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-service",
			Namespace:       "default",
			ResourceVersion: "11111",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "10.0.0.100"},
				},
			},
		},
	}

	raw := core.RawEvent{
		Type:         core.EventTypeAdded,
		Object:       service,
		ResourceKind: "Service",
		Namespace:    "default",
		Name:         "test-service",
		Timestamp:    time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process service event: %v", err)
	}

	// Verify service-specific data
	if serviceType, ok := event.Data["service_type"].(string); !ok || serviceType != "LoadBalancer" {
		t.Errorf("Expected service type LoadBalancer, got %v", event.Data["service_type"])
	}

	if lbIP, ok := event.Data["load_balancer_ip"].(string); !ok || lbIP != "10.0.0.100" {
		t.Errorf("Expected load balancer IP 10.0.0.100, got %v", event.Data["load_balancer_ip"])
	}
}

func TestEventContextCreation(t *testing.T) {
	processor := newEventProcessor()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "production",
			Labels: map[string]string{
				"app":         "api",
				"environment": "prod",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-node-1",
		},
	}

	raw := core.RawEvent{
		Type:         core.EventTypeModified,
		Object:       pod,
		ResourceKind: "Pod",
		Namespace:    "production",
		Name:         "test-pod",
		Timestamp:    time.Now(),
	}

	ctx := processor.(*eventProcessor).createEventContext(raw)

	// Verify context fields
	if ctx.Service != "kubernetes" {
		t.Errorf("Expected service kubernetes, got %s", ctx.Service)
	}

	if ctx.Component != "Pod" {
		t.Errorf("Expected component Pod, got %s", ctx.Component)
	}

	if ctx.Namespace != "production" {
		t.Errorf("Expected namespace production, got %s", ctx.Namespace)
	}

	if ctx.Node != "worker-node-1" {
		t.Errorf("Expected node worker-node-1, got %s", ctx.Node)
	}

	// Verify labels
	if ctx.Labels["app"] != "api" {
		t.Errorf("Expected label app=api, got %v", ctx.Labels["app"])
	}

	if ctx.Labels["event_type"] != "MODIFIED" {
		t.Errorf("Expected label event_type=MODIFIED, got %v", ctx.Labels["event_type"])
	}

	// Verify metadata
	expectedPath := "/Pod/production/test-pod"
	if path, ok := ctx.Metadata["resource_path"].(string); !ok || path != expectedPath {
		t.Errorf("Expected resource_path %s, got %v", expectedPath, ctx.Metadata["resource_path"])
	}
}