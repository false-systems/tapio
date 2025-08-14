package collectors

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test struct for safe parsing
type TestStruct struct {
	Field1 uint32
	Field2 uint16
	Field3 uint8
	_      uint8 // padding
}

func TestSafeParser(t *testing.T) {
	parser := NewSafeParser()

	t.Run("SafeCast", func(t *testing.T) {
		// Create test data
		original := TestStruct{
			Field1: 0x12345678,
			Field2: 0xABCD,
			Field3: 0xEF,
		}

		// Marshal to bytes
		buffer, err := parser.MarshalStruct(original)
		require.NoError(t, err)
		require.Equal(t, int(unsafe.Sizeof(TestStruct{})), len(buffer))

		// SafeCast back to struct
		parsed, err := SafeCast[TestStruct](parser, buffer)
		require.NoError(t, err)
		require.NotNil(t, parsed)

		assert.Equal(t, original.Field1, parsed.Field1)
		assert.Equal(t, original.Field2, parsed.Field2)
		assert.Equal(t, original.Field3, parsed.Field3)
	})

	t.Run("BufferTooSmall", func(t *testing.T) {
		buffer := make([]byte, 1) // Too small

		_, err := SafeCast[TestStruct](parser, buffer)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer size mismatch")
	})

	t.Run("BufferTooLarge", func(t *testing.T) {
		expectedSize := int(unsafe.Sizeof(TestStruct{}))
		buffer := make([]byte, expectedSize+10) // Too large

		_, err := SafeCast[TestStruct](parser, buffer)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer size mismatch")
	})

	t.Run("StringFromByteArray", func(t *testing.T) {
		// Test with null terminator
		byteArray := []byte("hello\x00world")
		result := parser.StringFromByteArray(byteArray)
		assert.Equal(t, "hello", result)

		// Test without null terminator
		byteArray2 := []byte("hello")
		result2 := parser.StringFromByteArray(byteArray2)
		assert.Equal(t, "hello", result2)

		// Test empty
		result3 := parser.StringFromByteArray([]byte{})
		assert.Equal(t, "", result3)
	})

	t.Run("ValidateEventType", func(t *testing.T) {
		// Valid range
		err := parser.ValidateEventType(5, 1, 10)
		assert.NoError(t, err)

		// Too low
		err = parser.ValidateEventType(0, 1, 10)
		assert.Error(t, err)

		// Too high
		err = parser.ValidateEventType(11, 1, 10)
		assert.Error(t, err)
	})

	t.Run("ValidateNetworkData", func(t *testing.T) {
		// Valid protocol and direction
		err := parser.ValidateNetworkData(6, 0) // TCP, outgoing
		assert.NoError(t, err)

		err = parser.ValidateNetworkData(17, 1) // UDP, incoming
		assert.NoError(t, err)

		// Invalid direction
		err = parser.ValidateNetworkData(6, 2)
		assert.Error(t, err)
	})

	t.Run("ValidateStringField", func(t *testing.T) {
		// Valid string with null terminator
		field := []byte("valid\x00remaining")
		err := parser.ValidateStringField(field, "test")
		assert.NoError(t, err)

		// Invalid character
		field2 := []byte{0x01, 0x02, 0x00} // non-printable
		err = parser.ValidateStringField(field2, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-printable character")
	})
}

func TestSafeParserPerformance(t *testing.T) {
	parser := NewSafeParser()

	original := TestStruct{
		Field1: 0x12345678,
		Field2: 0xABCD,
		Field3: 0xEF,
	}

	buffer, err := parser.MarshalStruct(original)
	require.NoError(t, err)

	// Run multiple iterations to ensure no memory leaks or performance regression
	t.Run("PerformanceTest", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			parsed, err := SafeCast[TestStruct](parser, buffer)
			require.NoError(t, err)
			require.Equal(t, original.Field1, parsed.Field1)
		}
	})
}
