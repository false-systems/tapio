package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// GRPCExporter exports spans via OTLP/gRPC to Urpo or other backends
type GRPCExporter struct {
	client   otlptrace.Client
	config   OTLPConfig
	logger   *zap.Logger
	exporter *otlptrace.Exporter
}

// NewGRPCExporter creates a new OTLP/gRPC exporter
func NewGRPCExporter(config OTLPConfig, logger *zap.Logger) (*GRPCExporter, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("OTLP export is disabled")
	}

	// Build gRPC options
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.Endpoint),
		otlptracegrpc.WithTimeout(config.Timeout),
	}

	// Add insecure option if needed
	if config.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	// Add headers if present
	if len(config.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
	}

	// Create gRPC client
	client := otlptracegrpc.NewClient(opts...)

	// Create exporter
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	logger.Info("OTLP/gRPC exporter created",
		zap.String("endpoint", config.Endpoint),
		zap.Bool("insecure", config.Insecure),
		zap.Duration("timeout", config.Timeout),
	)

	return &GRPCExporter{
		client:   client,
		config:   config,
		logger:   logger,
		exporter: exporter,
	}, nil
}

// ExportSpans sends spans to the OTLP endpoint
func (e *GRPCExporter) ExportSpans(ctx context.Context, spans []*domain.OTELSpanData) error {
	if len(spans) == 0 {
		return nil
	}

	// Add timeout to context
	exportCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Add headers to context
	if len(e.config.Headers) > 0 {
		md := metadata.New(e.config.Headers)
		exportCtx = metadata.NewOutgoingContext(exportCtx, md)
	}

	// Transform domain spans to OTLP format
	otlpSpans, err := TransformSpansToOTLP(spans)
	if err != nil {
		e.logger.Error("Failed to transform spans",
			zap.Error(err),
			zap.Int("span_count", len(spans)),
		)
		return fmt.Errorf("span transformation failed: %w", err)
	}

	// Export via OTLP exporter
	if err := e.exporter.ExportSpans(exportCtx, otlpSpans); err != nil {
		e.logger.Error("Failed to export spans",
			zap.Error(err),
			zap.Int("span_count", len(spans)),
			zap.String("endpoint", e.config.Endpoint),
		)
		return fmt.Errorf("OTLP export failed: %w", err)
	}

	e.logger.Debug("Exported spans",
		zap.Int("span_count", len(spans)),
		zap.String("endpoint", e.config.Endpoint),
	)

	return nil
}

// Shutdown gracefully closes the exporter
func (e *GRPCExporter) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := e.exporter.Shutdown(shutdownCtx); err != nil {
		e.logger.Error("Failed to shutdown OTLP exporter", zap.Error(err))
		return fmt.Errorf("exporter shutdown failed: %w", err)
	}

	e.logger.Info("OTLP exporter shutdown complete")
	return nil
}

// Verify GRPCExporter implements OTLPExporter
var _ OTLPExporter = (*GRPCExporter)(nil)

// NewExporter creates the appropriate exporter based on config
func NewExporter(config OTLPConfig, logger *zap.Logger) (OTLPExporter, error) {
	if !config.Enabled {
		logger.Info("OTLP export disabled, using NoopExporter")
		return &NoopExporter{}, nil
	}

	return NewGRPCExporter(config, logger)
}

// Helper for gRPC dial options (used in tests)
func dialOptions(config OTLPConfig) []grpc.DialOption {
	opts := []grpc.DialOption{}

	if config.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	return opts
}
