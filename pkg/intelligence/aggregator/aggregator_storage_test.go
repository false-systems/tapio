package aggregator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap/zaptest"
)

// MockCorrelationStorage is a mock implementation of CorrelationStorage
type MockCorrelationStorage struct {
	mock.Mock
}

func (m *MockCorrelationStorage) Store(ctx context.Context, result *FinalResult) error {
	args := m.Called(ctx, result)
	return args.Error(0)
}

func (m *MockCorrelationStorage) GetByID(ctx context.Context, id string) (*StoredCorrelation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*StoredCorrelation), args.Error(1)
}

func (m *MockCorrelationStorage) GetRecent(ctx context.Context, limit int) ([]*StoredCorrelation, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*StoredCorrelation), args.Error(1)
}

func (m *MockCorrelationStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*StoredCorrelation, error) {
	args := m.Called(ctx, resourceType, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*StoredCorrelation), args.Error(1)
}

func (m *MockCorrelationStorage) StoreFeedback(ctx context.Context, correlationID string, feedback CorrelationFeedback) error {
	args := m.Called(ctx, correlationID, feedback)
	return args.Error(0)
}

func (m *MockCorrelationStorage) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockGraphStore is a mock implementation of GraphStore
type MockGraphStore struct {
	mock.Mock
}

func (m *MockGraphStore) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) (interface{}, error) {
	args := m.Called(ctx, query, params)
	return args.Get(0), args.Error(1)
}

func (m *MockGraphStore) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// TestQueryCorrelations tests the QueryCorrelations method
func TestQueryCorrelations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	tests := []struct {
		name          string
		query         CorrelationQuery
		setupMocks    func(*MockCorrelationStorage, *MockGraphStore)
		expectedError bool
		errorContains string
	}{
		{
			name: "successful query with stored results",
			query: CorrelationQuery{
				ResourceType: "pod",
				Namespace:    "default",
				Name:         "test-pod",
			},
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storedResults := []*StoredCorrelation{
					{
						ID:           "corr-1",
						ResourceType: "pod",
						Namespace:    "default",
						Name:         "test-pod",
						RootCause:    "OOMKilled",
						Severity:     SeverityHigh,
						Confidence:   0.85,
						Timestamp:    time.Now(),
						Correlators:  []string{"ResourceCorrelator"},
						Result: &FinalResult{
							ID:         "corr-1",
							RootCause:  "OOMKilled",
							Confidence: 0.85,
							Impact:     "Service unavailable",
							Remediation: Remediation{
								Automatic:     false,
								Steps:         []string{"Increase memory limits"},
								EstimatedTime: 10 * time.Minute,
							},
						},
					},
				}
				storage.On("GetByResource", mock.Anything, "pod", "default", "test-pod").
					Return(storedResults, nil)
			},
			expectedError: false,
		},
		{
			name: "no stored results triggers graph analysis",
			query: CorrelationQuery{
				ResourceType: "pod",
				Namespace:    "default",
				Name:         "test-pod",
			},
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("GetByResource", mock.Anything, "pod", "default", "test-pod").
					Return([]*StoredCorrelation{}, nil)
				graph.On("ExecuteQuery", mock.Anything, mock.Anything, mock.Anything).
					Return(map[string]interface{}{}, nil)
			},
			expectedError: false,
		},
		{
			name: "invalid query - missing resource type",
			query: CorrelationQuery{
				Namespace: "default",
				Name:      "test-pod",
			},
			setupMocks:    func(storage *MockCorrelationStorage, graph *MockGraphStore) {},
			expectedError: true,
			errorContains: "resource type is required",
		},
		{
			name: "storage error",
			query: CorrelationQuery{
				ResourceType: "pod",
				Namespace:    "default",
				Name:         "test-pod",
			},
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("GetByResource", mock.Anything, "pod", "default", "test-pod").
					Return(nil, fmt.Errorf("storage error"))
			},
			expectedError: true,
			errorContains: "failed to query storage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := new(MockCorrelationStorage)
			graph := new(MockGraphStore)
			tt.setupMocks(storage, graph)

			agg := NewCorrelationAggregatorWithStorage(logger, config, storage, graph)
			result, err := agg.QueryCorrelations(context.Background(), tt.query)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}

			storage.AssertExpectations(t)
			graph.AssertExpectations(t)
		})
	}
}

