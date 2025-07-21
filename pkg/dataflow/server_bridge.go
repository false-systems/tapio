package dataflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServerBridge forwards semantic correlation findings to Tapio server
type ServerBridge struct {
	// Server connection
	serverAddr  string
	grpcConn    *grpc.ClientConn
	eventClient pb.TapioServiceClient

	// Data flow integration
	dataFlow       *TapioDataFlow
	findingsBuffer chan *EnrichedFinding
	semanticBuffer chan *SemanticUpdate

	// OTEL
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	findingsSent  uint64
	semanticsSent uint64
	errors        uint64
	lastFlush     time.Time
}

// EnrichedFinding contains correlation finding with OTEL context
type EnrichedFinding struct {
	Finding      *correlation.Finding
	TraceContext trace.SpanContext
	Timestamp    time.Time
	EventCount   int
}

// SemanticUpdate contains semantic group updates
type SemanticUpdate struct {
	GroupID         string
	Intent          string
	SemanticType    string
	EventCount      int
	ConfidenceScore float64
	Impact          *correlation.ImpactAssessment
	Prediction      *correlation.PredictedOutcome
	TraceID         string
	SpanContext     trace.SpanContext
}

// BridgeConfig holds configuration for ServerBridge
type BridgeConfig struct {
	ServerAddress string
	BufferSize    int
	FlushInterval time.Duration
	MaxBatchSize  int
	EnableTracing bool
}

// NewServerBridge creates a new bridge to forward semantic findings
func NewServerBridge(cfg BridgeConfig, dataFlow *TapioDataFlow) (*ServerBridge, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create gRPC connection
	conn, err := grpc.Dial(cfg.ServerAddress,
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(otelUnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelStreamClientInterceptor()),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	// Initialize OTEL
	tracer := otel.Tracer("tapio.dataflow.server_bridge")
	propagator := otel.GetTextMapPropagator()

	bridge := &ServerBridge{
		serverAddr:     cfg.ServerAddress,
		grpcConn:       conn,
		eventClient:    pb.NewTapioServiceClient(conn),
		dataFlow:       dataFlow,
		findingsBuffer: make(chan *EnrichedFinding, cfg.BufferSize),
		semanticBuffer: make(chan *SemanticUpdate, cfg.BufferSize),
		tracer:         tracer,
		propagator:     propagator,
		ctx:            ctx,
		cancel:         cancel,
		lastFlush:      time.Now(),
	}

	return bridge, nil
}

// Start begins forwarding semantic findings to server
func (sb *ServerBridge) Start() error {
	// Start processing goroutines
	sb.wg.Add(3)
	go sb.forwardFindings()
	go sb.forwardSemantics()
	go sb.flushRoutine()

	return nil
}

// Stop gracefully shuts down the bridge
func (sb *ServerBridge) Stop() error {
	sb.cancel()
	sb.wg.Wait()

	// Close gRPC connection
	if err := sb.grpcConn.Close(); err != nil {
		return fmt.Errorf("failed to close gRPC connection: %w", err)
	}

	return nil
}

// SendFinding sends an enriched correlation finding
func (sb *ServerBridge) SendFinding(finding *correlation.Finding, spanCtx trace.SpanContext) {
	enriched := &EnrichedFinding{
		Finding:      finding,
		TraceContext: spanCtx,
		Timestamp:    time.Now(),
		EventCount:   len(finding.RelatedEvents),
	}

	select {
	case sb.findingsBuffer <- enriched:
	case <-sb.ctx.Done():
	default:
		// Buffer full, record metric
		sb.errors++
	}
}

// SendSemanticUpdate sends semantic group updates
func (sb *ServerBridge) SendSemanticUpdate(group *correlation.SemanticTraceGroup) {
	update := &SemanticUpdate{
		GroupID:         group.ID,
		Intent:          group.Intent,
		SemanticType:    group.SemanticType,
		EventCount:      len(group.CausalChain),
		ConfidenceScore: group.ConfidenceScore,
		Impact:          group.ImpactAssessment,
		Prediction:      group.PredictedOutcome,
		TraceID:         group.TraceID,
		SpanContext:     group.SpanContext,
	}

	select {
	case sb.semanticBuffer <- update:
	case <-sb.ctx.Done():
	default:
		// Buffer full, record metric
		sb.errors++
	}
}

// forwardFindings forwards correlation findings to server
func (sb *ServerBridge) forwardFindings() {
	defer sb.wg.Done()

	batch := make([]*EnrichedFinding, 0, 100)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sb.ctx.Done():
			// Flush remaining
			sb.flushFindings(batch)
			return

		case finding := <-sb.findingsBuffer:
			batch = append(batch, finding)

			// Flush if batch is full
			if len(batch) >= 100 {
				sb.flushFindings(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				sb.flushFindings(batch)
				batch = batch[:0]
			}
		}
	}
}

