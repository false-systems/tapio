package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetectChanges_NoChanges(t *testing.T) {
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	changes := detectChanges(deployment, deployment)
	assert.Empty(t, changes)
}

func TestDetectChanges_ReplicaScale(t *testing.T) {
	oldReplicas := int32(3)
	newReplicas := int32(5)

	oldDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &oldReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	newDep := oldDep.DeepCopy()
	newDep.Spec.Replicas = &newReplicas

	changes := detectChanges(oldDep, newDep)
	require.Len(t, changes, 1)

	change := changes[0]
	assert.Equal(t, ChangeTypeScale, change.Type)
	assert.Equal(t, "spec.replicas", change.Field)
	assert.Equal(t, "3", change.OldValue)
	assert.Equal(t, "5", change.NewValue)
	assert.Contains(t, change.Description, "3 to 5")
}

func TestDetectChanges_ImageUpdate(t *testing.T) {
	replicas := int32(3)
	oldDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	newDep := oldDep.DeepCopy()
	newDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.22"

	changes := detectChanges(oldDep, newDep)
	require.Len(t, changes, 1)

	change := changes[0]
	assert.Equal(t, ChangeTypeImage, change.Type)
	assert.Equal(t, "spec.template.spec.containers[0].image", change.Field)
	assert.Equal(t, "nginx:1.21", change.OldValue)
	assert.Equal(t, "nginx:1.22", change.NewValue)
	assert.Contains(t, change.Description, "nginx:1.21")
	assert.Contains(t, change.Description, "nginx:1.22")
}

func TestDetectChanges_StrategyChange(t *testing.T) {
	replicas := int32(3)
	oldDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
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
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	newDep := oldDep.DeepCopy()
	newDep.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType

	changes := detectChanges(oldDep, newDep)
	require.Len(t, changes, 1)

	change := changes[0]
	assert.Equal(t, ChangeTypeStrategy, change.Type)
	assert.Equal(t, "spec.strategy.type", change.Field)
	assert.Contains(t, change.Description, "RollingUpdate")
	assert.Contains(t, change.Description, "Recreate")
}

func TestDetectChanges_ResourceLimits(t *testing.T) {
	replicas := int32(3)
	oldDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	newDep := oldDep.DeepCopy()
	newDep.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("1000m")

	changes := detectChanges(oldDep, newDep)
	require.Len(t, changes, 1)

	change := changes[0]
	assert.Equal(t, ChangeTypeResource, change.Type)
	assert.Contains(t, change.Field, "resources.limits.cpu")
	assert.Contains(t, change.Description, "CPU limit")
}

func TestDetectChanges_MultipleChanges(t *testing.T) {
	oldReplicas := int32(3)
	newReplicas := int32(5)
	oldDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &oldReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	newDep := oldDep.DeepCopy()
	newDep.Spec.Replicas = &newReplicas
	newDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.22"

	changes := detectChanges(oldDep, newDep)
	require.Len(t, changes, 2)

	// Should have both scale and image changes
	changeTypes := make(map[ChangeType]bool)
	for _, change := range changes {
		changeTypes[change.Type] = true
	}
	assert.True(t, changeTypes[ChangeTypeScale])
	assert.True(t, changeTypes[ChangeTypeImage])
}

func TestDetectChanges_NilDeployment(t *testing.T) {
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	// Test nil old deployment
	changes := detectChanges(nil, deployment)
	assert.Empty(t, changes)

	// Test nil new deployment
	changes = detectChanges(deployment, nil)
	assert.Empty(t, changes)

	// Test both nil
	changes = detectChanges(nil, nil)
	assert.Empty(t, changes)
}

