module github.com/falseyair/tapio/pkg/integrations/otel

go 1.24.3

require (
	github.com/falseyair/tapio/pkg/domain v0.0.0
	github.com/falseyair/tapio/pkg/integrations/core v0.0.0
	go.opentelemetry.io/otel v1.21.0
	go.opentelemetry.io/otel/metric v1.21.0
	go.opentelemetry.io/otel/trace v1.21.0
)

replace github.com/falseyair/tapio/pkg/domain => ../../domain

replace github.com/falseyair/tapio/pkg/integrations/core => ../core
