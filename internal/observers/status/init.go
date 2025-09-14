package status

import (
	"time"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/internal/observers/common"
)

func init() {
	// Registration will be handled by orchestrator when it's implemented
	// orchestrator.RegisterObserverFactory("status", Factory)
}

func Factory(cfg interface{}, logger *zap.Logger) (common.Observer, error) {
	config, ok := cfg.(*Config)
	if !ok {
		config = &Config{
			Enabled:         true,
			SampleRate:      0.01,
			MaxEventsPerSec: 1000,
			MaxMemoryMB:     100,
			FlushInterval:   10 * time.Second,
			RedactHeaders:   []string{"Authorization", "Cookie", "X-API-Key"},
		}
	}

	return NewObserver(config, logger)
}
