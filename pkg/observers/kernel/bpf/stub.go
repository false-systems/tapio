//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types and functions for non-Linux platforms
// Note: The specific struct types are defined in the generated files on Linux platforms

// KernelmonitorObjects contains the eBPF objects (stub for non-Linux)
type KernelmonitorObjects struct {
	KernelmonitorPrograms
	KernelmonitorMaps
}

func (o *KernelmonitorObjects) Close() error {
	return nil
}

// KernelmonitorPrograms contains the eBPF programs (stub for non-Linux)
type KernelmonitorPrograms struct {
	TraceConfigAccess *ebpf.Program
	TracePodSyscalls  *ebpf.Program
}

func (p *KernelmonitorPrograms) Close() error {
	return nil
}

// KernelmonitorMaps contains the eBPF maps (stub for non-Linux)
type KernelmonitorMaps struct {
	ContainerInfoMap    *ebpf.Map
	ContainerPids       *ebpf.Map
	Events              *ebpf.Map
	MountInfoMap        *ebpf.Map
	PodInfoMap          *ebpf.Map
	ProcessLineageMap   *ebpf.Map
	ServiceEndpointsMap *ebpf.Map
	VolumeInfoMap       *ebpf.Map
}

func (m *KernelmonitorMaps) Close() error {
	return nil
}

func LoadKernelmonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func LoadKernelmonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
