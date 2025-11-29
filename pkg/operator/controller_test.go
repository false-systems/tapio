package operator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestReconcile_ObjectNotFound(t *testing.T) {
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

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "tapio-system",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

// TestReconcile_HandleFinalizerError tests error handling in Reconcile
func TestReconcile_HandleFinalizerError(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	// Create an observer that will be deleted (has DeletionTimestamp)
	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-observer",
			Namespace:  "tapio-system",
			Finalizers: []string{"tapio.io/finalizer"},
			// Set DeletionTimestamp to trigger deletion path
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: tapiov1alpha1.TapioObserverSpec{
			Image:           "ghcr.io/yairfalse/tapio:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			OTLPEndpoint:    "otel-collector:4317",
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

	// This should return without error (cleanup successful)
	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

// TestSetupWithManager is tested in integration tests
// We can't fully unit test this without a real manager
// Coverage is achieved through integration testing
