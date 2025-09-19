package status

import (
	"time"

	"github.com/yairfalse/tapio/internal/observers/common"
	"go.uber.org/zap"
)

func init() {
	// Registration will be handled by orchestrator when it's implemented
	// orchestrator.RegisterObserverFactory("status", Factory)
}

// Factory creates a new status observer from configuration
func Factory(cfg interface{}, logger *zap.Logger) (common.Observer, error) {
	config, ok := cfg.(*Config)
	if !ok {
		// Use default config if type assertion fails
		config = &Config{
			Enabled:         true,
			BufferSize:      10000,
			SampleRate:      0.01,
			MaxEventsPerSec: 1000,
			FlushInterval:   30 * time.Second,
			EnableL7Parse:   true,
			HTTPPorts:       []int{80, 8080, 8000, 3000},
			GRPCPorts:       []int{50051, 9090},
			Logger:          logger,
		}
	}

	return NewObserver("status", config)
}
