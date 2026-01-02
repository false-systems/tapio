package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocality_String(t *testing.T) {
	tests := []struct {
		locality Locality
		expected string
	}{
		{LocalPod, "local_pod"},
		{ClusterService, "cluster_service"},
		{RemotePod, "remote_pod"},
		{External, "external"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.locality.String())
	}
}

func TestPodMeta_NamespacedName(t *testing.T) {
	pod := &PodMeta{
		Name:      "nginx",
		Namespace: "default",
	}
	assert.Equal(t, "default/nginx", pod.NamespacedName())
}

func TestContainerMeta_ShortID(t *testing.T) {
	c := ContainerMeta{
		ContainerID: "abc123def456789",
	}
	assert.Equal(t, "abc123def4", c.ShortID())

	// Short ID stays as-is
	c2 := ContainerMeta{ContainerID: "abc"}
	assert.Equal(t, "abc", c2.ShortID())
}
