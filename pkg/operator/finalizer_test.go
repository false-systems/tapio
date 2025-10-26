package operator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestHandleFinalizer_AddsFinalizer(t *testing.T) {
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
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(observer).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	shouldDelete, err := reconciler.handleFinalizer(context.Background(), observer)
	require.NoError(t, err)
	assert.False(t, shouldDelete)

	var updated tapiov1alpha1.TapioObserver
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer",
		Namespace: "tapio-system",
	}, &updated)

	require.NoError(t, err)
	assert.Contains(t, updated.Finalizers, "tapio.io/finalizer")
}

func TestHandleFinalizer_CleansUpOnDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	now := metav1.NewTime(time.Now())
	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-observer",
			Namespace:         "tapio-system",
			DeletionTimestamp: &now,
			Finalizers:        []string{"tapio.io/finalizer"},
		},
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tapio-observer-tapio-system-test-observer",
		},
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tapio-observer-tapio-system-test-observer",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(observer, cr, crb).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	shouldDelete, err := reconciler.handleFinalizer(context.Background(), observer)
	require.NoError(t, err)
	assert.True(t, shouldDelete)

	var deletedCR rbacv1.ClusterRole
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: cr.Name}, &deletedCR)
	assert.Error(t, err)

	var deletedCRB rbacv1.ClusterRoleBinding
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: crb.Name}, &deletedCRB)
	assert.Error(t, err)

	assert.NotContains(t, observer.Finalizers, "tapio.io/finalizer")
}
