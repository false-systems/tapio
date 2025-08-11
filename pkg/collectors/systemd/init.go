package systemd

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	factoryregistry "github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the Systemd collector typed factory with error handling
	factory := NewSystemdFactory()
	if err := factoryregistry.RegisterTypedFactory("systemd", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Systemd typed factory: %v", err)
		log.Printf("Systemd collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := factoryregistry.Register("systemd", CreateCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Systemd legacy factory: %v", err)
	}
}

// CreateCollector creates a new systemd collector from config
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
func CreateCollector(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	cfg := DefaultConfig()

	// Override with provided config
	if bufferSize, ok := config["buffer_size"].(int); ok {
		cfg.BufferSize = bufferSize
	}
	if enableEBPF, ok := config["enable_ebpf"].(bool); ok {
		cfg.EnableEBPF = enableEBPF
	}
	if enableJournal, ok := config["enable_journal"].(bool); ok {
		cfg.EnableJournal = enableJournal
	}

	// Use factory function - get name from config or default
	name := "systemd"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	return NewCollector(name, cfg)
}
