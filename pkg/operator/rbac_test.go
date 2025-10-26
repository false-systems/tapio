package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestReconcileServiceAccount_Creates(t *testing.T) {
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
	}

	err := reconciler.reconcileServiceAccount(context.Background(), observer)
	require.NoError(t, err)

	var sa corev1.ServiceAccount
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &sa)

	require.NoError(t, err)
	assert.Equal(t, "test-observer", sa.Name)
	assert.Equal(t, "tapio-system", sa.Namespace)
}

func TestReconcileClusterRole_Creates(t *testing.T) {
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
	}

	err := reconciler.reconcileClusterRole(context.Background(), observer)
	require.NoError(t, err)

	var cr rbacv1.ClusterRole
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "tapio-observer-test-observer",
	}, &cr)

	require.NoError(t, err)
	assert.Equal(t, "tapio-observer-test-observer", cr.Name)
	assert.Len(t, cr.Rules, 2)
	assert.Equal(t, []string{"pods"}, cr.Rules[0].Resources)
	assert.Equal(t, []string{"events"}, cr.Rules[1].Resources)
}
