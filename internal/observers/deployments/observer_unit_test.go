package deployments

import (
	"fmt"
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
	assert.Equal(t, "deployment_created", evt.Subtype, "Subtype must be set for NATS routing")
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
	assert.Equal(t, "deployment_scaled", evt.Subtype, "Subtype must be set for NATS routing")
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
	assert.Equal(t, "deployment_available", evt.Subtype, "Subtype must be set for NATS routing")
	assert.Contains(t, evt.K8sData.Reason, "Available")
	assert.Contains(t, evt.K8sData.Message, "True")
}

func TestCreateDomainEvent_ImageUpdated(t *testing.T) {
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	new := createDeploymentWithImage("app", "myapp:v2.0.0")

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_image_updated", evt.Type)
	assert.Equal(t, "deployment_image_updated", evt.Subtype, "Subtype must be set for NATS routing")
	assert.True(t, evt.K8sData.ImageChanged)
	assert.Equal(t, "myapp:v1.0.0", evt.K8sData.OldImage)
	assert.Equal(t, "myapp:v2.0.0", evt.K8sData.NewImage)
}

func TestCreateDomainEvent_Namespace(t *testing.T) {
	deploy := createDeployment("app", 3, 3)
	deploy.Namespace = "production"

	evt := createDomainEvent(nil, deploy)
	require.NotNil(t, evt)
	assert.Equal(t, "production", evt.K8sData.ResourceNamespace)
}

func TestCreateDomainEvent_ContainerAdded(t *testing.T) {
	// When a sidecar is ADDED (not just changed)
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	new := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v1.0.0"})

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_image_updated", evt.Type)
	assert.True(t, evt.K8sData.ImageChanged)

	// Should capture the added container
	assert.Equal(t, "", evt.K8sData.OldImage, "OldImage should be empty (container didn't exist)")
	assert.Equal(t, "sidecar:v1.0.0", evt.K8sData.NewImage, "NewImage should be the added container")
}

func TestCreateDomainEvent_ContainerRemoved(t *testing.T) {
	// When a sidecar is REMOVED
	old := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v1.0.0"})
	new := createDeploymentWithImage("app", "myapp:v1.0.0")

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_image_updated", evt.Type)
	assert.True(t, evt.K8sData.ImageChanged)

	// Should capture the removed container
	assert.Equal(t, "sidecar:v1.0.0", evt.K8sData.OldImage, "OldImage should be the removed container")
	assert.Equal(t, "", evt.K8sData.NewImage, "NewImage should be empty (container no longer exists)")
}

func TestCreateDomainEvent_SidecarImageChange(t *testing.T) {
	// When ONLY sidecar image changes (not main app), verify correct images captured
	old := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v1.0.0"})
	new := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v2.0.0"})

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)
	assert.Equal(t, "deployment_image_updated", evt.Type)
	assert.True(t, evt.K8sData.ImageChanged)

	// CRITICAL: Should capture the CHANGED image (sidecar), not the first image (app)
	assert.Equal(t, "sidecar:v1.0.0", evt.K8sData.OldImage, "Should capture sidecar old image")
	assert.Equal(t, "sidecar:v2.0.0", evt.K8sData.NewImage, "Should capture sidecar new image")
}

func TestCreateDomainEvent_MultipleChangesPriority(t *testing.T) {
	// When multiple changes happen simultaneously, verify priority:
	// Image > Condition > Replica
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	old.Spec.Replicas = int32Ptr(1)
	old.Status.Conditions = []appsv1.DeploymentCondition{
		{Type: "Available", Status: corev1.ConditionFalse},
	}

	new := createDeploymentWithImage("app", "myapp:v2.0.0")
	new.Spec.Replicas = int32Ptr(5) // Replica changed
	new.Status.Conditions = []appsv1.DeploymentCondition{
		{Type: "Available", Status: corev1.ConditionTrue}, // Condition changed
	}

	evt := createDomainEvent(old, new)
	require.NotNil(t, evt)

	// Image change has highest priority
	assert.Equal(t, "deployment_image_updated", evt.Type)
	assert.Equal(t, "deployment_image_updated", evt.Subtype)

	// But all changes should be recorded in K8sData
	assert.True(t, evt.K8sData.ImageChanged)
	assert.Equal(t, "myapp:v1.0.0", evt.K8sData.OldImage)
	assert.Equal(t, "myapp:v2.0.0", evt.K8sData.NewImage)
	assert.True(t, evt.K8sData.ReplicasChanged)
	assert.Equal(t, int32(1), evt.K8sData.OldReplicas)
	assert.Equal(t, int32(5), evt.K8sData.NewReplicas)
}

// Helper for int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}

// TDD Cycle 7: Image change detection

func TestDetectImageChange_SingleContainer(t *testing.T) {
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	new := createDeploymentWithImage("app", "myapp:v1.1.0")

	changed, oldImages, newImages := detectImageChange(old, new)
	assert.True(t, changed)
	require.Len(t, oldImages, 1)
	require.Len(t, newImages, 1)
	assert.Equal(t, "myapp:v1.0.0", oldImages[0])
	assert.Equal(t, "myapp:v1.1.0", newImages[0])
}

func TestDetectImageChange_NoChange(t *testing.T) {
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	new := createDeploymentWithImage("app", "myapp:v1.0.0")

	changed, oldImages, newImages := detectImageChange(old, new)
	assert.False(t, changed)
	require.Len(t, oldImages, 1)
	require.Len(t, newImages, 1)
	assert.Equal(t, "myapp:v1.0.0", oldImages[0])
	assert.Equal(t, "myapp:v1.0.0", newImages[0])
}

func TestDetectImageChange_MultiContainer(t *testing.T) {
	old := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v1.0.0"})
	new := createDeploymentWithImages("app", []string{"myapp:v1.1.0", "sidecar:v1.0.0"})

	changed, oldImages, newImages := detectImageChange(old, new)
	assert.True(t, changed, "Should detect change when first container image changes")
	require.Len(t, oldImages, 2)
	require.Len(t, newImages, 2)
	assert.Equal(t, "myapp:v1.0.0", oldImages[0])
	assert.Equal(t, "myapp:v1.1.0", newImages[0])
}

func TestDetectImageChange_SidecarUpdated(t *testing.T) {
	old := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v1.0.0"})
	new := createDeploymentWithImages("app", []string{"myapp:v1.0.0", "sidecar:v2.0.0"})

	changed, oldImages, newImages := detectImageChange(old, new)
	assert.True(t, changed, "Should detect change when sidecar image changes")
	require.Len(t, oldImages, 2)
	require.Len(t, newImages, 2)
	assert.Equal(t, "sidecar:v1.0.0", oldImages[1])
	assert.Equal(t, "sidecar:v2.0.0", newImages[1])
}

func TestDetectImageChange_NilDeployments(t *testing.T) {
	changed, oldImages, newImages := detectImageChange(nil, nil)
	assert.False(t, changed)
	assert.Empty(t, oldImages)
	assert.Empty(t, newImages)
}

// Helper to create deployment with single image
func createDeploymentWithImage(name, image string) *appsv1.Deployment {
	return createDeploymentWithImages(name, []string{image})
}

// Helper to create deployment with multiple images
func createDeploymentWithImages(name string, images []string) *appsv1.Deployment {
	containers := make([]corev1.Container, len(images))
	for i, image := range images {
		containers[i] = corev1.Container{
			Name:  fmt.Sprintf("container-%d", i),
			Image: image,
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}
