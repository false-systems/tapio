package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// setupSubscriptions sets up NATS JetStream subscriptions for observation streams
func (l *Loader) setupSubscriptions(ctx context.Context) error {
	ctx, span := l.tracer.Start(ctx, "loader.setup_subscriptions")
	defer span.End()

	// Define observation stream subjects
	subjects := []string{
		"observations.kernel",
		"observations.kubeapi",
		"observations.dns",
		"observations.etcd",
	}

	// Create stream configuration
	streamConfig := &nats.StreamConfig{
		Name:       "OBSERVATIONS",
		Subjects:   subjects,
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		MaxAge:     24 * time.Hour,          // Keep observations for 24 hours
		MaxBytes:   10 * 1024 * 1024 * 1024, // 10GB max
		Duplicates: 2 * time.Minute,         // Duplicate detection window
		Replicas:   1,
	}

	// Create or update stream
	_, err := l.js.StreamInfo("OBSERVATIONS")
	if err == nats.ErrStreamNotFound {
		_, err = l.js.AddStream(streamConfig)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("failed to create observations stream: %w", err)
		}
		l.logger.Info("Created OBSERVATIONS stream")
	} else if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	// Create consumer configuration
	consumerConfig := &nats.ConsumerConfig{
		Durable:       "neo4j-loader",
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    3,
		FilterSubject: "observations.>", // Subscribe to all observation subjects
		ReplayPolicy:  nats.ReplayInstantPolicy,
	}

	// Create or get consumer
	_, err = l.js.ConsumerInfo("OBSERVATIONS", "neo4j-loader")
	if err == nats.ErrConsumerNotFound {
		_, err = l.js.AddConsumer("OBSERVATIONS", consumerConfig)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("failed to create consumer: %w", err)
		}
		l.logger.Info("Created neo4j-loader consumer")
	} else if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to get consumer info: %w", err)
	}

	// Create pull subscription
	sub, err := l.js.PullSubscribe("observations.>", "neo4j-loader")
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to create pull subscription: %w", err)
	}

	l.subscription = sub

	// Add subscription cleanup
	l.addResource(func() error {
		if l.subscription != nil {
			return l.subscription.Unsubscribe()
		}
		return nil
	})

	span.SetAttributes(
		attribute.StringSlice("subjects", subjects),
		attribute.String("consumer", "neo4j-loader"),
	)

	l.logger.Info("NATS subscriptions setup successfully", zap.Strings("subjects", subjects))
	return nil
}

// startMessageFetcher starts the message fetching goroutine
func (l *Loader) startMessageFetcher(ctx context.Context) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.fetchMessages(ctx)
	}()
}

// fetchMessages continuously fetches messages from NATS subscription
func (l *Loader) fetchMessages(ctx context.Context) {
	ctx, span := l.tracer.Start(ctx, "loader.fetch_messages")
	defer span.End()

	l.logger.Info("Starting message fetcher")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("Message fetcher context cancelled")
			return
		case <-ticker.C:
			if l.isShutdown.Load() {
				return
			}

			// Fetch batch of messages
			msgs, err := l.subscription.Fetch(l.config.BatchSize, nats.MaxWait(5*time.Second))
			if err != nil {
				if err == nats.ErrTimeout {
					// Timeout is normal when no messages are available
					continue
				}
				l.logger.Error("Failed to fetch messages", zap.Error(err))
				continue
			}

			// Process fetched messages
			l.processMessages(ctx, msgs)
		}
	}
}

// processMessages processes a batch of fetched messages
func (l *Loader) processMessages(ctx context.Context, msgs []*nats.Msg) {
	ctx, span := l.tracer.Start(ctx, "loader.process_messages")
	defer span.End()

	span.SetAttributes(attribute.Int("message_count", len(msgs)))

	for _, msg := range msgs {
		select {
		case <-ctx.Done():
			return
		default:
			l.processMessage(ctx, msg)
		}
	}
}

