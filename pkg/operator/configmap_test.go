package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func TestReconcileConfigMap_Creates(t *testing.T) {
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
			OTLPEndpoint: "otel-collector:4317",
			OTLPInsecure: true,
			NetworkObserver: &tapiov1alpha1.NetworkObserverConfig{
				Enabled:         true,
				InterfaceFilter: "eth0",
				BufferSize:      4096,
				MapSize:         10000,
			},
		},
	}

	err := reconciler.reconcileConfigMap(context.Background(), observer)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer-config",
		Namespace: "tapio-system",
	}, &cm)

	require.NoError(t, err)
	assert.Equal(t, "test-observer-config", cm.Name)
	assert.Equal(t, "otel-collector:4317", cm.Data["otlp_endpoint"])
	assert.Equal(t, "true", cm.Data["otlp_insecure"])
	assert.Equal(t, "true", cm.Data["network_enabled"])
	assert.Equal(t, "eth0", cm.Data["network_interface_filter"])
	assert.Equal(t, "4096", cm.Data["network_buffer_size"])
	assert.Equal(t, "10000", cm.Data["network_map_size"])
}

func TestReconcileConfigMap_Updates(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := tapiov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add tapio scheme: %v", err)
	}

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer-config",
			Namespace: "tapio-system",
		},
		Data: map[string]string{
			"otlp_endpoint": "old-endpoint:4317",
		},
	}

	observer := &tapiov1alpha1.TapioObserver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-observer",
			Namespace: "tapio-system",
		},
		Spec: tapiov1alpha1.TapioObserverSpec{
			OTLPEndpoint: "new-endpoint:4317",
			OTLPInsecure: false,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, observer).Build()

	reconciler := &TapioObserverReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := reconciler.reconcileConfigMap(context.Background(), observer)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-observer-config",
		Namespace: "tapio-system",
	}, &cm)

	require.NoError(t, err)
	assert.Equal(t, "new-endpoint:4317", cm.Data["otlp_endpoint"])
}