// flushFindings sends a batch of findings to server
func (sb *ServerBridge) flushFindings(findings []*EnrichedFinding) {
	if len(findings) == 0 {
		return
	}

	ctx, span := sb.tracer.Start(sb.ctx, "bridge.flush_findings",
		trace.WithAttributes(
			attribute.Int("batch_size", len(findings)),
		),
	)
	defer span.End()

	// Convert to proto messages
	unifiedEvents := make([]*events.UnifiedEvent, 0, len(findings)*2)

	for _, enriched := range findings {
		// Add trace context to outgoing request
		ctx = trace.ContextWithSpanContext(ctx, enriched.TraceContext)

		// Convert related events
		for _, event := range enriched.Finding.RelatedEvents {
			unifiedEvent := sb.convertEventToUnified(event)

			// Add correlation metadata
			if unifiedEvent.Attributes == nil {
				unifiedEvent.Attributes = make(map[string]*events.AttributeValue)
			}
			unifiedEvent.Attributes["correlation_id"] = &events.AttributeValue{
				Value: &events.AttributeValue_StringValue{StringValue: enriched.Finding.ID},
			}
			unifiedEvent.Attributes["correlation_pattern"] = &events.AttributeValue{
				Value: &events.AttributeValue_StringValue{StringValue: enriched.Finding.PatternType},
			}
			unifiedEvent.Attributes["correlation_confidence"] = &events.AttributeValue{
				Value: &events.AttributeValue_DoubleValue{DoubleValue: enriched.Finding.Confidence},
			}

			// Set correlation context
			if unifiedEvent.Correlation == nil {
				unifiedEvent.Correlation = &events.CorrelationContext{}
			}
			unifiedEvent.Correlation.CorrelationId = enriched.Finding.ID
			unifiedEvent.Correlation.RelatedEvents = ExtractEventIDs(enriched.Finding.RelatedEvents)

			unifiedEvents = append(unifiedEvents, unifiedEvent)
		}
	}

	// Create batch request with trace propagation
	md := metadata.New(nil)
	sb.propagator.Inject(ctx, &metadataCarrier{md: &md})
	ctx = metadata.NewOutgoingContext(ctx, md)

	batch := &events.EventBatch{
		Events:  unifiedEvents,
		BatchId: fmt.Sprintf("findings_%d", time.Now().UnixNano()),
		Source:  "semantic_correlation",
	}

	// Send to server
	_, err := sb.eventClient.SendEventBatch(ctx, batch)
	if err != nil {
		span.RecordError(err)
		sb.errors++
	} else {
		sb.findingsSent += uint64(len(findings))
		span.SetAttributes(
			attribute.Int64("findings_sent_total", int64(sb.findingsSent)),
		)
	}
}

// forwardSemantics forwards semantic group updates
func (sb *ServerBridge) forwardSemantics() {
	defer sb.wg.Done()

	batch := make([]*SemanticUpdate, 0, 50)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sb.ctx.Done():
			sb.flushSemantics(batch)
			return

		case update := <-sb.semanticBuffer:
			batch = append(batch, update)

			if len(batch) >= 50 {
				sb.flushSemantics(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				sb.flushSemantics(batch)
				batch = batch[:0]
			}
		}
	}
}

