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

// TestDNSProcessor_DetectQuery verifies DNS query detection (UDP port 53)
func TestDNSProcessor_DetectQuery(t *testing.T) {
	proc := NewDNSProcessor()
	require.NotNil(t, proc)

	// DNS query: client (port 12345) → DNS server (port 53)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_UDP,
		SrcIP:     0x0100007f, // 127.0.0.1
		DstIP:     0x08080808, // 8.8.8.8 (Google DNS)
		SrcPort:   12345,
		DstPort:   53, // DNS port
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "dns_query", domainEvt.Subtype)
	assert.NotNil(t, domainEvt.NetworkData)
	assert.Equal(t, "DNS", domainEvt.NetworkData.Protocol)
	assert.Equal(t, "127.0.0.1", domainEvt.NetworkData.SrcIP)
	assert.Equal(t, "8.8.8.8", domainEvt.NetworkData.DstIP)
	assert.Equal(t, uint16(12345), domainEvt.NetworkData.SrcPort)
	assert.Equal(t, uint16(53), domainEvt.NetworkData.DstPort)
}

// TestDNSProcessor_DetectResponse verifies DNS response detection (UDP from port 53)
func TestDNSProcessor_DetectResponse(t *testing.T) {
	proc := NewDNSProcessor()
	require.NotNil(t, proc)

	// DNS response: DNS server (port 53) → client (port 12345)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_UDP,
		SrcIP:     0x08080808, // 8.8.8.8
		DstIP:     0x0100007f, // 127.0.0.1
		SrcPort:   53,         // DNS port
		DstPort:   12345,
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "dns_response", domainEvt.Subtype)
	assert.Equal(t, "DNS", domainEvt.NetworkData.Protocol)
}

// TestDNSProcessor_IgnoreNonDNS verifies non-DNS traffic is ignored
func TestDNSProcessor_IgnoreNonDNS(t *testing.T) {
	proc := NewDNSProcessor()
	require.NotNil(t, proc)

	// Non-DNS UDP traffic (port 80)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_UDP,
		SrcIP:     0x0100007f,
		DstIP:     0x6401a8c0,
		SrcPort:   12345,
		DstPort:   80, // Not DNS
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "Non-DNS traffic should be ignored")
}

// TestDNSProcessor_IgnoreTCP verifies TCP traffic is ignored
func TestDNSProcessor_IgnoreTCP(t *testing.T) {
	proc := NewDNSProcessor()
	require.NotNil(t, proc)

	// TCP traffic (even on port 53)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_TCP,
		SrcIP:     0x0100007f,
		DstIP:     0x08080808,
		SrcPort:   12345,
		DstPort:   53,
		Family:    AF_INET,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	assert.Nil(t, domainEvt, "TCP traffic should be ignored (DNS over TCP not implemented yet)")
}

// TestDNSProcessor_DetectQuery_IPv6 verifies IPv6 DNS query detection
func TestDNSProcessor_DetectQuery_IPv6(t *testing.T) {
	proc := NewDNSProcessor()
	require.NotNil(t, proc)

	// IPv6 DNS query: ::1 → 2001:4860:4860::8888 (Google DNS)
	evt := NetworkEventBPF{
		EventType: EventTypeStateChange,
		Protocol:  IPPROTO_UDP,
		SrcIPv6:   [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},                         // ::1
		DstIPv6:   [16]byte{0x20, 0x01, 0x48, 0x60, 0x48, 0x60, 0, 0, 0, 0, 0, 0, 0, 0, 0x88, 0x88}, // 2001:4860:4860::8888
		SrcPort:   12345,
		DstPort:   53,
		Family:    AF_INET6,
	}

	ctx := context.Background()
	domainEvt := proc.Process(ctx, evt)

	require.NotNil(t, domainEvt)
	assert.Equal(t, string(domain.EventTypeNetwork), domainEvt.Type)
	assert.Equal(t, "dns_query", domainEvt.Subtype)
	assert.Equal(t, "DNS", domainEvt.NetworkData.Protocol)
	assert.Equal(t, "0:0:0:0:0:0:0:1", domainEvt.NetworkData.SrcIP)
	assert.Contains(t, domainEvt.NetworkData.DstIP, "2001:4860")
}