// TestListCorrelations tests the ListCorrelations method
func TestListCorrelations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	tests := []struct {
		name          string
		limit         int
		offset        int
		setupMocks    func(*MockCorrelationStorage)
		expectedCount int
		expectedTotal int
		expectedError bool
	}{
		{
			name:   "successful list with pagination",
			limit:  10,
			offset: 0,
			setupMocks: func(storage *MockCorrelationStorage) {
				results := make([]*StoredCorrelation, 15)
				for i := 0; i < 15; i++ {
					results[i] = &StoredCorrelation{
						ID:           fmt.Sprintf("corr-%d", i),
						ResourceType: "pod",
						Namespace:    "default",
						Name:         fmt.Sprintf("pod-%d", i),
						RootCause:    "Test issue",
						Severity:     SeverityMedium,
						Confidence:   0.75,
						Timestamp:    time.Now().Add(-time.Duration(i) * time.Minute),
						Result: &FinalResult{
							ID: fmt.Sprintf("corr-%d", i),
						},
					}
				}
				storage.On("GetRecent", mock.Anything, mock.Anything).Return(results, nil)
			},
			expectedCount: 10,
			expectedTotal: 15,
			expectedError: false,
		},
		{
			name:   "empty results",
			limit:  10,
			offset: 0,
			setupMocks: func(storage *MockCorrelationStorage) {
				storage.On("GetRecent", mock.Anything, mock.Anything).
					Return([]*StoredCorrelation{}, nil)
			},
			expectedCount: 0,
			expectedTotal: 0,
			expectedError: false,
		},
		{
			name:   "storage error",
			limit:  10,
			offset: 0,
			setupMocks: func(storage *MockCorrelationStorage) {
				storage.On("GetRecent", mock.Anything, mock.Anything).
					Return(nil, fmt.Errorf("storage error"))
			},
			expectedError: true,
		},
		{
			name:   "limit exceeds max",
			limit:  200,
			offset: 0,
			setupMocks: func(storage *MockCorrelationStorage) {
				results := make([]*StoredCorrelation, 5)
				for i := 0; i < 5; i++ {
					results[i] = &StoredCorrelation{
						ID:        fmt.Sprintf("corr-%d", i),
						Timestamp: time.Now(),
						Result:    &FinalResult{ID: fmt.Sprintf("corr-%d", i)},
					}
				}
				storage.On("GetRecent", mock.Anything, mock.Anything).Return(results, nil)
			},
			expectedCount: 5, // Will be limited to actual results
			expectedTotal: 5,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := new(MockCorrelationStorage)
			tt.setupMocks(storage)

			agg := NewCorrelationAggregatorWithStorage(logger, config, storage, nil)
			result, err := agg.ListCorrelations(context.Background(), tt.limit, tt.offset)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Len(t, result.Correlations, tt.expectedCount)
				assert.Equal(t, tt.expectedTotal, result.Total)
			}

			storage.AssertExpectations(t)
		})
	}
}

// TestGetCorrelation tests the GetCorrelation method
func TestGetCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	tests := []struct {
		name          string
		correlationID string
		setupMocks    func(*MockCorrelationStorage)
		expectedError bool
		errorContains string
	}{
		{
			name:          "successful retrieval",
			correlationID: "corr-123",
			setupMocks: func(storage *MockCorrelationStorage) {
				stored := &StoredCorrelation{
					ID:           "corr-123",
					ResourceType: "pod",
					Namespace:    "default",
					Name:         "test-pod",
					RootCause:    "OOMKilled",
					Severity:     SeverityHigh,
					Confidence:   0.85,
					Timestamp:    time.Now(),
					Result: &FinalResult{
						ID:         "corr-123",
						RootCause:  "OOMKilled",
						Confidence: 0.85,
					},
				}
				storage.On("GetByID", mock.Anything, "corr-123").Return(stored, nil)
			},
			expectedError: false,
		},
		{
			name:          "correlation not found",
			correlationID: "corr-999",
			setupMocks: func(storage *MockCorrelationStorage) {
				storage.On("GetByID", mock.Anything, "corr-999").Return(nil, ErrNotFound)
			},
			expectedError: true,
			errorContains: "not found",
		},
		{
			name:          "empty correlation ID",
			correlationID: "",
			setupMocks:    func(storage *MockCorrelationStorage) {},
			expectedError: true,
			errorContains: "correlation ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := new(MockCorrelationStorage)
			tt.setupMocks(storage)

			agg := NewCorrelationAggregatorWithStorage(logger, config, storage, nil)
			result, err := agg.GetCorrelation(context.Background(), tt.correlationID)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.correlationID, result.ID)
			}

			storage.AssertExpectations(t)
		})
	}
}

