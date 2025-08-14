package loader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestBatchJobCreation(t *testing.T) {
	events := []*domain.ObservationEvent{
		{
			ID:        "event-1",
			Timestamp: time.Now(),
			Source:    "kernel",
			Type:      "syscall",
		},
		{
			ID:        "event-2",
			Timestamp: time.Now(),
			Source:    "kubeapi",
			Type:      "pod-created",
		},
	}

	batch := &BatchJob{
		ID:        "test-batch-1",
		Events:    events,
		CreatedAt: time.Now(),
		Retries:   0,
	}

	assert.Equal(t, "test-batch-1", batch.ID)
	assert.Equal(t, len(events), len(batch.Events))
	assert.Equal(t, 0, batch.Retries)
	assert.False(t, batch.CreatedAt.IsZero())

	// Verify events are correctly stored
	assert.Equal(t, "event-1", batch.Events[0].ID)
	assert.Equal(t, "event-2", batch.Events[1].ID)
}

func TestProcessingResult(t *testing.T) {
	tests := []struct {
		name     string
		result   *ProcessingResult
		expected ProcessingResult
	}{
		{
			name: "successful processing",
			result: &ProcessingResult{
				BatchID:              "batch-1",
				Success:              true,
				EventsProcessed:      100,
				ProcessingTime:       250 * time.Millisecond,
				NodesCreated:         50,
				RelationshipsCreated: 75,
			},
			expected: ProcessingResult{
				BatchID:              "batch-1",
				Success:              true,
				EventsProcessed:      100,
				ProcessingTime:       250 * time.Millisecond,
				NodesCreated:         50,
				RelationshipsCreated: 75,
			},
		},
		{
			name: "failed processing",
			result: &ProcessingResult{
				BatchID:         "batch-2",
				Success:         false,
				EventsProcessed: 0,
				ProcessingTime:  100 * time.Millisecond,
				Error:           assert.AnError,
			},
			expected: ProcessingResult{
				BatchID:         "batch-2",
				Success:         false,
				EventsProcessed: 0,
				ProcessingTime:  100 * time.Millisecond,
				Error:           assert.AnError,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.BatchID, tt.result.BatchID)
			assert.Equal(t, tt.expected.Success, tt.result.Success)
			assert.Equal(t, tt.expected.EventsProcessed, tt.result.EventsProcessed)
			assert.Equal(t, tt.expected.ProcessingTime, tt.result.ProcessingTime)
			assert.Equal(t, tt.expected.NodesCreated, tt.result.NodesCreated)
			assert.Equal(t, tt.expected.RelationshipsCreated, tt.result.RelationshipsCreated)
			assert.Equal(t, tt.expected.Error, tt.result.Error)
		})
	}
}

func TestRetryDelayCalculation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name            string
		retryBackoff    time.Duration
		maxRetryBackoff time.Duration
		retryAttempt    int
		expectedDelay   time.Duration
	}{
		{
			name:            "first retry",
			retryBackoff:    1 * time.Second,
			maxRetryBackoff: 30 * time.Second,
			retryAttempt:    1,
			expectedDelay:   1 * time.Second, // 1 * 2^0
		},
		{
			name:            "second retry",
			retryBackoff:    1 * time.Second,
			maxRetryBackoff: 30 * time.Second,
			retryAttempt:    2,
			expectedDelay:   2 * time.Second, // 1 * 2^1
		},
		{
			name:            "third retry",
			retryBackoff:    1 * time.Second,
			maxRetryBackoff: 30 * time.Second,
			retryAttempt:    3,
			expectedDelay:   4 * time.Second, // 1 * 2^2
		},
		{
			name:            "fourth retry",
			retryBackoff:    1 * time.Second,
			maxRetryBackoff: 30 * time.Second,
			retryAttempt:    4,
			expectedDelay:   8 * time.Second, // 1 * 2^3
		},
		{
			name:            "capped at maximum",
			retryBackoff:    1 * time.Second,
			maxRetryBackoff: 10 * time.Second,
			retryAttempt:    5,
			expectedDelay:   10 * time.Second, // 16s capped at 10s
		},
		{
			name:            "different base delay",
			retryBackoff:    500 * time.Millisecond,
			maxRetryBackoff: 15 * time.Second,
			retryAttempt:    3,
			expectedDelay:   2 * time.Second, // 0.5 * 2^2 = 2s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.RetryBackoff = tt.retryBackoff
			config.MaxRetryBackoff = tt.maxRetryBackoff

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			delay := loader.calculateRetryDelay(tt.retryAttempt)
			assert.Equal(t, tt.expectedDelay, delay)
		})
	}
}

