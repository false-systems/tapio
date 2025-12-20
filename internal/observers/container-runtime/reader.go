//go:build linux

package containerruntime

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/internal/observers/container"
)

// RingReader wraps cilium/ebpf ring buffer for typed event reading
type RingReader struct {
	ring *ringbuf.Reader
}

// RingRecord represents a record read from ring buffer
type RingRecord struct {
	Event container.ContainerEventBPF
	Raw   []byte
}

// NewRingReader creates a new ring buffer reader
func NewRingReader(ring *ringbuf.Reader) *RingReader {
	return &RingReader{
		ring: ring,
	}
}

// Read reads next event from ring buffer
func (r *RingReader) Read(ctx context.Context) (*RingRecord, error) {
	if r.ring == nil {
		return nil, fmt.Errorf("ring buffer is nil")
	}

	record, err := r.ring.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read from ring buffer: %w", err)
	}

	evt, err := r.ParseRecord(record.RawSample)
	if err != nil {
		return nil, fmt.Errorf("failed to parse record: %w", err)
	}

	return &RingRecord{
		Event: *evt,
		Raw:   record.RawSample,
	}, nil
}

// ParseRecord parses raw bytes into ContainerEventBPF
func (r *RingReader) ParseRecord(data []byte) (*container.ContainerEventBPF, error) {
	// C struct is 308 bytes (packed), Go struct is 312 bytes (with padding)
	// Issue #566: Added CgroupID field (8 bytes), changing 300 → 308
	if len(data) < 308 {
		return nil, fmt.Errorf("invalid record size: got %d, expected 308", len(data))
	}

	evt := &container.ContainerEventBPF{}
	buf := bytes.NewReader(data)

	if err := binary.Read(buf, binary.LittleEndian, evt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	return evt, nil
}

// Close closes the ring buffer reader
func (r *RingReader) Close() error {
	if r.ring == nil {
		return nil
	}

	if err := r.ring.Close(); err != nil {
		return fmt.Errorf("failed to close ring buffer: %w", err)
	}

	return nil
}
