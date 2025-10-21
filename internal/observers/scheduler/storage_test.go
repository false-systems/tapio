package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMakeSchedulingInfoKey verifies key generation for scheduling info
func TestMakeSchedulingInfoKey(t *testing.T) {
	key := makeSchedulingInfoKey("pod-abc-123")
	assert.Equal(t, "pod.scheduling.pod-abc-123", key)
}

// TestMakePluginMetricsKey verifies key generation for plugin metrics
func TestMakePluginMetricsKey(t *testing.T) {
	key := makePluginMetricsKey("NodeAffinity", "Filter")
	assert.Equal(t, "scheduler.plugin.NodeAffinity.Filter", key)
}

// TestMakeSchedulerMetricsKey verifies key generation for global metrics
func TestMakeSchedulerMetricsKey(t *testing.T) {
	key := makeSchedulerMetricsKey()
	assert.Equal(t, "scheduler.metrics", key)
}

// TestMakePreemptionInfoKey verifies key generation for preemption events
func TestMakePreemptionInfoKey(t *testing.T) {
	key := makePreemptionInfoKey("victim-pod-123")
	assert.Equal(t, "scheduler.preemption.victim-pod-123", key)
}

// TestSerializeSchedulingInfo verifies JSON serialization
func TestSerializeSchedulingInfo(t *testing.T) {
	info := SchedulingInfo{
		PodUID:        "pod-123",
		PodName:       "nginx-pod",
		Namespace:     "default",
		FailureCount:  2,
		FailureReason: "insufficient cpu",
		LastAttempt:   time.Now().UTC(),
		Scheduled:     false,
		NodeName:      "",
	}

	data, err := serializeSchedulingInfo(info)
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "pod-123")
	assert.Contains(t, string(data), "nginx-pod")
	assert.Contains(t, string(data), "insufficient cpu")
}

// TestSerializePluginMetrics verifies JSON serialization
func TestSerializePluginMetrics(t *testing.T) {
	metrics := PluginMetrics{
		PluginName:     "NodeResourcesFit",
		ExtensionPoint: "Filter",
		DurationMs:     15.5,
		Result:         "Success",
		Timestamp:      time.Now().UTC(),
	}

	data, err := serializePluginMetrics(metrics)
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "NodeResourcesFit")
	assert.Contains(t, string(data), "Filter")
	assert.Contains(t, string(data), "Success")
}

// TestSerializeSchedulerMetrics verifies JSON serialization
func TestSerializeSchedulerMetrics(t *testing.T) {
	metrics := SchedulerMetrics{
		PendingPods:        42,
		SchedulingAttempts: 1500,
		SchedulingErrors:   25,
		PreemptionAttempts: 10,
		PreemptionVictims:  8,
		LastUpdated:        time.Now().UTC(),
	}

	data, err := serializeSchedulerMetrics(metrics)
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "pending_pods")
	assert.Contains(t, string(data), "42")
}

// TestSerializePreemptionInfo verifies JSON serialization
func TestSerializePreemptionInfo(t *testing.T) {
	info := PreemptionInfo{
		PreemptorPodUID: "preemptor-123",
		PreemptorPod:    "high-priority-pod",
		VictimPodUID:    "victim-123",
		VictimPod:       "low-priority-pod",
		Namespace:       "default",
		Reason:          "Insufficient CPU",
		Timestamp:       time.Now().UTC(),
	}

	data, err := serializePreemptionInfo(info)
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.Contains(t, string(data), "high-priority-pod")
	assert.Contains(t, string(data), "low-priority-pod")
	assert.Contains(t, string(data), "Insufficient CPU")
}

