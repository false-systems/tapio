package otel

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// RetryConfig configures retry behavior for OTLP export
type RetryConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}
}

// ExportWithRetry exports spans with exponential backoff retry
func ExportWithRetry(
	ctx context.Context,
	exporter OTLPExporter,
	spans []*domain.OTELSpanData,
	config RetryConfig,
	logger *zap.Logger,
) error {
	if len(spans) == 0 {
		return nil
	}

	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// First attempt has no delay
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue after delay
			}
		}

		err := exporter.ExportSpans(ctx, spans)
		if err == nil {
			if attempt > 0 {
				logger.Info("OTLP export succeeded after retry",
					zap.Int("attempt", attempt+1),
					zap.Int("span_count", len(spans)),
				)
			}
			return nil
		}

		lastErr = err

		// Don't retry on last attempt
		if attempt == config.MaxRetries {
			break
		}

		logger.Warn("OTLP export failed, retrying",
			zap.Error(err),
			zap.Int("attempt", attempt+1),
			zap.Int("max_retries", config.MaxRetries),
			zap.Duration("next_delay", delay),
		)

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return fmt.Errorf("OTLP export failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

// calculateBackoff calculates exponential backoff delay
func calculateBackoff(attempt int, config RetryConfig) time.Duration {
	delay := float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt))
	if delay > float64(config.MaxDelay) {
		return config.MaxDelay
	}
	return time.Duration(delay)
}
