package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestReconcileDaemonSet_Creates(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
		Spec: tapiov1alpha1.TapioObserverSpec{
			Image:           "ghcr.io/yairfalse/tapio:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
	}

	err := reconciler.reconcileDaemonSet(context.Background(), observer)
	require.NoError(t, err)

	var ds appsv1.DaemonSet
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &ds)

	require.NoError(t, err)
	assert.Equal(t, "test-observer", ds.Name)

	// Verify eBPF privileges
	assert.True(t, ds.Spec.Template.Spec.HostNetwork)
	assert.Equal(t, "test-observer", ds.Spec.Template.Spec.ServiceAccountName)

	container := ds.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "ghcr.io/yairfalse/tapio:latest", container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.False(t, *container.SecurityContext.Privileged)
	assert.Contains(t, container.SecurityContext.Capabilities.Add, corev1.Capability("SYS_ADMIN"))
	assert.Contains(t, container.SecurityContext.Capabilities.Add, corev1.Capability("NET_ADMIN"))
	assert.Contains(t, container.SecurityContext.Capabilities.Add, corev1.Capability("SYS_RESOURCE"))

	// Verify volumes
	assert.Len(t, ds.Spec.Template.Spec.Volumes, 2)
	assert.Equal(t, "config", ds.Spec.Template.Spec.Volumes[0].Name)
	assert.Equal(t, "bpf-maps", ds.Spec.Template.Spec.Volumes[1].Name)
	assert.Equal(t, "/sys/fs/bpf", ds.Spec.Template.Spec.Volumes[1].HostPath.Path)

	// Verify volume mounts
	assert.Len(t, container.VolumeMounts, 2)
	assert.Equal(t, "config", container.VolumeMounts[0].Name)
	assert.Equal(t, "/etc/tapio", container.VolumeMounts[0].MountPath)
	assert.Equal(t, "bpf-maps", container.VolumeMounts[1].Name)
	assert.Equal(t, "/sys/fs/bpf", container.VolumeMounts[1].MountPath)
	assert.Equal(t, corev1.MountPropagationBidirectional, *container.VolumeMounts[1].MountPropagation)
}

func TestReconcileDaemonSet_Updates(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	existing := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "old"},
			},
		},
	}

	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
		Spec: tapiov1alpha1.TapioObserverSpec{
			Image:           "ghcr.io/yairfalse/tapio:v2.0",
			ImagePullPolicy: corev1.PullAlways,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, observer).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileDaemonSet(context.Background(), observer)
	require.NoError(t, err)

	var ds appsv1.DaemonSet
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &ds)

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/yairfalse/tapio:v2.0", ds.Spec.Template.Spec.Containers[0].Image)
}