func TestGetImpactLevel(t *testing.T) {
	tests := []struct {
		name     string
		changes  []Change
		expected string
	}{
		{
			name:     "no changes",
			changes:  []Change{},
			expected: "low",
		},
		{
			name: "image change",
			changes: []Change{
				{Type: ChangeTypeImage},
			},
			expected: "high",
		},
		{
			name: "scale change",
			changes: []Change{
				{Type: ChangeTypeScale},
			},
			expected: "medium",
		},
		{
			name: "resource change",
			changes: []Change{
				{Type: ChangeTypeResource},
			},
			expected: "high",
		},
		{
			name: "strategy change",
			changes: []Change{
				{Type: ChangeTypeStrategy},
			},
			expected: "high",
		},
		{
			name: "mixed changes with high priority",
			changes: []Change{
				{Type: ChangeTypeScale},
				{Type: ChangeTypeImage},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impact := getImpactLevel(tt.changes)
			assert.Equal(t, tt.expected, impact)
		})
	}
}

func TestGetPrimaryChangeType(t *testing.T) {
	tests := []struct {
		name     string
		changes  []Change
		expected string
	}{
		{
			name:     "no changes",
			changes:  []Change{},
			expected: "none",
		},
		{
			name: "image change",
			changes: []Change{
				{Type: ChangeTypeImage},
			},
			expected: "image",
		},
		{
			name: "scale change",
			changes: []Change{
				{Type: ChangeTypeScale},
			},
			expected: "scale",
		},
		{
			name: "image takes priority over scale",
			changes: []Change{
				{Type: ChangeTypeScale},
				{Type: ChangeTypeImage},
			},
			expected: "image",
		},
		{
			name: "scale takes priority over resource",
			changes: []Change{
				{Type: ChangeTypeResource},
				{Type: ChangeTypeScale},
			},
			expected: "scale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changeType := getPrimaryChangeType(tt.changes)
			assert.Equal(t, tt.expected, changeType)
		})
	}
}

func TestRequiresRestart(t *testing.T) {
	tests := []struct {
		name     string
		changes  []Change
		expected bool
	}{
		{
			name:     "no changes",
			changes:  []Change{},
			expected: false,
		},
		{
			name: "image change requires restart",
			changes: []Change{
				{Type: ChangeTypeImage},
			},
			expected: true,
		},
		{
			name: "config change requires restart",
			changes: []Change{
				{Type: ChangeTypeConfig},
			},
			expected: true,
		},
		{
			name: "resource change requires restart",
			changes: []Change{
				{Type: ChangeTypeResource},
			},
			expected: true,
		},
		{
			name: "scale change does not require restart",
			changes: []Change{
				{Type: ChangeTypeScale},
			},
			expected: false,
		},
		{
			name: "strategy change does not require restart",
			changes: []Change{
				{Type: ChangeTypeStrategy},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restart := requiresRestart(tt.changes)
			assert.Equal(t, tt.expected, restart)
		})
	}
}

func TestGetRelatedEventTypes(t *testing.T) {
	tests := []struct {
		name     string
		changes  []Change
		expected []string
	}{
		{
			name:     "no changes",
			changes:  []Change{},
			expected: []string{},
		},
		{
			name: "image change",
			changes: []Change{
				{Type: ChangeTypeImage},
			},
			expected: []string{"container.oom", "container.restart", "container.exit", "network.connection"},
		},
		{
			name: "scale change",
			changes: []Change{
				{Type: ChangeTypeScale},
			},
			expected: []string{"container.create", "network.connection", "memory.allocation"},
		},
		{
			name: "config change",
			changes: []Change{
				{Type: ChangeTypeConfig},
			},
			expected: []string{"container.restart", "k8s.configmap"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventTypes := getRelatedEventTypes(tt.changes)

			if len(tt.expected) == 0 {
				assert.Empty(t, eventTypes)
			} else {
				// Check that all expected types are present
				typeMap := make(map[string]bool)
				for _, et := range eventTypes {
					typeMap[et] = true
				}
				for _, expectedType := range tt.expected {
					assert.True(t, typeMap[expectedType], "Expected event type %s not found", expectedType)
				}
			}
		})
	}
}
