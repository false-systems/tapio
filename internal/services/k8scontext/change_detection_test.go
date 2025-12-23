package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDetectDeploymentChanges_ImageChanged verifies image change detection
func TestDetectDeploymentChanges_ImageChanged(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "nginx:1.19"},
					},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "nginx:1.20"},
					},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectDeploymentChanges(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 1, "should emit exactly one event")
	event := emitter.Events()[0]
	assert.Equal(t, "deployment", event.Type)
	assert.Equal(t, "image_changed", event.Subtype)
	assert.Equal(t, "k8scontext", event.Source)
	assert.NotNil(t, event.K8sData)
	assert.True(t, event.K8sData.ImageChanged)
	assert.Equal(t, "nginx:1.19", event.K8sData.OldImage)
	assert.Equal(t, "nginx:1.20", event.K8sData.NewImage)
}

// TestDetectDeploymentChanges_ReplicasScaled verifies replica change detection
func TestDetectDeploymentChanges_ReplicasScaled(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldReplicas := int32(3)
	newReplicas := int32(5)

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &oldReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "nginx:1.19"}},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &newReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "nginx:1.19"}},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectDeploymentChanges(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 1, "should emit exactly one event")
	event := emitter.Events()[0]
	assert.Equal(t, "deployment", event.Type)
	assert.Equal(t, "scaled", event.Subtype)
	assert.NotNil(t, event.K8sData)
	assert.True(t, event.K8sData.ReplicasChanged)
	assert.Equal(t, int32(3), event.K8sData.OldReplicas)
	assert.Equal(t, int32(5), event.K8sData.NewReplicas)
}

// TestDetectDeploymentChanges_NoChanges verifies no events when nothing changed
func TestDetectDeploymentChanges_NoChanges(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "nginx:1.19"}},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectDeploymentChanges(ctx, deployment, deployment)

	assert.Empty(t, emitter.Events(), "should not emit events when nothing changed")
}

// TestDetectPodChanges_CrashLoop verifies crash loop detection
func TestDetectPodChanges_CrashLoop(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "nginx",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "nginx",
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectPodChanges(ctx, oldPod, newPod)

	require.Len(t, emitter.Events(), 1, "should emit crash loop event")
	event := emitter.Events()[0]
	assert.Equal(t, "pod", event.Type)
	assert.Equal(t, "crash_loop", event.Subtype)
	assert.NotNil(t, event.ContainerData)
	assert.Equal(t, "nginx", event.ContainerData.ContainerName)
	assert.Equal(t, int32(5), event.ContainerData.RestartCount)
}

// TestDetectPodChanges_OOMKilled verifies OOM detection
func TestDetectPodChanges_OOMKilled(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "nginx",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "nginx",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectPodChanges(ctx, oldPod, newPod)

	require.Len(t, emitter.Events(), 1, "should emit OOM event")
	event := emitter.Events()[0]
	assert.Equal(t, "pod", event.Type)
	assert.Equal(t, "oom_killed", event.Subtype)
	assert.NotNil(t, event.ContainerData)
	assert.Equal(t, "nginx", event.ContainerData.ContainerName)
	assert.Equal(t, int32(137), event.ContainerData.ExitCode)
}

// TestDetectServiceChanges_ClusterIPChanged verifies ClusterIP change detection
func TestDetectServiceChanges_ClusterIPChanged(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
		},
	}

	newService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.2",
		},
	}

	ctx := context.Background()
	service.detectServiceChanges(ctx, oldService, newService)

	require.Len(t, emitter.Events(), 1, "should emit ClusterIP change event")
	event := emitter.Events()[0]
	assert.Equal(t, "service", event.Type)
	assert.Equal(t, "ip_changed", event.Subtype)
}

// TestDetectNodeChanges_NotReady verifies node not ready detection
func TestDetectNodeChanges_NotReady(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionFalse,
					Message: "kubelet stopped posting node status",
				},
			},
		},
	}

	ctx := context.Background()
	service.detectNodeChanges(ctx, oldNode, newNode)

	require.Len(t, emitter.Events(), 1, "should emit node not ready event")
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "not_ready", event.Subtype)
}

// TestEmitDomainEvent_NilEmitter verifies graceful handling when emitter not configured
func TestEmitDomainEvent_NilEmitter(t *testing.T) {
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: nil, // No emitter configured
	}

	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "deployment",
		Subtype: "image_changed",
		Source:  "k8scontext",
		K8sData: &domain.K8sEventData{
			ResourceKind: "Deployment",
		},
	}

	ctx := context.Background()
	// Should not panic
	service.emitDomainEvent(ctx, event)
}

