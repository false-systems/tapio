//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type dnsmonitorObjects struct{}

func (o *dnsmonitorObjects) Close() error {
	return nil
}

type dnsmonitorPrograms struct{}

func (p *dnsmonitorPrograms) Close() error {
	return nil
}

type dnsmonitorMaps struct{}

func (m *dnsmonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadDnsmonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadDnsmonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}