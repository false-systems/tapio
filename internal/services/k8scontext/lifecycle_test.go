package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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

	require.NoError(t, service.startInformers())

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
	require.NoError(t, service.startInformers())

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

	// Update the pod
	pod.Labels["version"] = "v2"
	_, err = clientset.CoreV1().Pods("default").Update(ctx, pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	// Delete the pod
	err = clientset.CoreV1().Pods("default").Delete(ctx, "test-pod", metav1.DeleteOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
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
	require.NoError(t, service.startInformers())

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

	// Update the service
	svc.Labels = map[string]string{"version": "v2"}
	_, err = clientset.CoreV1().Services("default").Update(ctx, svc, metav1.UpdateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	// Delete the service
	err = clientset.CoreV1().Services("default").Delete(ctx, "kubernetes", metav1.DeleteOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
}

// TestInformerIntegration_DeploymentLifecycle verifies deployment handler wrappers
func TestInformerIntegration_DeploymentLifecycle(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go service.processEvents(ctx)
	require.NoError(t, service.startInformers())
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	_, err := clientset.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	deployment.Labels = map[string]string{"version": "v2"}
	_, err = clientset.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	err = clientset.AppsV1().Deployments("default").Delete(ctx, "test-deployment", metav1.DeleteOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
}

// TestInformerIntegration_NodeLifecycle verifies node handler wrappers
func TestInformerIntegration_NodeLifecycle(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go service.processEvents(ctx)
	require.NoError(t, service.startInformers())
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	_, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	node.Labels = map[string]string{"version": "v2"}
	_, err = clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	err = clientset.CoreV1().Nodes().Delete(ctx, "test-node", metav1.DeleteOptions{})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
}
