package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test String decoder with null-terminated string
func TestString_DecodeNullTerminated(t *testing.T) {
	decoder := &String{}

	// "test" with null terminator and garbage after
	input := []byte{'t', 'e', 's', 't', 0, 'x', 'x', 'x'}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "test", string(result))
}

// RED: Test String decoder with no null terminator
func TestString_DecodeNoNull(t *testing.T) {
	decoder := &String{}

	// "test" without null terminator
	input := []byte{'t', 'e', 's', 't'}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "test", string(result))
}

// RED: Test String decoder with empty string
func TestString_DecodeEmpty(t *testing.T) {
	decoder := &String{}

	// Empty string (just null)
	input := []byte{0}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "", string(result))
}

// RED: Test String decoder with null at start
func TestString_DecodeNullAtStart(t *testing.T) {
	decoder := &String{}

	// Null at position 0
	input := []byte{0, 't', 'e', 's', 't'}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "", string(result))
}

// RED: Test String decoder with multiple nulls
func TestString_DecodeMultipleNulls(t *testing.T) {
	decoder := &String{}

	// "test" with multiple nulls
	input := []byte{'t', 'e', 0, 's', 0, 't', 0}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "te", string(result)) // Stops at first null
}

// RED: Test String decoder with full buffer
func TestString_DecodeFullBuffer(t *testing.T) {
	decoder := &String{}

	// Full buffer with no null
	input := []byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "abcdefgh", string(result))
}

// RED: Test clen function with null byte
func TestClen_WithNull(t *testing.T) {
	input := []byte{'t', 'e', 's', 't', 0, 'x', 'x'}
	result := clen(input)
	assert.Equal(t, 4, result)
}

// RED: Test clen function without null byte
func TestClen_WithoutNull(t *testing.T) {
	input := []byte{'t', 'e', 's', 't'}
	result := clen(input)
	assert.Equal(t, 4, result)
}

// RED: Test clen function with empty slice
func TestClen_Empty(t *testing.T) {
	input := []byte{}
	result := clen(input)
	assert.Equal(t, 0, result)
}

// RED: Test clen function with only null
func TestClen_OnlyNull(t *testing.T) {
	input := []byte{0}
	result := clen(input)
	assert.Equal(t, 0, result)
}

// RED: Test String integration with decoder set
func TestString_IntegrationWithSet(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "comm",
			Size: 16, // Typical comm field size
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	// "bash" with null terminator and padding
	input := make([]byte, 16)
	copy(input, []byte{'b', 'a', 's', 'h', 0})

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"bash"}, result)
}

// RED: Test String with special characters
func TestString_DecodeSpecialChars(t *testing.T) {
	decoder := &String{}

	// String with special chars before null
	input := []byte{'/', 'u', 's', 'r', '/', 'b', 'i', 'n', 0, 'x'}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin", string(result))
}

// RED: Test String with spaces
func TestString_DecodeWithSpaces(t *testing.T) {
	decoder := &String{}

	// "hello world" with null
	input := []byte{'h', 'e', 'l', 'l', 'o', ' ', 'w', 'o', 'r', 'l', 'd', 0}

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(result))
}

// RED: Test String decoder with UTF-8
func TestString_DecodeUTF8(t *testing.T) {
	decoder := &String{}

	// UTF-8 string with null terminator
	input := append([]byte("test"), 0)

	result, err := decoder.Decode(input, Decoder{})
	require.NoError(t, err)
	assert.Equal(t, "test", string(result))
}
