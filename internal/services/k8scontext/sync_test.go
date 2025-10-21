package k8scontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// TestSyncServices_Success verifies service synchronization
func TestSyncServices_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	// Pre-create services
	svc1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.10"},
	}
	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc2", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.20"},
	}
	_, err := clientset.CoreV1().Services("default").Create(context.Background(), svc1, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = clientset.CoreV1().Services("default").Create(context.Background(), svc2, metav1.CreateOptions{})
	require.NoError(t, err)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config:          Config{},
	}

	// Start informers and wait for sync
	factory.Start(context.Background().Done())
	factory.WaitForCacheSync(context.Background().Done())

	// Sync services - should complete without error
	err = service.syncServices(context.Background())
	assert.NoError(t, err, "syncServices should not error")
}

// TestSyncDeployments_Success verifies deployment synchronization
func TestSyncDeployments_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	// Pre-create deployments
	replicas := int32(2)
	deploy1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy1", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	deploy2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy2", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	_, err := clientset.AppsV1().Deployments("default").Create(context.Background(), deploy1, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = clientset.AppsV1().Deployments("default").Create(context.Background(), deploy2, metav1.CreateOptions{})
	require.NoError(t, err)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config:          Config{},
	}

	// Start informers and wait for sync
	factory.Start(context.Background().Done())
	factory.WaitForCacheSync(context.Background().Done())

	// Sync deployments - should complete without error
	err = service.syncDeployments(context.Background())
	assert.NoError(t, err, "syncDeployments should not error")
}

// TestSyncNodes_Success verifies node synchronization
func TestSyncNodes_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	// Pre-create nodes
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node2"},
	}
	_, err := clientset.CoreV1().Nodes().Create(context.Background(), node1, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = clientset.CoreV1().Nodes().Create(context.Background(), node2, metav1.CreateOptions{})
	require.NoError(t, err)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config:          Config{},
	}

	// Start informers and wait for sync
	factory.Start(context.Background().Done())
	factory.WaitForCacheSync(context.Background().Done())

	// Sync nodes - should complete without error
	err = service.syncNodes(context.Background())
	assert.NoError(t, err, "syncNodes should not error")
}

// TestInitialSync_Success verifies initial sync orchestration
func TestInitialSync_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	mockKV := newMockKV()
	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config:          Config{},
	}

	// Start informers and wait for sync
	ctx := context.Background()
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	// Run initial sync - should orchestrate all sync functions
	err := service.initialSync(ctx)
	assert.NoError(t, err, "initialSync should not error")
}
