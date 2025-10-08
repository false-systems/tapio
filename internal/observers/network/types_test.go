package network

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestNetworkEventBPF_Size(t *testing.T) {
	// Verify struct size matches C layout
	// C struct: 4 + 4 + 4 + 2 + 2 + 1 + 1 + 16 + 2 = 36 bytes
	// (includes _pad2 for 4-byte alignment)
	event := NetworkEventBPF{}
	size := unsafe.Sizeof(event)
	assert.Equal(t, uintptr(36), size, "NetworkEventBPF size must match C struct")
}

func TestNetworkEventBPF_FieldOffsets(t *testing.T) {
	// Verify field offsets match C struct layout
	event := NetworkEventBPF{}
	baseAddr := uintptr(unsafe.Pointer(&event))

	// PID at offset 0
	pidOffset := uintptr(unsafe.Pointer(&event.PID)) - baseAddr
	assert.Equal(t, uintptr(0), pidOffset, "PID offset")

	// SrcIP at offset 4
	srcIPOffset := uintptr(unsafe.Pointer(&event.SrcIP)) - baseAddr
	assert.Equal(t, uintptr(4), srcIPOffset, "SrcIP offset")

	// DstIP at offset 8
	dstIPOffset := uintptr(unsafe.Pointer(&event.DstIP)) - baseAddr
	assert.Equal(t, uintptr(8), dstIPOffset, "DstIP offset")

	// SrcPort at offset 12
	srcPortOffset := uintptr(unsafe.Pointer(&event.SrcPort)) - baseAddr
	assert.Equal(t, uintptr(12), srcPortOffset, "SrcPort offset")

	// DstPort at offset 14
	dstPortOffset := uintptr(unsafe.Pointer(&event.DstPort)) - baseAddr
	assert.Equal(t, uintptr(14), dstPortOffset, "DstPort offset")

	// Protocol at offset 16
	protocolOffset := uintptr(unsafe.Pointer(&event.Protocol)) - baseAddr
	assert.Equal(t, uintptr(16), protocolOffset, "Protocol offset")

	// Comm at offset 18 (after protocol + padding)
	commOffset := uintptr(unsafe.Pointer(&event.Comm)) - baseAddr
	assert.Equal(t, uintptr(18), commOffset, "Comm offset")
}

func TestProtocolConstants(t *testing.T) {
	// Verify protocol constants match IPPROTO_* values
	assert.Equal(t, uint8(6), uint8(ProtocolTCP), "TCP protocol value")
	assert.Equal(t, uint8(17), uint8(ProtocolUDP), "UDP protocol value")
}
