package k8scontext

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockKVWithErrors wraps mockKV to simulate failures
type mockKVWithErrors struct {
	inner       *mockKV
	putError    error
	deleteError error
	getError    error
}

func newMockKVWithErrors() *mockKVWithErrors {
	return &mockKVWithErrors{
		inner: newMockKV(),
	}
}

func (m *mockKVWithErrors) Put(key string, value []byte) (uint64, error) {
	if m.putError != nil {
		return 0, m.putError
	}
	return m.inner.Put(key, value)
}

func (m *mockKVWithErrors) Delete(key string, opts ...nats.DeleteOpt) error {
	if m.deleteError != nil {
		return m.deleteError
	}
	return m.inner.Delete(key, opts...)
}

func (m *mockKVWithErrors) Get(key string) (nats.KeyValueEntry, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	return m.inner.Get(key)
}

// Implement remaining nats.KeyValue interface methods by delegating to inner
func (m *mockKVWithErrors) Bucket() string { return m.inner.Bucket() }
func (m *mockKVWithErrors) Create(k string, v []byte) (uint64, error) {
	return m.inner.Create(k, v)
}
func (m *mockKVWithErrors) Update(k string, v []byte, r uint64) (uint64, error) {
	return m.inner.Update(k, v, r)
}
func (m *mockKVWithErrors) PutString(k string, v string) (uint64, error) {
	return m.inner.PutString(k, v)
}
func (m *mockKVWithErrors) GetRevision(k string, r uint64) (nats.KeyValueEntry, error) {
	return m.inner.GetRevision(k, r)
}
func (m *mockKVWithErrors) Purge(k string, opts ...nats.DeleteOpt) error {
	return m.inner.Purge(k, opts...)
}
func (m *mockKVWithErrors) Watch(k string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return m.inner.Watch(k, opts...)
}
func (m *mockKVWithErrors) WatchAll(opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return m.inner.WatchAll(opts...)
}
func (m *mockKVWithErrors) WatchFiltered(keys []string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return m.inner.WatchFiltered(keys, opts...)
}
func (m *mockKVWithErrors) Keys(opts ...nats.WatchOpt) ([]string, error) {
	return m.inner.Keys(opts...)
}
func (m *mockKVWithErrors) ListKeys(opts ...nats.WatchOpt) (nats.KeyLister, error) {
	return m.inner.ListKeys(opts...)
}
func (m *mockKVWithErrors) History(k string, opts ...nats.WatchOpt) ([]nats.KeyValueEntry, error) {
	return m.inner.History(k, opts...)
}
func (m *mockKVWithErrors) PurgeDeletes(opts ...nats.PurgeOpt) error {
	return m.inner.PurgeDeletes(opts...)
}
func (m *mockKVWithErrors) Status() (nats.KeyValueStatus, error) {
	return m.inner.Status()
}

// TestNegative_StorePodMetadata_PutFailure verifies error handling when KV.Put fails
func TestNegative_StorePodMetadata_PutFailure(t *testing.T) {
	mockKV := newMockKVWithErrors()
	mockKV.putError = errors.New("nats: connection closed")
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
		},
	}

	err := service.storePodMetadata(pod)
	assert.Error(t, err, "Should error when KV.Put fails")
	assert.Contains(t, err.Error(), "connection closed")
}

// TestNegative_DeletePodMetadata_DeleteFailure verifies multi-index deletion with errors
func TestNegative_DeletePodMetadata_DeleteFailure(t *testing.T) {
	mockKV := newMockKVWithErrors()
	mockKV.deleteError = errors.New("nats: timeout")

	// Create service with logger (needed for error logging)
	logger := base.NewLogger("test")
	service := &Service{
		kv:     mockKV,
		logger: logger,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "abc-123",
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
		},
	}

	// Multi-index deletion logs errors but doesn't fail
	// This is intentional: we want to try deleting from all indexes
	err := service.deletePodMetadata(pod)
	assert.NoError(t, err, "Multi-index deletion should not return error (logs warnings instead)")
}

// TestNegative_StoreServiceMetadata_PutFailure verifies service storage error handling
func TestNegative_StoreServiceMetadata_PutFailure(t *testing.T) {
	mockKV := newMockKVWithErrors()
	mockKV.putError = errors.New("nats: insufficient storage")
	service := &Service{kv: mockKV}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
		},
	}

	err := service.storeServiceMetadata(svc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient storage")
}

// TestNegative_HandlePodAdd_InvalidType verifies handler rejects invalid types
func TestNegative_HandlePodAdd_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{
		kv:          mockKV,
		eventBuffer: make(chan func() error, 10),
	}

	// Pass invalid type (Service instead of Pod)
	invalidObj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "not-a-pod",
		},
	}

	service.handlePodAdd(invalidObj)

	// Should not enqueue event for invalid type
	assert.Equal(t, 0, len(service.eventBuffer), "Should not enqueue event for invalid type")
}

// TestNegative_PodWithoutIP_Skipped verifies pods without IP are skipped
func TestNegative_PodWithoutIP_Skipped(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP assigned yet
		},
	}

	err := service.storePodMetadata(pod)
	require.NoError(t, err, "Should not error for pod without IP")

	// Verify nothing was stored
	assert.Equal(t, 0, mockKV.len(), "Pod without IP should not be stored")
}

// TestNegative_HeadlessService_Skipped verifies headless services are skipped
func TestNegative_HeadlessService_Skipped(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service
		},
	}

	err := service.storeServiceMetadata(svc)
	require.NoError(t, err, "Should not error for headless service")

	// Verify nothing was stored
	assert.Equal(t, 0, mockKV.len(), "Headless service should not be stored")
}

// TestNegative_StoreDeploymentMetadata_PutFailure verifies deployment storage error handling
func TestNegative_StoreDeploymentMetadata_PutFailure(t *testing.T) {
	mockKV := newMockKVWithErrors()
	mockKV.putError = errors.New("nats: permission denied")
	service := &Service{kv: mockKV}

	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	err := service.storeDeploymentMetadata(deployment)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// TestNegative_BufferOverflow verifies behavior when event buffer is full
func TestNegative_BufferOverflow(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{
		kv:          mockKV,
		eventBuffer: make(chan func() error, 2), // Small buffer
	}

	// Fill buffer to capacity
	service.enqueueEvent(func() error { return nil })
	service.enqueueEvent(func() error { return nil })

	// This should not block (non-blocking send)
	// enqueueEvent uses select with default case
	service.enqueueEvent(func() error { return nil })

	// Buffer should still be at capacity
	assert.Equal(t, 2, len(service.eventBuffer))
}
