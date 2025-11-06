package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test InetIP decodes IPv4 address
func TestInetIP_DecodeIPv4(t *testing.T) {
	decoder := &InetIP{}

	// IPv4: 192.168.1.1
	input := []byte{192, 168, 1, 1}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.1", string(result))
}

// RED: Test InetIP decodes IPv6 address
func TestInetIP_DecodeIPv6(t *testing.T) {
	decoder := &InetIP{}

	// IPv6: ::1 (loopback)
	input := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "::1", string(result))
}

// RED: Test InetIP decodes full IPv6 address
func TestInetIP_DecodeIPv6_Full(t *testing.T) {
	decoder := &InetIP{}

	// IPv6: 2001:0db8:85a3:0000:0000:8a2e:0370:7334
	input := []byte{
		0x20, 0x01, 0x0d, 0xb8,
		0x85, 0xa3, 0x00, 0x00,
		0x00, 0x00, 0x8a, 0x2e,
		0x03, 0x70, 0x73, 0x34,
	}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	// Go's net package will compress zeros
	assert.Contains(t, string(result), "2001:db8:85a3")
}

// RED: Test InetIP decodes localhost
func TestInetIP_DecodeLocalhost(t *testing.T) {
	decoder := &InetIP{}

	// 127.0.0.1
	input := []byte{127, 0, 0, 1}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", string(result))
}

// RED: Test InetIP decodes zero address
func TestInetIP_DecodeZeroAddress(t *testing.T) {
	decoder := &InetIP{}

	// 0.0.0.0
	input := []byte{0, 0, 0, 0}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", string(result))
}

// RED: Test InetIP decodes broadcast address
func TestInetIP_DecodeBroadcast(t *testing.T) {
	decoder := &InetIP{}

	// 255.255.255.255
	input := []byte{255, 255, 255, 255}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "255.255.255.255", string(result))
}

// RED: Test InetIP with empty input
func TestInetIP_DecodeEmpty(t *testing.T) {
	decoder := &InetIP{}

	input := []byte{}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	// Empty IP renders as "<nil>"
	assert.NotEmpty(t, result)
}

// RED: Test InetIP integration with decoder set
func TestInetIP_IntegrationWithSet(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "src_ip",
			Size: 4,
			Decoders: []Decoder{
				{Name: "inet_ip"},
			},
		},
	}

	// 10.0.0.1
	input := []byte{10, 0, 0, 1}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1"}, result)
}

// RED: Test InetIP with IPv6 in decoder set
func TestInetIP_IntegrationIPv6(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "src_ip",
			Size: 16, // IPv6 is 16 bytes
			Decoders: []Decoder{
				{Name: "inet_ip"},
			},
		},
	}

	// IPv6: ::1
	input := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"::1"}, result)
}