// TestSubmitFeedback tests the SubmitFeedback method
func TestSubmitFeedback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
		EnableLearning:     true,
	}

	tests := []struct {
		name          string
		correlationID string
		feedback      CorrelationFeedback
		setupMocks    func(*MockCorrelationStorage)
		expectedError bool
	}{
		{
			name:          "successful feedback submission",
			correlationID: "corr-123",
			feedback: CorrelationFeedback{
				UserID:    "user1",
				Useful:    true,
				CorrectRC: true,
				Comment:   "Accurate analysis",
			},
			setupMocks: func(storage *MockCorrelationStorage) {
				stored := &StoredCorrelation{
					ID:          "corr-123",
					Correlators: []string{"ResourceCorrelator", "DependencyCorrelator"},
					Result:      &FinalResult{ID: "corr-123"},
				}
				storage.On("GetByID", mock.Anything, "corr-123").Return(stored, nil)
				storage.On("StoreFeedback", mock.Anything, "corr-123", mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name:          "incorrect root cause triggers learning",
			correlationID: "corr-124",
			feedback: CorrelationFeedback{
				UserID:    "user1",
				Useful:    false,
				CorrectRC: false,
				Comment:   "Wrong root cause",
			},
			setupMocks: func(storage *MockCorrelationStorage) {
				stored := &StoredCorrelation{
					ID:          "corr-124",
					Correlators: []string{"ResourceCorrelator"},
					Result:      &FinalResult{ID: "corr-124"},
				}
				storage.On("GetByID", mock.Anything, "corr-124").Return(stored, nil)
				storage.On("StoreFeedback", mock.Anything, "corr-124", mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name:          "empty correlation ID",
			correlationID: "",
			feedback:      CorrelationFeedback{},
			setupMocks:    func(storage *MockCorrelationStorage) {},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := new(MockCorrelationStorage)
			tt.setupMocks(storage)

			agg := NewCorrelationAggregatorWithStorage(logger, config, storage, nil)
			err := agg.SubmitFeedback(context.Background(), tt.correlationID, tt.feedback)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			storage.AssertExpectations(t)
		})
	}
}

// TestHealth tests the Health method
func TestHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	tests := []struct {
		name          string
		setupMocks    func(*MockCorrelationStorage, *MockGraphStore)
		expectedError bool
		errorContains string
	}{
		{
			name: "all components healthy",
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("HealthCheck", mock.Anything).Return(nil)
				graph.On("HealthCheck", mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name: "storage unhealthy",
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("HealthCheck", mock.Anything).Return(fmt.Errorf("connection failed"))
				graph.On("HealthCheck", mock.Anything).Return(nil)
			},
			expectedError: true,
			errorContains: "storage unhealthy",
		},
		{
			name: "graph store unhealthy",
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("HealthCheck", mock.Anything).Return(nil)
				graph.On("HealthCheck", mock.Anything).Return(fmt.Errorf("neo4j down"))
			},
			expectedError: true,
			errorContains: "graph store unhealthy",
		},
		{
			name: "multiple components unhealthy",
			setupMocks: func(storage *MockCorrelationStorage, graph *MockGraphStore) {
				storage.On("HealthCheck", mock.Anything).Return(fmt.Errorf("storage error"))
				graph.On("HealthCheck", mock.Anything).Return(fmt.Errorf("graph error"))
			},
			expectedError: true,
			errorContains: "health check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := new(MockCorrelationStorage)
			graph := new(MockGraphStore)
			tt.setupMocks(storage, graph)

			agg := NewCorrelationAggregatorWithStorage(logger, config, storage, graph)
			err := agg.Health(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			storage.AssertExpectations(t)
			graph.AssertExpectations(t)
		})
	}
}

// TestValidateQuery tests the validateQuery helper method
func TestValidateQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	tests := []struct {
		name          string
		query         CorrelationQuery
		expectedError bool
		errorContains string
	}{
		{
			name: "valid query",
			query: CorrelationQuery{
				ResourceType: "pod",
				Namespace:    "default",
				Name:         "test-pod",
			},
			expectedError: false,
		},
		{
			name: "cluster-scoped resource without namespace",
			query: CorrelationQuery{
				ResourceType: "node",
				Name:         "worker-1",
			},
			expectedError: false,
		},
		{
			name: "missing resource type",
			query: CorrelationQuery{
				Namespace: "default",
				Name:      "test-pod",
			},
			expectedError: true,
			errorContains: "resource type is required",
		},
		{
			name: "missing name",
			query: CorrelationQuery{
				ResourceType: "pod",
				Namespace:    "default",
			},
			expectedError: true,
			errorContains: "resource name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := agg.validateQuery(tt.query)
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIsMoreRelevant tests the relevance comparison logic
func TestIsMoreRelevant(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	now := time.Now()

	tests := []struct {
		name           string
		result1        *StoredCorrelation
		result2        *StoredCorrelation
		expectedResult bool
	}{
		{
			name: "higher confidence wins",
			result1: &StoredCorrelation{
				Confidence: 0.9,
				Timestamp:  now.Add(-1 * time.Hour),
			},
			result2: &StoredCorrelation{
				Confidence: 0.7,
				Timestamp:  now,
			},
			expectedResult: true,
		},
		{
			name: "similar confidence - more recent wins",
			result1: &StoredCorrelation{
				Confidence: 0.75,
				Timestamp:  now,
			},
			result2: &StoredCorrelation{
				Confidence: 0.8,
				Timestamp:  now.Add(-1 * time.Hour),
			},
			expectedResult: true,
		},
		{
			name: "lower confidence loses",
			result1: &StoredCorrelation{
				Confidence: 0.5,
				Timestamp:  now,
			},
			result2: &StoredCorrelation{
				Confidence: 0.8,
				Timestamp:  now.Add(-1 * time.Hour),
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agg.isMoreRelevant(tt.result1, tt.result2)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
