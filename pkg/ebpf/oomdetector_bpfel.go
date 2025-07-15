//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"bytes"
	"fmt"
	"io"

	"github.com/cilium/ebpf"
)

// oomdetectorObjects contains all objects after they have been loaded into the kernel.
type oomdetectorObjects struct {
	oomdetectorPrograms
	oomdetectorMaps
}

func (o *oomdetectorObjects) Close() error {
	return _OomdetectorClose(
		&o.oomdetectorPrograms,
		&o.oomdetectorMaps,
	)
}

// oomdetectorMaps contains all maps after they have been loaded into the kernel.
type oomdetectorMaps struct {
	Events         *ebpf.Map `ebpf:"events"`
	LastAllocTime  *ebpf.Map `ebpf:"last_alloc_time"`
	ProcessMemory  *ebpf.Map `ebpf:"process_memory"`
}

func (m *oomdetectorMaps) Close() error {
	return _OomdetectorClose(
		m.Events,
		m.LastAllocTime,
		m.ProcessMemory,
	)
}

// oomdetectorPrograms contains all programs after they have been loaded into the kernel.
type oomdetectorPrograms struct {
	TrackMemoryAlloc *ebpf.Program `ebpf:"track_memory_alloc"`
	TrackMemoryFree  *ebpf.Program `ebpf:"track_memory_free"`
	TrackOomKill     *ebpf.Program `ebpf:"track_oom_kill"`
	TrackProcessExit *ebpf.Program `ebpf:"track_process_exit"`
}

func (p *oomdetectorPrograms) Close() error {
	return _OomdetectorClose(
		p.TrackMemoryAlloc,
		p.TrackMemoryFree,
		p.TrackOomKill,
		p.TrackProcessExit,
	)
}

func _OomdetectorClose(closers ...io.Closer) error {
	for _, closer := range closers {
		if closer != nil {
			if err := closer.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadOomdetector returns the embedded CollectionSpec for oomdetector.
func loadOomdetector() (*ebpf.CollectionSpec, error) {
	// For development without compiled BPF programs, return a minimal spec
	// that allows the collector to initialize but will use the enhanced collector
	return &ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			"track_memory_alloc": {
				Name:    "track_memory_alloc",
				Type:    ebpf.TracePoint,
				License: "GPL",
			},
			"track_memory_free": {
				Name:    "track_memory_free", 
				Type:    ebpf.TracePoint,
				License: "GPL",
			},
			"track_oom_kill": {
				Name:    "track_oom_kill",
				Type:    ebpf.TracePoint,
				License: "GPL",
			},
			"track_process_exit": {
				Name:    "track_process_exit",
				Type:    ebpf.TracePoint,
				License: "GPL",
			},
		},
		Maps: map[string]*ebpf.MapSpec{
			"events": {
				Type:       ebpf.RingBuf,
				MaxEntries: 256 * 1024,
			},
			"process_memory": {
				Type:       ebpf.Hash,
				MaxEntries: 10240,
				KeySize:    4,   // uint32
				ValueSize:  8,   // uint64
			},
			"last_alloc_time": {
				Type:       ebpf.Hash,
				MaxEntries: 1024,
				KeySize:    4,   // uint32
				ValueSize:  8,   // uint64
			},
		},
	}, nil
}

// parseRawMemoryEvent parses a raw event from the ring buffer
func parseRawMemoryEvent(data []byte) (*MemoryEvent, error) {
	if len(data) < 64 { // Minimum size for a memory event
		return nil, fmt.Errorf("event data too small: %d bytes", len(data))
	}

	// Parse the binary data into a MemoryEvent
	event := &MemoryEvent{
		Timestamp:    readUint64(data[0:8]),
		PID:          readUint32(data[8:12]),
		TID:          readUint32(data[12:16]),
		Size:         readUint64(data[16:24]),
		TotalMemory:  readUint64(data[24:32]),
		EventType:    EventType(readUint32(data[32:36])),
		InContainer:  data[36] != 0,
		ContainerPID: readUint32(data[37:41]),
	}

	// Extract command string (null-terminated)
	cmdStart := 41
	cmdEnd := bytes.IndexByte(data[cmdStart:], 0)
	if cmdEnd > 0 && cmdStart+cmdEnd < len(data) {
		event.Command = string(data[cmdStart : cmdStart+cmdEnd])
	}

	return event, nil
}

func readUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func readUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
