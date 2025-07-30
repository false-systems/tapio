package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	intelligencePipeline "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)

// MockIntelligencePipeline for testing
type MockIntelligencePipeline struct {
	events         []*domain.UnifiedEvent
	running        bool
	processEventFn func(*domain.UnifiedEvent) error
}

func (m *MockIntelligencePipeline) ProcessEvent(event *domain.UnifiedEvent) error {
	if m.processEventFn != nil {
		return m.processEventFn(event)
	}
	m.events = append(m.events, event)
	return nil
}

func (m *MockIntelligencePipeline) ProcessBatch(events []*domain.UnifiedEvent) error {
	for _, event := range events {
		if err := m.ProcessEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (m *MockIntelligencePipeline) Start(ctx context.Context) error {
	m.running = true
	return nil
}

func (m *MockIntelligencePipeline) Stop() error {
	m.running = false
	return nil
}

func (m *MockIntelligencePipeline) Shutdown() error {
	return m.Stop()
}

func (m *MockIntelligencePipeline) GetMetrics() intelligencePipeline.PipelineMetrics {
	return intelligencePipeline.PipelineMetrics{
		EventsProcessed: uint64(len(m.events)),
	}
}

func (m *MockIntelligencePipeline) IsRunning() bool {
	return m.running
}

func (m *MockIntelligencePipeline) GetConfig() intelligencePipeline.PipelineConfig {
	return intelligencePipeline.PipelineConfig{}
}

func TestIntelligenceBridge_ForwardEvents(t *testing.T) {
	ctx := context.Background()

	// Create pipelines
	collectorPipeline := NewPipeline(PipelineConfig{
		OutputBufferSize: 10,
	}).(*CollectorPipeline)

	// Register a converter
	collectorPipeline.RegisterConverter(&MockConverter{sourceType: "test"})

	mockIntelligence := &MockIntelligencePipeline{}

	// Create bridge
	bridge := NewIntelligenceBridge(collectorPipeline, mockIntelligence)

	// Start everything
	err := collectorPipeline.Start(ctx)
	require.NoError(t, err)

	err = bridge.Start(ctx)
	require.NoError(t, err)
	defer bridge.Stop()

	// Process a raw event
	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "test",
		Data:      []byte("test data"),
		Metadata:  map[string]string{"key": "value"},
	}

	err = bridge.ProcessRawEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Wait for event to be forwarded
	time.Sleep(100 * time.Millisecond)

	// Check that event was forwarded to intelligence pipeline
	assert.Len(t, mockIntelligence.events, 1)
	assert.Equal(t, "test", mockIntelligence.events[0].Source)
}

func TestIntelligenceBridge_StartStop(t *testing.T) {
	ctx := context.Background()

	collectorPipeline := NewPipeline(PipelineConfig{})
	mockIntelligence := &MockIntelligencePipeline{}

	bridge := NewIntelligenceBridge(collectorPipeline, mockIntelligence)

	// Start
	err := collectorPipeline.Start(ctx)
	require.NoError(t, err)

	err = bridge.Start(ctx)
	require.NoError(t, err)
	assert.True(t, mockIntelligence.IsRunning())

	// Can't start again
	err = bridge.Start(ctx)
	assert.Error(t, err)

	// Stop
	err = bridge.Stop()
	require.NoError(t, err)
	assert.False(t, mockIntelligence.IsRunning())

	// Can stop again (idempotent)
	err = bridge.Stop()
	assert.NoError(t, err)
}

func TestIntelligenceBridge_GetMetrics(t *testing.T) {
	collectorPipeline := NewPipeline(PipelineConfig{})
	mockIntelligence := &MockIntelligencePipeline{
		events:  make([]*domain.UnifiedEvent, 5),
		running: true,
	}

	bridge := NewIntelligenceBridge(collectorPipeline, mockIntelligence)

	metrics := bridge.GetMetrics()
	assert.True(t, metrics.CollectorPipelineHealthy)
	assert.True(t, metrics.IntelligencePipelineRunning)
	assert.Equal(t, uint64(5), metrics.IntelligenceMetrics.EventsProcessed)
}
