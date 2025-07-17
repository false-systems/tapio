package ebpf_new

import (
	"runtime"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/linux"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/stub"
)

// NewCollector creates a new eBPF collector appropriate for the current platform
func NewCollector(config core.Config) (core.Collector, error) {
	switch runtime.GOOS {
	case "linux":
		return linux.NewCollector(config)
	default:
		return stub.NewCollector(config)
	}
}