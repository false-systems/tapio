package deployments

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 6: Full observer with deps injection

func TestNew_CreatesSuccessfully(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		Namespace: "default",
	}
	deps := base.NewDeps(nil, nil)

	observer, err := New(config, deps)
	require.NoError(t, err)
	require.NotNil(t, observer)
	assert.Equal(t, "deployments", observer.name)
}

func TestNew_ValidatesConfig(t *testing.T) {
	config := Config{
		// Missing Clientset
		Namespace: "default",
	}
	deps := base.NewDeps(nil, nil)

	observer, err := New(config, deps)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset is required")
}

func TestDeploymentsObserver_Run(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}
	deps := base.NewDeps(nil, nil)

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Run with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run blocks until context cancelled, returns nil on clean shutdown
	err = observer.Run(ctx)
	assert.NoError(t, err)
}

func TestDeploymentsObserver_HandlesDeploymentCreate(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}
	deps := base.NewDeps(nil, nil)

	_, err := New(config, deps)
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
}
