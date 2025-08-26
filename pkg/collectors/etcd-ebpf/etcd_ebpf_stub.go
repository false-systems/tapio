//go:build !linux
// +build !linux

package etcdebpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms
// These are required for compilation on macOS/Windows where eBPF is not supported

type etcdMonitorSpecs struct {
	etcdMonitorProgramSpecs
	etcdMonitorMapSpecs
}

type etcdMonitorProgramSpecs struct {
	TraceSysEnterWrite *ebpf.ProgramSpec
	TraceSysEnterFsync *ebpf.ProgramSpec
}

type etcdMonitorMapSpecs struct {
	EtcdPids *ebpf.MapSpec
	Events   *ebpf.MapSpec
}

type etcdMonitorObjects struct {
	etcdMonitorPrograms
	etcdMonitorMaps
}

func (o *etcdMonitorObjects) Close() error {
	return nil
}

type etcdMonitorMaps struct {
	EtcdPids *ebpf.Map
	Events   *ebpf.Map
}

func (m *etcdMonitorMaps) Close() error {
	return nil
}

type etcdMonitorPrograms struct {
	TraceSysEnterWrite *ebpf.Program
	TraceSysEnterFsync *ebpf.Program
}

func (p *etcdMonitorPrograms) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadEtcdMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadEtcdMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
