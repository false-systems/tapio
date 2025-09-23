package registration

import (
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
)

// RegisterObserver registers an observer factory with the orchestrator.
// This package exists at the same level as orchestrator to avoid
// architecture violations where lower levels import from higher levels.
//
// Usage in observer's init.go:
//
//	import "github.com/yairfalse/tapio/internal/observers/registration"
//	func init() {
//	    registration.RegisterObserver("dns", CreateFactory())
//	}
func RegisterObserver(name string, factory orchestrator.ObserverFactory) {
	orchestrator.RegisterObserverFactory(name, factory)
}
