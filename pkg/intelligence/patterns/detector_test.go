package patterns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestOOMKillPattern_Detect(t *testing.T) {
	pattern := NewOOMKillPattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "oom kill with service impact",
			event: &domain.UnifiedEvent{
				Type:    "pod_oom_killed",
				Message: "OOMKilled",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"service":        "web",
					"totalPods":      int64(3),
					"recentRestarts": int64(2),
				},
			},
			want: &Detection{
				PatternName: "oom_kill_cascade",
				Confidence:  0.95,
				Severity:    domain.EventSeverityCritical,
				Message:     "OOM Kill detected with potential service disruption",
			},
			wantNil: false,
		},
		{
			name: "not an oom event",
			event: &domain.UnifiedEvent{
				Type: "pod_started",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
			},
			mockResults: []map[string]interface{}{},
			want:        nil,
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.wantNil {
				mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
					Return(tt.mockResults, nil).Once()
			}

			got, err := pattern.Detect(context.Background(), tt.event, mockClient)
			assert.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.PatternName, got.PatternName)
				assert.Equal(t, tt.want.Confidence, got.Confidence)
				assert.Equal(t, tt.want.Severity, got.Severity)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestConfigMapChangePattern_Detect(t *testing.T) {
	pattern := NewConfigMapChangePattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "configmap change with pod restarts",
			event: &domain.UnifiedEvent{
				Type: "modified",
				Entity: &domain.EntityContext{
					Type: "configmap",
					UID:  "cm-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"affectedPods": int64(5),
					"restarts":     int64(3),
					"deployments":  []interface{}{"web", "api"},
				},
			},
			want: &Detection{
				PatternName: "configmap_change_cascade",
				Confidence:  0.9,
				Severity:    domain.EventSeverityWarning,
				Message:     "ConfigMap change detected affecting multiple pods",
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.wantNil {
				mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
					Return(tt.mockResults, nil).Once()
			}

			got, err := pattern.Detect(context.Background(), tt.event, mockClient)
			assert.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.PatternName, got.PatternName)
				assert.Equal(t, tt.want.Confidence, got.Confidence)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestCrashLoopPattern_Detect(t *testing.T) {
	pattern := NewCrashLoopPattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "crash loop detected",
			event: &domain.UnifiedEvent{
				Type: "pod_restarted",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"restartCount": int64(5),
					"firstRestart": time.Now().Add(-8 * time.Minute).Unix(),
					"lastRestart":  time.Now().Unix(),
				},
			},
			want: &Detection{
				PatternName: "crash_loop_backoff",
				Confidence:  0.95,
				Severity:    domain.EventSeverityCritical,
				Message:     "Pod is in crash loop backoff",
			},
			wantNil: false,
		},
		{
			name: "normal restart",
			event: &domain.UnifiedEvent{
				Type: "pod_restarted",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"restartCount": int64(1),
				},
			},
			want:    nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
				Return(tt.mockResults, nil).Once()

			got, err := pattern.Detect(context.Background(), tt.event, mockClient)
			assert.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.PatternName, got.PatternName)
				assert.Equal(t, tt.want.Confidence, got.Confidence)
				assert.Equal(t, tt.want.Severity, got.Severity)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestDetector_DetectPatterns(t *testing.T) {
	mockClient := &mockGraphClient{}
	logger := zap.NewNop()
	detector := NewDetector(mockClient, logger)

	// Test event that should trigger OOM pattern
	event := &domain.UnifiedEvent{
		Type:    "pod_oom_killed",
		Message: "OOMKilled",
		Entity: &domain.EntityContext{
			Type: "pod",
			UID:  "pod-123",
		},
		Timestamp: time.Now(),
	}

	// Mock OOM pattern detection
	mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{
				"service":        "web",
				"totalPods":      int64(3),
				"recentRestarts": int64(2),
			},
		}, nil).Once()

	// Mock other pattern queries that might run
	mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
		Return([]map[string]interface{}{}, nil).Maybe()

	detections, err := detector.DetectPatterns(context.Background(), event)
	assert.NoError(t, err)
	assert.NotEmpty(t, detections)

	// Should detect at least the OOM pattern
	found := false
	for _, d := range detections {
		if d.PatternName == "oom_kill_cascade" {
			found = true
			assert.True(t, d.Confidence > 0.7)
		}
	}
	assert.True(t, found, "Should detect OOM kill pattern")
}

// Mock graph client
type mockGraphClient struct {
	mock.Mock
}

func (m *mockGraphClient) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	args := m.Called(ctx, query, params)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

// Benchmark pattern detection
func BenchmarkOOMKillPattern_Detect(b *testing.B) {
	pattern := NewOOMKillPattern()
	mockClient := &mockGraphClient{}

	event := &domain.UnifiedEvent{
		Type:    "pod_oom_killed",
		Message: "OOMKilled",
		Entity: &domain.EntityContext{
			Type: "pod",
			UID:  "pod-123",
		},
		Timestamp: time.Now(),
	}

	mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
		Return([]map[string]interface{}{
			{
				"service":        "web",
				"totalPods":      int64(3),
				"recentRestarts": int64(2),
			},
		}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pattern.Detect(context.Background(), event, mockClient)
	}
}
