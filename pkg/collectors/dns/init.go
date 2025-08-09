package dns

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the DNS collector factory with error handling
	if err := registry.Register("dns", CreateCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register DNS collector: %v", err)
		log.Printf("DNS collector will not be available")
	}
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
