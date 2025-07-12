//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"github.com/cilium/ebpf"
)

// DNSMonitor eBPF programs and maps
type dnsmonitorObjects struct {
	TrackDnsQuery    *ebpf.Program
	TrackDnsResponse *ebpf.Program
	DnsCache         *ebpf.Map
	Events        *ebpf.Map
}

func (o *dnsmonitorObjects) Close() error {
	if o.TrackDnsQuery != nil {
		o.TrackDnsQuery.Close()
	}
	if o.TrackDnsResponse != nil {
		o.TrackDnsResponse.Close()
	}
	if o.DnsCache != nil {
		o.DnsCache.Close()
	}
	if o.Events != nil {
		o.Events.Close()
	}
	return nil
}

type dnsmonitorSpecs struct {
	TrackDnsQuery    *ebpf.ProgramSpec
	TrackDnsResponse *ebpf.ProgramSpec
	DnsCache         *ebpf.MapSpec
	Events        *ebpf.MapSpec
}

func loadDnsmonitor() (*ebpf.CollectionSpec, error) {
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"track_dns_query":    nil,
			"track_dns_response": nil,
		},
		Maps: map[string]*ebpf.MapSpec{
			"dns_cache":  nil,
			"events": nil,
		},
	}, nil
}

func loadDnsmonitorObjects(obj *dnsmonitorObjects, opts *ebpf.CollectionOptions) error {
	// Stub implementation for non-Linux/non-eBPF builds
	return nil
}