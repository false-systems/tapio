package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestBatchSpanReader_Create(t *testing.T) {
	reader := NewBatchSpanReader(100)
	require.NotNil(t, reader)
	assert.Equal(t, 100, reader.bufSize)
	assert.NotNil(t, reader.buffer)

	// Clean up
	err := reader.Close()
	assert.NoError(t, err)
}

func TestBatchSpanReader_DefaultBufferSize(t *testing.T) {
	reader := NewBatchSpanReader(0)
	require.NotNil(t, reader)
	assert.Equal(t, 1000, reader.bufSize)

	err := reader.Close()
	assert.NoError(t, err)
}

func TestBatchSpanReader_ReadSpans_Empty(t *testing.T) {
	reader := NewBatchSpanReader(10)
	defer reader.Close()

	ctx := context.Background()
	spans, err := reader.ReadSpans(ctx)

	assert.NoError(t, err)
	assert.Empty(t, spans)
}

func TestBatchSpanReader_Close(t *testing.T) {
	reader := NewBatchSpanReader(10)

	err := reader.Close()
	assert.NoError(t, err)

	// Buffer should be closed
	_, ok := <-reader.buffer
	assert.False(t, ok, "buffer should be closed")
}

// MockSpanReader for testing
type MockSpanReader struct {
	spans []*domain.OTELSpanData
	err   error
}

func (m *MockSpanReader) ReadSpans(ctx context.Context) ([]*domain.OTELSpanData, error) {
	return m.spans, m.err
}

func (m *MockSpanReader) Close() error {
	return nil
}

func TestMockSpanReader_Interface(t *testing.T) {
	// Verify MockSpanReader implements SpanReader interface
	var _ SpanReader = (*MockSpanReader)(nil)

	mock := &MockSpanReader{
		spans: []*domain.OTELSpanData{
			{TraceID: "test-trace", SpanID: "test-span"},
		},
	}

	spans, err := mock.ReadSpans(context.Background())
	assert.NoError(t, err)
	assert.Len(t, spans, 1)
	assert.Equal(t, "test-trace", spans[0].TraceID)
}
