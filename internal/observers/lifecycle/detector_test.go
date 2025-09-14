package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetectDeploymentTransition(t *testing.T) {
	detector := NewTransitionDetector()

	tests := []struct {
		name           string
		old            *appsv1.Deployment
		new            *appsv1.Deployment
		expectedType   TransitionType
		expectedEffect string
		shouldDetect   bool
	}{
		{
			name:           "scale_to_zero",
			old:            createDeployment("app", 5),
			new:            createDeployment("app", 0),
			expectedType:   TransitionScaleToZero,
			expectedEffect: "service_unavailable",
			shouldDetect:   true,
		},
		{
			name:           "significant_scale_down",
			old:            createDeployment("app", 10),
			new:            createDeployment("app", 3),
			expectedType:   TransitionScaleDown,
			expectedEffect: "capacity_halved",
			shouldDetect:   true,
		},
		{
			name:         "minor_scale_down",
			old:          createDeployment("app", 10),
			new:          createDeployment("app", 8),
			shouldDetect: false, // Not significant enough
		},
		{
			name:         "scale_up",
			old:          createDeployment("app", 3),
			new:          createDeployment("app", 5),
			shouldDetect: false, // Scale up is not breaking
		},
		{
			name:           "deletion",
			old:            createDeployment("app", 5),
			new:            nil,
			expectedType:   TransitionDeletion,
			expectedEffect: "pods_terminating",
			shouldDetect:   true,
		},
		{
			name:           "image_update",
			old:            createDeploymentWithImage("app", 3, "v1.0"),
			new:            createDeploymentWithImage("app", 3, "v2.0"),
			expectedType:   TransitionImageUpdate,
			expectedEffect: "rolling_update",
			shouldDetect:   true,
		},
		{
			name:           "resource_cut",
			old:            createDeploymentWithResources("app", 3, "2", "4Gi"),
			new:            createDeploymentWithResources("app", 3, "1", "2Gi"),
			expectedType:   TransitionResourceCut,
			expectedEffect: "potential_oom",
			shouldDetect:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transition := detector.DetectTransition("Deployment", tt.old, tt.new)

			if !tt.shouldDetect {
				assert.Nil(t, transition)
				return
			}

			require.NotNil(t, transition)
			assert.Equal(t, tt.expectedType, transition.Type)

			// Check cascade effects
			if tt.expectedEffect != "" {
				found := false
				for _, effect := range transition.Cascade {
					if effect.Effect == tt.expectedEffect {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected cascade effect %s not found", tt.expectedEffect)
			}
		})
	}
}

func TestDetectPodTransition(t *testing.T) {
	detector := NewTransitionDetector()

	tests := []struct {
		name         string
		old          *corev1.Pod
		new          *corev1.Pod
		expectedType TransitionType
		shouldDetect bool
	}{
		{
			name:         "oom_kill",
			old:          createPod("pod", corev1.PodRunning, 0),
			new:          createPodWithOOMKill("pod"),
			expectedType: TransitionOOMKill,
			shouldDetect: true,
		},
		{
			name:         "crash_loop",
			old:          createPod("pod", corev1.PodRunning, 3),
			new:          createPod("pod", corev1.PodRunning, 6),
			expectedType: TransitionCrashLoop,
			shouldDetect: true,
		},
		{
			name:         "pod_failed",
			old:          createPod("pod", corev1.PodRunning, 0),
			new:          createPod("pod", corev1.PodFailed, 0),
			expectedType: TransitionNotReady,
			shouldDetect: true,
		},
		{
			name:         "eviction",
			old:          createEvictedPod("pod"),
			new:          nil,
			expectedType: TransitionEviction,
			shouldDetect: true,
		},
		{
			name:         "normal_deletion",
			old:          createPod("pod", corev1.PodRunning, 0),
			new:          nil,
			expectedType: TransitionDeletion,
			shouldDetect: true,
		},
		{
			name:         "minor_restart",
			old:          createPod("pod", corev1.PodRunning, 1),
			new:          createPod("pod", corev1.PodRunning, 2),
			shouldDetect: false, // Not enough restarts for crash loop
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transition := detector.DetectTransition("Pod", tt.old, tt.new)

			if !tt.shouldDetect {
				assert.Nil(t, transition)
				return
			}

			require.NotNil(t, transition)
			assert.Equal(t, tt.expectedType, transition.Type)
		})
	}
}

