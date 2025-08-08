package kernel

// Config holds kernel collector configuration
type Config struct {
	Name string
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name: "kernel-collector",
	}
}
