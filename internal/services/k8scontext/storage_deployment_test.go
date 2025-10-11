package k8scontext

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestStoreDeploymentMetadata verifies deployment metadata storage
func TestStoreDeploymentMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-deployment",
			Namespace: "default",
			Labels:    map[string]string{"app": "nginx"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "nginx:1.21"},
					},
				},
			},
		},
	}

	err := service.storeDeploymentMetadata(deployment)
	require.NoError(t, err)

	// Verify stored
	entry, err := mockKV.Get("deployment.default.nginx-deployment")
	require.NoError(t, err)

	var stored DeploymentInfo
	err = json.Unmarshal(entry.Value(), &stored)
	require.NoError(t, err)

	assert.Equal(t, "nginx-deployment", stored.Name)
	assert.Equal(t, "default", stored.Namespace)
	assert.Equal(t, int32(3), stored.Replicas)
	assert.Equal(t, "nginx:1.21", stored.Image)
	assert.Equal(t, "nginx", stored.Labels["app"])
}

// TestStoreNodeMetadata verifies node metadata storage
func TestStoreNodeMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"kubernetes.io/hostname":        "node-1",
				"topology.kubernetes.io/zone":   "us-east-1a",
				"topology.kubernetes.io/region": "us-east-1",
			},
		},
	}

	err := service.storeNodeMetadata(node)
	require.NoError(t, err)

	// Verify stored
	entry, err := mockKV.Get("node.node-1")
	require.NoError(t, err)

	var stored NodeInfo
	err = json.Unmarshal(entry.Value(), &stored)
	require.NoError(t, err)

	assert.Equal(t, "node-1", stored.Name)
	assert.Equal(t, "us-east-1a", stored.Zone)
	assert.Equal(t, "us-east-1", stored.Region)
}

// TestStoreOwnerMetadata verifies ownership tracking
func TestStoreOwnerMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pod",
			Namespace: "default",
			UID:       "abc-123",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "nginx-rs",
				},
			},
		},
	}

	err := service.storeOwnerMetadata(pod)
	require.NoError(t, err)

	// Verify stored
	entry, err := mockKV.Get("ownership.abc-123")
	require.NoError(t, err)

	var stored OwnerInfo
	err = json.Unmarshal(entry.Value(), &stored)
	require.NoError(t, err)

	assert.Equal(t, "ReplicaSet", stored.OwnerKind)
	assert.Equal(t, "nginx-rs", stored.OwnerName)
	assert.Equal(t, "default", stored.Namespace)
}

// TestStoreOwnerMetadata_NoOwner verifies pods without owners are skipped
func TestStoreOwnerMetadata_NoOwner(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "standalone-pod",
			Namespace:       "default",
			UID:             "xyz-789",
			OwnerReferences: []metav1.OwnerReference{}, // No owners
		},
	}

	err := service.storeOwnerMetadata(pod)
	require.NoError(t, err)

	// Verify nothing stored
	assert.Equal(t, 0, mockKV.len())
}

// TestDeleteDeploymentMetadata verifies deployment deletion
func TestDeleteDeploymentMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

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

	// Pre-populate
	service.storeDeploymentMetadata(deployment)
	require.Equal(t, 1, mockKV.len())

	err := service.deleteDeploymentMetadata(deployment)
	require.NoError(t, err)

	// Verify deleted
	_, err = mockKV.Get("deployment.default.test-deployment")
	assert.Error(t, err)
}

// TestDeleteNodeMetadata verifies node deletion
func TestDeleteNodeMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	// Pre-populate
	service.storeNodeMetadata(node)
	require.Equal(t, 1, mockKV.len())

	err := service.deleteNodeMetadata(node)
	require.NoError(t, err)

	// Verify deleted
	_, err = mockKV.Get("node.test-node")
	assert.Error(t, err)
}

// TestDeleteOwnerMetadata verifies owner metadata deletion
func TestDeleteOwnerMetadata(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pod",
			Namespace: "default",
			UID:       "abc-123",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "nginx-rs",
				},
			},
		},
	}

	// Pre-populate
	service.storeOwnerMetadata(pod)
	require.Equal(t, 1, mockKV.len())

	err := service.deleteOwnerMetadata(pod)
	require.NoError(t, err)

	// Verify deleted
	_, err = mockKV.Get("ownership.abc-123")
	assert.Error(t, err)
}

// TestToOwnerInfo_Deployment verifies direct deployment ownership
func TestToOwnerInfo_Deployment(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "nginx-deployment",
				},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	require.NotNil(t, ownerInfo)
	assert.Equal(t, "Deployment", ownerInfo.OwnerKind)
	assert.Equal(t, "nginx-deployment", ownerInfo.OwnerName)
	assert.Equal(t, "default", ownerInfo.Namespace)
}

// TestToOwnerInfo_StatefulSet verifies statefulset ownership
func TestToOwnerInfo_StatefulSet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "db-statefulset",
				},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	require.NotNil(t, ownerInfo)
	assert.Equal(t, "StatefulSet", ownerInfo.OwnerKind)
	assert.Equal(t, "db-statefulset", ownerInfo.OwnerName)
}

// TestToOwnerInfo_DaemonSet verifies daemonset ownership
func TestToOwnerInfo_DaemonSet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "daemon-pod",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "DaemonSet",
					Name: "daemon-set",
				},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	require.NotNil(t, ownerInfo)
	assert.Equal(t, "DaemonSet", ownerInfo.OwnerKind)
	assert.Equal(t, "daemon-set", ownerInfo.OwnerName)
}

// TestToDeploymentInfo_NoContainers verifies empty container handling
func TestToDeploymentInfo_NoContainers(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
		},
	}

	info := toDeploymentInfo(deployment)
	assert.Equal(t, "", info.Image)
	assert.Equal(t, "empty-deployment", info.Name)
	assert.Equal(t, int32(0), info.Replicas)
}

// TestToDeploymentInfo_NilReplicas verifies nil replicas handling
func TestToDeploymentInfo_NilReplicas(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: nil,
		},
	}

	info := toDeploymentInfo(deployment)
	assert.Equal(t, int32(0), info.Replicas)
}

// TestToNodeInfo_NoLabels verifies nil labels handling
func TestToNodeInfo_NoLabels(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: nil,
		},
	}

	info := toNodeInfo(node)
	assert.NotNil(t, info.Labels)
	assert.Equal(t, "", info.Zone)
	assert.Equal(t, "", info.Region)
}
