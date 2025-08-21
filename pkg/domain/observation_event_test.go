package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper function
func stringPtr(s string) *string {
	return &s
}

func TestObservationEvent_Validate(t *testing.T) {
	tests := []struct {
		name      string
		event     *ObservationEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_event_with_pid",
			event: &ObservationEvent{
				ID:        "test-id",
				Timestamp: time.Now(),
				Source:    "test",
				Type:      "test-type",
				PID:       int32Ptr(12345),
			},
			wantError: false,
		},
		{
			name: "valid_event_with_namespace",
			event: &ObservationEvent{
				ID:        "test-id-2",
				Timestamp: time.Now(),
				Source:    "kubeapi",
				Type:      "pod_created",
				Namespace: stringPtr("default"),
			},
			wantError: false,
		},
		{
			name: "valid_event_with_multiple_keys",
			event: &ObservationEvent{
				ID:          "test-id-3",
				Timestamp:   time.Now(),
				Source:      "dns",
				Type:        "dns_query",
				PID:         int32Ptr(9876),
				Namespace:   stringPtr("kube-system"),
				ServiceName: stringPtr("kube-dns"),
			},
			wantError: false,
		},
		{
			name: "missing_id",
			event: &ObservationEvent{
				Timestamp: time.Now(),
				Source:    "test",
				Type:      "test-type",
				PID:       int32Ptr(12345),
			},
			wantError: true,
			errorMsg:  "ID",
		},
		{
			name: "zero_timestamp",
			event: &ObservationEvent{
				ID:     "test-id",
				Source: "test",
				Type:   "test-type",
				PID:    int32Ptr(12345),
			},
			wantError: true,
			errorMsg:  "Timestamp",
		},
		{
			name: "missing_source",
			event: &ObservationEvent{
				ID:        "test-id",
				Timestamp: time.Now(),
				Type:      "test-type",
				PID:       int32Ptr(12345),
			},
			wantError: true,
			errorMsg:  "Source",
		},
		{
			name: "missing_type",
			event: &ObservationEvent{
				ID:        "test-id",
				Timestamp: time.Now(),
				Source:    "test",
				PID:       int32Ptr(12345),
			},
			wantError: true,
			errorMsg:  "Type",
		},
		{
			name: "no_correlation_keys",
			event: &ObservationEvent{
				ID:        "test-id",
				Timestamp: time.Now(),
				Source:    "test",
				Type:      "test-type",
			},
			wantError: true,
			errorMsg:  "CorrelationKeys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)

				// Verify it's a ValidationError
				var validationErr *ValidationError
				assert.ErrorAs(t, err, &validationErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestObservationEvent_HasCorrelationKey(t *testing.T) {
	tests := []struct {
		name     string
		event    *ObservationEvent
		expected bool
	}{
		{
			name: "has_pid",
			event: &ObservationEvent{
				PID: int32Ptr(12345),
			},
			expected: true,
		},
		{
			name: "has_container_id",
			event: &ObservationEvent{
				ContainerID: stringPtr("container-123"),
			},
			expected: true,
		},
		{
			name: "has_pod_name",
			event: &ObservationEvent{
				PodName: stringPtr("my-pod"),
			},
			expected: true,
		},
		{
			name: "has_namespace",
			event: &ObservationEvent{
				Namespace: stringPtr("default"),
			},
			expected: true,
		},
		{
			name: "has_service_name",
			event: &ObservationEvent{
				ServiceName: stringPtr("my-service"),
			},
			expected: true,
		},
		{
			name: "has_node_name",
			event: &ObservationEvent{
				NodeName: stringPtr("worker-node-1"),
			},
			expected: true,
		},
		{
			name: "has_multiple_keys",
			event: &ObservationEvent{
				PID:       int32Ptr(12345),
				Namespace: stringPtr("default"),
			},
			expected: true,
		},
		{
			name:     "no_correlation_keys",
			event:    &ObservationEvent{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.event.HasCorrelationKey()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestObservationEvent_GetCorrelationKeys(t *testing.T) {
	event := &ObservationEvent{
		PID:         int32Ptr(12345),
		ContainerID: stringPtr("container-abc123"),
		PodName:     stringPtr("test-pod"),
		Namespace:   stringPtr("default"),
		ServiceName: stringPtr("test-service"),
		NodeName:    stringPtr("worker-1"),
	}

	keys := event.GetCorrelationKeys()

	expected := map[string]string{
		"pid":          "12345",
		"container_id": "container-abc123",
		"pod_name":     "test-pod",
		"namespace":    "default",
		"service_name": "test-service",
		"node_name":    "worker-1",
	}

	assert.Equal(t, expected, keys)
}

func TestObservationEvent_GetCorrelationKeys_PartialKeys(t *testing.T) {
	event := &ObservationEvent{
		PID:       int32Ptr(9999),
		Namespace: stringPtr("kube-system"),
		// Other keys are nil
	}

	keys := event.GetCorrelationKeys()

	expected := map[string]string{
		"pid":       "9999",
		"namespace": "kube-system",
	}

	assert.Equal(t, expected, keys)
}

func TestObservationEvent_GetCorrelationKeys_NoKeys(t *testing.T) {
	event := &ObservationEvent{}

	keys := event.GetCorrelationKeys()

	assert.Empty(t, keys)
	assert.NotNil(t, keys) // Should return empty map, not nil
}

func TestValidationError(t *testing.T) {
	err := NewValidationError("TestField", "test-value", "must not be empty")

	assert.Equal(t, "TestField", err.Field)
	assert.Equal(t, "test-value", err.Value)
	assert.Equal(t, "must not be empty", err.Rule)

	expectedMsg := "validation failed for field TestField: must not be empty"
	assert.Equal(t, expectedMsg, err.Error())

	assert.Nil(t, err.Unwrap())
}

// func TestObservationEvent_GenerateEventID(t *testing.T) {
// 	id1 := GenerateEventID()
// 	id2 := GenerateEventID()
//
// 	// IDs should be non-empty and unique
// 	assert.NotEmpty(t, id1)
// 	assert.NotEmpty(t, id2)
// 	assert.NotEqual(t, id1, id2)
//
// 	// Should be valid hex string (32 characters for 16 bytes)
// 	assert.Len(t, id1, 32)
// 	assert.Len(t, id2, 32)
// }

// Helper functions for tests - removed stringPtr as it's now in event_parser.go

func int32Ptr(i int32) *int32 {
	return &i
}
