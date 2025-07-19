package ebpf

import (
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// Monitor is an alias for the core Monitor interface
type Monitor = core.Monitor

// GetDetailedStatus returns detailed eBPF status information
func GetDetailedStatus() map[string]interface{} {
	return core.GetDetailedStatus()
}

// GetAvailabilityStatus returns a human-readable availability status
func GetAvailabilityStatus() string {
	return core.GetAvailabilityStatus()
}
