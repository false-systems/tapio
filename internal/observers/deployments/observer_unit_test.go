package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 1: Config validation

func TestConfig_Validate_RequiresClientset(t *testing.T) {
	config := Config{
		Namespace: "default",
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientset is required")
}

func TestConfig_Validate_Success(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		Namespace: "default",
	}

	err := config.Validate()
	assert.NoError(t, err)
}

func TestConfig_Validate_DefaultsNamespace(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		// Namespace empty - should default to all namespaces
	}

	err := config.Validate()
	assert.NoError(t, err)
}

// TDD Cycle 2: Event type detection

func TestDetectEventType_Created(t *testing.T) {
	newDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}

	eventType := detectEventType(nil, newDeploy)
	assert.Equal(t, "deployment_created", eventType)
}

func TestDetectEventType_Updated(t *testing.T) {
	oldDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}
	newDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}

	eventType := detectEventType(oldDeploy, newDeploy)
	assert.Equal(t, "deployment_updated", eventType)
}

func TestDetectEventType_Deleted(t *testing.T) {
	oldDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}

	eventType := detectEventType(oldDeploy, nil)
	assert.Equal(t, "deployment_deleted", eventType)
}
