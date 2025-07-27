//go:build linux
// +build linux

package linux

import (
	"github.com/cilium/ebpf"
)

// Import types from parent package to avoid circular dependency
type memorytrackerObjects struct {
	memorytrackerPrograms
	memorytrackerMaps
}

type memorytrackerPrograms struct {
	TraceMmPageAlloc *ebpf.Program `ebpf:"trace_mm_page_alloc"`
	TraceMmPageFree  *ebpf.Program `ebpf:"trace_mm_page_free"`
}

type memorytrackerMaps struct {
	Events *ebpf.Map `ebpf:"events"`
}

func (o *memorytrackerObjects) Close() error {
	var err error
	if o.Events != nil {
		err = o.Events.Close()
	}
	if o.TraceMmPageAlloc != nil {
		if e := o.TraceMmPageAlloc.Close(); e != nil && err == nil {
			err = e
		}
	}
	if o.TraceMmPageFree != nil {
		if e := o.TraceMmPageFree.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Stub loader function for linux build
func loadMemorytrackerObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	// On actual Linux system, this would load the real eBPF bytecode
	// For now, return an error indicating eBPF programs need to be compiled
	return nil
}
