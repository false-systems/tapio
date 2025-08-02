package ebpf

import "go.uber.org/zap"

// Config holds eBPF collector configuration
type Config struct {
	Name    string
	NATSURL string // Optional NATS URL for publishing
	Logger  *zap.Logger
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "ebpf-collector",
		NATSURL: "", // Empty means no NATS publishing
		Logger:  zap.NewNop(),
	}
}
