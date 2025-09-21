//go:build linux
// +build linux

package services

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

// eBPF state for Linux implementation
type ebpfState struct {
	objs       *bpfObjects
	links      []link.Link
	perfReader *perf.Reader
}

// startEBPF starts eBPF connection tracking (Linux-specific)
func (t *ConnectionTracker) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load pre-compiled eBPF objects
	objs := &bpfObjects{}
	spec, err := loadBpf()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	if err := spec.LoadAndAssign(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Attach to kernel tracepoints
	links := []link.Link{}

	// TCP connect
	connectLink, err := link.Tracepoint("tcp", "tcp_connect", objs.TcpConnect)
	if err != nil {
		return fmt.Errorf("failed to attach tcp_connect: %w", err)
	}
	links = append(links, connectLink)

	// TCP accept
	acceptLink, err := link.Tracepoint("tcp", "tcp_accept", objs.TcpAccept)
	if err != nil {
		return fmt.Errorf("failed to attach tcp_accept: %w", err)
	}
	links = append(links, acceptLink)

	// TCP close
	closeLink, err := link.Tracepoint("tcp", "tcp_close", objs.TcpClose)
	if err != nil {
		return fmt.Errorf("failed to attach tcp_close: %w", err)
	}
	links = append(links, closeLink)

	// Create perf event reader
	perfReader, err := perf.NewReader(objs.Events, 4096)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}

	// Store eBPF state
	t.ebpfState = &ebpfState{
		objs:       objs,
		links:      links,
		perfReader: perfReader,
	}

	// Start event reader goroutine
	go t.readEBPFEvents()

	t.logger.Info("eBPF connection tracking started",
		zap.Int("attached_probes", len(links)))

	return nil
}

// stopEBPF stops eBPF connection tracking
func (t *ConnectionTracker) stopEBPF() {
	if t.ebpfState == nil {
		return
	}

	state := t.ebpfState.(*ebpfState)

	// Close perf reader
	if state.perfReader != nil {
		state.perfReader.Close()
	}

	// Detach probes
	for _, l := range state.links {
		l.Close()
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	t.logger.Info("eBPF connection tracking stopped")
}

// readEBPFEvents reads connection events from eBPF
func (t *ConnectionTracker) readEBPFEvents() {
	state := t.ebpfState.(*ebpfState)
	if state == nil || state.perfReader == nil {
		return
	}

	for {
		select {
		case <-t.stopCh:
			return
		default:
			record, err := state.perfReader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				t.logger.Error("Failed to read perf event", zap.Error(err))
				continue
			}

			// Parse the event
			event := &ConnectionEvent{}
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, event); err != nil {
				t.logger.Error("Failed to parse event", zap.Error(err))
				continue
			}

			// Send to event channel
			select {
			case t.eventCh <- event:
				// Successfully sent
			default:
				t.logger.Warn("Event channel full, dropping connection event")
			}
		}
	}
}

// bpfObjects contains all eBPF objects
type bpfObjects struct {
	TcpConnect *ebpf.Program `ebpf:"tcp_connect"`
	TcpAccept  *ebpf.Program `ebpf:"tcp_accept"`
	TcpClose   *ebpf.Program `ebpf:"tcp_close"`
	Events     *ebpf.Map     `ebpf:"events"`
}

// Close releases all resources
func (o *bpfObjects) Close() error {
	if o.TcpConnect != nil {
		o.TcpConnect.Close()
	}
	if o.TcpAccept != nil {
		o.TcpAccept.Close()
	}
	if o.TcpClose != nil {
		o.TcpClose.Close()
	}
	if o.Events != nil {
		o.Events.Close()
	}
	return nil
}

// loadBpf loads the compiled eBPF bytecode
func loadBpf() (*ebpf.CollectionSpec, error) {
	// Load the compiled eBPF bytecode
	spec, err := ebpf.LoadCollectionSpec("bpf/services.o")
	if err != nil {
		return nil, fmt.Errorf("failed to load eBPF collection: %w", err)
	}
	return spec, nil
}

// Helper to convert C array to Go slice
func cArrayToGoSlice(arr unsafe.Pointer, size int) []byte {
	return (*[1 << 30]byte)(arr)[:size:size]
}
