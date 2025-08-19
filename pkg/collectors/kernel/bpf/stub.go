//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type kernelmonitorObjects struct {
	kernelmonitorPrograms
	kernelmonitorMaps
}

func (o *kernelmonitorObjects) Close() error {
	return nil
}

type kernelmonitorPrograms struct {
	TraceExec       *ebpf.Program
	TraceFree       *ebpf.Program
	TraceMalloc     *ebpf.Program
	TraceOpenat     *ebpf.Program
	TraceTcpConnect *ebpf.Program
}

func (p *kernelmonitorPrograms) Close() error {
	return nil
}

type kernelmonitorMaps struct {
	ContainerInfoMap    *ebpf.Map
	ContainerPids       *ebpf.Map
	Events              *ebpf.Map
	MountInfoMap        *ebpf.Map
	PodInfoMap          *ebpf.Map
	ProcessLineageMap   *ebpf.Map
	ServiceEndpointsMap *ebpf.Map
	VolumeInfoMap       *ebpf.Map
}

func (m *kernelmonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadKernelmonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadKernelmonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
