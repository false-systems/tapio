package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSerializePodInfo_Success verifies successful serialization
func TestSerializePodInfo_Success(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.244.1.10",
		HostIP:    "192.168.1.100",
		Labels:    map[string]string{"app": "nginx"},
	}

	data, err := serializePodInfo(podInfo)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "test-pod")
}

// TestSerializeServiceInfo_Success verifies successful serialization
func TestSerializeServiceInfo_Success(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "test-service",
		Namespace: "default",
		ClusterIP: "10.96.0.50",
		Type:      "ClusterIP",
		Labels:    map[string]string{"app": "web"},
	}

	data, err := serializeServiceInfo(serviceInfo)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "test-service")
}

// TestSerializeDeploymentInfo_Success verifies successful serialization
func TestSerializeDeploymentInfo_Success(t *testing.T) {
	deploymentInfo := DeploymentInfo{
		Name:      "test-deployment",
		Namespace: "default",
		Replicas:  3,
		Image:     "nginx:1.21",
		Labels:    map[string]string{"app": "nginx"},
	}

	data, err := serializeDeploymentInfo(deploymentInfo)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "test-deployment")
}

// TestSerializeNodeInfo_Success verifies successful serialization
func TestSerializeNodeInfo_Success(t *testing.T) {
	nodeInfo := NodeInfo{
		Name:   "worker-1",
		Labels: map[string]string{"role": "worker"},
		Zone:   "us-east-1a",
		Region: "us-east-1",
	}

	data, err := serializeNodeInfo(nodeInfo)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "worker-1")
}

// TestSerializeOwnerInfo_Success verifies successful serialization
func TestSerializeOwnerInfo_Success(t *testing.T) {
	ownerInfo := OwnerInfo{
		OwnerKind: "ReplicaSet",
		OwnerName: "nginx-rs",
		Namespace: "default",
	}

	data, err := serializeOwnerInfo(ownerInfo)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "ReplicaSet")
}

// TestToServiceInfo_NilLabels verifies nil labels are handled correctly
func TestToServiceInfo_NilLabels(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Labels:    nil,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.50",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	serviceInfo := toServiceInfo(service)
	assert.NotNil(t, serviceInfo.Labels)
	assert.Equal(t, 0, len(serviceInfo.Labels))
}

// TestToDeploymentInfo_NilLabels verifies nil labels are handled correctly
func TestToDeploymentInfo_NilLabels(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			Labels:    nil,
		},
		Spec: appsv1.DeploymentSpec{},
	}

	deploymentInfo := toDeploymentInfo(deployment)
	assert.NotNil(t, deploymentInfo.Labels)
	assert.Equal(t, 0, len(deploymentInfo.Labels))
}

// TestToNodeInfo_NilLabels verifies nil labels are handled correctly
func TestToNodeInfo_NilLabels(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "worker-1",
			Labels: nil,
		},
	}

	nodeInfo := toNodeInfo(node)
	assert.NotNil(t, nodeInfo.Labels)
	assert.Equal(t, 0, len(nodeInfo.Labels))
}

// TestToOwnerInfo_NoOwners verifies nil is returned when no owners
func TestToOwnerInfo_NoOwners(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "standalone-pod",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{}, // No owners
		},
	}

	ownerInfo := toOwnerInfo(pod)
	assert.Nil(t, ownerInfo)
}

// TestToOwnerInfo_ReplicaSetOwner verifies ReplicaSet owner is extracted
func TestToOwnerInfo_ReplicaSetOwner(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "nginx-rs"},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	assert.NotNil(t, ownerInfo)
	assert.Equal(t, "ReplicaSet", ownerInfo.OwnerKind)
	assert.Equal(t, "nginx-rs", ownerInfo.OwnerName)
}

// TestToOwnerInfo_DeploymentOwner verifies Deployment owner is extracted
func TestToOwnerInfo_DeploymentOwner(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "app-deployment"},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	assert.NotNil(t, ownerInfo)
	assert.Equal(t, "Deployment", ownerInfo.OwnerKind)
	assert.Equal(t, "app-deployment", ownerInfo.OwnerName)
}

// TestToOwnerInfo_StatefulSetOwner verifies StatefulSet owner is extracted
func TestToOwnerInfo_StatefulSetOwner(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "StatefulSet", Name: "db-statefulset"},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	assert.NotNil(t, ownerInfo)
	assert.Equal(t, "StatefulSet", ownerInfo.OwnerKind)
	assert.Equal(t, "db-statefulset", ownerInfo.OwnerName)
}

// TestToOwnerInfo_DaemonSetOwner verifies DaemonSet owner is extracted
func TestToOwnerInfo_DaemonSetOwner(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "logging-pod",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "fluentd"},
			},
		},
	}

	ownerInfo := toOwnerInfo(pod)
	assert.NotNil(t, ownerInfo)
	assert.Equal(t, "DaemonSet", ownerInfo.OwnerKind)
	assert.Equal(t, "fluentd", ownerInfo.OwnerName)
}

// TestMakeDeploymentKey verifies deployment key format
func TestMakeDeploymentKey(t *testing.T) {
	key := makeDeploymentKey("default", "nginx")
	assert.Equal(t, "deployment.default.nginx", key)
}

// TestMakeNodeKey verifies node key format
func TestMakeNodeKey(t *testing.T) {
	key := makeNodeKey("worker-1")
	assert.Equal(t, "node.worker-1", key)
}

// TestMakeOwnerKey verifies ownership key format
func TestMakeOwnerKey(t *testing.T) {
	key := makeOwnerKey("pod-123")
	assert.Equal(t, "ownership.pod-123", key)
}

// TestShouldSkipPod_WithIP verifies pod with IP is not skipped
func TestShouldSkipPod_WithIP(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{PodIP: "10.244.1.10"},
	}
	assert.False(t, shouldSkipPod(pod))
}

// TestShouldSkipPod_WithoutIP verifies pod without IP is skipped
func TestShouldSkipPod_WithoutIP(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{PodIP: ""},
	}
	assert.True(t, shouldSkipPod(pod))
}

// TestShouldSkipService_WithClusterIP verifies service with ClusterIP is not skipped
func TestShouldSkipService_WithClusterIP(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.50"},
	}
	assert.False(t, shouldSkipService(service))
}

// TestShouldSkipService_WithoutClusterIP verifies service without ClusterIP is skipped
func TestShouldSkipService_WithoutClusterIP(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{ClusterIP: ""},
	}
	assert.True(t, shouldSkipService(service))
}

// TestShouldSkipService_Headless verifies headless service is skipped
func TestShouldSkipService_Headless(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{ClusterIP: "None"},
	}
	assert.True(t, shouldSkipService(service))
}
