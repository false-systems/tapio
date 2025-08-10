package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/integrations/transformer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TransformerInstrumentation holds OTEL instrumentation
type TransformerInstrumentation struct {
	Tracer               trace.Tracer
	Meter                metric.Meter
	EventsTransformed    metric.Int64Counter
	TransformationErrors metric.Int64Counter
	TransformationTime   metric.Float64Histogram
}

// StartSpan starts a new span
func (ti *TransformerInstrumentation) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return ti.Tracer.Start(ctx, name, opts...)
}

// EndSpan ends a span and records duration
func (ti *TransformerInstrumentation) EndSpan(span trace.Span, start time.Time, err error, name string) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(trace.Status{Code: trace.StatusCodeError, Description: err.Error()})
	}
	span.End()
}

type TransformerService struct {
	nc              *nats.Conn
	js              jetstream.JetStream
	transformer     *transformer.EventTransformer
	consumers       map[string]jetstream.Consumer
	mu              sync.RWMutex
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	natsConfig      *config.NATSConfig
	logger          *zap.Logger
	instrumentation *TransformerInstrumentation
}

func NewTransformerService(logger *zap.Logger, instrumentation *TransformerInstrumentation) (*TransformerService, error) {
	// Get NATS config
	natsConfig := config.DefaultNATSConfig()
	if url := os.Getenv("NATS_URL"); url != "" {
		natsConfig.URL = url
	}

	nc, err := nats.Connect(natsConfig.URL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.Timeout(10*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("Disconnected from NATS: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("Reconnected to NATS")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Printf("NATS error: %v", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	transformer := transformer.NewEventTransformer()
	ctx, cancel := context.WithCancel(context.Background())

	return &TransformerService{
		nc:              nc,
		js:              js,
		transformer:     transformer,
		consumers:       make(map[string]jetstream.Consumer),
		ctx:             ctx,
		cancel:          cancel,
		natsConfig:      natsConfig,
		logger:          logger,
		instrumentation: instrumentation,
	}, nil
}

func (s *TransformerService) Start() error {
	s.logger.Info("Starting Transformer Service...")

	stream, err := s.js.Stream(s.ctx, s.natsConfig.TracesStreamName)
	if err != nil {
		return fmt.Errorf("failed to get %s stream: %w", s.natsConfig.TracesStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(s.ctx, jetstream.ConsumerConfig{
		Name:          "transformer-consumer",
		Durable:       "transformer-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: s.natsConfig.GetTracesSubject(),
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 1000,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	s.mu.Lock()
	s.consumers["transformer-consumer"] = consumer
	s.mu.Unlock()

	s.wg.Add(1)
	go s.consumeMessages(consumer)

	s.logger.Info("Transformer Service started successfully")
	return nil
}

func (s *TransformerService) consumeMessages(consumer jetstream.Consumer) {
	defer s.wg.Done()

	cctx, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := s.processMessage(msg); err != nil {
			s.logger.Error("Error processing message", zap.Error(err))
			msg.Nak()
			return
		}
		msg.Ack()
	}, jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
		s.logger.Error("Consume error", zap.Error(err))
	}))

	if err != nil {
		s.logger.Error("Failed to start consuming", zap.Error(err))
		return
	}

	<-s.ctx.Done()
	cctx.Stop()
}

func (s *TransformerService) processMessage(msg jetstream.Msg) error {
	// Extract trace context from NATS headers
	// JetStream messages have Headers() method that returns nats.Header
	ctx := s.ctx
	if msg.Headers() != nil {
		// Create a temporary NATS message for trace extraction
		tmpMsg := &nats.Msg{Header: msg.Headers()}
		ctx = telemetry.ExtractTraceContext(ctx, tmpMsg)
	}

	// Start a new span for message processing
	ctx, span := s.instrumentation.StartSpan(ctx, "process_message",
		trace.WithAttributes(
			attribute.String("nats.subject", msg.Subject()),
		),
	)
	start := time.Now()
	var err error
	defer func() {
		s.instrumentation.EndSpan(span, start, err, "process_message")
	}()

	metadata, err := msg.Metadata()
	if err != nil {
		return fmt.Errorf("failed to get message metadata: %w", err)
	}

	s.logger.Debug("Processing message",
		zap.String("subject", msg.Subject()),
		zap.String("stream", metadata.Stream),
		zap.String("consumer", metadata.Consumer))

	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data(), &rawEvent); err != nil {
		return fmt.Errorf("failed to unmarshal raw event: %w", err)
	}

	// Transform the event with tracing
	transformCtx, transformSpan := s.instrumentation.Tracer.Start(ctx, "transform_event",
		trace.WithAttributes(
			attribute.String("event.type", rawEvent.Type),
			attribute.String("collector.name", rawEvent.Type), // Type identifies the collector
		),
	)
	transformStart := time.Now()
	unifiedEvent, transformErr := s.transformer.Transform(transformCtx, rawEvent)
	transformDuration := time.Since(transformStart).Seconds()

	if transformErr != nil {
		transformSpan.RecordError(transformErr)
		s.instrumentation.TransformationErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "transform_failed"),
			attribute.String("event_type", rawEvent.Type),
		))
	}
	transformSpan.End()

	// Record transformation metrics
	s.instrumentation.TransformationTime.Record(ctx, transformDuration, metric.WithAttributes(
		attribute.String("event_type", rawEvent.Type),
		attribute.Bool("success", transformErr == nil),
	))
	s.instrumentation.EventsTransformed.Add(ctx, 1, metric.WithAttributes(
		attribute.String("event_type", rawEvent.Type),
		attribute.Bool("success", transformErr == nil),
	))

	if transformErr != nil {
		err = transformErr
		return fmt.Errorf("failed to transform event: %w", err)
	}

	data, err := json.Marshal(unifiedEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal unified event: %w", err)
	}

	var entityType, namespace, name string
	if unifiedEvent.Entity != nil {
		entityType = unifiedEvent.Entity.Type
		namespace = unifiedEvent.Entity.Namespace
		name = unifiedEvent.Entity.Name
	}
	if entityType == "" {
		entityType = "unknown"
	}
	if namespace == "" {
		namespace = "default"
	}
	if name == "" {
		name = "unnamed"
	}

	subject := fmt.Sprintf("unified.%s.%s.%s", entityType, namespace, name)

	// Create a new NATS message with trace context
	natsMsg := nats.NewMsg(subject)
	natsMsg.Data = data
	telemetry.InjectTraceContext(ctx, natsMsg)

	if err := s.nc.PublishMsg(natsMsg); err != nil {
		return fmt.Errorf("failed to publish unified event: %w", err)
	}

	s.logger.Debug("Published unified event",
		zap.String("subject", subject),
		zap.String("entity_type", entityType),
		zap.String("namespace", namespace),
		zap.String("name", name))

	// Add span event for successful transformation
	span.AddEvent("event_transformed",
		trace.WithAttributes(
			attribute.String("unified.subject", subject),
			attribute.String("entity.type", entityType),
			attribute.String("entity.namespace", namespace),
			attribute.String("entity.name", name),
		),
	)

	return nil
}

