//go:build linux
// +build linux

package linux

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Helper functions
func int32Ptr(v int32) *int32 {
	return &v
}

// eventParser implements core.EventParser for Linux
type eventParser struct {
	// Parser configuration
	hostByteOrder binary.ByteOrder
}

// NewEventParser creates a new Linux eBPF event parser
func NewEventParser() core.EventParser {
	return &eventParser{
		hostByteOrder: getNativeEndian(),
	}
}

// Parse implements core.EventParser
func (ep *eventParser) Parse(data []byte, eventType core.EventType) (domain.Event, error) {
	if len(data) == 0 {
		return domain.Event{}, core.InvalidEventError{Reason: "empty event data"}
	}

	switch eventType {
	case core.EventTypeSyscall:
		return ep.parseSyscallEvent(data)
	case core.EventTypeNetworkIn, core.EventTypeNetworkOut:
		return ep.parseNetworkEvent(data, eventType)
	case core.EventTypeProcessExec:
		return ep.parseProcessExecEvent(data)
	case core.EventTypeProcessExit:
		return ep.parseProcessExitEvent(data)
	case core.EventTypeMemoryAlloc:
		return ep.parseMemoryAllocEvent(data)
	case core.EventTypeFileIO:
		return ep.parseFileIOEvent(data)
	default:
		return ep.parseCustomEvent(data)
	}
}

// CanParse implements core.EventParser
func (ep *eventParser) CanParse(eventType core.EventType) bool {
	switch eventType {
	case core.EventTypeSyscall,
		core.EventTypeNetworkIn,
		core.EventTypeNetworkOut,
		core.EventTypeProcessExec,
		core.EventTypeProcessExit,
		core.EventTypeMemoryAlloc,
		core.EventTypeMemoryFree,
		core.EventTypeFileIO:
		return true
	default:
		return true // Can parse custom events
	}
}

// Event structures for parsing

type syscallEvent struct {
	Timestamp  uint64
	PID        uint32
	TID        uint32
	UID        uint32
	GID        uint32
	SyscallNr  uint32
	Args       [6]uint64
	ReturnCode int64
	Comm       [16]byte
}

type networkEvent struct {
	Timestamp   uint64
	PID         uint32
	Protocol    uint8
	Family      uint8
	SourcePort  uint16
	DestPort    uint16
	SourceAddr  [16]byte
	DestAddr    [16]byte
	BytesSent   uint64
	BytesRecv   uint64
	PacketCount uint32
	Comm        [16]byte
}

type processExecEvent struct {
	Timestamp  uint64
	PID        uint32
	PPID       uint32
	UID        uint32
	GID        uint32
	ReturnCode int32
	Filename   [256]byte
	Args       [512]byte
	Comm       [16]byte
}

type processExitEvent struct {
	Timestamp  uint64
	PID        uint32
	PPID       uint32
	ExitCode   int32
	Signal     uint32
	CoreDumped uint8
	Comm       [16]byte
}

type memoryAllocEvent struct {
	Timestamp   uint64
	PID         uint32
	AllocSize   uint64
	AllocAddr   uint64
	CallSite    uint64
	GFPFlags    uint32
	AllocType   uint8
	NodeID      uint16
	Comm        [16]byte
}

type fileIOEvent struct {
	Timestamp   uint64
	PID         uint32
	FileHandle  uint64
	Offset      uint64
	Count       uint64
	Flags       uint32
	Mode        uint32
	Operation   uint8
	Filename    [256]byte
	Comm        [16]byte
}

// Parsing methods

