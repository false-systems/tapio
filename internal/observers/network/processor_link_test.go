//go:build linux
// +build linux

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestLinkProcessor_SYNTimeout verifies SYN timeout detection
func TestLinkProcessor_SYNTimeout(t *testing.T) {
	proc := NewLinkProcessor()
	require.NotNil(t, proc)

	evt := NetworkEventBPF{
		OldState: TCP_SYN_SENT,
		NewState: TCP_CLOSE,
		SrcIP:    0x0100007f, // 127.0.0.1
		DstIP:    0x6401a8c0, // 192.168.1.100
		SrcPort:  12345,
		DstPort:  80,
		Family:   AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "link_failure", domainEvt.Subtype)
	assert.NotNil(t, domainEvt.NetworkData)
	assert.Equal(t, "127.0.0.1", domainEvt.NetworkData.SrcIP)
	assert.Equal(t, "192.168.1.100", domainEvt.NetworkData.DstIP)
	assert.Equal(t, uint16(12345), domainEvt.NetworkData.SrcPort)
	assert.Equal(t, uint16(80), domainEvt.NetworkData.DstPort)
}

// TestLinkProcessor_NotSYNTimeout verifies non-SYN-timeout transitions are ignored
func TestLinkProcessor_NotSYNTimeout(t *testing.T) {
	proc := NewLinkProcessor()
	require.NotNil(t, proc)

	// Normal connection establishment
	evt := NetworkEventBPF{
		OldState: TCP_SYN_SENT,
		NewState: TCP_ESTABLISHED,
		SrcIP:    0x0100007f,
		DstIP:    0x6401a8c0,
		Family:   AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "Normal connection should not trigger link failure")
}

// TestLinkProcessor_EstablishedToClose verifies normal close is not a link failure
func TestLinkProcessor_EstablishedToClose(t *testing.T) {
	proc := NewLinkProcessor()
	require.NotNil(t, proc)

	evt := NetworkEventBPF{
		OldState: TCP_ESTABLISHED,
		NewState: TCP_CLOSE,
		SrcIP:    0x0100007f,
		DstIP:    0x6401a8c0,
		Family:   AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "Normal close should not trigger link failure")
}

// TestLinkProcessor_SYNTimeout_IPv6 verifies IPv6 SYN timeout detection
func TestLinkProcessor_SYNTimeout_IPv6(t *testing.T) {
	proc := NewLinkProcessor()
	require.NotNil(t, proc)

	// IPv6 SYN timeout: ::1 (localhost) → 2001:db8::1
	evt := NetworkEventBPF{
		OldState: TCP_SYN_SENT,
		NewState: TCP_CLOSE,
		SrcIPv6:  [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},             // ::1
		DstIPv6:  [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // 2001:db8::1
		SrcPort:  12345,
		DstPort:  80,
		Family:   AF_INET6,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "link_failure", domainEvt.Subtype)
	assert.NotNil(t, domainEvt.NetworkData)
	assert.Equal(t, "0:0:0:0:0:0:0:1", domainEvt.NetworkData.SrcIP)
	assert.Contains(t, domainEvt.NetworkData.DstIP, "2001:db8")
}

// TestLinkProcessor_SetsRequiredFields verifies ID, Timestamp, Source are set
// RED PHASE: This test MUST fail before implementation
func TestLinkProcessor_SetsRequiredFields(t *testing.T) {
	proc := NewLinkProcessor()
	require.NotNil(t, proc)

	evt := NetworkEventBPF{
		OldState: TCP_SYN_SENT,
		NewState: TCP_CLOSE,
		SrcIP:    0x0100007f,
		DstIP:    0x6401a8c0,
		Family:   AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)

	// RED: These assertions will FAIL until we implement
	assert.NotEmpty(t, domainEvt.ID, "ID must be set")
	assert.False(t, domainEvt.Timestamp.IsZero(), "Timestamp must be set")
	assert.Equal(t, "network", domainEvt.Source, "Source must be 'network'")
}
