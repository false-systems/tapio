package ebpf

// Config holds eBPF collector configuration
type Config struct {
	Name string
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name: "ebpf-collector",
	}
}
