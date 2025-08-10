package dns

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the DNS collector typed factory with error handling
	factory := NewDNSFactory()
	if err := registry.RegisterTypedFactory("dns", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register DNS typed factory: %v", err)
		log.Printf("DNS collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := registry.Register("dns", CreateCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register DNS legacy factory: %v", err)
	}
}

// CreateCollector creates a new DNS collector from config
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
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

