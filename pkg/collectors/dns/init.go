package dns

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	registry.Register("dns", CreateCollector)
}

// CreateCollector creates a new DNS collector from config
func CreateCollector(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	cfg := DefaultConfig()

	// Override with provided config
	if bufferSize, ok := config["buffer_size"].(int); ok {
		cfg.BufferSize = bufferSize
	}
	if iface, ok := config["interface"].(string); ok {
		cfg.Interface = iface
	}
	if enableEBPF, ok := config["enable_ebpf"].(bool); ok {
		cfg.EnableEBPF = enableEBPF
	}
	if enableSocket, ok := config["enable_socket"].(bool); ok {
		cfg.EnableSocket = enableSocket
	}

	// Use factory function - get name from config or default
	name := "dns"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	return NewCollector(name, cfg)
}
