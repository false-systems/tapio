package base

import (
	"context"
	"os"
	"runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// TapioResourceConfig holds resource configuration for OTEL
type TapioResourceConfig struct {
	ClusterID   string
	Namespace   string
	NodeName    string
	ServiceName string
	Version     string
}

// CreateTapioResource creates OTEL resource with Tapio agent metadata
// Resource represents the entity producing telemetry (service, instance, host)
func CreateTapioResource(ctx context.Context, config TapioResourceConfig) (*resource.Resource, error) {
	// Default values
	if config.ServiceName == "" {
		config.ServiceName = "tapio-agent"
	}
	if config.Version == "" {
		config.Version = os.Getenv("TAPIO_VERSION")
		if config.Version == "" {
			config.Version = "dev"
		}
	}

	return resource.New(ctx,
		resource.WithAttributes(
			// Service identification
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.Version),
			semconv.ServiceInstanceID(config.NodeName),

			// Kubernetes context (if available)
			semconv.K8SClusterName(config.ClusterID),
			semconv.K8SNamespaceName(config.Namespace),
			semconv.K8SNodeName(config.NodeName),

			// Host information
			semconv.HostName(config.NodeName),
			attribute.String("host.arch", runtime.GOARCH),
			semconv.OSName(runtime.GOOS),

			// Telemetry SDK
			semconv.TelemetrySDKName("opentelemetry"),
			semconv.TelemetrySDKLanguageGo,
			semconv.TelemetrySDKVersion(otel.Version()),
		),
	)
}
