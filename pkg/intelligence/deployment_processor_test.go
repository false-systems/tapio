package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewDeploymentProcessor(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)
	require.NotNil(t, processor)
	assert.NotNil(t, processor.deploymentEvents)
	assert.Equal(t, 1000, cap(processor.deploymentEvents))
}

func TestProcessRawEvent_NonKubeAPIEvent(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "other-collector",
		Timestamp: time.Now(),
		Data:      []byte("some data"),
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	assert.NoError(t, err)

	// Should not produce any deployment events
	select {
	case <-processor.deploymentEvents:
		t.Fatal("Should not have produced an event")
	default:
		// Expected
	}
}

func TestProcessRawEvent_NonDeploymentResource(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	resourceEvent := domain.ResourceEvent{
		EventType: "ADDED",
		Timestamp: time.Now(),
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
	}

	data, err := json.Marshal(resourceEvent)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      data,
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	assert.NoError(t, err)

	// Should not produce any deployment events
	select {
	case <-processor.deploymentEvents:
		t.Fatal("Should not have produced an event")
	default:
		// Expected
	}
}

func TestProcessRawEvent_DeploymentCreated(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			UID:       "12345",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.19",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:   3,
			ReadyReplicas:     3,
			AvailableReplicas: 3,
		},
	}

	resourceEvent := domain.ResourceEvent{
		EventType: "ADDED",
		Timestamp: time.Now(),
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "default",
		UID:       "12345",
		Object:    deployment,
		Labels:    deployment.Labels,
	}

	data, err := json.Marshal(resourceEvent)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      data,
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should produce a deployment event
	select {
	case deploymentEvent := <-processor.deploymentEvents:
		require.NotNil(t, deploymentEvent)
		assert.Equal(t, domain.DeploymentCreated, deploymentEvent.Action)
		assert.Equal(t, "test-deployment", deploymentEvent.Name)
		assert.Equal(t, "default", deploymentEvent.Namespace)
		assert.Equal(t, int32(3), deploymentEvent.Metadata.NewReplicas)
		assert.Equal(t, "nginx:1.19", deploymentEvent.Metadata.NewImage)
		assert.Equal(t, "RollingUpdate", deploymentEvent.Metadata.Strategy)
		assert.False(t, deploymentEvent.HasImageChange())
		assert.False(t, deploymentEvent.HasScaleChange())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for deployment event")
	}
}

func TestProcessRawEvent_DeploymentUpdatedWithImageChange(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	replicas := int32(3)

	oldDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.19",
						},
					},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			UID:       "12345",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.20",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:   1,
			ReadyReplicas:     3,
			AvailableReplicas: 3,
		},
	}

	resourceEvent := domain.ResourceEvent{
		EventType: "MODIFIED",
		Timestamp: time.Now(),
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "default",
		UID:       "12345",
		Object:    newDeployment,
		OldObject: oldDeployment,
	}

	data, err := json.Marshal(resourceEvent)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      data,
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should produce a deployment event
	select {
	case deploymentEvent := <-processor.deploymentEvents:
		require.NotNil(t, deploymentEvent)
		assert.Equal(t, domain.DeploymentUpdated, deploymentEvent.Action)
		assert.Equal(t, "test-deployment", deploymentEvent.Name)
		assert.Equal(t, "nginx:1.20", deploymentEvent.Metadata.NewImage)
		assert.Equal(t, "nginx:1.19", deploymentEvent.Metadata.OldImage)
		assert.True(t, deploymentEvent.HasImageChange())
		assert.False(t, deploymentEvent.HasScaleChange())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for deployment event")
	}
}

func TestProcessRawEvent_DeploymentScaled(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	oldReplicas := int32(3)
	newReplicas := int32(5)

	oldDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &oldReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.19",
						},
					},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			UID:       "12345",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &newReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.19",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:   5,
			ReadyReplicas:     5,
			AvailableReplicas: 5,
		},
	}

	resourceEvent := domain.ResourceEvent{
		EventType: "MODIFIED",
		Timestamp: time.Now(),
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "default",
		UID:       "12345",
		Object:    newDeployment,
		OldObject: oldDeployment,
	}

	data, err := json.Marshal(resourceEvent)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      data,
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should produce a deployment event
	select {
	case deploymentEvent := <-processor.deploymentEvents:
		require.NotNil(t, deploymentEvent)
		assert.Equal(t, domain.DeploymentUpdated, deploymentEvent.Action)
		assert.Equal(t, newReplicas, deploymentEvent.Metadata.NewReplicas)
		assert.Equal(t, oldReplicas, deploymentEvent.Metadata.OldReplicas)
		assert.False(t, deploymentEvent.HasImageChange())
		assert.True(t, deploymentEvent.HasScaleChange())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for deployment event")
	}
}

func TestProcessRawEvent_DeploymentDeleted(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	resourceEvent := domain.ResourceEvent{
		EventType: "DELETED",
		Timestamp: time.Now(),
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "default",
		UID:       "12345",
	}

	data, err := json.Marshal(resourceEvent)
	require.NoError(t, err)

	event := domain.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      data,
	}

	err = processor.ProcessRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should produce a deployment event
	select {
	case deploymentEvent := <-processor.deploymentEvents:
		require.NotNil(t, deploymentEvent)
		assert.Equal(t, domain.DeploymentScaled, deploymentEvent.Action)
		assert.Equal(t, int32(0), deploymentEvent.Metadata.NewReplicas)
		assert.Equal(t, "test-deployment", deploymentEvent.Name)
		assert.Equal(t, "default", deploymentEvent.Namespace)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for deployment event")
	}
}

func TestStart_ProcessesMultipleEvents(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	rawEvents := make(chan domain.RawEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor.Start(ctx, rawEvents)

	// Send multiple events
	for i := 0; i < 3; i++ {
		resourceEvent := domain.ResourceEvent{
			EventType: "ADDED",
			Timestamp: time.Now(),
			Kind:      "Deployment",
			Name:      fmt.Sprintf("deployment-%d", i),
			Namespace: "default",
			UID:       fmt.Sprintf("uid-%d", i),
		}

		data, err := json.Marshal(resourceEvent)
		require.NoError(t, err)

		rawEvents <- domain.RawEvent{
			Type:      "kubeapi",
			Timestamp: time.Now(),
			Data:      data,
		}
	}

	// Collect deployment events
	var events []*domain.DeploymentEvent
	for i := 0; i < 3; i++ {
		select {
		case event := <-processor.Events():
			events = append(events, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Timeout waiting for event %d", i)
		}
	}

	assert.Len(t, events, 3)
	for i, event := range events {
		assert.Equal(t, fmt.Sprintf("deployment-%d", i), event.Name)
		assert.Equal(t, domain.DeploymentCreated, event.Action)
	}
}

func TestGetStats(t *testing.T) {
	logger := zap.NewNop()
	processor, err := NewDeploymentProcessor(logger)
	require.NoError(t, err)

	stats := processor.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, 0, stats.ChannelSize)
	assert.Equal(t, 1000, stats.ChannelCapacity)
}
