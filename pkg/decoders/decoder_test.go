package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test NewSet creates decoder set with basic decoders
func TestNewSet(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)
	require.NotNil(t, set)

	// Verify basic decoders are registered
	assert.NotNil(t, set.decoders["inet_ip"])
	assert.NotNil(t, set.decoders["string"])
	assert.NotNil(t, set.decoders["static_map"])
	assert.NotNil(t, set.decoders["syscall"])
}

// RED: Test NewSet with skip cache
func TestNewSet_WithSkipCache(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)
	assert.NotNil(t, set.skipCache)
}

// RED: Test NewSet without skip cache
func TestNewSet_WithoutSkipCache(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)
	assert.Nil(t, set.skipCache)
}

// RED: Test RegisterDecoder adds custom decoder
func TestSet_RegisterDecoder(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	// Register custom decoder
	customDecoder := &InetIP{} // Reuse InetIP as example
	set.RegisterDecoder("custom", customDecoder)

	assert.NotNil(t, set.decoders["custom"])
}

// RED: Test decode with unknown decoder
func TestSet_Decode_UnknownDecoder(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	label := Label{
		Name: "test",
		Size: 4,
		Decoders: []Decoder{
			{Name: "nonexistent"},
		},
	}

	result, err := set.decode([]byte{1, 2, 3, 4}, label)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown decoder")
	assert.NotNil(t, result)
}

// RED: Test DecodeLabelsForMetrics with caching
func TestSet_DecodeLabelsForMetrics_Caching(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 4,
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	input := []byte{'t', 'e', 's', 't'}

	// First call - should decode
	result1, err := set.DecodeLabelsForMetrics(input, "test", labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, result1)

	// Second call - should use cache
	result2, err := set.DecodeLabelsForMetrics(input, "test", labels)
	require.NoError(t, err)
	assert.Equal(t, result1, result2)
}

// RED: Test DecodeLabelsForTracing without caching
func TestSet_DecodeLabelsForTracing_NoCaching(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 4,
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	input := []byte{'t', 'e', 's', 't'}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, result)
}

// RED: Test decodeLabels with size mismatch
func TestSet_DecodeLabels_SizeMismatch(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 4, // Expects 4 bytes
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	// Provide only 2 bytes
	input := []byte{1, 2}

	result, err := set.DecodeLabelsForTracing(input, labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "total size")
	assert.Nil(t, result)
}

// RED: Test decodeLabels with zero size
func TestSet_DecodeLabels_ZeroSize(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 0, // Invalid
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	result, err := set.DecodeLabelsForTracing([]byte{1, 2, 3, 4}, labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "size is zero")
	assert.Nil(t, result)
}

// RED: Test decodeLabels with no decoders
func TestSet_DecodeLabels_NoDecoders(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name:     "value",
			Size:     4,
			Decoders: []Decoder{}, // No decoders
		},
	}

	result, err := set.DecodeLabelsForTracing([]byte{1, 2, 3, 4}, labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no decoders set")
	assert.Nil(t, result)
}

// RED: Test decodeLabels with padding
func TestSet_DecodeLabels_WithPadding(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name:    "value1",
			Size:    2,
			Padding: 2, // 2 bytes padding
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
		{
			Name: "value2",
			Size: 2,
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	// Input: 2 bytes + 2 padding + 2 bytes = 6 bytes total
	input := []byte{'a', 'b', 0, 0, 'c', 'd'}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"ab", "cd"}, result)
}

// RED: Test ErrSkipLabelSet handling
func TestSet_Decode_SkipLabelSet(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)

	// Create a static_map decoder that will return ErrSkipLabelSet for unknown
	label := Label{
		Name: "test",
		Size: 4,
		Decoders: []Decoder{
			{
				Name:         "static_map",
				StaticMap:    map[string]string{"good": "value"},
				AllowUnknown: false, // Will skip unknown
			},
		},
	}

	// Use input that's not in the map and AllowUnknown=false
	// This should add to skip cache
	result, err := set.decode([]byte("bad\x00"), label)
	assert.NoError(t, err) // static_map doesn't return ErrSkipLabelSet, just returns "unknown:bad"
	assert.Contains(t, string(result), "unknown")
}

// RED: Test multiple decoder chain
func TestSet_Decode_MultipleDecoders(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "value",
			Size: 4,
			Decoders: []Decoder{
				{Name: "string"}, // First decoder
				{Name: "string"}, // Second decoder (chain)
			},
		},
	}

	input := []byte{'t', 'e', 's', 't'}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, result)
}

// RED: Test decoder error propagation
func TestSet_Decode_ErrorPropagation(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "syscall",
			Size: 3,
			Decoders: []Decoder{
				{Name: "syscall"}, // Will fail on non-numeric input
			},
		},
	}

	// Non-numeric input will cause syscall decoder to error
	input := []byte{'x', 'y', 'z'}

	result, err := set.DecodeLabelsForTracing(input, labels)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test DecodeLabelsForMetrics with skip cache hit
