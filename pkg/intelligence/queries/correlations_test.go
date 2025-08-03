package queries

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCorrelationQuery_WhyDidPodFail(t *testing.T) {
	// Create query with mock client
	query := &CorrelationQuery{}

	tests := []struct {
		name       string
		namespace  string
		podName    string
		timeWindow time.Duration
		mockData   []map[string]interface{}
		want       *RootCauseAnalysis
		wantErr    bool
	}{
		{
			name:       "oom kill root cause",
			namespace:  "default",
			podName:    "nginx-123",
			timeWindow: 1 * time.Hour,
			mockData: []map[string]interface{}{
				{
					"p": map[string]interface{}{
						"name":      "nginx-123",
						"namespace": "default",
						"uid":       "pod-123",
					},
					"roots": []interface{}{
						map[string]interface{}{
							"type":      "memory_pressure",
							"message":   "Container exceeded memory limit",
							"timestamp": time.Now().Unix(),
						},
					},
					"events": []interface{}{
						map[string]interface{}{
							"id":        "event-1",
							"type":      "oom_killed",
							"message":   "Container killed due to OOM",
							"severity":  "critical",
							"timestamp": time.Now().Unix(),
						},
					},
				},
			},
			want: &RootCauseAnalysis{
				FailedEntity: EntityInfo{
					Type:      "Pod",
					Name:      "nginx-123",
					Namespace: "default",
					UID:       "pod-123",
				},
				RootCauses: []CauseInfo{
					{
						Type:      "memory_pressure",
						Message:   "Container exceeded memory limit",
						Timestamp: time.Now(),
					},
				},
				RelatedEvents: []EventInfo{
					{
						ID:       "event-1",
						Type:     "oom_killed",
						Message:  "Container killed due to OOM",
						Severity: "critical",
					},
				},
			},
			wantErr: false,
		},
		{
			name:       "pod not found",
			namespace:  "default",
			podName:    "not-exists",
			timeWindow: 1 * time.Hour,
			mockData:   []map[string]interface{}{},
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh mock for each test
			mockClient := &mockGraphClient{}
			query.client = mockClient

			
 main
			// Set mock expectations
			mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
				Return(tt.mockData, nil).Once()

			got, err := query.WhyDidPodFail(context.Background(), tt.namespace, tt.podName, tt.timeWindow)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.FailedEntity.Name, got.FailedEntity.Name)
				assert.Equal(t, len(tt.want.RootCauses), len(got.RootCauses))
			}
		})
	}
}

func TestCorrelationQuery_WhatImpactsService(t *testing.T) {
	mockClient := &mockGraphClient{}
	query := NewCorrelationQuery(mockClient)

	tests := []struct {
		name         string
		namespace    string
		serviceName  string
		mockData     []map[string]interface{}
		wantPodCount int
		wantErr      bool
	}{
		{
			name:        "service with pods",
			namespace:   "default",
			serviceName: "web",
			mockData: []map[string]interface{}{
				{
					"s": map[string]interface{}{
						"name":      "web",
						"namespace": "default",
						"uid":       "svc-123",
					},
					"pods": []interface{}{
						map[string]interface{}{
							"name":      "web-1",
							"namespace": "default",
							"uid":       "pod-1",
						},
						map[string]interface{}{
							"name":      "web-2",
							"namespace": "default",
							"uid":       "pod-2",
						},
					},
					"deployments": []interface{}{
						map[string]interface{}{
							"name":      "web-deployment",
							"namespace": "default",
							"uid":       "dep-123",
						},
					},
				},
			},
			wantPodCount: 2,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
				Return(tt.mockData, nil)

			got, err := query.WhatImpactsService(context.Background(), tt.namespace, tt.serviceName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				assert.Equal(t, tt.wantPodCount, len(got.AffectedPods))
			}
		})
	}
}

func TestCorrelationQuery_FindCascadingFailures(t *testing.T) {
	mockClient := &mockGraphClient{}
	query := NewCorrelationQuery(mockClient)

	tests := []struct {
		name      string
		startTime time.Time
		mockData  []map[string]interface{}
		wantCount int
		wantErr   bool
	}{
		{
			name:      "cascade detected",
			startTime: time.Now().Add(-1 * time.Hour),
			mockData: []map[string]interface{}{
				{
					"trigger": map[string]interface{}{
						"id":        "event-trigger",
						"type":      "configmap_update",
						"timestamp": time.Now().Unix(),
					},
					"effects": []interface{}{
						map[string]interface{}{
							"id":   "event-1",
							"type": "pod_restart",
						},
						map[string]interface{}{
							"id":   "event-2",
							"type": "pod_restart",
						},
						map[string]interface{}{
							"id":   "event-3",
							"type": "service_disruption",
						},
					},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
				Return(tt.mockData, nil)

			got, err := query.FindCascadingFailures(context.Background(), tt.startTime)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, got, tt.wantCount)
			}
		})
	}
}

// Mock graph client for testing
type mockGraphClient struct {
	mock.Mock
}

func (m *mockGraphClient) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	args := m.Called(ctx, query, params)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

// Add more mock methods as needed...