func TestDetectNodeTransition(t *testing.T) {
	detector := NewTransitionDetector()

	tests := []struct {
		name         string
		old          *corev1.Node
		new          *corev1.Node
		expectedType TransitionType
		shouldDetect bool
	}{
		{
			name:         "node_not_ready",
			old:          createNode("node1", corev1.ConditionTrue),
			new:          createNode("node1", corev1.ConditionFalse),
			expectedType: TransitionNotReady,
			shouldDetect: true,
		},
		{
			name:         "memory_pressure",
			old:          createNode("node1", corev1.ConditionTrue),
			new:          createNodeWithPressure("node1", corev1.NodeMemoryPressure),
			expectedType: TransitionNodePressure,
			shouldDetect: true,
		},
		{
			name:         "disk_pressure",
			old:          createNode("node1", corev1.ConditionTrue),
			new:          createNodeWithPressure("node1", corev1.NodeDiskPressure),
			expectedType: TransitionNodePressure,
			shouldDetect: true,
		},
		{
			name:         "node_deletion",
			old:          createNode("node1", corev1.ConditionTrue),
			new:          nil,
			expectedType: TransitionDeletion,
			shouldDetect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transition := detector.DetectTransition("Node", tt.old, tt.new)

			if !tt.shouldDetect {
				assert.Nil(t, transition)
				return
			}

			require.NotNil(t, transition)
			assert.Equal(t, tt.expectedType, transition.Type)
		})
	}
}

func TestIsBreaking(t *testing.T) {
	detector := NewTransitionDetector()

	tests := []struct {
		name       string
		transition *LifecycleTransition
		isBreaking bool
	}{
		{
			name: "scale_to_zero_is_breaking",
			transition: &LifecycleTransition{
				Type: TransitionScaleToZero,
			},
			isBreaking: true,
		},
		{
			name: "oom_kill_is_breaking",
			transition: &LifecycleTransition{
				Type: TransitionOOMKill,
			},
			isBreaking: true,
		},
		{
			name: "major_scale_down_is_breaking",
			transition: &LifecycleTransition{
				Type: TransitionScaleDown,
				State: StateChange{
					ReplicasBefore: 10,
					ReplicasAfter:  3,
				},
			},
			isBreaking: true,
		},
		{
			name: "minor_scale_down_not_breaking",
			transition: &LifecycleTransition{
				Type: TransitionScaleDown,
				State: StateChange{
					ReplicasBefore: 10,
					ReplicasAfter:  8,
				},
			},
			isBreaking: false,
		},
		{
			name: "image_update_not_breaking",
			transition: &LifecycleTransition{
				Type: TransitionImageUpdate,
			},
			isBreaking: false,
		},
		{
			name: "resource_cut_is_breaking",
			transition: &LifecycleTransition{
				Type: TransitionResourceCut,
			},
			isBreaking: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.IsBreaking(tt.transition)
			assert.Equal(t, tt.isBreaking, result)
		})
	}
}

// Helper functions to create test objects

func createDeployment(name string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "test-uid",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "container",
						Image: "image:latest",
					}},
				},
			},
		},
	}
}

func createDeploymentWithImage(name string, replicas int32, image string) *appsv1.Deployment {
	dep := createDeployment(name, replicas)
	dep.Spec.Template.Spec.Containers[0].Image = image
	return dep
}

func createDeploymentWithResources(name string, replicas int32, cpu, memory string) *appsv1.Deployment {
	dep := createDeployment(name, replicas)
	dep.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
	}
	return dep
}

func createPod(name string, phase corev1.PodPhase, restartCount int32) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "pod-uid",
		},
		Status: corev1.PodStatus{
			Phase: phase,
			ContainerStatuses: []corev1.ContainerStatus{{
				RestartCount: restartCount,
			}},
		},
	}
}

func createPodWithOOMKill(name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "pod-uid",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason: "OOMKilled",
					},
				},
			}},
		},
	}
}

func createEvictedPod(name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "pod-uid",
		},
		Status: corev1.PodStatus{
			Phase:  corev1.PodFailed,
			Reason: "Evicted",
		},
	}
}

func createNode(name string, ready corev1.ConditionStatus) *corev1.Node {
	return &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "node-uid",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: ready,
			}},
		},
	}
}

func createNodeWithPressure(name string, pressureType corev1.NodeConditionType) *corev1.Node {
	return &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "node-uid",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   pressureType,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}
