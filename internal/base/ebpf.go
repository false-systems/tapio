//go:build linux
// +build linux

package base

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

// EBPFManager manages eBPF program lifecycle
type EBPFManager struct {
	collection *ebpf.Collection
	links      []link.Link
	ringbuf    *ringbuf.Reader
}

// LoadEBPF loads eBPF programs from compiled object file
func LoadEBPF(objectPath string) (*EBPFManager, error) {
	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("eBPF object file not found: %s: %w", objectPath, err)
	}

	spec, err := ebpf.LoadCollectionSpec(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load eBPF spec from %s: %w", objectPath, err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF collection: %w", err)
	}

	return &EBPFManager{
		collection: coll,
		links:      make([]link.Link, 0),
	}, nil
}

// AttachKprobe attaches a program to a kprobe
func (m *EBPFManager) AttachKprobe(progName, symbol string) error {
	prog := m.collection.Programs[progName]
	if prog == nil {
		return fmt.Errorf("program %s not found in collection", progName)
	}

	lnk, err := link.Kprobe(symbol, prog, nil)
	if err != nil {
		return fmt.Errorf("failed to attach kprobe %s to %s: %w", symbol, progName, err)
	}

	m.links = append(m.links, lnk)
	return nil
}

// AttachKretprobe attaches a program to a kretprobe
func (m *EBPFManager) AttachKretprobe(progName, symbol string) error {
	prog := m.collection.Programs[progName]
	if prog == nil {
		return fmt.Errorf("program %s not found in collection", progName)
	}

	lnk, err := link.Kretprobe(symbol, prog, nil)
	if err != nil {
		return fmt.Errorf("failed to attach kretprobe %s to %s: %w", symbol, progName, err)
	}

	m.links = append(m.links, lnk)
	return nil
}

// AttachTracepoint attaches a program to a tracepoint
func (m *EBPFManager) AttachTracepoint(progName, group, name string) error {
	prog := m.collection.Programs[progName]
	if prog == nil {
		return fmt.Errorf("program %s not found in collection", progName)
	}

	lnk, err := link.Tracepoint(group, name, prog, nil)
	if err != nil {
		return fmt.Errorf("failed to attach tracepoint %s:%s to %s: %w", group, name, progName, err)
	}

	m.links = append(m.links, lnk)
	return nil
}

// OpenRingBuffer opens a ring buffer for reading events
func (m *EBPFManager) OpenRingBuffer(mapName string) error {
	rb := m.collection.Maps[mapName]
	if rb == nil {
		return fmt.Errorf("ring buffer map %s not found in collection", mapName)
	}

	reader, err := ringbuf.NewReader(rb)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer %s: %w", mapName, err)
	}

	m.ringbuf = reader
	return nil
}

// ReadEvents reads events from ring buffer with timeout
func (m *EBPFManager) ReadEvents(ctx context.Context, handler func([]byte) error) error {
	if m.ringbuf == nil {
		return fmt.Errorf("ring buffer not opened")
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			record, err := m.ringbuf.Read()
			if err != nil {
				return fmt.Errorf("failed to read from ring buffer: %w", err)
			}

			if err := handler(record.RawSample); err != nil {
				return fmt.Errorf("event handler failed: %w", err)
			}
		}
	}
}

// GetMap returns an eBPF map by name
func (m *EBPFManager) GetMap(name string) (*ebpf.Map, error) {
	mp := m.collection.Maps[name]
	if mp == nil {
		return nil, fmt.Errorf("map %s not found in collection", name)
	}
	return mp, nil
}

// Close cleans up all eBPF resources
func (m *EBPFManager) Close() error {
	var errs []error

	if m.ringbuf != nil {
		if err := m.ringbuf.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close ring buffer: %w", err))
		}
	}

	for _, lnk := range m.links {
		if err := lnk.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close link: %w", err))
		}
	}

	if m.collection != nil {
		m.collection.Close()
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errs)
	}

	return nil
}

// WaitForReady waits for eBPF programs to be attached and ready
func (m *EBPFManager) WaitForReady(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for eBPF programs to be ready")
		case <-ticker.C:
			if len(m.links) > 0 {
				return nil
			}
		}
	}
}
