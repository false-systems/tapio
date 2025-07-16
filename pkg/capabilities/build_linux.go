//go:build linux
// +build linux

package capabilities

import (
	"github.com/yairfalse/tapio/pkg/capabilities/plugins"
)

func registerLinuxCapabilities() error {
	// Create plugin adapters that satisfy the Capability interface
	
	// eBPF capabilities (will check availability at runtime)
	ebpfPlugin := plugins.NewEBPFMemoryPlugin(nil)
	if err := Register(&pluginAdapter{plugin: ebpfPlugin}); err != nil {
		return err
	}

	// Native capabilities as fallback
	nativePlugin := plugins.NewNativeMemoryPlugin()
	if err := Register(&pluginAdapter{plugin: nativePlugin}); err != nil {
		return err
	}

	// Network capabilities (not yet implemented)
	networkPlugin := plugins.NewNotAvailablePlugin("ebpf-network", "eBPF network monitoring not yet implemented")
	if err := Register(&pluginAdapter{plugin: networkPlugin}); err != nil {
		return err
	}

	// System capabilities (journald)
	systemPlugin := plugins.NewNotAvailablePlugin("journald", "journald integration not yet implemented")
	if err := Register(&pluginAdapter{plugin: systemPlugin}); err != nil {
		return err
	}

	return nil
}