// flushSemantics sends semantic updates to server
func (sb *ServerBridge) flushSemantics(updates []*SemanticUpdate) {
	if len(updates) == 0 {
		return
	}

	ctx, span := sb.tracer.Start(sb.ctx, "bridge.flush_semantics",
		trace.WithAttributes(
			attribute.Int("batch_size", len(updates)),
		),
	)
	defer span.End()

	// Create semantic metadata events
	unifiedEvents := make([]*events.UnifiedEvent, 0, len(updates))

	for _, update := range updates {
		// Propagate trace context
		ctx = trace.ContextWithSpanContext(ctx, update.SpanContext)

		event := &events.UnifiedEvent{
			Id:        update.GroupID,
			Timestamp: timestamppb.Now(),
			Metadata: &events.EventMetadata{
				Type:     "semantic_group_update",
				Category: events.EventCategory_CATEGORY_OBSERVABILITY,
				Severity: sb.mapImpactToEventSeverity(update.Impact),
				Priority: 8, // High priority for semantic groups
			},
			Source: &events.EventSource{
				Type:      "correlation",
				Collector: "semantic_otel_tracer",
			},
			Attributes: map[string]*events.AttributeValue{
				"semantic_intent":  {Value: &events.AttributeValue_StringValue{StringValue: update.Intent}},
				"semantic_type":    {Value: &events.AttributeValue_StringValue{StringValue: update.SemanticType}},
				"event_count":      {Value: &events.AttributeValue_IntValue{IntValue: int64(update.EventCount)}},
				"confidence_score": {Value: &events.AttributeValue_DoubleValue{DoubleValue: update.ConfidenceScore}},
				"trace_id":         {Value: &events.AttributeValue_StringValue{StringValue: update.TraceID}},
			},
			Correlation: &events.CorrelationContext{
				CorrelationId: update.GroupID,
				TraceId:       update.TraceID,
			},
		}

		// Add impact assessment
		if update.Impact != nil {
			event.Attributes["impact_business"] = &events.AttributeValue{
				Value: &events.AttributeValue_DoubleValue{DoubleValue: float64(update.Impact.BusinessImpact)},
			}
			event.Attributes["impact_cascade_risk"] = &events.AttributeValue{
				Value: &events.AttributeValue_DoubleValue{DoubleValue: float64(update.Impact.CascadeRisk)},
			}
			event.Attributes["impact_severity"] = &events.AttributeValue{
				Value: &events.AttributeValue_StringValue{StringValue: update.Impact.TechnicalSeverity},
			}
		}

		// Add predictions
		if update.Prediction != nil {
			event.Attributes["prediction_scenario"] = &events.AttributeValue{
				Value: &events.AttributeValue_StringValue{StringValue: update.Prediction.Scenario},
			}
			event.Attributes["prediction_probability"] = &events.AttributeValue{
				Value: &events.AttributeValue_DoubleValue{DoubleValue: update.Prediction.Probability},
			}
		}

		unifiedEvents = append(unifiedEvents, event)
	}

	// Send with trace propagation
	md := metadata.New(nil)
	sb.propagator.Inject(ctx, &metadataCarrier{md: &md})
	ctx = metadata.NewOutgoingContext(ctx, md)

	batch := &events.EventBatch{
		Events:  unifiedEvents,
		BatchId: fmt.Sprintf("semantics_%d", time.Now().UnixNano()),
		Source:  "semantic_groups",
	}

	_, err := sb.eventClient.SendEventBatch(ctx, batch)
	if err != nil {
		span.RecordError(err)
		sb.errors++
	} else {
		sb.semanticsSent += uint64(len(updates))
		span.SetAttributes(
			attribute.Int64("semantics_sent_total", int64(sb.semanticsSent)),
		)
	}
}

// flushRoutine periodically flushes metrics
func (sb *ServerBridge) flushRoutine() {
	defer sb.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sb.ctx.Done():
			return
		case <-ticker.C:
			sb.reportMetrics()
		}
	}
}

// reportMetrics reports bridge metrics via OTEL
func (sb *ServerBridge) reportMetrics() {
	_, span := sb.tracer.Start(sb.ctx, "bridge.metrics",
		trace.WithAttributes(
			attribute.Int64("findings_sent", int64(sb.findingsSent)),
			attribute.Int64("semantics_sent", int64(sb.semanticsSent)),
			attribute.Int64("errors", int64(sb.errors)),
			attribute.Float64("findings_per_second", sb.calculateRate(sb.findingsSent)),
			attribute.Float64("semantics_per_second", sb.calculateRate(sb.semanticsSent)),
		),
	)
	span.End()

	sb.lastFlush = time.Now()
}

// Helper methods

func (sb *ServerBridge) convertEventToUnified(event *domain.Event) *events.UnifiedEvent {
	unifiedEvent := &events.UnifiedEvent{
		Id:        string(event.ID),
		Timestamp: timestamppb.New(event.Timestamp),
		Metadata: &events.EventMetadata{
			Type:     string(event.Type),
			Category: sb.mapTypeToCategory(event.Type),
			Severity: sb.mapSeverityToProto(event.Severity),
			Priority: int32(event.Confidence * 10), // Convert confidence to priority
		},
		Source: &events.EventSource{
			Type: string(event.Source),
			Node: event.Context.Host,
		},
		Attributes: make(map[string]*events.AttributeValue),
		Labels:     make(map[string]string),
		Quality: &events.QualityMetadata{
			Confidence: float32(event.Confidence),
		},
	}

	// Add entity context
	if event.Context.Namespace != "" || event.Context.Host != "" {
		unifiedEvent.Entity = &events.EntityContext{
			Type:      events.EntityType_ENTITY_POD,
			Namespace: event.Context.Namespace,
			Node: &events.NodeInfo{
				Name: event.Context.Host,
			},
		}
	}

	// Add labels
	if event.Context.Labels != nil {
		for k, v := range event.Context.Labels {
			unifiedEvent.Labels[k] = v
		}
	}

	// Add metadata as attributes
	if event.Context.Metadata != nil {
		for k, v := range event.Context.Metadata {
			if strVal, ok := v.(string); ok {
				unifiedEvent.Attributes[k] = &events.AttributeValue{
					Value: &events.AttributeValue_StringValue{StringValue: strVal},
				}
			}
		}
	}

	return unifiedEvent
}

