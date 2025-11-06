package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test StaticMap with valid mapping
func TestStaticMap_DecodeValid(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		"1": "TCP",
		"2": "UDP",
		"3": "ICMP",
	}

	conf := Decoder{
		StaticMap: mapping,
	}

	result, err := decoder.Decode([]byte("1"), conf)
	require.NoError(t, err)
	assert.Equal(t, "TCP", string(result))
}

// RED: Test StaticMap with unknown value and AllowUnknown=true
func TestStaticMap_DecodeUnknown_Allowed(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		"1": "TCP",
	}

	conf := Decoder{
		StaticMap:    mapping,
		AllowUnknown: true,
	}

	result, err := decoder.Decode([]byte("99"), conf)
	require.NoError(t, err)
	assert.Equal(t, "99", string(result)) // Returns input unchanged
}

// RED: Test StaticMap with unknown value and AllowUnknown=false
func TestStaticMap_DecodeUnknown_NotAllowed(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		"1": "TCP",
	}

	conf := Decoder{
		StaticMap:    mapping,
		AllowUnknown: false,
	}

	result, err := decoder.Decode([]byte("99"), conf)
	require.NoError(t, err)
	assert.Equal(t, "unknown:99", string(result)) // Returns "unknown:value"
}

// RED: Test StaticMap with nil map
func TestStaticMap_DecodeNilMap(t *testing.T) {
	decoder := &StaticMap{}

	conf := Decoder{
		StaticMap: nil,
	}

	result, err := decoder.Decode([]byte("test"), conf)
	require.NoError(t, err)
	assert.Equal(t, "empty mapping", string(result))
}

// RED: Test StaticMap with empty string key
func TestStaticMap_DecodeEmptyKey(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		"":  "empty",
		"1": "one",
	}

	conf := Decoder{
		StaticMap: mapping,
	}

	result, err := decoder.Decode([]byte(""), conf)
	require.NoError(t, err)
	assert.Equal(t, "empty", string(result))
}

// RED: Test StaticMap with protocol numbers
func TestStaticMap_ProtocolMapping(t *testing.T) {
	decoder := &StaticMap{}

	protocolMap := map[string]string{
		"6":  "TCP",
		"17": "UDP",
		"1":  "ICMP",
		"58": "ICMPv6",
	}

	conf := Decoder{
		StaticMap: protocolMap,
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"6", "TCP"},
		{"17", "UDP"},
		{"1", "ICMP"},
		{"58", "ICMPv6"},
	}

	for _, tt := range tests {
		result, err := decoder.Decode([]byte(tt.input), conf)
		require.NoError(t, err)
		assert.Equal(t, tt.expected, string(result), "Failed for input: %s", tt.input)
	}
}

// RED: Test StaticMap integration with decoder set
func TestStaticMap_IntegrationWithSet(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "protocol",
			Size: 1,
			Decoders: []Decoder{
				{
					Name: "static_map",
					StaticMap: map[string]string{
						"\x06": "TCP",
						"\x11": "UDP",
					},
				},
			},
		},
	}

	// Protocol 6 (TCP)
	input := []byte{6}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"TCP"}, result)
}

// RED: Test StaticMap chained with string decoder
func TestStaticMap_ChainedWithString(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 8,
			Decoders: []Decoder{
				{Name: "string"}, // First: extract null-terminated string
				{
					Name: "static_map", // Second: map the string
					StaticMap: map[string]string{
						"tcp": "TCP Protocol",
						"udp": "UDP Protocol",
					},
				},
			},
		},
	}

	// "tcp" with null and padding
	input := []byte{'t', 'c', 'p', 0, 0, 0, 0, 0}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"TCP Protocol"}, result)
}

// RED: Test StaticMap with byte values
func TestStaticMap_ByteValues(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		string([]byte{0x01}): "value1",
		string([]byte{0xff}): "value255",
	}

	conf := Decoder{
		StaticMap: mapping,
	}

	result, err := decoder.Decode([]byte{0x01}, conf)
	require.NoError(t, err)
	assert.Equal(t, "value1", string(result))

	result2, err := decoder.Decode([]byte{0xff}, conf)
	require.NoError(t, err)
	assert.Equal(t, "value255", string(result2))
}

// RED: Test StaticMap with multi-byte values
func TestStaticMap_MultiByteValues(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		string([]byte{0, 1}):    "two_bytes",
		string([]byte{1, 2, 3}): "three_bytes",
	}

	conf := Decoder{
		StaticMap: mapping,
	}

	result, err := decoder.Decode([]byte{0, 1}, conf)
	require.NoError(t, err)
	assert.Equal(t, "two_bytes", string(result))
}

// RED: Test StaticMap case sensitivity
func TestStaticMap_CaseSensitive(t *testing.T) {
	decoder := &StaticMap{}

	mapping := map[string]string{
		"tcp": "lowercase",
		"TCP": "uppercase",
	}

	conf := Decoder{
		StaticMap: mapping,
	}

	result1, err := decoder.Decode([]byte("tcp"), conf)
	require.NoError(t, err)
	assert.Equal(t, "lowercase", string(result1))

	result2, err := decoder.Decode([]byte("TCP"), conf)
	require.NoError(t, err)
	assert.Equal(t, "uppercase", string(result2))
}
