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

// TestStatusProcessor_DetectHTTPConnection verifies HTTP connection detection (TCP port 80)
func TestStatusProcessor_DetectHTTPConnection(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// HTTP connection: client → server (port 80)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_ESTABLISHED,
		SrcIP:     0x0100007f, // 127.0.0.1
		DstIP:     0x6401a8c0, // 192.168.1.100
		SrcPort:   12345,
		DstPort:   80, // HTTP port
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "http_connection", domainEvt.Subtype)
	assert.NotNil(t, domainEvt.NetworkData)
	assert.Equal(t, "HTTP", domainEvt.NetworkData.Protocol)
	assert.Equal(t, "127.0.0.1", domainEvt.NetworkData.SrcIP)
	assert.Equal(t, "192.168.1.100", domainEvt.NetworkData.DstIP)
	assert.Equal(t, uint16(12345), domainEvt.NetworkData.SrcPort)
	assert.Equal(t, uint16(80), domainEvt.NetworkData.DstPort)
}

// TestStatusProcessor_DetectHTTPSConnection verifies HTTPS connection detection (TCP port 443)
func TestStatusProcessor_DetectHTTPSConnection(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// HTTPS connection: client → server (port 443)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_ESTABLISHED,
		SrcIP:     0x0100007f,
		DstIP:     0x6401a8c0,
		SrcPort:   12345,
		DstPort:   443, // HTTPS port
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "https_connection", domainEvt.Subtype)
	assert.Equal(t, "HTTPS", domainEvt.NetworkData.Protocol)
}

// TestStatusProcessor_IgnoreNonHTTP verifies non-HTTP traffic is ignored
func TestStatusProcessor_IgnoreNonHTTP(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// Non-HTTP TCP connection (port 22 - SSH)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_ESTABLISHED,
		SrcIP:     0x0100007f,
		DstIP:     0x6401a8c0,
		SrcPort:   12345,
		DstPort:   22, // SSH, not HTTP
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "Non-HTTP traffic should be ignored")
}

// TestStatusProcessor_IgnoreNonEstablished verifies only ESTABLISHED connections are tracked
func TestStatusProcessor_IgnoreNonEstablished(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// HTTP port but not ESTABLISHED state
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_SYN_RECV, // Not ESTABLISHED
		SrcIP:     0x0100007f,
		DstIP:     0x6401a8c0,
		SrcPort:   12345,
		DstPort:   80,
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "Non-ESTABLISHED connections should be ignored")
}

// TestStatusProcessor_IgnoreUDP verifies UDP traffic is ignored
func TestStatusProcessor_IgnoreUDP(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// UDP traffic (even on port 80)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_UDP,
		SrcIP:     0x0100007f,
		DstIP:     0x6401a8c0,
		SrcPort:   12345,
		DstPort:   80,
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "UDP traffic should be ignored")
}

// TestStatusProcessor_DetectHTTPConnection_IPv6 verifies IPv6 HTTP connection detection
func TestStatusProcessor_DetectHTTPConnection_IPv6(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// IPv6 HTTP connection: ::1 → 2001:db8::1 (port 80)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_ESTABLISHED,
		SrcIPv6:   [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},             // ::1
		DstIPv6:   [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // 2001:db8::1
		SrcPort:   12345,
		DstPort:   80,
		Family:    AF_INET6,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "http_connection", domainEvt.Subtype)
	assert.Equal(t, "HTTP", domainEvt.NetworkData.Protocol)
	assert.Equal(t, "0:0:0:0:0:0:0:1", domainEvt.NetworkData.SrcIP)
	assert.Contains(t, domainEvt.NetworkData.DstIP, "2001:db8")
}

// TestStatusProcessor_DetectHTTPSConnection_IPv6 verifies IPv6 HTTPS connection detection
func TestStatusProcessor_DetectHTTPSConnection_IPv6(t *testing.T) {
	proc := NewStatusProcessor()
	require.NotNil(t, proc)

	// IPv6 HTTPS connection: ::1 → 2001:db8::1 (port 443)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		OldState:  TCP_SYN_SENT,
		NewState:  TCP_ESTABLISHED,
		SrcIPv6:   [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},             // ::1
		DstIPv6:   [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // 2001:db8::1
		SrcPort:   12345,
		DstPort:   443,
		Family:    AF_INET6,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "https_connection", domainEvt.Subtype)
	assert.Equal(t, "HTTPS", domainEvt.NetworkData.Protocol)
	assert.Equal(t, "0:0:0:0:0:0:0:1", domainEvt.NetworkData.SrcIP)
	assert.Contains(t, domainEvt.NetworkData.DstIP, "2001:db8")
}
