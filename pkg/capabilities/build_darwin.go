//go:build darwin
// +build darwin

package capabilities

import (
	"github.com/yairfalse/tapio/pkg/capabilities/plugins"
)

func registerDarwinCapabilities() error {
	// Native memory monitoring
	nativePlugin := plugins.NewNativeMemoryPlugin()
	if err := Register(&pluginAdapter{plugin: nativePlugin}); err != nil {
		return err
	}

	// Other capabilities not available
	ebpfPlugin := plugins.NewNotAvailablePlugin("ebpf-memory", "eBPF only available on Linux")
	if err := Register(&pluginAdapter{plugin: ebpfPlugin}); err != nil {
		return err
	}

	networkPlugin := plugins.NewNotAvailablePlugin("native-network", "macOS network monitoring not yet implemented")
	if err := Register(&pluginAdapter{plugin: networkPlugin}); err != nil {
		return err
	}

	systemPlugin := plugins.NewNotAvailablePlugin("native-system", "macOS system monitoring not yet implemented")
	if err := Register(&pluginAdapter{plugin: systemPlugin}); err != nil {
		return err
	}

	return nil
}