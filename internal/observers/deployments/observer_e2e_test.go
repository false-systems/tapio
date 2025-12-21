package deployments

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 6: Full observer with BaseObserver

func TestNewDeploymentsObserver_CreatesSuccessfully(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		Namespace: "default",
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)
	require.NotNil(t, observer)
	assert.Equal(t, "deployments", observer.Name())
}

func TestNewDeploymentsObserver_ValidatesConfig(t *testing.T) {
	config := Config{
		// Missing Clientset
		Namespace: "default",
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset is required")
}

func TestDeploymentsObserver_Lifecycle(t *testing.T) {
	// Create fake clientset with a deployment
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Start observer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start should not error (informer will run in background)
	err = observer.Start(ctx)
	// Error expected when context times out
	assert.Error(t, err)

	// Stop observer (may fail if already stopped by context timeout)
	err = observer.Stop()
	// Accept either no error or "not running" error
	if err != nil {
		assert.Contains(t, err.Error(), "not running")
	}
}

func TestDeploymentsObserver_HandlesDeploymentCreate(t *testing.T) {
	// Create fake clientset
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Create a deployment
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	// Test the handler directly (informer wiring tested in integration)
	evt := createDomainEvent(nil, deployment)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_created", evt.Type)
	assert.Equal(t, "deployments", evt.Source)
	assert.Equal(t, "test-app", evt.K8sData.ResourceName)

	// Cleanup (may fail if not started)
	err = observer.Stop()
	// Accept either no error or "not running" error
	if err != nil {
		assert.Contains(t, err.Error(), "not running")
	}
}