// TestStoreSchedulingInfo verifies storage of scheduling metadata
func TestStoreSchedulingInfo(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	info := SchedulingInfo{
		PodUID:        "pod-abc",
		PodName:       "test-pod",
		Namespace:     "default",
		FailureCount:  1,
		FailureReason: "node not found",
		LastAttempt:   time.Now().UTC(),
	}

	err := observer.storeSchedulingInfo(info)
	require.NoError(t, err)

	// Verify stored in KV
	entry, err := mockKV.Get("pod.scheduling.pod-abc")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestStorePluginMetrics verifies storage of plugin metrics
func TestStorePluginMetrics(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	metrics := PluginMetrics{
		PluginName:     "NodeAffinity",
		ExtensionPoint: "Filter",
		DurationMs:     10.5,
		Result:         "Success",
		Timestamp:      time.Now().UTC(),
	}

	err := observer.storePluginMetrics(metrics)
	require.NoError(t, err)

	// Verify stored in KV
	entry, err := mockKV.Get("scheduler.plugin.NodeAffinity.Filter")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestStoreSchedulerMetrics verifies storage of global scheduler metrics
func TestStoreSchedulerMetrics(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	metrics := SchedulerMetrics{
		PendingPods:        42,
		SchedulingAttempts: 1500,
		SchedulingErrors:   25,
		LastUpdated:        time.Now().UTC(),
	}

	err := observer.storeSchedulerMetrics(metrics)
	require.NoError(t, err)

	// Verify stored in KV
	entry, err := mockKV.Get("scheduler.metrics")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestStorePreemptionInfo verifies storage of preemption events
func TestStorePreemptionInfo(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	info := PreemptionInfo{
		PreemptorPodUID: "preemptor-xyz",
		PreemptorPod:    "high-priority",
		VictimPodUID:    "victim-abc",
		VictimPod:       "low-priority",
		Namespace:       "default",
		Reason:          "Resource pressure",
		Timestamp:       time.Now().UTC(),
	}

	err := observer.storePreemptionInfo(info)
	require.NoError(t, err)

	// Verify stored in KV
	entry, err := mockKV.Get("scheduler.preemption.victim-abc")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestDeleteSchedulingInfo verifies deletion of scheduling metadata
func TestDeleteSchedulingInfo(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	// First store the data
	info := SchedulingInfo{
		PodUID:    "pod-to-delete",
		PodName:   "test-pod",
		Namespace: "default",
	}
	err := observer.storeSchedulingInfo(info)
	require.NoError(t, err)

	// Verify it exists
	_, err = mockKV.Get("pod.scheduling.pod-to-delete")
	require.NoError(t, err)

	// Delete it
	err = observer.deleteSchedulingInfo("pod-to-delete")
	require.NoError(t, err)

	// Verify it's gone
	_, err = mockKV.Get("pod.scheduling.pod-to-delete")
	assert.Error(t, err, "Entry should be deleted")
}

// TestGetSchedulingInfo_Success verifies retrieval of scheduling info
func TestGetSchedulingInfo_Success(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	// Store scheduling info
	info := SchedulingInfo{
		PodUID:        "pod-xyz",
		PodName:       "test-pod",
		Namespace:     "default",
		FailureCount:  2,
		FailureReason: "insufficient memory",
		LastAttempt:   time.Now().UTC(),
	}
	err := observer.storeSchedulingInfo(info)
	require.NoError(t, err)

	// Retrieve it
	retrieved, err := observer.getSchedulingInfo("pod-xyz")
	require.NoError(t, err)
	assert.Equal(t, "pod-xyz", retrieved.PodUID)
	assert.Equal(t, "test-pod", retrieved.PodName)
	assert.Equal(t, 2, retrieved.FailureCount)
}

// TestGetSchedulingInfo_NotFound verifies error when scheduling info doesn't exist
func TestGetSchedulingInfo_NotFound(t *testing.T) {
	mockKV := newMockKV()
	observer := &SchedulerObserver{kv: mockKV}

	// Try to get non-existent scheduling info
	_, err := observer.getSchedulingInfo("nonexistent-pod")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get scheduling info")
}