func (sb *ServerBridge) mapImpactToEventSeverity(impact *correlation.ImpactAssessment) events.EventSeverity {
	if impact == nil {
		return events.EventSeverity_SEVERITY_INFO
	}

	switch impact.TechnicalSeverity {
	case "critical":
		return events.EventSeverity_SEVERITY_CRITICAL
	case "high":
		return events.EventSeverity_SEVERITY_ERROR
	case "medium":
		return events.EventSeverity_SEVERITY_WARNING
	case "low":
		return events.EventSeverity_SEVERITY_INFO
	default:
		return events.EventSeverity_SEVERITY_INFO
	}
}

func (sb *ServerBridge) mapSeverityToProto(severity domain.EventSeverity) events.EventSeverity {
	switch severity {
	case "critical":
		return events.EventSeverity_SEVERITY_CRITICAL
	case "high", "error":
		return events.EventSeverity_SEVERITY_ERROR
	case "medium", "warning":
		return events.EventSeverity_SEVERITY_WARNING
	case "low", "info":
		return events.EventSeverity_SEVERITY_INFO
	case "debug":
		return events.EventSeverity_SEVERITY_DEBUG
	default:
		return events.EventSeverity_SEVERITY_INFO
	}
}

func (sb *ServerBridge) mapTypeToCategory(eventType domain.EventType) events.EventCategory {
	switch {
	case contains(string(eventType), "network"):
		return events.EventCategory_CATEGORY_NETWORK
	case contains(string(eventType), "memory"):
		return events.EventCategory_CATEGORY_MEMORY
	case contains(string(eventType), "cpu"):
		return events.EventCategory_CATEGORY_CPU
	case contains(string(eventType), "io"), contains(string(eventType), "disk"):
		return events.EventCategory_CATEGORY_IO
	case contains(string(eventType), "pod"), contains(string(eventType), "container"):
		return events.EventCategory_CATEGORY_INFRASTRUCTURE
	case contains(string(eventType), "service"):
		return events.EventCategory_CATEGORY_APPLICATION
	default:
		return events.EventCategory_CATEGORY_SYSTEM
	}
}

// contains is a helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 &&
		(s[0:len(substr)] == substr || (len(s) > len(substr) && contains(s[1:], substr))))
}

func (sb *ServerBridge) calculateRate(count uint64) float64 {
	elapsed := time.Since(sb.lastFlush).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(count) / elapsed
}

// OTEL interceptors

func otelUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	tracer := otel.Tracer("tapio.grpc.client")
	propagator := otel.GetTextMapPropagator()

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
			),
		)
		defer span.End()

		// Propagate trace context
		md, _ := metadata.FromOutgoingContext(ctx)
		if md == nil {
			md = metadata.New(nil)
		}
		propagator.Inject(ctx, &metadataCarrier{md: &md})
		ctx = metadata.NewOutgoingContext(ctx, md)

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", "ERROR"))
		} else {
			span.SetAttributes(attribute.String("rpc.grpc.status_code", "OK"))
		}

		return err
	}
}

func otelStreamClientInterceptor() grpc.StreamClientInterceptor {
	tracer := otel.Tracer("tapio.grpc.client")
	propagator := otel.GetTextMapPropagator()

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
			),
		)

		// Propagate trace context
		md, _ := metadata.FromOutgoingContext(ctx)
		if md == nil {
			md = metadata.New(nil)
		}
		propagator.Inject(ctx, &metadataCarrier{md: &md})
		ctx = metadata.NewOutgoingContext(ctx, md)

		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			span.RecordError(err)
			span.End()
			return nil, err
		}

		return &tracedClientStream{
			ClientStream: stream,
			span:         span,
		}, nil
	}
}

// metadataCarrier adapts gRPC metadata for OTEL propagation
type metadataCarrier struct {
	md *metadata.MD
}

func (mc *metadataCarrier) Get(key string) string {
	values := (*mc.md).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (mc *metadataCarrier) Set(key string, value string) {
	(*mc.md).Set(key, value)
}

func (mc *metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(*mc.md))
	for k := range *mc.md {
		keys = append(keys, k)
	}
	return keys
}

// tracedClientStream wraps gRPC client stream with tracing
type tracedClientStream struct {
	grpc.ClientStream
	span trace.Span
}

func (s *tracedClientStream) CloseSend() error {
	err := s.ClientStream.CloseSend()
	if err != nil {
		s.span.RecordError(err)
	}
	s.span.End()
	return err
}
