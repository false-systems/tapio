//go:build !linux
// +build !linux

package etcd

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type etcdMonitorSpecs struct {
	etcdMonitorProgramSpecs
	etcdMonitorMapSpecs
}

type etcdMonitorProgramSpecs struct{}

type etcdMonitorMapSpecs struct{}

type etcdMonitorObjects struct {
	etcdMonitorPrograms
	etcdMonitorMaps
}

func (o *etcdMonitorObjects) Close() error {
	return nil
}

type etcdMonitorMaps struct{}

func (m *etcdMonitorMaps) Close() error {
	return nil
}

type etcdMonitorPrograms struct{}

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