package extraction

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sContextExtractor_Process(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid-123",
			Labels: map[string]string{
				"app":  "test",
				"tier": "backend",
			},
			Annotations: map[string]string{
				"prometheus.io/scrape": "true",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "test-rs-123",
					UID:        "rs-uid-123",
					Controller: ptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node-1",
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "test:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase:    corev1.PodRunning,
			PodIP:    "10.244.1.5",
			QOSClass: corev1.PodQOSBurstable,
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					ContainerID:  "docker://abc123def456",
					Ready:        true,
					RestartCount: 0,
				},
			},
		},
	}

	// Create fake client with test data
	k8sClient := fake.NewSimpleClientset(testPod)

	// Create extractor
	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	require.NoError(t, err)

	tests := []struct {
		name             string
		event            *domain.UnifiedEvent
		expectExtraction bool
		checkFunc        func(t *testing.T, event *domain.UnifiedEvent)
	}{
		{
			name: "Extract from container ID",
			event: &domain.UnifiedEvent{
				ID:        "test-1",
				Timestamp: time.Now(),
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					ContainerID: "abc123def456",
				},
			},
			expectExtraction: true,
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				require.NotNil(t, event.K8sContext)
				assert.Equal(t, "test-pod", event.K8sContext.Name)
				assert.Equal(t, "default", event.K8sContext.Namespace)
				assert.Equal(t, "test-node-1", event.K8sContext.NodeName)
				assert.Equal(t, "ReplicaSet", event.K8sContext.WorkloadKind)
				assert.Equal(t, "test-rs-123", event.K8sContext.WorkloadName)
			},
		},
		{
			name: "Extract from entity context",
			event: &domain.UnifiedEvent{
				ID:        "test-2",
				Timestamp: time.Now(),
				Source:    "k8s",
				Entity: &domain.EntityContext{
					Type:      "pod",
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectExtraction: true,
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				require.NotNil(t, event.K8sContext)
				assert.Equal(t, "test-pod", event.K8sContext.Name)
				assert.Equal(t, "Pod", event.K8sContext.Kind)
				assert.Equal(t, string(testPod.UID), event.K8sContext.UID)
				assert.Equal(t, testPod.Labels, event.K8sContext.Labels)
				assert.Equal(t, testPod.Annotations, event.K8sContext.Annotations)
			},
		},
		{
			name: "Deep extraction for critical event",
			event: &domain.UnifiedEvent{
				ID:        "test-3",
				Timestamp: time.Now(),
				Source:    "k8s",
				Severity:  domain.EventSeverityCritical,
				Entity: &domain.EntityContext{
					Type:      "pod",
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectExtraction: true,
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				require.NotNil(t, event.K8sContext)
				// Should have resource specs due to deep extraction
				assert.NotNil(t, event.K8sContext.ResourceRequests)
				assert.Equal(t, "100m", event.K8sContext.ResourceRequests["cpu"])
				assert.Equal(t, "128Mi", event.K8sContext.ResourceRequests["memory"])
				// Should have conditions
				assert.Len(t, event.K8sContext.Conditions, 1)
				assert.Equal(t, "Ready", event.K8sContext.Conditions[0].Type)
			},
		},
		{
			name: "Skip non-K8s event",
			event: &domain.UnifiedEvent{
				ID:        "test-4",
				Timestamp: time.Now(),
				Source:    "systemd",
				Application: &domain.ApplicationData{
					Message: "System log message",
				},
			},
			expectExtraction: false,
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Nil(t, event.K8sContext)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := extractor.Process(ctx, tt.event)
			assert.NoError(t, err)
			tt.checkFunc(t, tt.event)
		})
	}

	// Check metrics
	metrics := extractor.GetMetrics()
	assert.NotEmpty(t, metrics)
}

func TestK8sContextExtractor_ExtractionDepth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	k8sClient := fake.NewSimpleClientset()

	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected ExtractionDepth
	}{
		{
			name: "Critical severity gets deep extraction",
			event: &domain.UnifiedEvent{
				Severity: domain.EventSeverityCritical,
			},
			expected: Deep,
		},
		{
			name: "High anomaly score gets deep extraction",
			event: &domain.UnifiedEvent{
				Anomaly: &domain.AnomalyInfo{
					Score: 0.9,
				},
			},
			expected: Deep,
		},
		{
			name: "High business impact gets medium extraction",
			event: &domain.UnifiedEvent{
				Impact: &domain.ImpactContext{
					BusinessImpact: 0.8,
				},
			},
			expected: Medium,
		},
		{
			name: "Normal event gets shallow extraction",
			event: &domain.UnifiedEvent{
				Severity: domain.EventSeverityInfo,
			},
			expected: Shallow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depth := extractor.determineExtractionDepth(tt.event)
			assert.Equal(t, tt.expected, depth)
		})
	}
}

func TestK8sCache_Indexes(t *testing.T) {
	// Create test pod with multiple container IDs
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-container-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.10",
			PodIPs: []corev1.PodIP{
				{IP: "10.244.1.10"},
				{IP: "fd00::10"},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "app",
					ContainerID: "docker://container1",
				},
				{
					Name:        "sidecar",
					ContainerID: "docker://container2",
				},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "init",
					ContainerID: "docker://init-container",
				},
			},
		},
	}

	k8sClient := fake.NewSimpleClientset(testPod)
	cache, err := NewK8sCache(k8sClient)
	require.NoError(t, err)

	// Wait a bit for informers to sync
	time.Sleep(100 * time.Millisecond)

	// Test container ID lookup
	t.Run("Lookup by container ID", func(t *testing.T) {
		pod, err := cache.GetPodByContainerID("container1")
		assert.NoError(t, err)
		assert.NotNil(t, pod)
		assert.Equal(t, "multi-container-pod", pod.Name)

		pod, err = cache.GetPodByContainerID("container2")
		assert.NoError(t, err)
		assert.NotNil(t, pod)

		pod, err = cache.GetPodByContainerID("init-container")
		assert.NoError(t, err)
		assert.NotNil(t, pod)
	})

	// Test IP lookup
	t.Run("Lookup by IP", func(t *testing.T) {
		pod, err := cache.GetPodByIP("10.244.1.10")
		assert.NoError(t, err)
		assert.NotNil(t, pod)
		assert.Equal(t, "multi-container-pod", pod.Name)

		pod, err = cache.GetPodByIP("fd00::10")
		assert.NoError(t, err)
		assert.NotNil(t, pod)
	})
}

// Helper function
func ptr[T any](v T) *T {
	return &v
}
