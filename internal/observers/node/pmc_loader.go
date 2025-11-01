//go:build linux

package node

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/internal/observers/node/bpf"
	"golang.org/x/sys/unix"
)

// PMCLoader loads eBPF program and reads PMC events from ring buffer
type PMCLoader struct {
	mu      sync.Mutex
	objs    *bpf.NodePMCObjects
	links   []link.Link
	reader  *ringbuf.Reader
	eventCh chan PMCEvent
	stopCh  chan struct{}
	started bool
}

// NewPMCLoader creates a new PMC loader
func NewPMCLoader() (*PMCLoader, error) {
	return &PMCLoader{
		eventCh: make(chan PMCEvent, 100),
		stopCh:  make(chan struct{}),
	}, nil
}

// Start loads eBPF program and begins sampling PMC counters
func (l *PMCLoader) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		return fmt.Errorf("PMCLoader already started")
	}

	// Load eBPF objects (bpf2go-generated)
	objs := &bpf.NodePMCObjects{}
	if err := bpf.LoadNodePMCObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	l.objs = objs

	// Get eBPF program
	prog := objs.SamplePmc
	if prog == nil {
		l.cleanup()
		return fmt.Errorf("sample_pmc program not found in objects")
	}

	// Attach to perf_event for each CPU
	numCPU := runtime.NumCPU()
	l.links = make([]link.Link, 0, numCPU*3) // 3 PMC counters per CPU

	for cpu := 0; cpu < numCPU; cpu++ {
		// Attach to CPU cycles counter
		cyclesLink, err := l.attachPMCPerfEvent(prog, cpu, unix.PERF_COUNT_HW_CPU_CYCLES, objs.PmcCycles)
		if err != nil {
			l.cleanup()
			return fmt.Errorf("failed to attach cycles perf_event for CPU %d: %w", cpu, err)
		}
		l.links = append(l.links, cyclesLink)

		// Attach to instructions counter
		instrLink, err := l.attachPMCPerfEvent(prog, cpu, unix.PERF_COUNT_HW_INSTRUCTIONS, objs.PmcInstructions)
		if err != nil {
			l.cleanup()
			return fmt.Errorf("failed to attach instructions perf_event for CPU %d: %w", cpu, err)
		}
		l.links = append(l.links, instrLink)

		// Attach to stalls counter (may fail on some CPUs - not fatal)
		stallsLink, err := l.attachPMCPerfEvent(prog, cpu, unix.PERF_COUNT_HW_STALLED_CYCLES_FRONTEND, objs.PmcStalls)
		if err == nil {
			l.links = append(l.links, stallsLink)
		}
		// Ignore stalls error - not all CPUs support this counter
	}

	// Open ring buffer reader
	var err error
	l.reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		l.cleanup()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	l.started = true

	// Start reading events from ring buffer
	go l.readEvents(ctx)

	return nil
}

// Stop closes eBPF program and stops event reading
func (l *PMCLoader) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil // Already stopped or never started
	}

	close(l.stopCh)
	l.cleanup()
	l.started = false

	return nil
}

// Events returns channel for receiving PMC events
func (l *PMCLoader) Events() <-chan PMCEvent {
	return l.eventCh
}

// cleanup closes all resources
func (l *PMCLoader) cleanup() {
	// Close ring buffer reader
	if l.reader != nil {
		l.reader.Close()
		l.reader = nil
	}

	// Detach all perf_event links
	for _, link := range l.links {
		link.Close()
	}
	l.links = nil

	// Close eBPF objects
	if l.objs != nil {
		l.objs.Close()
		l.objs = nil
	}
}

// attachPMCPerfEvent attaches eBPF program to perf_event for PMC
func (l *PMCLoader) attachPMCPerfEvent(prog *ebpf.Program, cpu int, pmcType uint64, pmcMap *ebpf.Map) (link.Link, error) {
	// Configure perf event
	attr := &unix.PerfEventAttr{
		Type:   unix.PERF_TYPE_HARDWARE,
		Config: pmcType,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Sample: 100000000, // Sample every 100ms (100 million nanoseconds)
		Bits:   unix.PerfBitFreq,
	}

	// Open perf_event for specific CPU
	fd, err := unix.PerfEventOpen(attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("perf_event_open failed: %w", err)
	}

	// Update perf event array map with file descriptor
	cpuKey := uint32(cpu)
	fdValue := uint32(fd)
	if err := pmcMap.Update(&cpuKey, &fdValue, ebpf.UpdateAny); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to update PMC map: %w", err)
	}

	// Attach eBPF program to perf event
	lnk, err := link.AttachRawLink(link.RawLinkOptions{
		Target:  fd,
		Program: prog,
		Attach:  ebpf.AttachPerfEvent,
	})
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to attach program: %w", err)
	}

	return lnk, nil
}

// readEvents reads PMC events from ring buffer and sends to channel
func (l *PMCLoader) readEvents(ctx context.Context) {
	defer close(l.eventCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.stopCh:
			return
		default:
		}

		record, err := l.reader.Read()
		if err != nil {
			if err == ringbuf.ErrClosed {
				return
			}
			continue
		}

		// Parse PMC event from ring buffer
		if len(record.RawSample) < 40 { // sizeof(pmc_event) = 40 bytes
			continue
		}

		event := PMCEvent{
			CPU:          binary.LittleEndian.Uint32(record.RawSample[0:4]),
			Cycles:       binary.LittleEndian.Uint64(record.RawSample[4:12]),
			Instructions: binary.LittleEndian.Uint64(record.RawSample[12:20]),
			StallCycles:  binary.LittleEndian.Uint64(record.RawSample[20:28]),
			Timestamp:    binary.LittleEndian.Uint64(record.RawSample[28:36]),
		}

		select {
		case l.eventCh <- event:
		case <-ctx.Done():
			return
		case <-l.stopCh:
			return
		}
	}
}
