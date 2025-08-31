//go:build linux
// +build linux

package bpf

import (
	"fmt"
)

// NetworkmonitorObjects is an exported alias for the generated type
type NetworkmonitorObjects = networkmonitorObjects

// LoadNetworkMonitor loads the network monitoring eBPF collection
func LoadNetworkMonitor() (interface{}, error) {
	var objs networkmonitorObjects
	if err := loadNetworkmonitorObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading network monitor BPF objects: %w", err)
	}
	return &objs, nil
}
