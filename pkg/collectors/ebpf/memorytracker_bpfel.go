//go:build 386 || amd64 || arm || arm64 || loong64 || mips64le || mipsle || ppc64le || riscv64

package ebpf

import (
	"github.com/cilium/ebpf"
)

type memorytrackerMaps struct {
	Events *ebpf.Map `ebpf:"events"`
}

type memorytrackerPrograms struct {
	TraceMmPageAlloc *ebpf.Program `ebpf:"trace_mm_page_alloc"`
	TraceMmPageFree  *ebpf.Program `ebpf:"trace_mm_page_free"`
}

type memorytrackerObjects struct {
	memorytrackerPrograms
	memorytrackerMaps
}

func (o *memorytrackerObjects) Close() error {
	return _MemorytrackerClose(&o.memorytrackerMaps, &o.memorytrackerPrograms)
}

type memorytrackerSpecs struct {
	memorytrackerProgramSpecs
	memorytrackerMapSpecs
}

type memorytrackerMapSpecs struct {
	Events *ebpf.MapSpec `ebpf:"events"`
}

type memorytrackerProgramSpecs struct {
	TraceMmPageAlloc *ebpf.ProgramSpec `ebpf:"trace_mm_page_alloc"`
	TraceMmPageFree  *ebpf.ProgramSpec `ebpf:"trace_mm_page_free"`
}

func loadMemorytracker() (*ebpf.CollectionSpec, error) {
	// This is a stub - actual implementation would load from embedded bytecode
	return &ebpf.CollectionSpec{
		Maps: map[string]*ebpf.MapSpec{
			"events": {
				Type:      ebpf.PerfEventArray,
				KeySize:   4,
				ValueSize: 4,
			},
		},
		Programs: map[string]*ebpf.ProgramSpec{
			"trace_mm_page_alloc": {
				Type: ebpf.TracePoint,
			},
			"trace_mm_page_free": {
				Type: ebpf.TracePoint,
			},
		},
	}, nil
}

func loadMemorytrackerObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	spec, err := loadMemorytracker()
	if err != nil {
		return err
	}
	return spec.LoadAndAssign(obj, opts)
}

func _MemorytrackerClose(maps *memorytrackerMaps, progs *memorytrackerPrograms) error {
	var err error
	if maps.Events != nil {
		err = maps.Events.Close()
	}
	if progs.TraceMmPageAlloc != nil {
		if e := progs.TraceMmPageAlloc.Close(); e != nil && err == nil {
			err = e
		}
	}
	if progs.TraceMmPageFree != nil {
		if e := progs.TraceMmPageFree.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}
