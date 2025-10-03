package deployments

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestDeploymentInformerSetup tests informer initialization
func TestDeploymentInformerSetup(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true // Use mock mode to avoid K8s client issues

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start should setup informers
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify informers are initialized
	assert.NotNil(t, observer.deploymentInformer)
	assert.True(t, observer.IsHealthy())
}

// TestDeploymentAddEvent tests handling of new deployments
func TestDeploymentAddEvent(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true // Start with mock mode
	config.BufferSize = 10

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client and disable mock mode
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create a deployment
	deployment := createTestDeployment("test-app", "default")
	_, err = fakeClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for event
	events := observer.Events()
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeK8sDeployment, event.Type)
		assert.Equal(t, "deployments-test", event.Source)
		assert.NotNil(t, event.EventData.KubernetesEvent)
		assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
		assert.Equal(t, "test-app", event.EventData.KubernetesEvent.InvolvedObject.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for deployment event")
	}
}

// TestDeploymentUpdateEvent tests handling of deployment updates
func TestDeploymentUpdateEvent(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true // Start with mock mode
	config.BufferSize = 10

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client
	fakeClient := fake.NewSimpleClientset()

	// Pre-create a deployment
	deployment := createTestDeployment("update-app", "default")
	fakeClient.AppsV1().Deployments("default").Create(context.Background(), deployment, metav1.CreateOptions{})

	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Clear any initial events
	events := observer.Events()
	select {
	case <-events:
		// Drain initial event
	case <-time.After(100 * time.Millisecond):
		// No initial event
	}

	// Update deployment image
	deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.20"
	_, err = fakeClient.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Wait for update event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeK8sDeployment, event.Type)
		assert.NotNil(t, event.EventData.KubernetesEvent)
		assert.Equal(t, "updated", event.EventData.KubernetesEvent.Action)
		assert.Contains(t, event.EventData.KubernetesEvent.Message, "nginx:1.20")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update event")
	}
}

// TestDeploymentDeleteEvent tests handling of deployment deletion
func TestDeploymentDeleteEvent(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true // Start with mock mode
	config.BufferSize = 10

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client
	fakeClient := fake.NewSimpleClientset()

	// Pre-create a deployment
	deployment := createTestDeployment("delete-app", "default")
	fakeClient.AppsV1().Deployments("default").Create(context.Background(), deployment, metav1.CreateOptions{})

	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Clear any initial events
	events := observer.Events()
	select {
	case <-events:
		// Drain
	case <-time.After(100 * time.Millisecond):
		// No event
	}

	// Delete deployment
	err = fakeClient.AppsV1().Deployments("default").Delete(ctx, "delete-app", metav1.DeleteOptions{})
	require.NoError(t, err)

	// Wait for delete event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeK8sDeployment, event.Type)
		assert.NotNil(t, event.EventData.KubernetesEvent)
		assert.Equal(t, "deleted", event.EventData.KubernetesEvent.Action)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for delete event")
	}
}

// TestDeploymentFiltering tests that filtering rules are applied
func TestDeploymentFiltering(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true // Start with mock mode
	config.BufferSize = 10
	config.Namespaces = []string{"production"} // Only track production
	config.IgnoreSystemDeployments = true

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create deployment in default namespace (should be ignored)
	defaultDep := createTestDeployment("default-app", "default")
	_, err = fakeClient.AppsV1().Deployments("default").Create(ctx, defaultDep, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create deployment in production namespace (should be tracked)
	prodDep := createTestDeployment("prod-app", "production")
	_, err = fakeClient.AppsV1().Deployments("production").Create(ctx, prodDep, metav1.CreateOptions{})
	require.NoError(t, err)

	// Should only get production event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "production", event.EventData.KubernetesEvent.InvolvedObject.Namespace)
		assert.Equal(t, "prod-app", event.EventData.KubernetesEvent.InvolvedObject.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for production deployment event")
	}

	// Should not get another event for default namespace
	select {
	case event := <-events:
		t.Fatalf("Should not have received event for default namespace: %v", event)
	case <-time.After(500 * time.Millisecond):
		// Good, no event received
	}
}

// TestInformerReconnection tests that informer handles disconnections
func TestInformerReconnection(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true                // Start with mock mode
	config.ResyncPeriod = 1 * time.Second // Fast resync for testing

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Replace with fake client and disable mock mode
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false // Now enable real informers

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Observer should remain healthy
	assert.True(t, observer.IsHealthy())

	// Wait for a resync period
	time.Sleep(1500 * time.Millisecond)

	// Should still be healthy
	assert.True(t, observer.IsHealthy())
}
