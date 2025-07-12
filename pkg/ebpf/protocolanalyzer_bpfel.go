//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"github.com/cilium/ebpf"
)

// ProtocolAnalyzer eBPF programs and maps
type protocolanalyzerObjects struct {
	TrackHttpRequest  *ebpf.Program
	TrackHttpResponse *ebpf.Program
	TrackGrpcCall     *ebpf.Program
	ProtocolStats     *ebpf.Map
	Events            *ebpf.Map
}

func (o *protocolanalyzerObjects) Close() error {
	if o.TrackHttpRequest != nil {
		o.TrackHttpRequest.Close()
	}
	if o.TrackHttpResponse != nil {
		o.TrackHttpResponse.Close()
	}
	if o.TrackGrpcCall != nil {
		o.TrackGrpcCall.Close()
	}
	if o.ProtocolStats != nil {
		o.ProtocolStats.Close()
	}
	if o.Events != nil {
		o.Events.Close()
	}
	return nil
}

type protocolanalyzerSpecs struct {
	TrackHttpRequest  *ebpf.ProgramSpec
	TrackHttpResponse *ebpf.ProgramSpec
	TrackGrpcCall     *ebpf.ProgramSpec
	ProtocolStats     *ebpf.MapSpec
	Events            *ebpf.MapSpec
}

func loadProtocolanalyzer() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"track_http_request":  nil,
			"track_http_response": nil,
			"track_grpc_call":     nil,
		},
		Maps: map[string]*ebpf.MapSpec{
			"protocol_stats": nil,
			"events":         nil,
		},
	}, nil
}

func loadProtocolanalyzerObjects(obj *protocolanalyzerObjects, opts *ebpf.CollectionOptions) error {
	// Stub implementation for non-Linux/non-eBPF builds
	return nil
}
