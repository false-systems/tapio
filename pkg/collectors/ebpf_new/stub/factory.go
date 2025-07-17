//go:build !linux
// +build !linux

package stub

import (
	"runtime"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
)

// NewCollector creates a stub eBPF collector for non-Linux platforms
func NewCollector(config core.Config) (core.Collector, error) {
	return nil, core.NotSupportedError{
		Feature:  "eBPF collector",
		Platform: runtime.GOOS,
		Reason:   "eBPF is only supported on Linux",
	}
}