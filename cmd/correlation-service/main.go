package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/intelligence/nats"
	"github.com/yairfalse/tapio/pkg/intelligence/storage"
)

var (
	otlpEndpoint   = flag.String("otlp-endpoint", os.Getenv("OTLP_ENDPOINT"), "OTLP endpoint for traces and metrics")
	prometheusPort = flag.Int("prometheus-port", 9090, "Port for Prometheus metrics")
	enableTraces   = flag.Bool("enable-traces", true, "Enable OpenTelemetry traces")
	enableMetrics  = flag.Bool("enable-metrics", true, "Enable OpenTelemetry metrics")
	serviceVersion = flag.String("service-version", "1.0.0", "Service version")
)

func main() {
	flag.Parse()

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry
	telemetryConfig := telemetry.DefaultConfig("correlation-service")
	telemetryConfig.OTLPEndpoint = *otlpEndpoint
	telemetryConfig.PrometheusPort = *prometheusPort
	telemetryConfig.EnableTraces = *enableTraces
	telemetryConfig.EnableMetrics = *enableMetrics
	telemetryConfig.ServiceVersion = *serviceVersion
	telemetryConfig.Logger = logger

	provider, err := telemetry.NewProvider(ctx, telemetryConfig)
	if err != nil {
		logger.Fatal("Failed to initialize telemetry", zap.Error(err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			logger.Error("Failed to shutdown telemetry", zap.Error(err))
		}
	}()

	// Create instrumentation
	instrumentation, err := telemetry.NewCorrelationInstrumentation(logger)
	if err != nil {
		logger.Fatal("Failed to create instrumentation", zap.Error(err))
	}

	// Start Prometheus metrics server
	if *enableMetrics {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "OK")
			})
			addr := fmt.Sprintf(":%d", *prometheusPort)
			logger.Info("Starting metrics server", zap.String("addr", addr))
			if err := http.ListenAndServe(addr, mux); err != nil {
				logger.Error("Metrics server failed", zap.Error(err))
			}
		}()
	}

	// Create K8s client - try in-cluster first, then kubeconfig
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Info("Not running in cluster, trying kubeconfig...")
		// Try kubeconfig for local development
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = clientcmd.RecommendedHomeFile
		}
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			logger.Fatal("Failed to get kubeconfig", zap.Error(err))
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		logger.Fatal("Failed to create K8s client", zap.Error(err))
	}

	// 1. Create storage
	storageConfig := storage.DefaultMemoryStorageConfig()
	memStorage := storage.NewMemoryStorage(logger, storageConfig)

	// 2. Create correlation engine
	engineConfig := correlation.DefaultEngineConfig()
	engine, err := correlation.NewEngine(logger, engineConfig, clientset, memStorage)
	if err != nil {
		logger.Fatal("Failed to create correlation engine", zap.Error(err))
	}

	// Start the engine
	if err := engine.Start(ctx); err != nil {
		logger.Fatal("Failed to start correlation engine", zap.Error(err))
	}

	// 3. Create NATS subscriber
	natsConfig := config.DefaultNATSConfig()
	// Override with environment variables if set
	if url := os.Getenv("NATS_URL"); url != "" {
		natsConfig.URL = url
	}
	if consumer := os.Getenv("CONSUMER_NAME"); consumer != "" {
		natsConfig.ConsumerName = consumer
	}

	subscriber, err := nats.NewSubscriber(logger, natsConfig, engine)
	if err != nil {
		logger.Fatal("Failed to create NATS subscriber", zap.Error(err))
	}

	// 4. Start processing correlation results
	go handleCorrelationResults(ctx, engine, logger, instrumentation)

	// 5. Start NATS subscriber in background
	go func() {
		if err := subscriber.Start(ctx); err != nil {
			logger.Error("NATS subscriber error", zap.Error(err))
		}
	}()

	logger.Info("Correlation service started",
		zap.String("nats_url", natsConfig.URL),
		zap.String("stream", natsConfig.TracesStreamName),
		zap.String("subject", natsConfig.GetTracesSubject()),
		zap.Bool("k8s_enabled", engineConfig.EnableK8s),
		zap.Bool("temporal_enabled", engineConfig.EnableTemporal),
		zap.Bool("sequence_enabled", engineConfig.EnableSequence),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down correlation service...")

	// Cancel context to trigger graceful shutdown
	cancel()

	// Give components time to shut down
	time.Sleep(2 * time.Second)

	// Stop engine
	if err := engine.Stop(); err != nil {
		logger.Error("Failed to stop engine", zap.Error(err))
	}

	logger.Info("Correlation service stopped")
}

// handleCorrelationResults processes correlation results from the engine
func handleCorrelationResults(ctx context.Context, engine *correlation.Engine, logger *zap.Logger, instrumentation *telemetry.CorrelationInstrumentation) {
	results := engine.Results()

	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			// Start a span for result processing
			spanCtx, span := instrumentation.StartSpan(ctx, "process_correlation_result",
				trace.WithAttributes(
					attribute.String("correlation.id", result.ID),
					attribute.String("correlation.type", result.Type),
					attribute.Float64("correlation.confidence", result.Confidence),
				))

			// Record correlation metrics
			rootCauseDesc := ""
			if result.RootCause != nil {
				rootCauseDesc = result.RootCause.Description
			}
			instrumentation.RecordCorrelation(spanCtx, result.Type, result.Confidence, len(result.Events), rootCauseDesc)

			// Log significant correlations
			if result.Confidence >= 0.8 {
				logger.Info("High confidence correlation detected",
					zap.String("id", result.ID),
					zap.String("type", result.Type),
					zap.Float64("confidence", result.Confidence),
					zap.String("summary", result.Summary),
					zap.Int("events", len(result.Events)),
				)

				// Add high confidence event to span
				span.AddEvent("high_confidence_correlation", trace.WithAttributes(
					attribute.String("summary", result.Summary),
				))
			}

			// Add correlated event IDs to span
			if len(result.Events) > 0 {
				span.SetAttributes(attribute.StringSlice("correlation.event_ids", result.Events))

				// For now, we only have event IDs. In a full implementation,
				// we'd look up the events to get their trace IDs for creating span links.
				// The correlation engine would need to store event metadata including trace IDs.

				// If the result has a trace ID (from one of the correlated events), link to it
				if result.TraceID != "" {
					span.SetAttributes(attribute.String("correlation.source_trace_id", result.TraceID))
					// Could create a span link here if we had the span ID
				}
			}

			// Here you could:
			// - Send to alerting system
			// - Store in time-series DB
			// - Publish to NATS for other services
			// - Update Kubernetes annotations
			// - Send to UI/dashboard

			instrumentation.EndSpan(span, time.Now(), nil, "process_correlation_result")
		}
	}
}
