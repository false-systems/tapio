//go:build !linux && !darwin && !windows
// +build !linux,!darwin,!windows

package capabilities

import (
	"runtime"
	"github.com/yairfalse/tapio/pkg/capabilities/plugins"
)

func registerGenericCapabilities() error {
	// Basic memory monitoring
	nativePlugin := plugins.NewNativeMemoryPlugin()
	if err := Register(&pluginAdapter{plugin: nativePlugin}); err != nil {
		return err
	}

	// Everything else unavailable
	platform := runtime.GOOS
	
	ebpfPlugin := plugins.NewNotAvailablePlugin("ebpf-memory", "eBPF only available on Linux")
	if err := Register(&pluginAdapter{plugin: ebpfPlugin}); err != nil {
		return err
	}

	networkPlugin := plugins.NewNotAvailablePlugin("native-network", platform+" network monitoring not implemented")
	if err := Register(&pluginAdapter{plugin: networkPlugin}); err != nil {
		return err
	}

	systemPlugin := plugins.NewNotAvailablePlugin("native-system", platform+" system monitoring not implemented")  
	if err := Register(&pluginAdapter{plugin: systemPlugin}); err != nil {
		return err
	}

	return nil
}