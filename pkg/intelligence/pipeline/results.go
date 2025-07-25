package pipeline

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// CorrelationOutput represents the output of the intelligence pipeline
type CorrelationOutput struct {
	// Original event that was processed
	OriginalEvent *domain.UnifiedEvent `json:"original_event"`

	// Which stage produced this result
	ProcessingStage string `json:"processing_stage"`

	// Correlation findings (if any)
	CorrelationData *correlation.Finding `json:"correlation_data,omitempty"`

	// Vector embedding for AI similarity search
	Embedding []float32 `json:"embedding,omitempty"`

	// Confidence score (0.0 - 1.0)
	Confidence float64 `json:"confidence"`

	// Processing metadata
	ProcessedAt    time.Time         `json:"processed_at"`
	ProcessingTime time.Duration     `json:"processing_time"`
	Metadata       map[string]string `json:"metadata,omitempty"`

	// Result type classification
	ResultType CorrelationType `json:"result_type"`
}

// CorrelationType defines the type of correlation output
type CorrelationType string

const (
	CorrelationTypeValidation  CorrelationType = "validation"
	CorrelationTypeContext     CorrelationType = "context"
	CorrelationTypeCorrelation CorrelationType = "correlation"
	CorrelationTypeAnalytics   CorrelationType = "analytics"
	CorrelationTypeAnomaly     CorrelationType = "anomaly"
)

// IsSignificant returns true if this correlation output should be persisted
func (c *CorrelationOutput) IsSignificant() bool {
	switch c.ResultType {
	case CorrelationTypeCorrelation:
		return c.CorrelationData != nil && c.Confidence > 0.7
	case CorrelationTypeAnomaly:
		return c.Confidence > 0.8
	case CorrelationTypeAnalytics:
		return c.Confidence > 0.6
	default:
		return false
	}
}

// CorrelationBuffer manages correlation outputs using ring buffer
type CorrelationBuffer struct {
	buffer   []CorrelationOutput
	capacity uint64
	mask     uint64
	readIdx  uint64
	writeIdx uint64
}

// NewCorrelationBuffer creates a new correlation buffer
func NewCorrelationBuffer(capacity uint64) *CorrelationBuffer {
	// Ensure capacity is power of 2
	if capacity == 0 || capacity&(capacity-1) != 0 {
		capacity = 1024 // Default to 1024 if not power of 2
	}

	return &CorrelationBuffer{
		buffer:   make([]CorrelationOutput, capacity),
		capacity: capacity,
		mask:     capacity - 1,
	}
}

// Put adds a correlation output to the buffer
func (cb *CorrelationBuffer) Put(output CorrelationOutput) bool {
	if cb.writeIdx-cb.readIdx >= cb.capacity {
		return false // Buffer full
	}

	idx := cb.writeIdx & cb.mask
	cb.buffer[idx] = output
	cb.writeIdx++
	return true
}

// GetBatch retrieves multiple correlation outputs from the buffer
func (cb *CorrelationBuffer) GetBatch(outputs []CorrelationOutput) int {
	available := cb.writeIdx - cb.readIdx
	if available == 0 {
		return 0
	}

	maxOutputs := uint64(len(outputs))
	if available > maxOutputs {
		available = maxOutputs
	}

	for i := uint64(0); i < available; i++ {
		idx := (cb.readIdx + i) & cb.mask
		outputs[i] = cb.buffer[idx]
	}

	cb.readIdx += available
	return int(available)
}

// Size returns the current number of correlation outputs in buffer
func (cb *CorrelationBuffer) Size() uint64 {
	return cb.writeIdx - cb.readIdx
}

// IsEmpty returns true if buffer is empty
func (cb *CorrelationBuffer) IsEmpty() bool {
	return cb.readIdx >= cb.writeIdx
}

// CorrelationProcessor interface for handling correlation outputs
type CorrelationProcessor interface {
	ProcessOutput(output *CorrelationOutput) error
	ProcessBatch(outputs []*CorrelationOutput) error
}

// CorrelationStore interface for persisting correlation outputs
type CorrelationStore interface {
	Store(output *CorrelationOutput) error
	StoreBatch(outputs []*CorrelationOutput) error
	Query(filters map[string]interface{}) ([]*CorrelationOutput, error)
	GetSimilar(embedding []float32, threshold float64) ([]*CorrelationOutput, error)
}