// TestEmitDomainEvent_MissingK8sData verifies validation
func TestEmitDomainEvent_MissingK8sData(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "deployment",
		Subtype: "image_changed",
		Source:  "k8scontext",
		K8sData: nil, // Missing K8s data
	}

	ctx := context.Background()
	service.emitDomainEvent(ctx, event)

	// Should not emit event with missing K8s data
	assert.Empty(t, emitter.Events(), "should not emit event without K8s data")
}

// TestGenerateEventID verifies unique event ID generation
func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "event IDs should be unique")
}

// TestGetContainerImage verifies image extraction from deployment
func TestGetContainerImage(t *testing.T) {
	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		want       string
	}{
		{
			name: "with container",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Image: "nginx:1.19"},
							},
						},
					},
				},
			},
			want: "nginx:1.19",
		},
		{
			name: "no containers",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{},
						},
					},
				},
			},
			want: "",
		},
		{
			name:       "nil deployment",
			deployment: nil,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getContainerImage(tt.deployment)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDetectRolloutStatus_Complete verifies rollout completion detection
func TestDetectRolloutStatus_Complete(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "ReplicaSetUpdated",
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentProgressing,
					Status:             corev1.ConditionTrue,
					Reason:             "NewReplicaSetAvailable",
					Message:            "ReplicaSet has successfully progressed",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectRolloutStatus(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 1, "should emit rollout complete event")
	event := emitter.Events()[0]
	assert.Equal(t, "deployment", event.Type)
	assert.Equal(t, "rollout_complete", event.Subtype)
	assert.Equal(t, "NewReplicaSetAvailable", event.K8sData.Reason)
}

// TestDetectRolloutStatus_Failed verifies rollout failure detection
func TestDetectRolloutStatus_Failed(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "ReplicaSetUpdated",
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentProgressing,
					Status:             corev1.ConditionFalse,
					Reason:             "ProgressDeadlineExceeded",
					Message:            "ReplicaSet has timed out progressing",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectRolloutStatus(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 1, "should emit rollout failed event")
	event := emitter.Events()[0]
	assert.Equal(t, "deployment", event.Type)
	assert.Equal(t, "rollout_failed", event.Subtype)
	assert.Equal(t, "ProgressDeadlineExceeded", event.K8sData.Reason)
}

// TestDetectRolloutStatus_Progressing verifies generic rollout progress detection
func TestDetectRolloutStatus_Progressing(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentProgressing,
					Status:             corev1.ConditionTrue,
					Reason:             "ReplicaSetUpdated",
					Message:            "Updated replica set",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectRolloutStatus(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 1, "should emit rollout progressing event")
	event := emitter.Events()[0]
	assert.Equal(t, "deployment", event.Type)
	assert.Equal(t, "rollout_progressing", event.Subtype)
}

// TestDetectPodChanges_PhaseChanged verifies pod phase transition detection
func TestDetectPodChanges_PhaseChanged(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	ctx := context.Background()
	service.detectPodChanges(ctx, oldPod, newPod)

	require.Len(t, emitter.Events(), 1, "should emit phase change event")
	event := emitter.Events()[0]
	assert.Equal(t, "pod", event.Type)
	assert.Equal(t, "phase_changed", event.Subtype)
	assert.Contains(t, event.K8sData.Message, "Pending -> Running")
}

// TestDetectPodChanges_MultipleCrashLoops verifies we don't emit duplicate crash loop events
func TestDetectPodChanges_MultipleCrashLoops(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "nginx",
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "nginx",
					RestartCount: 6,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectPodChanges(ctx, oldPod, newPod)

	// Should not emit event if already crashing
	assert.Empty(t, emitter.Events(), "should not emit duplicate crash loop event")
}

// TestDetectServiceChanges_TypeChanged verifies service type change detection
func TestDetectServiceChanges_TypeChanged(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	newService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	ctx := context.Background()
	service.detectServiceChanges(ctx, oldService, newService)

	require.Len(t, emitter.Events(), 1, "should emit type change event")
	event := emitter.Events()[0]
	assert.Equal(t, "service", event.Type)
	assert.Equal(t, "type_changed", event.Subtype)
	assert.Contains(t, event.K8sData.Message, "ClusterIP -> LoadBalancer")
}

// TestDetectNodeChanges_MemoryPressure verifies node memory pressure detection
func TestDetectNodeChanges_MemoryPressure(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeMemoryPressure,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeMemoryPressure,
					Status:  corev1.ConditionTrue,
					Message: "Node has memory pressure",
				},
			},
		},
	}

	ctx := context.Background()
	service.detectNodeChanges(ctx, oldNode, newNode)

	require.Len(t, emitter.Events(), 1, "should emit memory pressure event")
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "memory_pressure", event.Subtype)
}

