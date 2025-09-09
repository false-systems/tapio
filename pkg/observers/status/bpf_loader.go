package status

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -Wall" status ./bpf/status.c

func (o *Observer) loadBPF() error {
	spec, err := loadStatus()
	if err != nil {
		return fmt.Errorf("loading BPF spec: %w", err)
	}

	o.objs = &statusObjects{}
	if err := spec.LoadAndAssign(o.objs, nil); err != nil {
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	o.perfReader, err = perf.NewReader(o.objs.Events, 4096)
	if err != nil {
		return fmt.Errorf("creating perf reader: %w", err)
	}

	return nil
}

func (o *Observer) attachProbes() error {
	kprobeConnect, err := link.Kprobe("tcp_connect", o.objs.TraceConnect, nil)
	if err != nil {
		return fmt.Errorf("attaching tcp_connect kprobe: %w", err)
	}
	o.links = append(o.links, kprobeConnect)

	kprobeClose, err := link.Kprobe("tcp_done", o.objs.TraceClose, nil)
	if err != nil {
		return fmt.Errorf("attaching tcp_done kprobe: %w", err)
	}
	o.links = append(o.links, kprobeClose)

	return nil
}

func parseStatusEvent(data []byte) (*StatusEvent, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("event data too short: %d bytes", len(data))
	}

	event := &StatusEvent{
		ServiceHash:  nativeEndian.Uint32(data[0:4]),
		EndpointHash: nativeEndian.Uint32(data[4:8]),
		StatusCode:   nativeEndian.Uint16(data[8:10]),
		ErrorType:    ErrorType(nativeEndian.Uint16(data[10:12])),
		Timestamp:    nativeEndian.Uint64(data[12:20]),
		Latency:      nativeEndian.Uint32(data[20:24]),
		PID:          nativeEndian.Uint32(data[24:28]),
	}

	return event, nil
}

var nativeEndian = getNativeEndian()

func getNativeEndian() binary.ByteOrder {
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)

	switch buf {
	case [2]byte{0xCD, 0xAB}:
		return binary.LittleEndian
	case [2]byte{0xAB, 0xCD}:
		return binary.BigEndian
	default:
		panic("unknown endian")
	}
}
