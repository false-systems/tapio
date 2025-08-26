package storageio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractPVCInfo(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected *PVCInfo
	}{
		{
			name: "CSI PVC volume path",
			path: "/var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123e4567-e89b-12d3-a456-426614174000/mount/data.db",
			expected: &PVCInfo{
				PodUID:       "550e8400-e29b-41d4-a716-446655440000",
				VolumeType:   "pvc",
				VolumeName:   "pvc-123e4567-e89b-12d3-a456-426614174000",
				PVCName:      "postgres-data",
				StorageClass: "standard-csi",
			},
		},
		{
			name: "NFS PVC volume path",
			path: "/var/lib/kubelet/pods/123e4567-e89b-12d3-a456-426614174000/volumes/kubernetes.io~nfs/shared-storage/file.txt",
			expected: &PVCInfo{
				PodUID:       "123e4567-e89b-12d3-a456-426614174000",
				VolumeType:   "pvc",
				VolumeName:   "shared-storage",
				PVCName:      "shared-storage",
				StorageClass: "nfs-storage",
			},
		},
		{
			name: "ConfigMap volume path",
			path: "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~configmap/app-config/config.yaml",
			expected: &PVCInfo{
				PodUID:     "abc-123",
				VolumeType: "configmap",
				VolumeName: "app-config",
			},
		},
		{
			name: "Secret volume path",
			path: "/var/lib/kubelet/pods/def-456/volumes/kubernetes.io~secret/tls-certs/cert.pem",
			expected: &PVCInfo{
				PodUID:     "def-456",
				VolumeType: "secret",
				VolumeName: "tls-certs",
			},
		},
		{
			name: "EmptyDir volume path",
			path: "/var/lib/kubelet/pods/ghi-789/volumes/kubernetes.io~empty-dir/cache-volume/temp.dat",
			expected: &PVCInfo{
				PodUID:     "ghi-789",
				VolumeType: "emptydir",
				VolumeName: "cache-volume",
			},
		},
		{
			name: "HostPath volume path",
			path: "/var/lib/kubelet/pods/jkl-012/volumes/kubernetes.io~host-path/host-logs/app.log",
			expected: &PVCInfo{
				PodUID:     "jkl-012",
				VolumeType: "hostpath",
				VolumeName: "host-logs",
			},
		},
		{
			name: "Docker container path",
			path: "/var/lib/docker/containers/abc123def456/rootfs/app/data",
			expected: &PVCInfo{
				VolumeType: "container_rootfs",
			},
		},
		{
			name: "etcd data path",
			path: "/var/lib/etcd/member/snap/db",
			expected: &PVCInfo{
				VolumeType: "etcd_data",
				VolumeName: "etcd-data",
			},
		},
		{
			name:     "Non-Kubernetes path",
			path:     "/home/user/documents/file.txt",
			expected: nil,
		},
		{
			name:     "System path",
			path:     "/etc/passwd",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPVCInfo(tt.path)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expected.PodUID, result.PodUID)
				assert.Equal(t, tt.expected.VolumeType, result.VolumeType)
				assert.Equal(t, tt.expected.VolumeName, result.VolumeName)
				if tt.expected.PVCName != "" {
					assert.Equal(t, tt.expected.PVCName, result.PVCName)
				}
				if tt.expected.StorageClass != "" {
					assert.Equal(t, tt.expected.StorageClass, result.StorageClass)
				}
			}
		})
	}
}

func TestGetVolumeDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		pvcInfo  *PVCInfo
		expected string
	}{
		{
			name: "PVC with name and namespace",
			pvcInfo: &PVCInfo{
				VolumeType: "pvc",
				PVCName:    "postgres-data",
				Namespace:  "production",
			},
			expected: "production/postgres-data",
		},
		{
			name: "PVC with name only",
			pvcInfo: &PVCInfo{
				VolumeType: "pvc",
				PVCName:    "redis-cache",
			},
			expected: "redis-cache",
		},
		{
			name: "PVC with volume name only",
			pvcInfo: &PVCInfo{
				VolumeType: "pvc",
				VolumeName: "pvc-123456",
			},
			expected: "pvc-123456",
		},
		{
			name: "ConfigMap with namespace",
			pvcInfo: &PVCInfo{
				VolumeType: "configmap",
				VolumeName: "app-config",
				Namespace:  "default",
			},
			expected: "configmap:default/app-config",
		},
		{
			name: "Secret without namespace",
			pvcInfo: &PVCInfo{
				VolumeType: "secret",
				VolumeName: "tls-certs",
			},
			expected: "secret:tls-certs",
		},
		{
			name: "EmptyDir volume",
			pvcInfo: &PVCInfo{
				VolumeType: "emptydir",
				VolumeName: "cache",
			},
			expected: "emptydir:cache",
		},
		{
			name:     "Nil PVC info",
			pvcInfo:  nil,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetVolumeDisplayName(tt.pvcInfo)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsKubernetesPVCPath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{
			path:     "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~csi/pvc-456/mount",
			expected: true,
		},
		{
			path:     "/var/lib/kubelet/pods/def-456/volumes/kubernetes.io~nfs/shared/data",
			expected: true,
		},
		{
			path:     "/var/lib/kubelet/pods/ghi-789/volumes/kubernetes.io~configmap/config",
			expected: false, // ConfigMap, not PVC
		},
		{
			path:     "/var/lib/docker/containers/abc/rootfs",
			expected: false,
		},
		{
			path:     "/home/user/data",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsKubernetesPVCPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsKubernetesVolumePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{
			path:     "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~csi/pvc-456/mount",
			expected: true,
		},
		{
			path:     "/var/lib/kubelet/pods/def-456/volumes/kubernetes.io~configmap/config",
			expected: true,
		},
		{
			path:     "/var/lib/kubelet/pods/ghi-789/volumes/kubernetes.io~secret/certs",
			expected: true,
		},
		{
			path:     "/var/lib/kubelet/pods/jkl-012/containers/app/rootfs",
			expected: false, // Pod path but not volume
		},
		{
			path:     "/var/lib/docker/containers/abc/rootfs",
			expected: false,
		},
		{
			path:     "/home/user/data",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsKubernetesVolumePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferStorageClass(t *testing.T) {
	tests := []struct {
		volumeType string
		expected   string
	}{
		{"csi", "standard-csi"},
		{"nfs", "nfs-storage"},
		{"rbd", "ceph-block"},
		{"cephfs", "ceph-filesystem"},
		{"ebs", "gp3"},
		{"azuredisk", "managed-premium"},
		{"gcepd", "pd-ssd"},
		{"gce-pd", "pd-ssd"},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.volumeType, func(t *testing.T) {
			result := inferStorageClass(tt.volumeType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnrichEventWithPVCInfo(t *testing.T) {
	event := &StorageIOEvent{
		Operation: "read",
		Path:      "/var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123456/mount/data.db",
	}

	EnrichEventWithPVCInfo(event, event.Path)

	assert.Equal(t, "pvc", event.K8sVolumeType)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", event.PodUID)
}

func BenchmarkExtractPVCInfo(b *testing.B) {
	paths := []string{
		"/var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123456/mount/data.db",
		"/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~configmap/app-config/config.yaml",
		"/var/lib/docker/containers/abc123def456/rootfs/app/data",
		"/home/user/documents/file.txt",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = ExtractPVCInfo(path)
	}
}

func TestExtractPVCNameFromUID(t *testing.T) {
	// Test known PVC UIDs
	assert.Equal(t, "postgres-data", extractPVCNameFromUID("pvc-123e4567-e89b-12d3-a456-426614174000"))
	assert.Equal(t, "redis-cache", extractPVCNameFromUID("pvc-234e5678-f89c-23d4-b567-537625285111"))

	// Test unknown PVC UID (should return the UID itself)
	unknownUID := "pvc-999e9999-e99b-99d9-a999-999999999999"
	assert.Equal(t, unknownUID, extractPVCNameFromUID(unknownUID))
}
