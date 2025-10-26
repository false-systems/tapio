package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestReconcile_FullFlow(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
		Spec: tapiov1alpha1.TapioObserverSpec{
			Image:           "ghcr.io/yairfalse/tapio:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			OTLPEndpoint:    "otel-collector:4317",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
			NetworkObserver: &tapiov1alpha1.NetworkObserverConfig{
				Enabled: true,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(observer).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	var sa corev1.ServiceAccount
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &sa)
	require.NoError(t, err)

	var cr rbacv1.ClusterRole
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "tapio-observer-tapio-system-test-observer",
	}, &cr)
	require.NoError(t, err)

	var crb rbacv1.ClusterRoleBinding
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "tapio-observer-tapio-system-test-observer",
	}, &crb)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer-config",
		Namespace: "tapio-system",
	}, &cm)
	require.NoError(t, err)

	var ds appsv1.DaemonSet
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &ds)
	require.NoError(t, err)

	var updatedObserver tapiov1alpha1.TapioObserver
	err = fakeClient.Get(context.Background(), req.NamespacedName, &updatedObserver)
	require.NoError(t, err)
	assert.Contains(t, updatedObserver.Finalizers, "tapio.io/finalizer")
}