func (ep *eventParser) parseSyscallEvent(data []byte) (domain.Event, error) {
	if len(data) < binary.Size(syscallEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeSyscall,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event syscallEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeSyscall,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	// Convert to domain event
	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  determineSyscallSeverity(event.SyscallNr),
		Payload: domain.SystemEventPayload{
			Component:   "syscall",
			Operation:   syscallNumberToName(event.SyscallNr),
			Status:      determineSyscallStatus(event.ReturnCode),
			Message:     formatSyscallMessage(event),
			ErrorCode:   event.ReturnCode,
			Details:     formatSyscallDetails(event),
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			UID: int32Ptr(int32(event.UID)),
			GID: int32Ptr(int32(event.GID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseNetworkEvent(data []byte, eventType core.EventType) (domain.Event, error) {
	if len(data) < binary.Size(networkEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: eventType,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event networkEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: eventType,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	// Convert addresses
	var srcAddr, dstAddr string
	if event.Family == 2 { // AF_INET
		srcAddr = net.IP(event.SourceAddr[:4]).String()
		dstAddr = net.IP(event.DestAddr[:4]).String()
	} else if event.Family == 10 { // AF_INET6
		srcAddr = net.IP(event.SourceAddr[:]).String()
		dstAddr = net.IP(event.DestAddr[:]).String()
	}

	// Determine direction
	direction := "outbound"
	if eventType == core.EventTypeNetworkIn {
		direction = "inbound"
	}

	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeService,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  domain.SeverityInfo,
		Payload: domain.ServiceEventPayload{
			ServiceName: string(bytes.TrimRight(event.Comm[:], "\x00")),
			Operation:   fmt.Sprintf("network_%s", direction),
			Status:      "success",
			Message:     formatNetworkMessage(event, srcAddr, dstAddr, direction),
			Details: map[string]interface{}{
				"protocol":     getProtocolName(event.Protocol),
				"source_addr":  srcAddr,
				"source_port":  event.SourcePort,
				"dest_addr":    dstAddr,
				"dest_port":    event.DestPort,
				"bytes_sent":   event.BytesSent,
				"bytes_recv":   event.BytesRecv,
				"packet_count": event.PacketCount,
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseProcessExecEvent(data []byte) (domain.Event, error) {
	if len(data) < binary.Size(processExecEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeProcessExec,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event processExecEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeProcessExec,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	filename := string(bytes.TrimRight(event.Filename[:], "\x00"))
	args := string(bytes.TrimRight(event.Args[:], "\x00"))

	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  domain.SeverityInfo,
		Payload: domain.SystemEventPayload{
			Component: "process",
			Operation: "exec",
			Status:    determineExecStatus(event.ReturnCode),
			Message:   fmt.Sprintf("Process %d executed %s", event.PID, filename),
			Details: map[string]interface{}{
				"filename":    filename,
				"args":        args,
				"ppid":        event.PPID,
				"uid":         event.UID,
				"gid":         event.GID,
				"return_code": event.ReturnCode,
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			UID: int32Ptr(int32(event.UID)),
			GID: int32Ptr(int32(event.GID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
				"ppid":         fmt.Sprintf("%d", event.PPID),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseProcessExitEvent(data []byte) (domain.Event, error) {
	if len(data) < binary.Size(processExitEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeProcessExit,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event processExitEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeProcessExit,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	severity := domain.SeverityInfo
	if event.Signal != 0 || event.CoreDumped != 0 {
		severity = domain.SeverityWarn
	}

	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  severity,
		Payload: domain.SystemEventPayload{
			Component: "process",
			Operation: "exit",
			Status:    "completed",
			Message:   fmt.Sprintf("Process %d exited with code %d", event.PID, event.ExitCode),
			Details: map[string]interface{}{
				"exit_code":   event.ExitCode,
				"signal":      event.Signal,
				"core_dumped": event.CoreDumped != 0,
				"ppid":        event.PPID,
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
				"ppid":         fmt.Sprintf("%d", event.PPID),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseMemoryAllocEvent(data []byte) (domain.Event, error) {
	if len(data) < binary.Size(memoryAllocEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeMemoryAlloc,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event memoryAllocEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeMemoryAlloc,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	severity := domain.SeverityInfo
	if event.AllocSize > 1024*1024 { // Large allocation
		severity = domain.SeverityWarn
	}

	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  severity,
		Payload: domain.SystemEventPayload{
			Component: "memory",
			Operation: "alloc",
			Status:    "success",
			Message:   fmt.Sprintf("Process %d allocated %d bytes", event.PID, event.AllocSize),
			Details: map[string]interface{}{
				"size":       event.AllocSize,
				"address":    fmt.Sprintf("0x%x", event.AllocAddr),
				"call_site":  fmt.Sprintf("0x%x", event.CallSite),
				"gfp_flags":  event.GFPFlags,
				"alloc_type": getAllocTypeName(event.AllocType),
				"node_id":    event.NodeID,
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseFileIOEvent(data []byte) (domain.Event, error) {
	if len(data) < binary.Size(fileIOEvent{}) {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeFileIO,
			DataSize:  len(data),
			Cause:     fmt.Errorf("insufficient data size"),
		}
	}

	var event fileIOEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, ep.hostByteOrder, &event); err != nil {
		return domain.Event{}, core.ParseError{
			EventType: core.EventTypeFileIO,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	filename := string(bytes.TrimRight(event.Filename[:], "\x00"))
	operation := getFileOperationName(event.Operation)

	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Severity:  domain.SeverityInfo,
		Payload: domain.SystemEventPayload{
			Component: "filesystem",
			Operation: operation,
			Status:    "success",
			Message:   fmt.Sprintf("Process %d performed %s on %s", event.PID, operation, filename),
			Details: map[string]interface{}{
				"filename": filename,
				"offset":   event.Offset,
				"count":    event.Count,
				"flags":    event.Flags,
				"mode":     event.Mode,
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(int32(event.PID)),
			Labels: domain.Labels{
				"process_name": string(bytes.TrimRight(event.Comm[:], "\x00")),
			},
		},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

func (ep *eventParser) parseCustomEvent(data []byte) (domain.Event, error) {
	// For custom events, we create a generic event with raw data
	return domain.Event{
		ID:        generateEventID(),
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
		Severity:  domain.SeverityInfo,
		Payload: domain.LogEventPayload{
			Level:   "info",
			Message: "Custom eBPF event",
			Logger:  "ebpf",
			Details: map[string]interface{}{
				"data_size": len(data),
				"data_hex":  fmt.Sprintf("%x", data),
			},
		},
		Context: domain.EventContext{},
		Metadata: domain.EventMetadata{
			CollectedAt: time.Now(),
			Collector:   "ebpf",
			Version:     "1.0.0",
		},
	}, nil
}

// Helper functions

func getNativeEndian() binary.ByteOrder {
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)

	if buf[0] == 0xCD {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func generateEventID() string {
	// In a real implementation, use a proper UUID generator
	return fmt.Sprintf("ebpf-%d", time.Now().UnixNano())
}

func syscallNumberToName(nr uint32) string {
	// Simplified syscall name mapping
	// In a real implementation, use proper syscall tables
	switch nr {
	case 0:
		return "read"
	case 1:
		return "write"
	case 2:
		return "open"
	case 3:
		return "close"
	case 59:
		return "execve"
	case 60:
		return "exit"
	default:
		return fmt.Sprintf("syscall_%d", nr)
	}
}

func determineSyscallSeverity(nr uint32) domain.Severity {
	// Security-sensitive syscalls get higher severity
	switch nr {
	case 59, 60, 62, 101, 102, 103, 104, 105: // exec, exit, kill, ptrace, etc.
		return domain.SeverityWarn
	default:
		return domain.SeverityInfo
	}
}

func determineSyscallStatus(returnCode int64) string {
	if returnCode < 0 {
		return "failed"
	}
	return "success"
}

func formatSyscallMessage(event syscallEvent) string {
	return fmt.Sprintf("Process %d (%s) called %s",
		event.PID,
		string(bytes.TrimRight(event.Comm[:], "\x00")),
		syscallNumberToName(event.SyscallNr))
}

func formatSyscallDetails(event syscallEvent) map[string]interface{} {
	return map[string]interface{}{
		"syscall_nr":  event.SyscallNr,
		"args":        event.Args,
		"return_code": event.ReturnCode,
		"tid":         event.TID,
		"uid":         event.UID,
		"gid":         event.GID,
	}
}

func getProtocolName(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("proto_%d", proto)
	}
}

func formatNetworkMessage(event networkEvent, srcAddr, dstAddr, direction string) string {
	return fmt.Sprintf("%s %s connection: %s:%d -> %s:%d",
		getProtocolName(event.Protocol),
		direction,
		srcAddr, event.SourcePort,
		dstAddr, event.DestPort)
}

func determineExecStatus(returnCode int32) string {
	if returnCode == 0 {
		return "success"
	}
	return "failed"
}

func getAllocTypeName(allocType uint8) string {
	switch allocType {
	case 0:
		return "kmalloc"
	case 1:
		return "vmalloc"
	case 2:
		return "page_alloc"
	default:
		return fmt.Sprintf("type_%d", allocType)
	}
}

func getFileOperationName(op uint8) string {
	switch op {
	case 0:
		return "open"
	case 1:
		return "read"
	case 2:
		return "write"
	case 3:
		return "close"
	case 4:
		return "stat"
	case 5:
		return "lseek"
	default:
		return fmt.Sprintf("op_%d", op)
	}
}

