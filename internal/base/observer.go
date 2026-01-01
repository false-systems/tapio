package base

import "context"

// Observer defines the interface all observers must implement for telemetry readiness checks.
// Note: Observers now use the deps pattern with Run(ctx) instead of Start/Stop.
// This interface is maintained for telemetry readiness endpoint compatibility.
type Observer interface {
	Start(ctx context.Context) error
	Stop() error
	Name() string
	IsHealthy() bool
}
