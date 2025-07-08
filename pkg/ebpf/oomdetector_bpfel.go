//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"github.com/cilium/ebpf"
)

// Stub implementation for non-Linux platforms
type oomdetectorObjects struct {
	TrackMemoryAlloc *ebpf.Program
	TrackMemoryFree  *ebpf.Program
	TrackOomKill     *ebpf.Program
	TrackProcessExit *ebpf.Program
	ProcessMemory    *ebpf.Map
	Events           *ebpf.Map
}

func (o *oomdetectorObjects) Close() error {
	return nil // Stub implementation
}

type oomdetectorSpecs struct {
	OomDetector   *ebpf.ProgramSpec
	ProcessMemory *ebpf.MapSpec
	Events        *ebpf.MapSpec
}

func loadOomdetector() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"oom_detector": nil,
		},
		Maps: map[string]*ebpf.MapSpec{
			"process_memory": nil,
			"events":         nil,
		},
	}, nil
}
