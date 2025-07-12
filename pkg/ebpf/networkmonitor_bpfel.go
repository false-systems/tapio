//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"github.com/cilium/ebpf"
)

// NetworkMonitor eBPF programs and maps
type networkmonitorObjects struct {
	TraceConnect    *ebpf.Program
	TraceClose      *ebpf.Program
	TraceRetransmit *ebpf.Program
	TracePacketDrop *ebpf.Program
	Events          *ebpf.Map
}

func (o *networkmonitorObjects) Close() error {
	if o.TraceConnect != nil {
		o.TraceConnect.Close()
	}
	if o.TraceClose != nil {
		o.TraceClose.Close()
	}
	if o.TraceRetransmit != nil {
		o.TraceRetransmit.Close()
	}
	if o.TracePacketDrop != nil {
		o.TracePacketDrop.Close()
	}
	if o.Events != nil {
		o.Events.Close()
	}
	return nil
}

type networkmonitorSpecs struct {
	TraceConnect    *ebpf.ProgramSpec
	TraceClose      *ebpf.ProgramSpec
	TraceRetransmit *ebpf.ProgramSpec
	TracePacketDrop *ebpf.ProgramSpec
	Events          *ebpf.MapSpec
}

func loadNetworkmonitor() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"trace_connect":     nil,
			"trace_close":       nil,
			"trace_retransmit":  nil,
			"trace_packet_drop": nil,
		},
		Maps: map[string]*ebpf.MapSpec{
			"events": nil,
		},
	}, nil
}

func loadNetworkmonitorObjects(obj *networkmonitorObjects, opts *ebpf.CollectionOptions) error {
	// Stub implementation for non-Linux/non-eBPF builds
	return nil
}