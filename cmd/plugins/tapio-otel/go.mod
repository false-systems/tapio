module github.com/yairfalse/tapio/cmd/plugins/tapio-otel

go 1.21

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/viper v1.19.0
	github.com/yairfalse/tapio/pkg/exports v0.0.0
	go.opentelemetry.io/otel v1.31.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.31.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.31.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.31.0
	go.opentelemetry.io/otel/metric v1.31.0
	go.opentelemetry.io/otel/sdk v1.31.0
	go.opentelemetry.io/otel/trace v1.31.0
)

replace github.com/yairfalse/tapio/pkg/exports => ../../../pkg/exports