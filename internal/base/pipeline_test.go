package base

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeline_NewPipeline(t *testing.T) {
	p := NewPipeline()
	require.NotNil(t, p)
	assert.Empty(t, p.stages)
}

func TestPipeline_Add(t *testing.T) {
	p := NewPipeline()

	stage1 := func(ctx context.Context) error { return nil }
	stage2 := func(ctx context.Context) error { return nil }

	p.Add(stage1)
	assert.Len(t, p.stages, 1)

	p.Add(stage2)
	assert.Len(t, p.stages, 2)
}

func TestPipeline_Run_Success(t *testing.T) {
	p := NewPipeline()

	var counter atomic.Int64

	// Add 3 stages that increment counter
	for i := 0; i < 3; i++ {
		p.Add(func(ctx context.Context) error {
			counter.Add(1)
			return nil
		})
	}

	ctx := context.Background()
	err := p.Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(3), counter.Load())
}

func TestPipeline_Run_StageError(t *testing.T) {
	p := NewPipeline()

	expectedErr := errors.New("stage failed")

	// Add successful stage
	p.Add(func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Add failing stage
	p.Add(func(ctx context.Context) error {
		return expectedErr
	})

	// Add another successful stage
	p.Add(func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	ctx := context.Background()
	err := p.Run(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage failed")
}

func TestPipeline_Run_ContextCancellation(t *testing.T) {
	p := NewPipeline()

	var stage1Started atomic.Bool
	var stage2Started atomic.Bool

	// Stage 1: blocks until context is cancelled
	p.Add(func(ctx context.Context) error {
		stage1Started.Store(true)
		<-ctx.Done()
		return ctx.Err()
	})

	// Stage 2: blocks until context is cancelled
	p.Add(func(ctx context.Context) error {
		stage2Started.Store(true)
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.Run(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, stage1Started.Load())
	assert.True(t, stage2Started.Load())
}

func TestPipeline_Run_EmptyPipeline(t *testing.T) {
	p := NewPipeline()

	ctx := context.Background()
	err := p.Run(ctx)

	require.NoError(t, err)
}

func TestPipeline_Run_ParallelExecution(t *testing.T) {
	p := NewPipeline()

	var startTimes [3]time.Time
	var counter atomic.Int64

	// Add 3 stages that sleep for 50ms each
	for i := 0; i < 3; i++ {
		idx := i
		p.Add(func(ctx context.Context) error {
			startTimes[idx] = time.Now()
			time.Sleep(50 * time.Millisecond)
			counter.Add(1)
			return nil
		})
	}

	ctx := context.Background()
	start := time.Now()
	err := p.Run(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, int64(3), counter.Load())

	// Should complete in ~50ms (parallel), not ~150ms (sequential)
	assert.Less(t, elapsed, 100*time.Millisecond, "stages should run in parallel")

	// All stages should start around the same time
	maxTimeDiff := time.Duration(0)
	for i := 1; i < 3; i++ {
		diff := startTimes[i].Sub(startTimes[0])
		if diff < 0 {
			diff = -diff
		}
		if diff > maxTimeDiff {
			maxTimeDiff = diff
		}
	}
	assert.Less(t, maxTimeDiff, 20*time.Millisecond, "all stages should start nearly simultaneously")
}
