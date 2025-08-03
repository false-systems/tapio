package patterns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNodePressurePattern_Detect(t *testing.T) {
	pattern := NewNodePressurePattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "node pressure causing evictions",
			event: &domain.UnifiedEvent{
				Type: "pod_evicted",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"nodeName":       "node-1",
					"pressureEvents": int64(3),
					"evictions":      int64(5),
					"pressureReason": "NodeMemoryPressure",
				},
			},
			want: &Detection{
				PatternName: "node_pressure_eviction",
				Confidence:  0.9,
				Severity:    domain.EventSeverityCritical,
				Message:     "Node pressure causing pod evictions",
			},
			wantNil: false,
		},
		{
			name: "not a pressure event",
			event: &domain.UnifiedEvent{
				Type: "pod_started",
				Entity: &domain.EntityContext{
					Type: "pod",
					UID:  "pod-123",
				},
			},
			wantNil: true,
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

func TestServiceDisruptionPattern_Detect(t *testing.T) {
	pattern := NewServiceDisruptionPattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "service with no pods",
			event: &domain.UnifiedEvent{
				Type: "service_modified",
				Entity: &domain.EntityContext{
					Type: "service",
					UID:  "svc-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"serviceName": "web",
					"totalPods":   int64(0),
					"runningPods": int64(0),
					"podIssues":   int64(0),
					"endpoints":   nil,
				},
			},
			want: &Detection{
				PatternName: "service_disruption",
				Confidence:  1.0,
				Severity:    domain.EventSeverityCritical,
				Message:     "Service has no backing pods",
			},
			wantNil: false,
		},
		{
			name: "service partially disrupted",
			event: &domain.UnifiedEvent{
				Type: "service_modified",
				Entity: &domain.EntityContext{
					Type: "service",
					UID:  "svc-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"serviceName": "web",
					"totalPods":   int64(4),
					"runningPods": int64(1),
					"podIssues":   int64(3),
					"endpoints":   []string{"10.0.0.1"},
				},
			},
			want: &Detection{
				PatternName: "service_disruption",
				Confidence:  0.8,
				Severity:    domain.EventSeverityWarning,
				Message:     "Service experiencing disruption",
			},
			wantNil: false,
		},
		{
			name: "healthy service",
			event: &domain.UnifiedEvent{
				Type: "service_modified",
				Entity: &domain.EntityContext{
					Type: "service",
					UID:  "svc-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"serviceName": "web",
					"totalPods":   int64(3),
					"runningPods": int64(3),
					"podIssues":   int64(0),
					"endpoints":   []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
				},
			},
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

func TestRollingUpdateFailurePattern_Detect(t *testing.T) {
	pattern := NewRollingUpdateFailurePattern()
	mockClient := &mockGraphClient{}

	tests := []struct {
		name        string
		event       *domain.UnifiedEvent
		mockResults []map[string]interface{}
		want        *Detection
		wantNil     bool
	}{
		{
			name: "failed rolling update",
			event: &domain.UnifiedEvent{
				Type: "deployment_modified",
				Entity: &domain.EntityContext{
					Type: "deployment",
					UID:  "dep-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"deploymentName":    "web",
					"desiredReplicas":   int64(3),
					"updatedReplicas":   int64(1),
					"availableReplicas": int64(0),
					"replicaSets": []interface{}{
						map[string]interface{}{
							"revision":   int64(2),
							"podCount":   int64(1),
							"readyPods":  int64(0),
							"failedPods": int64(1),
						},
						map[string]interface{}{
							"revision":   int64(1),
							"podCount":   int64(3),
							"readyPods":  int64(3),
							"failedPods": int64(0),
						},
					},
				},
			},
			want: &Detection{
				PatternName: "rolling_update_failure",
				Confidence:  0.9,
				Severity:    domain.EventSeverityCritical,
				Message:     "Rolling update is failing",
			},
			wantNil: false,
		},
		{
			name: "slow rolling update",
			event: &domain.UnifiedEvent{
				Type: "deployment_modified",
				Entity: &domain.EntityContext{
					Type: "deployment",
					UID:  "dep-123",
				},
				Timestamp: time.Now(),
			},
			mockResults: []map[string]interface{}{
				{
					"deploymentName":    "web",
					"desiredReplicas":   int64(10),
					"updatedReplicas":   int64(3),
					"availableReplicas": int64(3),
					"replicaSets":       []interface{}{},
				},
			},
			want: &Detection{
				PatternName: "rolling_update_failure",
				Confidence:  0.7,
				Severity:    domain.EventSeverityWarning,
				Message:     "Rolling update progressing slowly",
			},
			wantNil: false,
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