func (s *TransformerService) Stop() {
	s.logger.Info("Stopping Transformer Service...")

	s.cancel()

	s.wg.Wait()

	s.mu.Lock()
	for name := range s.consumers {
		s.logger.Info("Stopping consumer", zap.String("name", name))
		// Consumer stops automatically when context is cancelled
	}
	s.mu.Unlock()

	s.nc.Close()
	s.logger.Info("Transformer Service stopped")
}

var (
	otlpEndpoint   = flag.String("otlp-endpoint", os.Getenv("OTLP_ENDPOINT"), "OTLP endpoint")
	prometheusPort = flag.Int("prometheus-port", 9091, "Port for Prometheus metrics")
	enableTraces   = flag.Bool("enable-traces", true, "Enable OpenTelemetry traces")
	enableMetrics  = flag.Bool("enable-metrics", true, "Enable OpenTelemetry metrics")
	logLevel       = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Create logger
	var logger *zap.Logger
	var err error
	switch *logLevel {
	case "debug":
		logger, err = zap.NewDevelopment()
	default:
		logger, err = zap.NewProduction()
	}
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Initialize OTEL
	ctx := context.Background()

	// For now, create a simple instrumentation without full OTEL setup
	tracer := otel.Tracer("transformer-service")
	meter := otel.Meter("transformer-service")

	eventsTransformed, err := meter.Int64Counter("events_transformed",
		metric.WithDescription("Number of events transformed"),
		metric.WithUnit("1"))
	if err != nil {
		logger.Fatal("Failed to create events_transformed counter", zap.Error(err))
	}

	transformationErrors, err := meter.Int64Counter("transformation_errors",
		metric.WithDescription("Number of transformation errors"),
		metric.WithUnit("1"))
	if err != nil {
		logger.Fatal("Failed to create transformation_errors counter", zap.Error(err))
	}

	transformationTime, err := meter.Float64Histogram("transformation_time",
		metric.WithDescription("Time to transform events"),
		metric.WithUnit("ms"))
	if err != nil {
		logger.Fatal("Failed to create transformation_time histogram", zap.Error(err))
	}

	instrumentation := &TransformerInstrumentation{
		Tracer:               tracer,
		Meter:                meter,
		EventsTransformed:    eventsTransformed,
		TransformationErrors: transformationErrors,
		TransformationTime:   transformationTime,
	}
	defer func() {
		// OTEL shutdown would go here when properly configured
		logger.Info("Shutting down instrumentation")
	}()

	// Instrumentation already created above

	// Start Prometheus metrics endpoint if enabled
	if *enableMetrics {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			addr := fmt.Sprintf(":%d", *prometheusPort)
			logger.Info("Starting Prometheus metrics endpoint", zap.String("address", addr))
			if err := http.ListenAndServe(addr, mux); err != nil {
				logger.Error("Failed to start metrics server", zap.Error(err))
			}
		}()
	}

	service, err := NewTransformerService(logger, instrumentation)
	if err != nil {
		logger.Fatal("Failed to create transformer service", zap.Error(err))
	}

	if err := service.Start(); err != nil {
		logger.Fatal("Failed to start transformer service", zap.Error(err))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	logger.Info("Received shutdown signal")

	service.Stop()
}
