package correlation

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelatorFactory creates a correlator instance with the provided dependencies
type CorrelatorFactory func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error)

// CorrelatorRegistration holds the metadata and factory for a correlator type
type CorrelatorRegistration struct {
	Name        string
	Description string
	Factory     CorrelatorFactory
}

// CorrelatorRegistry manages the registration and creation of correlators
// This replaces the complex switch-based factory pattern in engine.go
type CorrelatorRegistry struct {
	mu          sync.RWMutex
	correlators map[string]*CorrelatorRegistration
	logger      *zap.Logger
}

// NewCorrelatorRegistry creates a new correlator registry
func NewCorrelatorRegistry(logger *zap.Logger) *CorrelatorRegistry {
	registry := &CorrelatorRegistry{
		correlators: make(map[string]*CorrelatorRegistration),
		logger:      logger,
	}

	// Register all built-in correlators
	registry.registerBuiltInCorrelators()

	return registry
}

// Register adds a new correlator factory to the registry
func (r *CorrelatorRegistry) Register(name, description string, factory CorrelatorFactory) error {
	if name == "" {
		return fmt.Errorf("correlator name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("correlator factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.correlators[name]; exists {
		return fmt.Errorf("correlator %s is already registered", name)
	}

	r.correlators[name] = &CorrelatorRegistration{
		Name:        name,
		Description: description,
		Factory:     factory,
	}

	r.logger.Debug("Correlator registered",
		zap.String("name", name),
		zap.String("description", description))

	return nil
}

// Create creates a correlator instance by name
func (r *CorrelatorRegistry) Create(ctx context.Context, name string, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
	r.mu.RLock()
	registration, exists := r.correlators[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown correlator type: %s", name)
	}

	correlator, err := registration.Factory(ctx, logger, k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create correlator %s: %w", name, err)
	}

	r.logger.Debug("Correlator created",
		zap.String("name", name),
		zap.String("type", fmt.Sprintf("%T", correlator)))

	return correlator, nil
}

// ListAvailable returns all registered correlator names
func (r *CorrelatorRegistry) ListAvailable() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.correlators))
	for name := range r.correlators {
		names = append(names, name)
	}
	return names
}

// GetInfo returns registration info for a correlator
func (r *CorrelatorRegistry) GetInfo(name string) (*CorrelatorRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, exists := r.correlators[name]
	if !exists {
		return nil, fmt.Errorf("correlator %s not found", name)
	}

	// Return a copy to prevent external modification
	return &CorrelatorRegistration{
		Name:        registration.Name,
		Description: registration.Description,
		Factory:     registration.Factory,
	}, nil
}

// registerBuiltInCorrelators registers all standard correlator types
// This replaces the switch statement in engine.go with a clean registration pattern
func (r *CorrelatorRegistry) registerBuiltInCorrelators() {
	// K8s correlator with special start requirement
	r.correlators["k8s"] = &CorrelatorRegistration{
		Name:        "k8s",
		Description: "Kubernetes resource correlation and event analysis",
		Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			if k8sClient == nil {
				return nil, fmt.Errorf("k8s correlator requires k8s client")
			}

			correlator := NewK8sCorrelator(logger, k8sClient)

			// K8s correlator requires starting for resource watching
			if err := correlator.Start(ctx); err != nil {
				return nil, fmt.Errorf("failed to start K8s correlator: %w", err)
			}

			return correlator, nil
		},
	}

	// Temporal correlator
	r.correlators["temporal"] = &CorrelatorRegistration{
		Name:        "temporal",
		Description: "Time-based event correlation and sequence analysis",
		Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return NewTemporalCorrelator(logger, *TestTemporalConfig()), nil
		},
	}

	// Sequence correlator
	r.correlators["sequence"] = &CorrelatorRegistration{
		Name:        "sequence",
		Description: "Sequential event pattern correlation",
		Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return NewSequenceCorrelator(logger, *TestSequenceConfig()), nil
		},
	}

	// Performance correlator
	r.correlators["performance"] = &CorrelatorRegistration{
		Name:        "performance",
		Description: "Performance metric correlation and anomaly detection",
		Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return NewPerformanceCorrelator(logger), nil
		},
	}

	// Service map correlator
	r.correlators["servicemap"] = &CorrelatorRegistration{
		Name:        "servicemap",
		Description: "Service topology correlation and dependency mapping",
		Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return NewServiceMapCorrelator(logger), nil
		},
	}

	// Dependency correlator - requires graph store (TEMPORARILY DISABLED - needs Process method implementation)
	// r.correlators["dependency"] = &CorrelatorRegistration{
	// 	Name:        "dependency",
	// 	Description: "Service dependency correlation and failure propagation analysis",
	// 	Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
	// 		// Create a mock graph store for testing
	// 		mockStore := &MockGraphStore{}
	// 		return NewDependencyCorrelator(mockStore, logger)
	// 	},
	// }

	// Ownership correlator - requires graph store (TEMPORARILY DISABLED - needs Process method implementation)
	// r.correlators["ownership"] = &CorrelatorRegistration{
	// 	Name:        "ownership",
	// 	Description: "Resource ownership correlation and responsible entity tracking",
	// 	Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
	// 		// Create a mock graph store for testing
	// 		mockStore := &MockGraphStore{}
	// 		return NewOwnershipCorrelator(mockStore, logger)
	// 	},
	// }

	// Config impact correlator - requires graph store (TEMPORARILY DISABLED - needs Process method implementation)
	// r.correlators["config-impact"] = &CorrelatorRegistration{
	// 	Name:        "config-impact",
	// 	Description: "Configuration change impact analysis and correlation",
	// 	Factory: func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
	// 		// Create a mock graph store for testing
	// 		mockStore := &MockGraphStore{}
	// 		return NewConfigImpactCorrelator(mockStore, logger)
	// 	},
	// }

	r.logger.Info("Built-in correlators registered",
		zap.Int("count", len(r.correlators)),
		zap.Strings("types", r.ListAvailable()))
}
