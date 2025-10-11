package network

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// TestNetworkEventBPF_Size verifies struct size matches C definition
// Note: Go adds 2 bytes alignment padding (72 total), but binary.Read handles this correctly
func TestNetworkEventBPF_Size(t *testing.T) {
	var evt NetworkEventBPF
	size := unsafe.Sizeof(evt)
	// Go enforces natural alignment, adding 2 bytes at end
	// Actual data is 70 bytes, Go struct is 72 bytes with padding
	assert.Equal(t, uintptr(72), size, "NetworkEventBPF Go struct size (includes alignment padding)")

	// Verify the actual data portion is 70 bytes (up to end of Comm)
	dataSize := unsafe.Offsetof(evt.Comm) + unsafe.Sizeof(evt.Comm)
	assert.Equal(t, uintptr(70), dataSize, "Actual data size must be 70 bytes")
}

// TestNetworkEventBPF_FieldOffsets verifies field alignment matches C struct
func TestNetworkEventBPF_FieldOffsets(t *testing.T) {
	var evt NetworkEventBPF

	// Field offsets must match C struct layout exactly
	assert.Equal(t, uintptr(0), unsafe.Offsetof(evt.PID), "PID offset")
	assert.Equal(t, uintptr(4), unsafe.Offsetof(evt.SrcIP), "SrcIP offset")
	assert.Equal(t, uintptr(8), unsafe.Offsetof(evt.DstIP), "DstIP offset")
	assert.Equal(t, uintptr(12), unsafe.Offsetof(evt.SrcIPv6), "SrcIPv6 offset")
	assert.Equal(t, uintptr(28), unsafe.Offsetof(evt.DstIPv6), "DstIPv6 offset")
	assert.Equal(t, uintptr(44), unsafe.Offsetof(evt.SrcPort), "SrcPort offset")
	assert.Equal(t, uintptr(46), unsafe.Offsetof(evt.DstPort), "DstPort offset")
	assert.Equal(t, uintptr(48), unsafe.Offsetof(evt.Family), "Family offset")
	assert.Equal(t, uintptr(50), unsafe.Offsetof(evt.Protocol), "Protocol offset")
	assert.Equal(t, uintptr(51), unsafe.Offsetof(evt.OldState), "OldState offset")
	assert.Equal(t, uintptr(52), unsafe.Offsetof(evt.NewState), "NewState offset")
	assert.Equal(t, uintptr(53), unsafe.Offsetof(evt.Pad), "Pad offset")
	assert.Equal(t, uintptr(54), unsafe.Offsetof(evt.Comm), "Comm offset")
}

// TestConstants_AddressFamilies verifies address family constants
func TestConstants_AddressFamilies(t *testing.T) {
	assert.Equal(t, 2, AF_INET, "AF_INET must be 2")
	assert.Equal(t, 10, AF_INET6, "AF_INET6 must be 10")
}

// TestConstants_Protocols verifies IP protocol constants
func TestConstants_Protocols(t *testing.T) {
	assert.Equal(t, 6, IPPROTO_TCP, "IPPROTO_TCP must be 6")
	assert.Equal(t, 17, IPPROTO_UDP, "IPPROTO_UDP must be 17")
}

// TestConstants_TCPStates verifies TCP state constants match kernel
func TestConstants_TCPStates(t *testing.T) {
	assert.Equal(t, 1, TCP_ESTABLISHED, "TCP_ESTABLISHED")
	assert.Equal(t, 2, TCP_SYN_SENT, "TCP_SYN_SENT")
	assert.Equal(t, 3, TCP_SYN_RECV, "TCP_SYN_RECV")
	assert.Equal(t, 4, TCP_FIN_WAIT1, "TCP_FIN_WAIT1")
	assert.Equal(t, 5, TCP_FIN_WAIT2, "TCP_FIN_WAIT2")
	assert.Equal(t, 6, TCP_TIME_WAIT, "TCP_TIME_WAIT")
	assert.Equal(t, 7, TCP_CLOSE, "TCP_CLOSE")
	assert.Equal(t, 8, TCP_CLOSE_WAIT, "TCP_CLOSE_WAIT")
	assert.Equal(t, 9, TCP_LAST_ACK, "TCP_LAST_ACK")
	assert.Equal(t, 10, TCP_LISTEN, "TCP_LISTEN")
	assert.Equal(t, 11, TCP_CLOSING, "TCP_CLOSING")
}
