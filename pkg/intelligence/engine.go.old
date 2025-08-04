package intelligence

import (
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Engine is the main correlation engine interface implementation
type engine struct {
	*correlation.Engine
}

// NewEngine creates a new correlation engine
func NewEngine(config correlation.EngineConfig, deps correlation.EngineDependencies) (Engine, error) {
	e, err := correlation.NewEngine(deps.Logger, config, deps.K8sClient, deps.Storage)
	if err != nil {
		return nil, err
	}
	return &engine{Engine: e}, nil
}
