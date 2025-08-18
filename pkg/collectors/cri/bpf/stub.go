//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type crimonitorObjects struct {
	crimonitorPrograms
	crimonitorMaps
}

func (o *crimonitorObjects) Close() error {
	return nil
}

type crimonitorPrograms struct {
	TraceContainerCreate *ebpf.Program
	TraceContainerStart  *ebpf.Program
	TraceContainerStop   *ebpf.Program
	TraceContainerRemove *ebpf.Program
}

func (p *crimonitorPrograms) Close() error {
	return nil
}

type crimonitorMaps struct {
	ContainerEvents *ebpf.Map
	ContainerStats  *ebpf.Map
}

func (m *crimonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadCrimonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadCrimonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