func TestBatchJobValidation(t *testing.T) {
	tests := []struct {
		name    string
		batch   *BatchJob
		isValid bool
	}{
		{
			name: "valid batch with events",
			batch: &BatchJob{
				ID: "valid-batch",
				Events: []*domain.ObservationEvent{
					{
						ID:        "event-1",
						Timestamp: time.Now(),
						Source:    "kernel",
						Type:      "syscall",
					},
				},
				CreatedAt: time.Now(),
				Retries:   0,
			},
			isValid: true,
		},
		{
			name: "batch with empty ID",
			batch: &BatchJob{
				ID: "",
				Events: []*domain.ObservationEvent{
					{
						ID:        "event-1",
						Timestamp: time.Now(),
						Source:    "kernel",
						Type:      "syscall",
					},
				},
				CreatedAt: time.Now(),
				Retries:   0,
			},
			isValid: false,
		},
		{
			name: "batch with no events",
			batch: &BatchJob{
				ID:        "empty-batch",
				Events:    []*domain.ObservationEvent{},
				CreatedAt: time.Now(),
				Retries:   0,
			},
			isValid: false,
		},
		{
			name: "batch with nil events",
			batch: &BatchJob{
				ID:        "nil-events-batch",
				Events:    nil,
				CreatedAt: time.Now(),
				Retries:   0,
			},
			isValid: false,
		},
		{
			name: "batch with zero timestamp",
			batch: &BatchJob{
				ID: "zero-timestamp-batch",
				Events: []*domain.ObservationEvent{
					{
						ID:        "event-1",
						Timestamp: time.Now(),
						Source:    "kernel",
						Type:      "syscall",
					},
				},
				CreatedAt: time.Time{}, // Zero value
				Retries:   0,
			},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check basic validation criteria
			isValid := tt.batch.ID != "" &&
				tt.batch.Events != nil &&
				len(tt.batch.Events) > 0 &&
				!tt.batch.CreatedAt.IsZero()

			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

func TestWorkerPoolConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name            string
		maxConcurrency  int
		expectedWorkers int
	}{
		{
			name:            "single worker",
			maxConcurrency:  1,
			expectedWorkers: 1,
		},
		{
			name:            "multiple workers",
			maxConcurrency:  4,
			expectedWorkers: 4,
		},
		{
			name:            "high concurrency",
			maxConcurrency:  10,
			expectedWorkers: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaxConcurrency = tt.maxConcurrency

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			// Worker pool should have capacity for the configured number of workers
			assert.Equal(t, tt.maxConcurrency, cap(loader.workerPool))
		})
	}
}

func TestBatchChannelConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name                string
		batchSize           int
		expectedChannelSize int
	}{
		{
			name:                "small batch size",
			batchSize:           10,
			expectedChannelSize: 20, // batchSize * 2
		},
		{
			name:                "default batch size",
			batchSize:           100,
			expectedChannelSize: 200, // batchSize * 2
		},
		{
			name:                "large batch size",
			batchSize:           500,
			expectedChannelSize: 1000, // batchSize * 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.BatchSize = tt.batchSize

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			// Batch channel should have capacity for 2x batch size
			assert.Equal(t, tt.expectedChannelSize, cap(loader.batchChannel))
		})
	}
}

func TestJobQueueConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name              string
		maxConcurrency    int
		expectedQueueSize int
	}{
		{
			name:              "low concurrency",
			maxConcurrency:    2,
			expectedQueueSize: 4, // maxConcurrency * 2
		},
		{
			name:              "medium concurrency",
			maxConcurrency:    4,
			expectedQueueSize: 8, // maxConcurrency * 2
		},
		{
			name:              "high concurrency",
			maxConcurrency:    8,
			expectedQueueSize: 16, // maxConcurrency * 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaxConcurrency = tt.maxConcurrency

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			// Job queue should have capacity for 2x max concurrency
			assert.Equal(t, tt.expectedQueueSize, cap(loader.jobQueue))
		})
	}
}

func TestBatchAging(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		batch     *BatchJob
		threshold time.Duration
		isAged    bool
	}{
		{
			name: "fresh batch",
			batch: &BatchJob{
				ID:        "fresh-batch",
				CreatedAt: now,
			},
			threshold: 5 * time.Minute,
			isAged:    false,
		},
		{
			name: "aged batch",
			batch: &BatchJob{
				ID:        "aged-batch",
				CreatedAt: now.Add(-10 * time.Minute),
			},
			threshold: 5 * time.Minute,
			isAged:    true,
		},
		{
			name: "exactly at threshold",
			batch: &BatchJob{
				ID:        "threshold-batch",
				CreatedAt: now.Add(-5 * time.Minute),
			},
			threshold: 5 * time.Minute,
			isAged:    true, // At threshold counts as aged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age := now.Sub(tt.batch.CreatedAt)
			isAged := age >= tt.threshold
			assert.Equal(t, tt.isAged, isAged)
		})
	}
}

func TestBatchRetryLimits(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		maxRetries     int
		currentRetries int
		shouldRetry    bool
	}{
		{
			name:           "first failure - should retry",
			maxRetries:     3,
			currentRetries: 0,
			shouldRetry:    true,
		},
		{
			name:           "second failure - should retry",
			maxRetries:     3,
			currentRetries: 1,
			shouldRetry:    true,
		},
		{
			name:           "at max retries - should not retry",
			maxRetries:     3,
			currentRetries: 3,
			shouldRetry:    false,
		},
		{
			name:           "exceeded max retries - should not retry",
			maxRetries:     3,
			currentRetries: 5,
			shouldRetry:    false,
		},
		{
			name:           "no retries allowed",
			maxRetries:     0,
			currentRetries: 0,
			shouldRetry:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaxRetries = tt.maxRetries

			loader, err := NewLoader(logger, config)
			require.NoError(t, err)
			defer loader.cancel()

			shouldRetry := tt.currentRetries < loader.config.MaxRetries
			assert.Equal(t, tt.shouldRetry, shouldRetry)
		})
	}
}
