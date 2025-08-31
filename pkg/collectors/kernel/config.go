package kernel

// Config holds simple kernel collector configuration
type Config struct {
	Name       string `json:"name" yaml:"name"`
	BufferSize int    `json:"buffer_size" yaml:"buffer_size"`
	EnableEBPF bool   `json:"enable_ebpf" yaml:"enable_ebpf"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig(name string) *Config {
	return &Config{
		Name:       name,
		BufferSize: 10000,
		EnableEBPF: true,
	}
}
