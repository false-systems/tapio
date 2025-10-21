package k8scontext

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// TestIntegration_FullLifecycle tests complete service lifecycle with real NATS KV
func TestIntegration_FullLifecycle(t *testing.T) {
	// Skip if NATS not available
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		t.Skip("Skipping integration test - set NATS_URL environment variable")
	}

	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err, "Failed to connect to NATS")
	defer nc.Close()

	// Create service with real NATS
	config := Config{
		NATSConn:        nc,
		KVBucket:        "test-k8s-context-" + time.Now().Format("20060102150405"),
		EventBufferSize: 100,
		MaxRetries:      3,
		RetryInterval:   100 * time.Millisecond,
	}

	// Use fake K8s client for testing
	clientset := fake.NewSimpleClientset()
	config.K8sConfig = &rest.Config{} // Fake K8s config for testing

	service, err := NewService(config)
	require.NoError(t, err, "Failed to create service")
	require.NotNil(t, service)

	// Start service
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = service.Start(ctx)
	require.NoError(t, err, "Failed to start service")
	defer func() { _ = service.Stop() }()

	// Give service time to initialize
	time.Sleep(200 * time.Millisecond)

	// Test pod lifecycle
	t.Run("PodLifecycle", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				UID:       "pod-123",
			},
			Status: corev1.PodStatus{
				PodIP: "10.244.1.100",
			},
		}

		// Create pod through fake client (triggers informer)
		_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for async processing
		time.Sleep(500 * time.Millisecond)

		// Verify stored in NATS KV
		js, _ := nc.JetStream()
		kv, _ := js.KeyValue(config.KVBucket)

		entry, err := kv.Get("pod.ip.10.244.1.100")
		assert.NoError(t, err, "Pod should be stored in NATS KV")
		if err == nil {
			assert.NotNil(t, entry)
		}

		// Delete pod
		err = clientset.CoreV1().Pods("default").Delete(ctx, "test-pod", metav1.DeleteOptions{})
		require.NoError(t, err)

		time.Sleep(500 * time.Millisecond)

		// Verify deleted from NATS KV
		_, err = kv.Get("pod.ip.10.244.1.100")
		assert.Error(t, err, "Pod should be deleted from NATS KV")
	})

	// Cleanup KV bucket
	js, _ := nc.JetStream()
	_ = js.DeleteKeyValue(config.KVBucket)
}

// TestIntegration_InformerSync tests informer cache synchronization
func TestIntegration_InformerSync(t *testing.T) {
	// This test uses fake K8s client without NATS to test informer behavior
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	// Pre-create resources
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.50",
		},
	}
	_, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start worker
	go service.processEvents(ctx)

	// Start informers
	require.NoError(t, service.startInformers())
	factory.Start(ctx.Done())

	// Wait for cache sync
	synced := factory.WaitForCacheSync(ctx.Done())
	for _, ok := range synced {
		assert.True(t, ok, "Informer should sync")
	}

	// Perform initial sync
	err = service.initialSync(ctx)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify existing pod was synced
	_, err = mockKV.Get("pod.ip.10.244.1.50")
	assert.NoError(t, err, "Existing pod should be synced to KV")
}

// TestIntegration_MultipleResourceTypes tests handling multiple resource types concurrently
func TestIntegration_MultipleResourceTypes(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 1000),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start worker
	go service.processEvents(ctx)

	// Start informers
	require.NoError(t, service.startInformers())
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	// Create multiple resource types

	// 1. Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-pod", Namespace: "default"},
		Status:     corev1.PodStatus{PodIP: "10.244.2.20"},
	}
	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err)

	// 2. Create service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "app-service", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.50"},
	}
	_, err = clientset.CoreV1().Services("default").Create(ctx, svc, metav1.CreateOptions{})
	require.NoError(t, err)

	// 3. Create deployment
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app-deployment", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	_, err = clientset.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// 4. Create node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-node-1",
			Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
			},
		},
	}
	_, err = clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for all events to process
	time.Sleep(1 * time.Second)

	// Verify all resources stored
	_, err = mockKV.Get("pod.ip.10.244.2.20")
	assert.NoError(t, err, "Pod should be stored")

	_, err = mockKV.Get("service.ip.10.96.0.50")
	assert.NoError(t, err, "Service should be stored")

	_, err = mockKV.Get("deployment.default.app-deployment")
	assert.NoError(t, err, "Deployment should be stored")

	_, err = mockKV.Get("node.worker-node-1")
	assert.NoError(t, err, "Node should be stored")
}

// TestIntegration_StopGracefully tests graceful shutdown
func TestIntegration_StopGracefully(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{
		kv:          mockKV,
		eventBuffer: make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.ctx = ctx
	service.cancel = cancel

	// Start worker
	go service.processEvents(ctx)

	// Enqueue some events
	for i := 0; i < 10; i++ {
		service.enqueueEvent(func() error {
			time.Sleep(10 * time.Millisecond) // Simulate work
			return nil
		})
	}

	// Stop service
	err := service.Stop()
	require.NoError(t, err)

	// Verify context was cancelled
	select {
	case <-service.ctx.Done():
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Context should be cancelled after Stop()")
	}
}