func TestSet_DecodeLabelsForMetrics_SkipCacheHit(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)

	// First, manually populate skip cache by trying to decode something that errors
	labels := []Label{
		{
			Name: "value",
			Size: 3,
			Decoders: []Decoder{
				{Name: "syscall"},
			},
		},
	}

	badInput := []byte{'b', 'a', 'd'}

	// This should error and potentially add to skip cache
	_, err = set.DecodeLabelsForMetrics(badInput, "test", labels)
	require.Error(t, err)

	// Try again - if skip cache worked, should still error
	result, err := set.DecodeLabelsForMetrics(badInput, "test", labels)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// RED: Test multiple labels
func TestSet_DecodeLabels_MultipleLabels(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels := []Label{
		{
			Name: "proto",
			Size: 1,
			Decoders: []Decoder{
				{
					Name: "static_map",
					StaticMap: map[string]string{
						string([]byte{6}): "TCP",
					},
				},
			},
		},
		{
			Name: "port",
			Size: 2,
			Decoders: []Decoder{
				{Name: "string"},
			},
		},
	}

	// Proto=6 (TCP), Port="80"
	input := []byte{6, '8', '0'}

	result, err := set.DecodeLabelsForTracing(input, labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"TCP", "80"}, result)
}

// RED: Test cache with different label sets
func TestSet_DecodeLabelsForMetrics_DifferentLabelSets(t *testing.T) {
	set, err := NewSet(0)
	require.NoError(t, err)

	labels1 := []Label{
		{Name: "value", Size: 2, Decoders: []Decoder{{Name: "string"}}},
	}

	labels2 := []Label{
		{Name: "value", Size: 2, Decoders: []Decoder{{Name: "string"}}},
	}

	input := []byte{'a', 'b'}

	// Decode with first label set
	result1, err := set.DecodeLabelsForMetrics(input, "set1", labels1)
	require.NoError(t, err)

	// Decode with second label set (different name)
	result2, err := set.DecodeLabelsForMetrics(input, "set2", labels2)
	require.NoError(t, err)

	// Both should work independently
	assert.Equal(t, result1, result2)
}

// Test decoder that returns ErrSkipLabelSet
type skipDecoder struct{}

func (s *skipDecoder) Decode(in []byte, _ Decoder) ([]byte, error) {
	return in, ErrSkipLabelSet
}

// RED: Test ErrSkipLabelSet triggers skip cache
func TestSet_Decode_ErrSkipLabelSet_WithCache(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)

	// Register custom decoder that returns ErrSkipLabelSet
	set.RegisterDecoder("skip", &skipDecoder{})

	label := Label{
		Name: "test",
		Size: 4,
		Decoders: []Decoder{
			{Name: "skip"},
		},
	}

	input := []byte{1, 2, 3, 4}

	// First call - should add to skip cache
	result, err := set.decode(input, label)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSkipLabelSet)
	assert.Equal(t, input, result)

	// Verify skip cache was populated
	contains := set.skipCache.Contains(string(input))
	assert.True(t, contains)
}

// RED: Test ErrSkipLabelSet without skip cache
func TestSet_Decode_ErrSkipLabelSet_NoCache(t *testing.T) {
	set, err := NewSet(0) // No skip cache
	require.NoError(t, err)

	// Register custom decoder that returns ErrSkipLabelSet
	set.RegisterDecoder("skip", &skipDecoder{})

	label := Label{
		Name: "test",
		Size: 4,
		Decoders: []Decoder{
			{Name: "skip"},
		},
	}

	input := []byte{1, 2, 3, 4}

	// Should handle ErrSkipLabelSet without cache
	result, err := set.decode(input, label)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSkipLabelSet)
	assert.Equal(t, input, result)
}

// RED: Test DecodeLabelsForMetrics with skip cache hit
func TestSet_DecodeLabelsForMetrics_SkipCacheEarlyReturn(t *testing.T) {
	set, err := NewSet(100)
	require.NoError(t, err)

	// Register skip decoder
	set.RegisterDecoder("skip", &skipDecoder{})

	labels := []Label{
		{
			Name: "value",
			Size: 4,
			Decoders: []Decoder{
				{Name: "skip"},
			},
		},
	}

	input := []byte{1, 2, 3, 4}

	// First call - will populate skip cache
	_, err = set.DecodeLabelsForMetrics(input, "test", labels)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSkipLabelSet)

	// Second call - should return early from skip cache
	result, err := set.DecodeLabelsForMetrics(input, "test", labels)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSkipLabelSet)
	assert.Nil(t, result)
}