// TestDetectNodeChanges_DiskPressure verifies node disk pressure detection
func TestDetectNodeChanges_DiskPressure(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeDiskPressure,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeDiskPressure,
					Status:  corev1.ConditionTrue,
					Message: "Node has disk pressure",
				},
			},
		},
	}

	ctx := context.Background()
	service.detectNodeChanges(ctx, oldNode, newNode)

	require.Len(t, emitter.Events(), 1, "should emit disk pressure event")
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "disk_pressure", event.Subtype)
}

// TestDetectDeploymentChanges_ImageAndReplicasBothChanged verifies multiple changes emit multiple events
func TestDetectDeploymentChanges_ImageAndReplicasBothChanged(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldReplicas := int32(3)
	newReplicas := int32(5)

	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &oldReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "nginx:1.19"}},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &newReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "nginx:1.20"}},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectDeploymentChanges(ctx, oldDeployment, newDeployment)

	require.Len(t, emitter.Events(), 2, "should emit both image change and replica scale events")

	// Find events by subtype
	var imageEvent, replicaEvent *domain.ObserverEvent
	for _, evt := range emitter.Events() {
		switch evt.Subtype {
		case "image_changed":
			imageEvent = evt
		case "scaled":
			replicaEvent = evt
		}
	}

	require.NotNil(t, imageEvent, "should have image change event")
	require.NotNil(t, replicaEvent, "should have replica scale event")

	assert.Equal(t, "nginx:1.19", imageEvent.K8sData.OldImage)
	assert.Equal(t, "nginx:1.20", imageEvent.K8sData.NewImage)
	assert.Equal(t, int32(3), replicaEvent.K8sData.OldReplicas)
	assert.Equal(t, int32(5), replicaEvent.K8sData.NewReplicas)
}

// TestEmitDomainEvent_EmitError verifies error handling during emission
func TestEmitDomainEvent_EmitError(t *testing.T) {
	emitter := intelligence.NewMock()
	emitter.SetEmitError(assert.AnError)
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "deployment",
		Subtype: "image_changed",
		Source:  "k8scontext",
		K8sData: &domain.K8sEventData{
			ResourceKind: "Deployment",
			ResourceName: "test-deployment",
		},
	}

	ctx := context.Background()
	// Should handle error gracefully (logs but doesn't panic)
	service.emitDomainEvent(ctx, event)

	// Event should not be added due to error
	assert.Empty(t, emitter.Events(), "event should not be added when emit fails")
}

// TestEmitDomainEvent_SetSource verifies source is auto-set
func TestEmitDomainEvent_SetSource(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "deployment",
		Subtype: "image_changed",
		Source:  "", // Empty source - should be set automatically
		K8sData: &domain.K8sEventData{
			ResourceKind: "Deployment",
		},
	}

	ctx := context.Background()
	service.emitDomainEvent(ctx, event)

	require.Len(t, emitter.Events(), 1)
	assert.Equal(t, "k8scontext", emitter.Events()[0].Source)
}

// TestDetectPodChanges_MultipleOOMKills verifies we don't emit duplicate OOM events
func TestDetectPodChanges_MultipleOOMKills(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "nginx",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
				},
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "nginx",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	service.detectPodChanges(ctx, oldPod, newPod)

	// Should not emit event if already OOMKilled
	assert.Empty(t, emitter.Events(), "should not emit duplicate OOM event")
}

// TestDetectServiceChanges_NoChange verifies no events when service unchanged
func TestDetectServiceChanges_NoChange(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	ctx := context.Background()
	service.detectServiceChanges(ctx, svc, svc)

	assert.Empty(t, emitter.Events(), "should not emit events when nothing changed")
}

// TestDetectNodeChanges_NoChange verifies no events when node unchanged
func TestDetectNodeChanges_NoChange(t *testing.T) {
	emitter := intelligence.NewMock()
	service := &Service{
		logger:  base.NewLogger("test"),
		emitter: emitter,
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	ctx := context.Background()
	service.detectNodeChanges(ctx, node, node)

	assert.Empty(t, emitter.Events(), "should not emit events when nothing changed")
}
