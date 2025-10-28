package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

// TDD Cycle 3: Replica change detection

func TestDetectReplicaChange_NoChange(t *testing.T) {
	old := createDeployment("app", 3, 3)
	new := createDeployment("app", 3, 3)

	changed, oldCount, newCount := detectReplicaChange(old, new)
	assert.False(t, changed)
	assert.Equal(t, int32(3), oldCount)
	assert.Equal(t, int32(3), newCount)
}

func TestDetectReplicaChange_ScaledUp(t *testing.T) {
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	changed, oldCount, newCount := detectReplicaChange(old, new)
	assert.True(t, changed)
	assert.Equal(t, int32(1), oldCount)
	assert.Equal(t, int32(5), newCount)
}

func TestDetectReplicaChange_ScaledDown(t *testing.T) {
	old := createDeployment("app", 10, 10)
	new := createDeployment("app", 2, 2)

	changed, oldCount, newCount := detectReplicaChange(old, new)
	assert.True(t, changed)
	assert.Equal(t, int32(10), oldCount)
	assert.Equal(t, int32(2), newCount)
}

// Helper to create deployment with replica counts
func createDeployment(name string, replicas, availableReplicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: availableReplicas,
		},
	}
}

// TDD Cycle 4: Deployment condition analysis

func TestDetectConditionChange_BecameAvailable(t *testing.T) {
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")

	changed, condType, status := detectConditionChange(old, new)
	assert.True(t, changed)
	assert.Equal(t, "Available", condType)
	assert.Equal(t, "True", status)
}

func TestDetectConditionChange_BecameUnavailable(t *testing.T) {
	old := createDeploymentWithCondition("app", "Available", "True")
	new := createDeploymentWithCondition("app", "Available", "False")

	changed, condType, status := detectConditionChange(old, new)
	assert.True(t, changed)
	assert.Equal(t, "Available", condType)
	assert.Equal(t, "False", status)
}

func TestDetectConditionChange_NoChange(t *testing.T) {
	old := createDeploymentWithCondition("app", "Available", "True")
	new := createDeploymentWithCondition("app", "Available", "True")

	changed, _, _ := detectConditionChange(old, new)
	assert.False(t, changed)
}

// Helper to create deployment with specific condition
func createDeploymentWithCondition(name, condType, status string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentConditionType(condType),
					Status: corev1.ConditionStatus(status),
				},
			},
		},
	}
}

// TDD Cycle 5: Domain event creation

func TestCreateDomainEvent_Created(t *testing.T) {
	deploy := createDeployment("app", 3, 3)

	evt := createDomainEvent(nil, deploy)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_created", evt.Type)
	assert.Equal(t, "deployments", evt.Source)
	assert.NotNil(t, evt.K8sData)
	assert.Equal(t, "Deployment", evt.K8sData.ResourceKind)
	assert.Equal(t, "app", evt.K8sData.ResourceName)
	assert.Equal(t, "created", evt.K8sData.Action)
}

func TestCreateDomainEvent_ScaledUp(t *testing.T) {
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_scaled", evt.Type)
	assert.True(t, evt.K8sData.ReplicasChanged)
	assert.Equal(t, int32(1), evt.K8sData.OldReplicas)
	assert.Equal(t, int32(5), evt.K8sData.NewReplicas)
}

func TestCreateDomainEvent_BecameAvailable(t *testing.T) {
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_available", evt.Type)
	assert.Contains(t, evt.K8sData.Reason, "Available")
	assert.Contains(t, evt.K8sData.Message, "True")
}
