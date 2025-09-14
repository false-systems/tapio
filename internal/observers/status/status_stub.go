//go:build linux
// +build linux

package status

import (
	"github.com/cilium/ebpf"
)

// Stub for statusObjects
type statusObjects struct {
	TraceConnect      *ebpf.Program
	TraceClose        *ebpf.Program
	ParseHttpResponse *ebpf.Program
	Events            *ebpf.Map
}

func (o *statusObjects) Close() error {
	return nil
}

func loadStatus() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Maps: map[string]*ebpf.MapSpec{
			"events": &ebpf.MapSpec{
				Type:       ebpf.PerfEventArray,
				MaxEntries: 1024,
			},
		},
		Programs: map[string]*ebpf.ProgramSpec{},
	}, nil
}
