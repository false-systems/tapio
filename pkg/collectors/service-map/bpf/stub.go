//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types and functions for non-Linux platforms

// ServicemonitorObjects contains the eBPF objects (stub for non-Linux)
type ServicemonitorObjects struct {
	ServicemonitorPrograms
	ServicemonitorMaps
}

func (o *ServicemonitorObjects) Close() error {
	return nil
}

// ServicemonitorPrograms contains the eBPF programs (stub for non-Linux)
type ServicemonitorPrograms struct {
	TraceTcpConnect     *ebpf.Program
	TraceTcpAccept      *ebpf.Program
	TraceTcpSendmsg     *ebpf.Program
	TraceTcpCleanupRbuf *ebpf.Program
	TraceTcpClose       *ebpf.Program
	TraceUdpSendmsg     *ebpf.Program
}

func (p *ServicemonitorPrograms) Close() error {
	return nil
}

// ServicemonitorMaps contains the eBPF maps (stub for non-Linux)
type ServicemonitorMaps struct {
	Connections *ebpf.Map
	Events      *ebpf.Map
}

func (m *ServicemonitorMaps) Close() error {
	return nil
}

// LoadServicemonitor is a stub for non-Linux platforms
func LoadServicemonitor() (*ServicemonitorObjects, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

// LoadServicemonitorObjects is a stub for non-Linux platforms
func LoadServicemonitorObjects(obj interface{}, opts interface{}) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
