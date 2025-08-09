package systemd

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the SystemD collector factory with error handling
	if err := registry.Register("systemd", CreateCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register SystemD collector: %v", err)
		log.Printf("SystemD collector will not be available")
	}
}

// CreateCollector creates a new systemd collector from config
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
