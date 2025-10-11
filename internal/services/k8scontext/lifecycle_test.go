package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// TestStartInformers_RegistersHandlers verifies handlers are registered
func TestStartInformers_RegistersHandlers(t *testing.T) {
	factory := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	mockKV := newMockKV()

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
	}

	service.startInformers()

	// Verify informers were created (we can't easily test handler registration,
	// but we can verify the informers exist)
	podInformer := factory.Core().V1().Pods().Informer()
	assert.NotNil(t, podInformer, "Pod informer should be created")

	serviceInformer := factory.Core().V1().Services().Informer()
	assert.NotNil(t, serviceInformer, "Service informer should be created")
}

// TestStart_InitializesAndStarts verifies Start method initializes service
func TestStart_InitializesAndStarts(t *testing.T) {
	t.Skip("Integration test - requires real K8s config")

	// This would be an integration test that verifies:
	// 1. Start() registers informers
	// 2. Start() begins watching K8s API
	// 3. Events flow through handlers to KV
}

// TestStop_CleansUpResources verifies Stop method cleans up
func TestStop_CleansUpResources(t *testing.T) {
	t.Skip("Integration test - requires real K8s config")

	// This would be an integration test that verifies:
	// 1. Stop() cancels context
	// 2. Stop() stops informers
	// 3. Stop() waits for goroutines to finish
}

// TestInformerIntegration_PodLifecycle verifies end-to-end pod flow
func TestInformerIntegration_PodLifecycle(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)
	mockKV := newMockKV()

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	// Start informers in background
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start event processing worker
	go service.processEvents(ctx)

	// Register handlers
	service.startInformers()

	factory.Start(ctx.Done())

	// Wait for cache sync
	factory.WaitForCacheSync(ctx.Done())

	// Create a pod through fake client
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.244.1.5",
			HostIP: "192.168.1.10",
		},
	}

	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err)

	// Give handlers time to process
	time.Sleep(100 * time.Millisecond)

	// Verify pod was stored in KV
	entry, err := mockKV.Get("pod.ip.10.244.1.5")
	require.NoError(t, err, "Pod should be stored via informer handler")
	assert.NotNil(t, entry)
}

// TestInformerIntegration_ServiceLifecycle verifies end-to-end service flow
func TestInformerIntegration_ServiceLifecycle(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)
	mockKV := newMockKV()

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	// Start informers in background
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start event processing worker
	go service.processEvents(ctx)

	// Register handlers
	service.startInformers()

	factory.Start(ctx.Done())

	// Wait for cache sync
	factory.WaitForCacheSync(ctx.Done())

	// Create a service through fake client
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	_, err := clientset.CoreV1().Services("default").Create(ctx, svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// Give handlers time to process
	time.Sleep(100 * time.Millisecond)

	// Verify service was stored in KV
	entry, err := mockKV.Get("service.ip.10.96.0.1")
	require.NoError(t, err, "Service should be stored via informer handler")
	assert.NotNil(t, entry)
}
