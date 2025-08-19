//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type securitymonitorObjects struct {
	securitymonitorPrograms
	securitymonitorMaps
}

func (o *securitymonitorObjects) Close() error {
	return nil
}

type securitymonitorPrograms struct {
	TraceCommitCreds    *ebpf.Program
	TraceDoCoredump     *ebpf.Program
	TraceModuleFree     *ebpf.Program
	TraceModuleLoad     *ebpf.Program
	TraceProcessVmReadv *ebpf.Program
	TracePtraceAttach   *ebpf.Program
	TraceSetgid         *ebpf.Program
	TraceSetuid         *ebpf.Program
}

func (p *securitymonitorPrograms) Close() error {
	return nil
}

type securitymonitorMaps struct {
	ContainerPids       *ebpf.Map
	ProcessCapabilities *ebpf.Map
	SecurityEvents      *ebpf.Map
}

func (m *securitymonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadSecuritymonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadSecuritymonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
