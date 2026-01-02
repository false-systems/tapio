package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_AddPod(t *testing.T) {
	s := NewStore()

	pod := &PodMeta{
		UID:       "uid-1",
		Name:      "nginx",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		Containers: []ContainerMeta{
			{Name: "nginx", ContainerID: "abc123"},
		},
	}

	s.AddPod(pod)

	// Lookup by IP
	got, ok := s.PodByIP("10.0.1.5")
	require.True(t, ok)
	assert.Equal(t, "nginx", got.Name)

	// Lookup by container ID
	got, ok = s.PodByContainerID("abc123")
	require.True(t, ok)
	assert.Equal(t, "nginx", got.Name)

	// Lookup by name
	got, ok = s.PodByName("default", "nginx")
	require.True(t, ok)
	assert.Equal(t, "uid-1", got.UID)
}

func TestStore_DeletePod(t *testing.T) {
	s := NewStore()

	pod := &PodMeta{
		UID:       "uid-1",
		Name:      "nginx",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		Containers: []ContainerMeta{
			{Name: "nginx", ContainerID: "abc123"},
		},
	}

	s.AddPod(pod)
	s.DeletePod(pod)

	_, ok := s.PodByIP("10.0.1.5")
	assert.False(t, ok)

	_, ok = s.PodByContainerID("abc123")
	assert.False(t, ok)
}

func TestStore_AddService(t *testing.T) {
	s := NewStore()

	svc := &ServiceMeta{
		UID:       "svc-1",
		Name:      "nginx-svc",
		Namespace: "default",
		ClusterIP: "10.96.0.10",
	}

	s.AddService(svc)

	got, ok := s.ServiceByClusterIP("10.96.0.10")
	require.True(t, ok)
	assert.Equal(t, "nginx-svc", got.Name)

	got, ok = s.ServiceByName("default", "nginx-svc")
	require.True(t, ok)
	assert.Equal(t, "svc-1", got.UID)
}

func TestStore_Counts(t *testing.T) {
	s := NewStore()

	s.AddPod(&PodMeta{UID: "p1", Name: "a", Namespace: "ns"})
	s.AddPod(&PodMeta{UID: "p2", Name: "b", Namespace: "ns"})
	s.AddService(&ServiceMeta{UID: "s1", Name: "svc", Namespace: "ns"})

	assert.Equal(t, 2, s.PodCount())
	assert.Equal(t, 1, s.ServiceCount())
}
