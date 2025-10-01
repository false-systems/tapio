package base

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// initializeOutputs initializes output emitters based on configuration
func (bc *BaseObserver) initializeOutputs(config BaseObserverConfig) {
	logger := config.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// Initialize OTEL emitter if enabled
	if bc.outputTargets.OTEL {
		if config.OTELConfig != nil {
			if err := config.OTELConfig.Validate(); err != nil {
				logger.Warn("Invalid OTEL config, OTEL output disabled",
					zap.Error(err))
			} else {
				emitter, err := NewOTELEmitter(logger, bc.name)
				if err != nil {
					logger.Warn("Failed to create OTEL emitter",
						zap.Error(err))
				} else {
					bc.otelEmitter = emitter
					logger.Info("OTEL domain metrics emitter enabled",
						zap.String("observer", bc.name))
				}
			}
		} else {
			// OTEL enabled but no config - still create emitter (uses defaults)
			emitter, err := NewOTELEmitter(logger, bc.name)
			if err != nil {
				logger.Warn("Failed to create OTEL emitter",
					zap.Error(err))
			} else {
				bc.otelEmitter = emitter
				logger.Info("OTEL domain metrics emitter enabled (defaults)",
					zap.String("observer", bc.name))
			}
		}
	}

	// Initialize Stdout emitter if enabled
	if bc.outputTargets.Stdout {
		stdoutCfg := StdoutEmitterConfig{
			Pretty: true, // Default to pretty print
		}
		if config.StdoutConfig != nil {
			stdoutCfg = *config.StdoutConfig
		}

		emitter, err := NewStdoutEmitter(logger, stdoutCfg)
		if err != nil {
			logger.Warn("Failed to create Stdout emitter",
				zap.Error(err))
		} else {
			bc.stdoutEmitter = emitter
			logger.Info("Stdout emitter enabled",
				zap.String("observer", bc.name),
				zap.Bool("pretty", stdoutCfg.Pretty))
		}
	}

	// Future: Initialize NATS emitter
	// if bc.outputTargets.NATS { ... }
}

// EmitEvent emits an event to all configured outputs
// This is the main method observers should use instead of direct channel sends
func (bc *BaseObserver) EmitEvent(ctx context.Context, event *domain.CollectorEvent, channel chan<- *domain.CollectorEvent) {
	if event == nil {
		return
	}

	// Always send to local channel if provided (backward compatibility)
	if channel != nil {
		select {
		case channel <- event:
		default:
			bc.RecordDrop()
		}
	}

	// Emit to Stdout if enabled
	if bc.outputTargets.Stdout && bc.stdoutEmitter != nil {
		if err := bc.stdoutEmitter.EmitEvent(ctx, event); err != nil {
			if bc.logger != nil {
				bc.logger.Debug("Failed to emit to stdout",
					zap.String("event_id", event.EventID),
					zap.Error(err))
			}
		}
	}

	// Future: Emit to NATS if enabled
	// if bc.outputTargets.NATS && bc.natsEmitter != nil { ... }

	// Note: OTEL domain metrics are emitted separately via EmitDomainMetric/EmitDomainGauge
	// because they require converting events to metrics

	// Record meta-metrics
	bc.RecordEventWithContext(ctx)
}

// EmitDomainMetric emits a domain-specific counter metric to OTEL
// Observers should call this to emit K8s-specific metrics
func (bc *BaseObserver) EmitDomainMetric(ctx context.Context, metric DomainMetric) error {
	if !bc.outputTargets.OTEL || bc.otelEmitter == nil {
		return nil // OTEL not enabled, silently skip
	}

	return bc.otelEmitter.EmitDomainMetric(ctx, metric)
}

// EmitDomainGauge emits a domain-specific gauge metric to OTEL
// Observers should call this to emit K8s-specific gauges
func (bc *BaseObserver) EmitDomainGauge(ctx context.Context, gauge DomainGauge) error {
	if !bc.outputTargets.OTEL || bc.otelEmitter == nil {
		return nil // OTEL not enabled, silently skip
	}

	return bc.otelEmitter.EmitDomainGauge(ctx, gauge)
}

// CloseOutputs closes all output emitters and releases resources
func (bc *BaseObserver) CloseOutputs() error {
	var lastErr error

	if bc.otelEmitter != nil {
		if err := bc.otelEmitter.Close(); err != nil {
			lastErr = err
		}
	}

	if bc.stdoutEmitter != nil {
		if err := bc.stdoutEmitter.Close(); err != nil {
			lastErr = err
		}
	}

	// Future: Close NATS emitter
	// if bc.natsEmitter != nil { ... }

	return lastErr
}
