//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"github.com/cilium/ebpf"
)

// PacketAnalyzer eBPF programs and maps
type packetanalyzerObjects struct {
	TrackPacketLoss       *ebpf.Program
	TrackPacketLatency    *ebpf.Program
	TrackPacketReorder    *ebpf.Program
	PacketStats           *ebpf.Map
	Events          *ebpf.Map
}

func (o *packetanalyzerObjects) Close() error {
	if o.TrackPacketLoss != nil {
		o.TrackPacketLoss.Close()
	}
	if o.TrackPacketLatency != nil {
		o.TrackPacketLatency.Close()
	}
	if o.TrackPacketReorder != nil {
		o.TrackPacketReorder.Close()
	}
	if o.PacketStats != nil {
		o.PacketStats.Close()
	}
	if o.Events != nil {
		o.Events.Close()
	}
	return nil
}

type packetanalyzerSpecs struct {
	TrackPacketLoss       *ebpf.ProgramSpec
	TrackPacketLatency    *ebpf.ProgramSpec
	TrackPacketReorder    *ebpf.ProgramSpec
	PacketStats           *ebpf.MapSpec
	Events          *ebpf.MapSpec
}

func loadPacketanalyzer() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"track_packet_loss":    nil,
			"track_packet_latency": nil,
			"track_packet_reorder": nil,
		},
		Maps: map[string]*ebpf.MapSpec{
			"packet_stats":  nil,
			"events": nil,
		},
	}, nil
}

func loadPacketanalyzerObjects(obj *packetanalyzerObjects, opts *ebpf.CollectionOptions) error {
	// Stub implementation for non-Linux/non-eBPF builds
	return nil
}