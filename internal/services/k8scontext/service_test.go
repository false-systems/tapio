package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestService_New(t *testing.T) {
	client := fake.NewClientset()
	cfg := Config{
		NodeName: "test-node",
	}

	svc := New(client, cfg)

	require.NotNil(t, svc)
	assert.Equal(t, "test-node", svc.nodeName)
	assert.False(t, svc.Ready())
}

func TestService_StartStop(t *testing.T) {
	client := fake.NewClientset()
	cfg := Config{NodeName: "test-node"}
	svc := New(client, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := svc.Start(ctx)
	require.NoError(t, err)

	// Wait for sync (may take a moment with fake client)
	require.Eventually(t, func() bool {
		return svc.Ready()
	}, 2*time.Second, 10*time.Millisecond, "service should become ready")

	// Stop
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestService_PodLookup(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "pod-1",
			Name:      "nginx",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.1.5",
			HostIP: "192.168.1.1",
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "nginx", ContainerID: "containerd://abc123"},
			},
		},
	}

	client := fake.NewClientset(pod)
	cfg := Config{NodeName: "test-node"}
	svc := New(client, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := svc.Start(ctx)
	require.NoError(t, err)

	// Wait for sync
	require.Eventually(t, func() bool {
		return svc.Ready()
	}, 2*time.Second, 10*time.Millisecond)

	// Lookup by IP
	got, ok := svc.PodByIP("10.0.1.5")
	require.True(t, ok, "pod should be found by IP")
	assert.Equal(t, "nginx", got.Name)

	// Lookup by container ID
	got, ok = svc.PodByContainerID("abc123")
	require.True(t, ok, "pod should be found by container ID")
	assert.Equal(t, "nginx", got.Name)
}

func TestService_ServiceLookup(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "svc-1",
			Name:      "nginx-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	client := fake.NewClientset(svc)
	cfg := Config{NodeName: "test-node"}
	service := New(client, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	// Wait for sync
	require.Eventually(t, func() bool {
		return service.Ready()
	}, 2*time.Second, 10*time.Millisecond)

	got, ok := service.ServiceByClusterIP("10.96.0.10")
	require.True(t, ok, "service should be found by ClusterIP")
	assert.Equal(t, "nginx-svc", got.Name)
}
