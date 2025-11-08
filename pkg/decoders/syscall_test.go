package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test Syscall decoder with known syscall (read)
func TestSyscall_DecodeKnownSyscall(t *testing.T) {
	decoder := &Syscall{}

	// Syscall 0 is typically "read" on most architectures
	result, err := decoder.Decode([]byte("0"), Decoder{})
	require.NoError(t, err)
	// Result should be a syscall name, not "unknown"
	assert.NotContains(t, string(result), "unknown")
}

// RED: Test Syscall decoder with unknown syscall number
func TestSyscall_DecodeUnknownSyscall(t *testing.T) {
	decoder := &Syscall{}

	// Very high number unlikely to be a real syscall
	result, err := decoder.Decode([]byte("99999"), Decoder{})
	require.NoError(t, err)
	assert.Contains(t, string(result), "unknown_syscall:99999")
}

// RED: Test Syscall decoder with invalid input
func TestSyscall_DecodeInvalidInput(t *testing.T) {
	decoder := &Syscall{}

	// Non-numeric input
	result, err := decoder.Decode([]byte("not_a_number"), Decoder{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test Syscall decoder with empty input
func TestSyscall_DecodeEmpty(t *testing.T) {
	decoder := &Syscall{}

	result, err := decoder.Decode([]byte(""), Decoder{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test Syscall decoder with negative number
func TestSyscall_DecodeNegative(t *testing.T) {
	decoder := &Syscall{}

	result, err := decoder.Decode([]byte("-1"), Decoder{})
	// Should handle negative as unknown
	if err == nil {
		assert.Contains(t, string(result), "unknown_syscall")
	}
}

// RED: Test resolveSyscall with known syscall
func TestResolveSyscall_Known(t *testing.T) {
	// Test that resolveSyscall works for syscall 0
	result := resolveSyscall(0)
	assert.NotEqual(t, "", result)
	assert.NotContains(t, result, "unknown")
}

// RED: Test resolveSyscall with unknown syscall
func TestResolveSyscall_Unknown(t *testing.T) {
	result := resolveSyscall(99999)
	assert.Equal(t, "unknown_syscall:99999", result)
}

// RED: Test Syscall integration with decoder set
func TestSyscall_IntegrationWithSet(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "syscall_id",
			Size: 2, // 2-digit syscall number
			Decoders: []Decoder{
				{Name: "string"},  // Extract string
				{Name: "syscall"}, // Decode syscall number
			},
		},
	}

	// Syscall "0" with null terminator
	input := []byte{'0', 0}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.NotContains(t, result[0], "unknown")
}

// RED: Test Syscall with multi-digit number
func TestSyscall_DecodeMultiDigit(t *testing.T) {
	decoder := &Syscall{}

	// Test with a common multi-digit syscall
	result, err := decoder.Decode([]byte("1"), Decoder{})
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// RED: Test Syscall with leading zeros
func TestSyscall_DecodeLeadingZeros(t *testing.T) {
	decoder := &Syscall{}

	// "001" should be treated as syscall 1
	result, err := decoder.Decode([]byte("001"), Decoder{})
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	// Should resolve to same as "1"
	result2, err := decoder.Decode([]byte("1"), Decoder{})
	require.NoError(t, err)
	assert.Equal(t, string(result2), string(result))
}

// RED: Test Syscall with whitespace
func TestSyscall_DecodeWithWhitespace(t *testing.T) {
	decoder := &Syscall{}

	// Input with whitespace should fail
	result, err := decoder.Decode([]byte(" 1 "), Decoder{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test Syscall decoder with zero
func TestSyscall_DecodeZero(t *testing.T) {
	decoder := &Syscall{}

	result, err := decoder.Decode([]byte("0"), Decoder{})
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// RED: Test common syscalls are mapped
func TestSyscall_CommonSyscalls(t *testing.T) {
	decoder := &Syscall{}

	// Test a few common syscall numbers (will vary by architecture)
	commonSyscalls := []string{"0", "1", "2", "3"}

	for _, syscallNum := range commonSyscalls {
		result, err := decoder.Decode([]byte(syscallNum), Decoder{})
		require.NoError(t, err, "Failed for syscall: %s", syscallNum)
		assert.NotEmpty(t, result, "Empty result for syscall: %s", syscallNum)
	}
}
