module github.com/falseyair/tapio/pkg/integrations/security

go 1.24.3

require (
	github.com/falseyair/tapio/pkg/domain v0.0.0
	github.com/falseyair/tapio/pkg/integrations/core v0.0.0
)

replace github.com/falseyair/tapio/pkg/domain => ../../domain

replace github.com/falseyair/tapio/pkg/integrations/core => ../core