package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGetEventTime_LastTimestamp verifies LastTimestamp priority
func TestGetEventTime_LastTimestamp(t *testing.T) {
	now := time.Now()
	event := &corev1.Event{
		LastTimestamp:  metav1.Time{Time: now},
		FirstTimestamp: metav1.Time{Time: now.Add(-1 * time.Hour)},
	}

	eventTime := getEventTime(event)

	assert.Equal(t, now.Unix(), eventTime.Unix())
}

// TestGetEventTime_FirstTimestamp verifies FirstTimestamp fallback
func TestGetEventTime_FirstTimestamp(t *testing.T) {
	now := time.Now()
	event := &corev1.Event{
		FirstTimestamp: metav1.Time{Time: now},
	}

	eventTime := getEventTime(event)

	assert.Equal(t, now.Unix(), eventTime.Unix())
}

// TestGetEventTime_NoTimestamp verifies current time fallback
func TestGetEventTime_NoTimestamp(t *testing.T) {
	event := &corev1.Event{}

	before := time.Now()
	eventTime := getEventTime(event)
	after := time.Now()

	assert.True(t, eventTime.After(before) || eventTime.Equal(before))
	assert.True(t, eventTime.Before(after) || eventTime.Equal(after))
}

// TestOnAdd_SkipsEvents verifies OnAdd does nothing
func TestOnAdd_SkipsEvents(t *testing.T) {
	handler := &eventsEventHandler{
		watcher: &EventsWatcher{},
	}

	// Should not panic or do anything
	handler.OnAdd(&corev1.Event{}, false)
	handler.OnAdd(&corev1.Event{}, true)
}

// TestOnDelete_SkipsEvents verifies OnDelete does nothing
func TestOnDelete_SkipsEvents(t *testing.T) {
	handler := &eventsEventHandler{
		watcher: &EventsWatcher{},
	}

	// Should not panic or do anything
	handler.OnDelete(&corev1.Event{})
}

// TestOnUpdate_InvalidOldType verifies type assertion error handling
func TestOnUpdate_InvalidOldType(t *testing.T) {
	deps := base.NewDeps(nil, nil)
	config := Config{SchedulerMetricsURL: "http://test:10251/metrics", ScrapeInterval: 30 * time.Second}
	obs, err := New(config, deps)
	require.NoError(t, err)

	handler := &eventsEventHandler{
		watcher: &EventsWatcher{observer: obs},
	}

	// Pass wrong type - should log error and return
	handler.OnUpdate("not an event", &corev1.Event{})
	// Should not panic
}

// TestOnUpdate_InvalidNewType verifies type assertion error handling
func TestOnUpdate_InvalidNewType(t *testing.T) {
	deps := base.NewDeps(nil, nil)
	config := Config{SchedulerMetricsURL: "http://test:10251/metrics", ScrapeInterval: 30 * time.Second}
	obs, err := New(config, deps)
	require.NoError(t, err)

	handler := &eventsEventHandler{
		watcher: &EventsWatcher{observer: obs},
	}

	// Pass wrong type for new object - should log error and return
	handler.OnUpdate(&corev1.Event{}, "not an event")
	// Should not panic
}

// TestParseSchedulingFailure_NoColon verifies handling of malformed messages
func TestParseSchedulingFailure_NoColon(t *testing.T) {
	message := "nodes are available without colon separator"

	failure := parseSchedulingFailure(message)

	// Should return empty failure
	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 0, failure.NodesTotal)
	assert.Equal(t, 0, len(failure.Reasons))
}

// TestParseSchedulingFailure_InvalidNumbers verifies handling of invalid numbers
func TestParseSchedulingFailure_InvalidNumbers(t *testing.T) {
	message := "abc/xyz nodes are available: notanumber Insufficient cpu"

	failure := parseSchedulingFailure(message)

	// Should handle gracefully
	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 0, failure.NodesTotal)
	assert.Equal(t, 0, len(failure.Reasons))
}
