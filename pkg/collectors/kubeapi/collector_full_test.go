package kubeapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollector_New(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()

	collector, err := New(logger, config)
	assert.NoError(t, err)
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.traceManager)
	assert.NotNil(t, collector.events)
}

func TestCollector_Lifecycle(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.BufferSize = 10

	collector, err := New(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Check health
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Check not healthy after stop
	assert.False(t, collector.IsHealthy())
}

func TestCollector_HandleResourceEvent(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()

	collector := &Collector{
		logger:       logger,
		config:       config,
		clientset:    fake.NewSimpleClientset(),
		traceManager: NewTraceManager(),
		events:       make(chan collectors.RawEvent, 10),
		ctx:          context.Background(),
	}

	// Create test pod
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       types.UID("test-uid"),
			Labels: map[string]string{
				"app": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "test-deployment",
					UID:        types.UID("deployment-uid"),
				},
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "test-sa",
			NodeName:           "test-node",
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-config",
							},
						},
					},
				},
				{
					Name: "secret",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "test:latest",
				},
			},
		},
	}

	// Handle the event
	collector.handleResourceEvent("ADDED", "Pod", pod, nil)

	// Check event was sent
	select {
	case event := <-collector.events:
		assert.Equal(t, "k8s_ADDED", event.Type)
		assert.Equal(t, "kubeapi", event.Metadata["collector"])
		assert.Equal(t, "Pod", event.Metadata["k8s_kind"])
		assert.Equal(t, "test-pod", event.Metadata["k8s_name"])
		assert.Equal(t, "default", event.Metadata["k8s_namespace"])
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)

		// Verify event data
		var resourceEvent ResourceEvent
		err := json.Unmarshal(event.Data, &resourceEvent)
		require.NoError(t, err)

		assert.Equal(t, "ADDED", resourceEvent.EventType)
		assert.Equal(t, "Pod", resourceEvent.Kind)
		assert.Equal(t, "test-pod", resourceEvent.Name)
		assert.Equal(t, "default", resourceEvent.Namespace)

		// Check owner references
		assert.Len(t, resourceEvent.OwnerReferences, 1)
		assert.Equal(t, "Deployment", resourceEvent.OwnerReferences[0].Kind)
		assert.Equal(t, "test-deployment", resourceEvent.OwnerReferences[0].Name)

		// Check related objects
		assert.Len(t, resourceEvent.RelatedObjects, 4) // ConfigMap, Secret, ServiceAccount, Node

		// Verify relationships
		relations := make(map[string]string)
		for _, rel := range resourceEvent.RelatedObjects {
			relations[rel.Kind] = rel.Relation
		}
		assert.Equal(t, "mounts", relations["ConfigMap"])
		assert.Equal(t, "mounts", relations["Secret"])
		assert.Equal(t, "uses", relations["ServiceAccount"])
		assert.Equal(t, "scheduled-on", relations["Node"])

	case <-time.After(time.Second):
		t.Fatal("No event received")
	}
}

func TestCollector_ExtractRelationships(t *testing.T) {
	collector := &Collector{
		logger: zap.NewNop(),
	}

	tests := []struct {
		name     string
		obj      runtime.Object
		expected []ObjectReference
	}{
		{
			name: "pod with volumes",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "test-sa",
					NodeName:           "node-1",
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "app-config",
									},
								},
							},
						},
						{
							Name: "secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "app-secret",
								},
							},
						},
						{
							Name: "pvc",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "data-pvc",
								},
							},
						},
					},
				},
			},
			expected: []ObjectReference{
				{Kind: "ConfigMap", Name: "app-config", Namespace: "default", Relation: "mounts"},
				{Kind: "Secret", Name: "app-secret", Namespace: "default", Relation: "mounts"},
				{Kind: "PersistentVolumeClaim", Name: "data-pvc", Namespace: "default", Relation: "mounts"},
				{Kind: "ServiceAccount", Name: "test-sa", Namespace: "default", Relation: "uses"},
				{Kind: "Node", Name: "node-1", Relation: "scheduled-on"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &ResourceEvent{}
			collector.extractRelationships(event, tt.obj)

			assert.Len(t, event.RelatedObjects, len(tt.expected))

			// Check all expected relationships exist
			for _, expected := range tt.expected {
				found := false
				for _, actual := range event.RelatedObjects {
					if actual.Kind == expected.Kind &&
						actual.Name == expected.Name &&
						actual.Relation == expected.Relation {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected relationship not found: %+v", expected)
			}
		})
	}
}

func TestCollector_ShouldIgnoreNamespace(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		namespace    string
		shouldIgnore bool
	}{
		{
			name: "ignore system namespace",
			config: Config{
				IgnoreNamespaces: []string{"kube-system", "kube-public"},
			},
			namespace:    "kube-system",
			shouldIgnore: true,
		},
		{
			name: "allow user namespace",
			config: Config{
				IgnoreNamespaces: []string{"kube-system", "kube-public"},
			},
			namespace:    "default",
			shouldIgnore: false,
		},
		{
			name: "watch specific namespace only",
			config: Config{
				WatchNamespaces: []string{"production", "staging"},
			},
			namespace:    "production",
			shouldIgnore: false,
		},
		{
			name: "ignore namespace not in watch list",
			config: Config{
				WatchNamespaces: []string{"production", "staging"},
			},
			namespace:    "development",
			shouldIgnore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := &Collector{config: tt.config}
			result := collector.shouldIgnoreNamespace(tt.namespace)
			assert.Equal(t, tt.shouldIgnore, result)
		})
	}
}

func TestTraceManager(t *testing.T) {
	tm := NewTraceManager()

	// Create parent object
	parent := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	// Get trace for parent
	parentTrace := tm.GetOrCreateTrace(parent)
	assert.NotEmpty(t, parentTrace)

	// Create child with owner reference
	child := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "test-deployment",
				},
			},
		},
	}

	// First set the parent trace
	tm.SetTrace("Deployment", "default", "test-deployment", parentTrace)

	// Child should inherit parent's trace
	childTrace := tm.GetOrCreateTrace(child)
	assert.Equal(t, parentTrace, childTrace)

	// Test explicit propagation
	tm.PropagateTrace("Deployment", "default", "test-deployment", "Pod", "default", "test-pod-2")

	// Check metrics
	metrics := tm.GetMetrics()
	assert.GreaterOrEqual(t, metrics["total_objects"], 2)
	assert.GreaterOrEqual(t, metrics["unique_traces"], 1)
}

func TestObjectKey(t *testing.T) {
	tests := []struct {
		kind      string
		namespace string
		name      string
		expected  string
	}{
		{"Pod", "default", "test-pod", "Pod/default/test-pod"},
		{"Node", "", "node-1", "Node/node-1"},
		{"ClusterRole", "", "admin", "ClusterRole/admin"},
	}

	for _, tt := range tests {
		result := ObjectKey(tt.kind, tt.namespace, tt.name)
		assert.Equal(t, tt.expected, result)
	}
}
