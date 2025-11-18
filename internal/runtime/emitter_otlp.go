package runtime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OTLPEmitter exports observer events to OpenTelemetry Collector using OTLP/HTTP.
// Uses OpenTelemetry Logs API to export ObserverEvents as structured log records.
// This is the PRIMARY emitter for Simple Mode deployments (no NATS dependency).
type OTLPEmitter struct {
	endpoint    string
	logExporter *otlploghttp.Exporter
	logProvider *sdklog.LoggerProvider
}

// NewOTLPEmitter creates an OTLP emitter that exports to the given endpoint.
// endpoint: OTLP HTTP endpoint (e.g., "localhost:4318")
// insecure: If true, uses insecure connection (for dev/test)
func NewOTLPEmitter(endpoint string, insecure bool) (*OTLPEmitter, error) {
	// Create OTLP HTTP log exporter
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	exporter, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP log exporter: %w", err)
	}

	// Create log provider with batch processor
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	return &OTLPEmitter{
		endpoint:    endpoint,
		logExporter: exporter,
		logProvider: provider,
	}, nil
}

// Emit sends an observer event to OTLP collector as a structured log record.
func (e *OTLPEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	// Check context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get logger from provider
	logger := e.logProvider.Logger("tapio.observer")

	// Build structured attributes from ObserverEvent
	attrs := e.buildAttributes(event)

	// Create log record
	var record log.Record
	record.SetTimestamp(event.Timestamp)
	record.SetSeverity(log.SeverityInfo)
	record.SetBody(log.StringValue(fmt.Sprintf("%s: %s.%s", event.Source, event.Type, event.Subtype)))
	record.AddAttributes(attrs...)

	// Emit OTLP log record
	logger.Emit(ctx, record)

	// Increment success metric
	e.logsExported.Inc()

	return nil
}

// buildAttributes converts ObserverEvent to OTLP log attributes
func (e *OTLPEmitter) buildAttributes(event *domain.ObserverEvent) []log.KeyValue {
	attrs := []log.KeyValue{
		log.String("event.id", event.ID),
		log.String("event.type", event.Type),
		log.String("event.subtype", event.Subtype),
		log.String("event.source", event.Source),
	}

	// Add trace context if present
	if event.TraceID != "" {
		attrs = append(attrs, log.String("trace.id", event.TraceID))
	}
	if event.SpanID != "" {
		attrs = append(attrs, log.String("span.id", event.SpanID))
	}

	// Add NetworkData attributes
	if event.NetworkData != nil {
		attrs = append(attrs,
			log.String("network.src_ip", event.NetworkData.SrcIP),
			log.String("network.dst_ip", event.NetworkData.DstIP),
			log.Int("network.src_port", int(event.NetworkData.SrcPort)),
			log.Int("network.dst_port", int(event.NetworkData.DstPort)),
			log.String("network.protocol", event.NetworkData.Protocol),
		)
	}

	// Add SchedulingData attributes
	if event.SchedulingData != nil {
		attrs = append(attrs,
			log.String("scheduling.pod_uid", event.SchedulingData.PodUID),
			log.Int("scheduling.attempts", int(event.SchedulingData.Attempts)),
			log.Int("scheduling.nodes_failed", event.SchedulingData.NodesFailed),
			log.Int("scheduling.nodes_total", event.SchedulingData.NodesTotal),
		)
		// Add failure reasons as separate attributes
		for reason, count := range event.SchedulingData.FailureReasons {
			attrKey := fmt.Sprintf("scheduling.failure.%s", reason)
			attrs = append(attrs, log.Int(attrKey, count))
		}
	}

	return attrs
}

// Name returns the emitter name for logging and metrics.
func (e *OTLPEmitter) Name() string {
	return "otlp"
}

// IsCritical returns true - OTLP is the primary emitter and is critical.
func (e *OTLPEmitter) IsCritical() bool {
	return true
}

// Close shuts down the OTLP exporter and flushes any pending log records.
func (e *OTLPEmitter) Close() error {
	if e.logProvider != nil {
		ctx := context.Background()
		if err := e.logProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown log provider: %w", err)
		}
	}
	return nil
}
