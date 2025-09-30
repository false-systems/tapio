package deployments

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TestRealKubernetesIntegration tests the observer with a real Kubernetes cluster
// Run with: KUBECONFIG=/path/to/kubeconfig go test -tags=integration ./internal/observers/deployments/ -run TestRealKubernetesIntegration
func TestRealKubernetesIntegration(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run")
	}

	// Get kubeconfig from environment or default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		t.Skip("No kubeconfig found. Skipping integration test")
	}

	// Create real Kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err, "Failed to build config from kubeconfig")

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "Failed to create Kubernetes client")

	// Test namespace
	testNamespace := "tapio-test"

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	_, err = client.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil {
		// Namespace might already exist, that's ok
		t.Logf("Namespace creation: %v", err)
	}

	// Cleanup function
	defer func() {
		// Delete test namespace
		err := client.CoreV1().Namespaces().Delete(context.Background(), testNamespace, metav1.DeleteOptions{})
		if err != nil {
			t.Logf("Failed to cleanup namespace: %v", err)
		}
	}()

	// Create observer with real kubeconfig
	observerConfig := DefaultConfig()
	observerConfig.KubeConfig = kubeconfig
	observerConfig.Namespaces = []string{testNamespace}
	observerConfig.BufferSize = 100

	observer, err := NewObserver("integration-test", observerConfig)
	require.NoError(t, err, "Failed to create observer")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err, "Failed to start observer")
	defer observer.Stop()

	// Get events channel
	events := observer.Events()

	// Create a test deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: testNamespace,
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.14",
						},
					},
				},
			},
		},
	}

	t.Run("Create Deployment", func(t *testing.T) {
		// Create deployment
		_, err = client.AppsV1().Deployments(testNamespace).Create(ctx, deployment, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for create event
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "k8s.deployment", string(event.Type))
			assert.Contains(t, event.Source, "integration-test")
			assert.NotNil(t, event.EventData.KubernetesEvent)
			assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
			assert.Equal(t, "test-deployment", event.EventData.KubernetesEvent.InvolvedObject.Name)
			assert.Equal(t, testNamespace, event.EventData.KubernetesEvent.InvolvedObject.Namespace)
			t.Logf("Received deployment created event: %s", event.EventID)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for deployment created event")
		}
	})

	t.Run("Update Deployment", func(t *testing.T) {
		// Update deployment image
		deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.15"
		_, err = client.AppsV1().Deployments(testNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		// Wait for update event
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "k8s.deployment", string(event.Type))
			assert.NotNil(t, event.EventData.KubernetesEvent)
			assert.Equal(t, "updated", event.EventData.KubernetesEvent.Action)
			assert.Contains(t, event.EventData.KubernetesEvent.Message, "nginx:1.15")
			t.Logf("Received deployment updated event: %s", event.EventID)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for deployment updated event")
		}
	})

	t.Run("Scale Deployment", func(t *testing.T) {
		// Scale deployment
		deployment.Spec.Replicas = int32Ptr(3)
		_, err = client.AppsV1().Deployments(testNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		// Wait for scale event
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "k8s.deployment", string(event.Type))
			assert.NotNil(t, event.EventData.KubernetesEvent)
			// Should be "scaled" based on our logic
			assert.Contains(t, []string{"scaled", "updated"}, event.EventData.KubernetesEvent.Action)
			t.Logf("Received deployment scaled event: %s", event.EventID)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for deployment scaled event")
		}
	})

	t.Run("ConfigMap Tracking", func(t *testing.T) {
		if !observerConfig.TrackConfigMaps {
			t.Skip("ConfigMap tracking disabled")
		}

		// Create a ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"key": "value",
			},
		}

		_, err = client.CoreV1().ConfigMaps(testNamespace).Create(ctx, cm, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for ConfigMap event
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "k8s.configmap", string(event.Type))
			assert.NotNil(t, event.EventData.KubernetesEvent)
			assert.Equal(t, "created", event.EventData.KubernetesEvent.Action)
			assert.Equal(t, "test-config", event.EventData.KubernetesEvent.InvolvedObject.Name)
			t.Logf("Received ConfigMap created event: %s", event.EventID)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for ConfigMap created event")
		}

		// Cleanup ConfigMap
		client.CoreV1().ConfigMaps(testNamespace).Delete(ctx, "test-config", metav1.DeleteOptions{})
	})

	t.Run("Delete Deployment", func(t *testing.T) {
		// Delete deployment
		err = client.AppsV1().Deployments(testNamespace).Delete(ctx, "test-deployment", metav1.DeleteOptions{})
		require.NoError(t, err)

		// Wait for delete event
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "k8s.deployment", string(event.Type))
			assert.NotNil(t, event.EventData.KubernetesEvent)
			assert.Equal(t, "deleted", event.EventData.KubernetesEvent.Action)
			t.Logf("Received deployment deleted event: %s", event.EventID)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for deployment deleted event")
		}
	})

	// Check observer health
	assert.True(t, observer.IsHealthy(), "Observer should be healthy")

	// Check statistics
	stats := observer.Statistics()
	assert.Greater(t, stats.EventsProcessed, int64(0), "Should have processed events")
	t.Logf("Observer statistics: %d events processed, %d errors", stats.EventsProcessed, stats.ErrorCount)
}

// Helper function for int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}

// TestRealKubernetesWithRollback tests rollback detection with real cluster
func TestRealKubernetesWithRollback(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run")
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}

	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		t.Skip("No kubeconfig found")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	testNamespace := "tapio-rollback-test"

	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	client.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})

	defer func() {
		client.CoreV1().Namespaces().Delete(context.Background(), testNamespace, metav1.DeleteOptions{})
	}()

	// Create observer
	observerConfig := DefaultConfig()
	observerConfig.KubeConfig = kubeconfig
	observerConfig.Namespaces = []string{testNamespace}

	observer, err := NewObserver("rollback-test", observerConfig)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create deployment with revision annotation
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollback-test",
			Namespace: testNamespace,
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "rollback",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "rollback",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.14",
						},
					},
				},
			},
		},
	}

	// Create initial deployment
	_, err = client.AppsV1().Deployments(testNamespace).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// Drain create event
	select {
	case <-events:
		// Consumed
	case <-time.After(5 * time.Second):
		t.Fatal("No create event")
	}

	// Simulate rollback by decreasing revision
	deployment.Annotations["deployment.kubernetes.io/revision"] = "2"
	deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.15"
	_, err = client.AppsV1().Deployments(testNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Drain update event
	select {
	case <-events:
		// Consumed
	case <-time.After(5 * time.Second):
		t.Fatal("No update event")
	}

	// Now rollback
	deployment.Annotations["deployment.kubernetes.io/revision"] = "1"
	deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.14"
	_, err = client.AppsV1().Deployments(testNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Wait for rollback event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.NotNil(t, event.EventData.KubernetesEvent)
		assert.Equal(t, "rolled-back", event.EventData.KubernetesEvent.Action)
		t.Logf("Detected rollback event: %s", event.EventID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for rollback event")
	}

	// Cleanup
	client.AppsV1().Deployments(testNamespace).Delete(ctx, "rollback-test", metav1.DeleteOptions{})
}