// processMessage processes a single NATS message
func (l *Loader) processMessage(ctx context.Context, msg *nats.Msg) {
	ctx, span := l.tracer.Start(ctx, "loader.process_message")
	defer span.End()

	start := time.Now()

	// Record received event
	if l.eventsReceived != nil {
		l.eventsReceived.Add(ctx, 1, metric.WithAttributes(
			attribute.String("subject", msg.Subject),
		))
	}

	// Update backlog size
	if l.backlogSize != nil {
		l.backlogSize.Add(ctx, 1)
	}

	// Extract source from subject
	source := l.extractSourceFromSubject(msg.Subject)
	span.SetAttributes(
		attribute.String("message.subject", msg.Subject),
		attribute.String("message.source", source),
	)

	// Parse raw event
	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data, &rawEvent); err != nil {
		l.logger.Error("Failed to unmarshal raw event",
			zap.Error(err),
			zap.String("subject", msg.Subject))

		l.recordProcessingError(ctx, "unmarshal_failed", source)
		msg.Nak()
		return
	}

	// Parse to observation event
	obsEvent, err := l.eventParser.ParseEvent(ctx, rawEvent)
	if err != nil {
		l.logger.Error("Failed to parse observation event",
			zap.Error(err),
			zap.String("subject", msg.Subject),
			zap.String("raw_event_type", rawEvent.Type))

		l.recordProcessingError(ctx, "parse_failed", source)
		msg.Nak()
		return
	}

	// Validate observation event
	if err := obsEvent.Validate(); err != nil {
		l.logger.Error("Invalid observation event",
			zap.Error(err),
			zap.String("subject", msg.Subject),
			zap.String("event_id", obsEvent.ID))

		l.recordProcessingError(ctx, "validation_failed", source)
		msg.Nak()
		return
	}

	// Send to batch channel for processing
	select {
	case l.batchChannel <- obsEvent:
		// Successfully queued for batch processing
		msg.Ack()

		// Update metrics
		if l.eventsProcessed != nil {
			l.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source", source),
				attribute.String("event_type", obsEvent.Type),
			))
		}

		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if l.processingLatency != nil {
			l.processingLatency.Record(ctx, duration, metric.WithAttributes(
				attribute.String("source", source),
				attribute.String("stage", "parse"),
			))
		}

		// Update backlog size
		if l.backlogSize != nil {
			l.backlogSize.Add(ctx, -1)
		}

		span.SetAttributes(
			attribute.String("observation.id", obsEvent.ID),
			attribute.String("observation.type", obsEvent.Type),
			attribute.Float64("processing_time_ms", duration),
		)

		l.logger.Debug("Successfully processed observation event",
			zap.String("event_id", obsEvent.ID),
			zap.String("source", source),
			zap.String("type", obsEvent.Type),
			zap.Duration("processing_time", time.Since(start)))

	case <-ctx.Done():
		msg.Nak()
		return
	case <-time.After(l.config.ProcessTimeout):
		l.logger.Error("Timeout queuing event for batch processing",
			zap.String("event_id", obsEvent.ID),
			zap.Duration("timeout", l.config.ProcessTimeout))

		l.recordProcessingError(ctx, "queue_timeout", source)
		msg.Nak()
	}
}

// extractSourceFromSubject extracts the source from NATS subject
func (l *Loader) extractSourceFromSubject(subject string) string {
	// Subject format: observations.{source}
	// Examples: observations.kernel, observations.kubeapi, etc.
	parts := strings.Split(subject, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "unknown"
}

// recordProcessingError records processing error metrics
func (l *Loader) recordProcessingError(ctx context.Context, errorType, source string) {
	if l.eventsFailed != nil {
		l.eventsFailed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
			attribute.String("source", source),
		))
	}

	// Update error rate in metrics
	l.updateMetrics(func(m *LoaderMetrics) {
		m.EventsFailed++
		if m.EventsReceived > 0 {
			m.ErrorRate = float64(m.EventsFailed) / float64(m.EventsReceived)
		}
	})
}
