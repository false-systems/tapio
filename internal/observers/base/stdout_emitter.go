package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// StdoutEmitter emits events as JSON to stdout
// Useful for debugging and local development
type StdoutEmitter struct {
	logger *zap.Logger
	writer io.Writer
	pretty bool // Pretty-print JSON
}

// StdoutEmitterConfig configures the stdout emitter
type StdoutEmitterConfig struct {
	Pretty bool      // Pretty-print JSON (default: true)
	Writer io.Writer // Custom writer (default: os.Stdout)
}

// NewStdoutEmitter creates a new stdout emitter
func NewStdoutEmitter(logger *zap.Logger, config StdoutEmitterConfig) (*StdoutEmitter, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	writer := config.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return &StdoutEmitter{
		logger: logger,
		writer: writer,
		pretty: config.Pretty,
	}, nil
}

// EmitEvent emits a CollectorEvent as JSON to stdout
func (e *StdoutEmitter) EmitEvent(ctx context.Context, event *domain.CollectorEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	var data []byte
	var err error

	if e.pretty {
		data, err = json.MarshalIndent(event, "", "  ")
	} else {
		data, err = json.Marshal(event)
	}

	if err != nil {
		e.logger.Error("Failed to marshal event",
			zap.String("event_id", event.EventID),
			zap.Error(err))
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write to stdout with newline
	if _, err := fmt.Fprintln(e.writer, string(data)); err != nil {
		e.logger.Error("Failed to write to stdout",
			zap.Error(err))
		return fmt.Errorf("failed to write to stdout: %w", err)
	}

	return nil
}

// EmitDomainMetric emits domain metric info to stdout (not implemented)
// Stdout emitter focuses on raw events, not metrics
func (e *StdoutEmitter) EmitDomainMetric(ctx context.Context, metric DomainMetric) error {
	// Stdout emitter doesn't emit metrics, only events
	// This method exists to satisfy the OutputEmitter interface
	return nil
}

// EmitDomainGauge emits domain gauge info to stdout (not implemented)
// Stdout emitter focuses on raw events, not gauges
func (e *StdoutEmitter) EmitDomainGauge(ctx context.Context, gauge DomainGauge) error {
	// Stdout emitter doesn't emit gauges, only events
	// This method exists to satisfy the OutputEmitter interface
	return nil
}

// Close closes the emitter and releases resources
func (e *StdoutEmitter) Close() error {
	// Nothing to close for stdout
	return nil
}
