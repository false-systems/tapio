package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// MockConverter for testing
type MockConverter struct {
	sourceType string
	convertFn  func(context.Context, collectors.RawEvent) (*domain.UnifiedEvent, error)
}

func (m *MockConverter) SourceType() string {
	return m.sourceType
}

func (m *MockConverter) Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	if m.convertFn != nil {
		return m.convertFn(ctx, raw)
	}
	return domain.NewUnifiedEvent().
		WithSource(m.sourceType).
		WithTimestamp(raw.Timestamp).
		Build(), nil
}

// MockEnricher for testing
type MockEnricher struct {
	enrichFn func(context.Context, *domain.UnifiedEvent) error
}

func (m *MockEnricher) Enrich(ctx context.Context, event *domain.UnifiedEvent) error {
	if m.enrichFn != nil {
		return m.enrichFn(ctx, event)
	}
	return nil
}

func TestPipeline_Process(t *testing.T) {
	ctx := context.Background()
	
	config := PipelineConfig{
		OutputBufferSize: 10,
		Workers:          2,
	}
	
	pipeline := NewPipeline(config).(*CollectorPipeline)
	
	// Register a mock converter
	converter := &MockConverter{
		sourceType: "test",
		convertFn: func(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
			return domain.NewUnifiedEvent().
				WithSource("test").
				WithTimestamp(raw.Timestamp).
				WithEntity("test-entity", "test-id", "test").
				Build(), nil
		},
	}
	pipeline.RegisterConverter(converter)
	
	// Add a mock enricher
	enricher := &MockEnricher{
		enrichFn: func(ctx context.Context, event *domain.UnifiedEvent) error {
			event.Metadata = map[string]string{"enriched": "true"}
			return nil
		},
	}
	pipeline.AddEnricher(enricher)
	
	// Start pipeline
	err := pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()
	
	// Process an event
	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "test",
		Data:      []byte("test data"),
		Metadata:  map[string]string{"key": "value"},
	}
	
	err = pipeline.Process(ctx, rawEvent)
	require.NoError(t, err)
	
	// Check output
	select {
	case event := <-pipeline.Output():
		assert.NotNil(t, event)
		assert.Equal(t, "test", event.Source)
		assert.Equal(t, "test-entity", event.Entity.Type)
		assert.Equal(t, "true", event.Metadata["enriched"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestPipeline_UnknownSourceType(t *testing.T) {
	ctx := context.Background()
	
	pipeline := NewPipeline(PipelineConfig{}).(*CollectorPipeline)
	
	rawEvent := collectors.RawEvent{
		Type: "unknown",
		Data: []byte("data"),
	}
	
	err := pipeline.Process(ctx, rawEvent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no converter for source type")
}

func TestPipeline_HealthCheck(t *testing.T) {
	pipeline := NewPipeline(PipelineConfig{}).(*CollectorPipeline)
	
	// Should be healthy initially
	assert.True(t, pipeline.IsHealthy())
	
	// Simulate many errors
	for i := 0; i < 150; i++ {
		pipeline.incrementErrorCount()
	}
	
	// Start pipeline to trigger health monitor
	ctx := context.Background()
	err := pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()
	
	// Wait for health check
	time.Sleep(100 * time.Millisecond)
	
	// Should be unhealthy after many errors
	assert.False(t, pipeline.IsHealthy())
}

func TestPipeline_BufferFull(t *testing.T) {
	ctx := context.Background()
	
	// Small buffer to test overflow
	config := PipelineConfig{
		OutputBufferSize: 1,
	}
	
	pipeline := NewPipeline(config).(*CollectorPipeline)
	
	// Register converter
	pipeline.RegisterConverter(&MockConverter{sourceType: "test"})
	
	// Start pipeline
	err := pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()
	
	// Process multiple events quickly
	for i := 0; i < 3; i++ {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "test",
			Data:      []byte("test"),
		}
		_ = pipeline.Process(ctx, rawEvent)
	}
	
	// At least one should have failed due to buffer full
	assert.Greater(t, pipeline.errorCount, uint64(0))
}

func TestPipeline_StartStop(t *testing.T) {
	ctx := context.Background()
	pipeline := NewPipeline(PipelineConfig{})
	
	// Start
	err := pipeline.Start(ctx)
	assert.NoError(t, err)
	
	// Can't start again
	err = pipeline.Start(ctx)
	assert.Error(t, err)
	
	// Stop
	err = pipeline.Stop()
	assert.NoError(t, err)
	
	// Can stop again (idempotent)
	err = pipeline.Stop()
	assert.NoError(t, err)
}