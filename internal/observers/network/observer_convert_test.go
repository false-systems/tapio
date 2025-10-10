//go:build linux

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToDomainEvent_TCP(t *testing.T) {
	setupOTEL(t)

	observer, err := NewNetworkObserver("test-convert", Config{})
	require.NoError(t, err)

	// Simulate eBPF TCP event
	ebpfEvent := NetworkEventBPF{
		PID:      1234,
		SrcIP:    0x0100007f, // 127.0.0.1 in network byte order
		DstIP:    0x08080808, // 8.8.8.8
		SrcPort:  12345,
		DstPort:  80,
		Protocol: 6, // TCP
	}
	copy(ebpfEvent.Comm[:], []byte("curl"))

	domainEvent := observer.convertToDomainEvent(ebpfEvent)

	// Verify event structure
	assert.NotEmpty(t, domainEvent.ID)
	assert.Equal(t, "tcp_connect", domainEvent.Type)
	assert.Equal(t, "test-convert", domainEvent.Source)
	assert.WithinDuration(t, time.Now(), domainEvent.Timestamp, 1*time.Second)

	// Verify network data
	require.NotNil(t, domainEvent.NetworkData)
	assert.Equal(t, "TCP", domainEvent.NetworkData.Protocol)
	assert.Equal(t, uint16(12345), domainEvent.NetworkData.SrcPort)
	assert.Equal(t, uint16(80), domainEvent.NetworkData.DstPort)

	// Verify process data
	require.NotNil(t, domainEvent.ProcessData)
	assert.Equal(t, int32(1234), domainEvent.ProcessData.PID)
	assert.Equal(t, "curl", domainEvent.ProcessData.ProcessName)
}

func TestConvertToDomainEvent_UDP(t *testing.T) {
	setupOTEL(t)

	observer, err := NewNetworkObserver("test-convert", Config{})
	require.NoError(t, err)

	ebpfEvent := NetworkEventBPF{
		PID:      5678,
		Protocol: 17, // UDP
	}
	copy(ebpfEvent.Comm[:], []byte("dig"))

	domainEvent := observer.convertToDomainEvent(ebpfEvent)

	assert.Equal(t, "udp_send", domainEvent.Type)
	assert.Equal(t, "UDP", domainEvent.NetworkData.Protocol)
	assert.Equal(t, "dig", domainEvent.ProcessData.ProcessName)
}
