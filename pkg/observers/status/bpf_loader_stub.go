//go:build !linux
// +build !linux

package status

import (
	"errors"

	"github.com/cilium/ebpf"
)

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
	return nil, errors.New("eBPF not supported on this platform")
}