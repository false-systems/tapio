package deployments

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestEndToEndDeploymentLifecycle tests the complete deployment event flow
func TestEndToEndDeploymentLifecycle(t *testing.T) {
	// Create observer with fake client
	config := DefaultConfig()
	config.MockMode = true // Start with mock mode
	config.BufferSize = 100
	config.DeduplicationWindow = 50 * time.Millisecond

	observer, err := NewObserver("e2e-test", config)
	require.NoError(t, err)

	// Replace with fake client and enable informers
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Test 1: Create deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "webapp",
				"version": "v1",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "webapp",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "webapp",
						"version": "v1",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "webapp",
							Image: "webapp:v1.0.0",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = fakeClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify create event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.deployment", string(event.Type))
		assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
		assert.Equal(t, "webapp", event.EventData.KubernetesEvent.InvolvedObject.Name)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "webapp:v1.0.0")
		t.Logf("✅ Create event received: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for create event")
	}

	// Test 2: Update deployment (new version)
	deployment.Spec.Template.Spec.Containers[0].Image = "webapp:v1.1.0"
	deployment.Annotations["deployment.kubernetes.io/revision"] = "2"
	_, err = fakeClient.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Verify update event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.deployment", string(event.Type))
		assert.Equal(t, "updated", event.EventData.KubernetesEvent.Action)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "webapp:v1.1.0")
		t.Logf("✅ Update event received: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update event")
	}

	// Test 3: Scale deployment
	deployment.Spec.Replicas = int32Ptr(5)
	_, err = fakeClient.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Verify scale event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.deployment", string(event.Type))
		// Could be either "scaled" or "updated"
		assert.Contains(t, []string{"scaled", "updated"}, event.EventData.KubernetesEvent.Action)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "replicas: 5")
		t.Logf("✅ Scale event received: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for scale event")
	}

	// Test 4: Rollback deployment
	deployment.Spec.Template.Spec.Containers[0].Image = "webapp:v1.0.0"
	deployment.Annotations["deployment.kubernetes.io/revision"] = "3"
	deployment.Annotations["deployment.kubernetes.io/rollback"] = "true"
	_, err = fakeClient.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Verify rollback event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.deployment", string(event.Type))
		assert.Equal(t, "rolled-back", event.EventData.KubernetesEvent.Action)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "webapp:v1.0.0")
		t.Logf("✅ Rollback event received: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for rollback event")
	}

	// Test 5: Delete deployment
	err = fakeClient.AppsV1().Deployments("default").Delete(ctx, "webapp", metav1.DeleteOptions{})
	require.NoError(t, err)

	// Verify delete event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.deployment", string(event.Type))
		assert.Equal(t, "deleted", event.EventData.KubernetesEvent.Action)
		assert.Equal(t, "webapp", event.EventData.KubernetesEvent.InvolvedObject.Name)
		t.Logf("✅ Delete event received: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for delete event")
	}

	// Verify observer health and statistics
	assert.True(t, observer.IsHealthy())
	stats := observer.Statistics()
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(5))
	assert.Equal(t, int64(0), stats.ErrorCount)
	t.Logf("📊 Final statistics: %d events processed, %d errors", stats.EventsProcessed, stats.ErrorCount)
}

// TestEndToEndWithConfigMaps tests ConfigMap tracking end-to-end
func TestEndToEndWithConfigMaps(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	config.BufferSize = 100
	config.TrackConfigMaps = true

	observer, err := NewObserver("e2e-cm", config)
	require.NoError(t, err)

	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"database": "postgres://localhost",
			"cache":    "redis://localhost",
		},
	}

	_, err = fakeClient.CoreV1().ConfigMaps("default").Create(ctx, cm, metav1.CreateOptions{})
	require.NoError(t, err)

	// Verify ConfigMap create event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.configmap", string(event.Type))
		assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
		assert.Equal(t, "app-config", event.EventData.KubernetesEvent.InvolvedObject.Name)
		t.Logf("✅ ConfigMap create event: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ConfigMap create event")
	}

	// Update ConfigMap
	cm.Data["cache"] = "redis://cache-server:6379"
	_, err = fakeClient.CoreV1().ConfigMaps("default").Update(ctx, cm, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Verify ConfigMap update event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.configmap", string(event.Type))
		assert.Equal(t, "updated", event.EventData.KubernetesEvent.Action)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "cache-server")
		t.Logf("✅ ConfigMap update event: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ConfigMap update event")
	}

	// Delete ConfigMap
	err = fakeClient.CoreV1().ConfigMaps("default").Delete(ctx, "app-config", metav1.DeleteOptions{})
	require.NoError(t, err)

	// Verify ConfigMap delete event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "k8s.configmap", string(event.Type))
		assert.Equal(t, "deleted", event.EventData.KubernetesEvent.Action)
		t.Logf("✅ ConfigMap delete event: %s", event.EventID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ConfigMap delete event")
	}
}

// TestEndToEndEventOrdering tests that events are processed in order
func TestEndToEndEventOrdering(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	config.BufferSize = 100

	observer, err := NewObserver("e2e-ordering", config)
	require.NoError(t, err)

	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create multiple deployments in quick succession
	for i := 1; i <= 3; i++ {
		deployment := createTestDeployment(string(rune('a'+i-1))+"-app", "default")
		_, err = fakeClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	// Collect events and verify ordering
	var receivedEvents []string
	for i := 0; i < 3; i++ {
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
			receivedEvents = append(receivedEvents, event.EventData.KubernetesEvent.InvolvedObject.Name)
			t.Logf("Event %d: %s", i+1, event.EventData.KubernetesEvent.InvolvedObject.Name)
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for event %d", i+1)
		}
	}

	// Events should be received in the order they were created
	assert.Equal(t, []string{"a-app", "b-app", "c-app"}, receivedEvents)
}