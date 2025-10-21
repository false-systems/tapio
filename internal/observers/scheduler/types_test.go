package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulingInfo_JSONMarshaling verifies SchedulingInfo can be marshaled/unmarshaled
func TestSchedulingInfo_JSONMarshaling(t *testing.T) {
	original := SchedulingInfo{
		PodUID:        "abc-123",
		PodName:       "nginx-pod",
		Namespace:     "default",
		FailureCount:  3,
		FailureReason: "0/3 nodes available: insufficient cpu (2), node(s) had taints (1)",
		LastAttempt:   time.Now().UTC().Truncate(time.Second),
		Scheduled:     false,
		NodeName:      "",
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal SchedulingInfo")

	// Unmarshal back
	var decoded SchedulingInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal SchedulingInfo")

	// Verify all fields match
	assert.Equal(t, original.PodUID, decoded.PodUID)
	assert.Equal(t, original.PodName, decoded.PodName)
	assert.Equal(t, original.Namespace, decoded.Namespace)
	assert.Equal(t, original.FailureCount, decoded.FailureCount)
	assert.Equal(t, original.FailureReason, decoded.FailureReason)
	assert.Equal(t, original.LastAttempt.Unix(), decoded.LastAttempt.Unix())
	assert.Equal(t, original.Scheduled, decoded.Scheduled)
	assert.Equal(t, original.NodeName, decoded.NodeName)
}

// TestPluginMetrics_JSONMarshaling verifies PluginMetrics can be marshaled/unmarshaled
func TestPluginMetrics_JSONMarshaling(t *testing.T) {
	original := PluginMetrics{
		PluginName:     "NodeResourcesFit",
		ExtensionPoint: "Filter",
		DurationMs:     15.5,
		Result:         "Success",
		Timestamp:      time.Now().UTC().Truncate(time.Second),
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal PluginMetrics")

	// Unmarshal back
	var decoded PluginMetrics
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal PluginMetrics")

	// Verify all fields match
	assert.Equal(t, original.PluginName, decoded.PluginName)
	assert.Equal(t, original.ExtensionPoint, decoded.ExtensionPoint)
	assert.Equal(t, original.DurationMs, decoded.DurationMs)
	assert.Equal(t, original.Result, decoded.Result)
	assert.Equal(t, original.Timestamp.Unix(), decoded.Timestamp.Unix())
}

// TestPreemptionInfo_JSONMarshaling verifies PreemptionInfo can be marshaled/unmarshaled
func TestPreemptionInfo_JSONMarshaling(t *testing.T) {
	original := PreemptionInfo{
		PreemptorPodUID: "xyz-789",
		PreemptorPod:    "high-priority-pod",
		VictimPodUID:    "abc-123",
		VictimPod:       "low-priority-pod",
		Namespace:       "default",
		Reason:          "Insufficient CPU",
		Timestamp:       time.Now().UTC().Truncate(time.Second),
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal PreemptionInfo")

	// Unmarshal back
	var decoded PreemptionInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal PreemptionInfo")

	// Verify all fields match
	assert.Equal(t, original.PreemptorPodUID, decoded.PreemptorPodUID)
	assert.Equal(t, original.PreemptorPod, decoded.PreemptorPod)
	assert.Equal(t, original.VictimPodUID, decoded.VictimPodUID)
	assert.Equal(t, original.VictimPod, decoded.VictimPod)
	assert.Equal(t, original.Namespace, decoded.Namespace)
	assert.Equal(t, original.Reason, decoded.Reason)
	assert.Equal(t, original.Timestamp.Unix(), decoded.Timestamp.Unix())
}

// TestSchedulingInfo_SuccessfulScheduling verifies successful scheduling scenario
func TestSchedulingInfo_SuccessfulScheduling(t *testing.T) {
	info := SchedulingInfo{
		PodUID:       "abc-123",
		PodName:      "nginx-pod",
		Namespace:    "default",
		FailureCount: 0,
		Scheduled:    true,
		NodeName:     "worker-1",
		LastAttempt:  time.Now().UTC(),
	}

	assert.True(t, info.Scheduled)
	assert.Equal(t, "worker-1", info.NodeName)
	assert.Equal(t, 0, info.FailureCount)
}

// TestSchedulerMetrics_JSONMarshaling verifies SchedulerMetrics can be marshaled/unmarshaled
func TestSchedulerMetrics_JSONMarshaling(t *testing.T) {
	original := SchedulerMetrics{
		PendingPods:        42,
		SchedulingAttempts: 1500,
		SchedulingErrors:   25,
		PreemptionAttempts: 10,
		PreemptionVictims:  8,
		LastUpdated:        time.Now().UTC().Truncate(time.Second),
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal SchedulerMetrics")

	// Unmarshal back
	var decoded SchedulerMetrics
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal SchedulerMetrics")

	// Verify all fields match
	assert.Equal(t, original.PendingPods, decoded.PendingPods)
	assert.Equal(t, original.SchedulingAttempts, decoded.SchedulingAttempts)
	assert.Equal(t, original.SchedulingErrors, decoded.SchedulingErrors)
	assert.Equal(t, original.PreemptionAttempts, decoded.PreemptionAttempts)
	assert.Equal(t, original.PreemptionVictims, decoded.PreemptionVictims)
	assert.Equal(t, original.LastUpdated.Unix(), decoded.LastUpdated.Unix())
}
