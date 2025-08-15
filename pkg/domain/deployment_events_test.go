package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentAction_Constants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		action   DeploymentAction
		expected string
	}{
		{"Created action", DeploymentCreated, "created"},
		{"Updated action", DeploymentUpdated, "updated"},
		{"RolledBack action", DeploymentRolledBack, "rolledback"},
		{"Scaled action", DeploymentScaled, "scaled"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(tt.action))
		})
	}
}

func TestDeploymentAction_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action DeploymentAction
		valid  bool
	}{
		{"Valid created action", DeploymentCreated, true},
		{"Valid updated action", DeploymentUpdated, true},
		{"Valid rolledback action", DeploymentRolledBack, true},
		{"Valid scaled action", DeploymentScaled, true},
		{"Invalid empty action", DeploymentAction(""), false},
		{"Invalid unknown action", DeploymentAction("unknown"), false},
		{"Invalid mixed case action", DeploymentAction("Created"), false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, tt.action.IsValid())
		})
	}
}

func TestDeploymentMetadata_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata DeploymentMetadata
		wantErr  bool
		errMsg   string
	}{
		{
			name: "Valid metadata with all fields",
			metadata: DeploymentMetadata{
				OldImage:    "nginx:1.20",
				NewImage:    "nginx:1.21",
				OldReplicas: 2,
				NewReplicas: 3,
				Strategy:    "RollingUpdate",
				Labels: map[string]string{
					"app":     "nginx",
					"version": "1.21",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid metadata with minimal fields",
			metadata: DeploymentMetadata{
				NewImage:    "nginx:1.21",
				NewReplicas: 1,
				Strategy:    "Recreate",
			},
			wantErr: false,
		},
		{
			name: "Invalid metadata with negative old replicas",
			metadata: DeploymentMetadata{
				OldImage:    "nginx:1.20",
				NewImage:    "nginx:1.21",
				OldReplicas: -1,
				NewReplicas: 3,
				Strategy:    "RollingUpdate",
			},
			wantErr: true,
			errMsg:  "old replicas cannot be negative",
		},
		{
			name: "Invalid metadata with negative new replicas",
			metadata: DeploymentMetadata{
				OldImage:    "nginx:1.20",
				NewImage:    "nginx:1.21",
				OldReplicas: 2,
				NewReplicas: -1,
				Strategy:    "RollingUpdate",
			},
			wantErr: true,
			errMsg:  "new replicas cannot be negative",
		},
		{
			name: "Invalid metadata with invalid strategy",
			metadata: DeploymentMetadata{
				OldImage:    "nginx:1.20",
				NewImage:    "nginx:1.21",
				OldReplicas: 2,
				NewReplicas: 3,
				Strategy:    "InvalidStrategy",
			},
			wantErr: true,
			errMsg:  "invalid strategy: InvalidStrategy",
		},
		{
			name: "Valid metadata with empty strategy (defaults allowed)",
			metadata: DeploymentMetadata{
				NewImage:    "nginx:1.21",
				NewReplicas: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.metadata.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeploymentEvent_Validate(t *testing.T) {
	t.Parallel()

	validTime := time.Now()
	validMetadata := DeploymentMetadata{
		NewImage:    "nginx:1.21",
		NewReplicas: 3,
		Strategy:    "RollingUpdate",
	}

	tests := []struct {
		name    string
		event   DeploymentEvent
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid deployment event",
			event: DeploymentEvent{
				Timestamp: validTime,
				Namespace: "default",
				Name:      "nginx-deployment",
				Action:    DeploymentCreated,
				Metadata:  validMetadata,
			},
			wantErr: false,
		},
		{
			name: "Invalid event with zero timestamp",
			event: DeploymentEvent{
				Timestamp: time.Time{},
				Namespace: "default",
				Name:      "nginx-deployment",
				Action:    DeploymentCreated,
				Metadata:  validMetadata,
			},
			wantErr: true,
			errMsg:  "timestamp cannot be zero",
		},
		{
			name: "Invalid event with empty namespace",
			event: DeploymentEvent{
				Timestamp: validTime,
				Namespace: "",
				Name:      "nginx-deployment",
				Action:    DeploymentCreated,
				Metadata:  validMetadata,
			},
			wantErr: true,
			errMsg:  "namespace cannot be empty",
		},
		{
			name: "Invalid event with empty name",
			event: DeploymentEvent{
				Timestamp: validTime,
				Namespace: "default",
				Name:      "",
				Action:    DeploymentCreated,
				Metadata:  validMetadata,
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "Invalid event with invalid action",
			event: DeploymentEvent{
				Timestamp: validTime,
				Namespace: "default",
				Name:      "nginx-deployment",
				Action:    DeploymentAction("invalid"),
				Metadata:  validMetadata,
			},
			wantErr: true,
			errMsg:  "invalid deployment action",
		},
		{
			name: "Invalid event with invalid metadata",
			event: DeploymentEvent{
				Timestamp: validTime,
				Namespace: "default",
				Name:      "nginx-deployment",
				Action:    DeploymentCreated,
				Metadata: DeploymentMetadata{
					NewReplicas: -1,
				},
			},
			wantErr: true,
			errMsg:  "new replicas cannot be negative",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.event.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeploymentEvent_GetResourceRef(t *testing.T) {
	t.Parallel()

	event := DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "production",
		Name:      "api-deployment",
		Action:    DeploymentUpdated,
		Metadata: DeploymentMetadata{
			NewImage:    "api:v2.0",
			NewReplicas: 5,
		},
	}

	ref := event.GetResourceRef()

	assert.Equal(t, "Deployment", ref.Kind)
	assert.Equal(t, "api-deployment", ref.Name)
	assert.Equal(t, "production", ref.Namespace)
}

func TestDeploymentEvent_GetEventID(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	event := DeploymentEvent{
		Timestamp: timestamp,
		Namespace: "default",
		Name:      "nginx-deployment",
		Action:    DeploymentCreated,
	}

	eventID := event.GetEventID()

	// Event ID should be deterministic based on event properties
	assert.NotEmpty(t, eventID)
	assert.Contains(t, string(eventID), "deployment-nginx-deployment-")
	assert.Len(t, strings.Split(string(eventID), "-"), 4) // deployment-{name}-{hash}

	// Same event should produce same ID
	eventID2 := event.GetEventID()
	assert.Equal(t, eventID, eventID2)

	// Different event should produce different ID
	event2 := event
	event2.Action = DeploymentUpdated
	eventID3 := event2.GetEventID()
	assert.NotEqual(t, eventID, eventID3)
}

func TestDeploymentEvent_HasImageChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata DeploymentMetadata
		expected bool
	}{
		{
			name: "Has image change",
			metadata: DeploymentMetadata{
				OldImage: "nginx:1.20",
				NewImage: "nginx:1.21",
			},
			expected: true,
		},
		{
			name: "No image change - same images",
			metadata: DeploymentMetadata{
				OldImage: "nginx:1.20",
				NewImage: "nginx:1.20",
			},
			expected: false,
		},
		{
			name: "No old image",
			metadata: DeploymentMetadata{
				NewImage: "nginx:1.21",
			},
			expected: false,
		},
		{
			name: "No new image",
			metadata: DeploymentMetadata{
				OldImage: "nginx:1.20",
			},
			expected: false,
		},
		{
			name:     "No images",
			metadata: DeploymentMetadata{},
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := DeploymentEvent{
				Metadata: tt.metadata,
			}
			assert.Equal(t, tt.expected, event.HasImageChange())
		})
	}
}

func TestDeploymentEvent_HasScaleChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata DeploymentMetadata
		expected bool
	}{
		{
			name: "Has scale change - increase",
			metadata: DeploymentMetadata{
				OldReplicas: 2,
				NewReplicas: 5,
			},
			expected: true,
		},
		{
			name: "Has scale change - decrease",
			metadata: DeploymentMetadata{
				OldReplicas: 5,
				NewReplicas: 2,
			},
			expected: true,
		},
		{
			name: "No scale change - same replicas",
			metadata: DeploymentMetadata{
				OldReplicas: 3,
				NewReplicas: 3,
			},
			expected: false,
		},
		{
			name: "Scale from zero",
			metadata: DeploymentMetadata{
				OldReplicas: 0,
				NewReplicas: 3,
			},
			expected: true,
		},
		{
			name: "Scale to zero",
			metadata: DeploymentMetadata{
				OldReplicas: 3,
				NewReplicas: 0,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := DeploymentEvent{
				Metadata: tt.metadata,
			}
			assert.Equal(t, tt.expected, event.HasScaleChange())
		})
	}
}

// Benchmarks for performance validation
func BenchmarkDeploymentEvent_Validate(b *testing.B) {
	event := DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "default",
		Name:      "nginx-deployment",
		Action:    DeploymentCreated,
		Metadata: DeploymentMetadata{
			OldImage:    "nginx:1.20",
			NewImage:    "nginx:1.21",
			OldReplicas: 2,
			NewReplicas: 3,
			Strategy:    "RollingUpdate",
			Labels: map[string]string{
				"app":     "nginx",
				"version": "1.21",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.Validate()
	}
}

func BenchmarkDeploymentEvent_GetEventID(b *testing.B) {
	event := DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "default",
		Name:      "nginx-deployment",
		Action:    DeploymentCreated,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.GetEventID()
	}
}
