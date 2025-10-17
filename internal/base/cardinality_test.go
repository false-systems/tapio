package base

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// TestCardinalityLimiter_UnderLimit tests that metrics are recorded when under the limit
func TestCardinalityLimiter_UnderLimit(t *testing.T) {
	limiter, err := NewCardinalityLimiter(100)
	require.NoError(t, err)

	// Add 50 unique label combinations (under limit)
	for i := 0; i < 50; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("connection.id", strconv.Itoa(i)),
		}
		allowed := limiter.ShouldRecord(attrs)
		assert.True(t, allowed, "Should allow metrics under limit")
	}

	// Verify count
	assert.Equal(t, 50, limiter.Count(), "Should track 50 unique combinations")
}

// TestCardinalityLimiter_AtLimit tests LRU eviction when limit is reached
func TestCardinalityLimiter_AtLimit(t *testing.T) {
	limiter, err := NewCardinalityLimiter(10) // Small limit
	require.NoError(t, err)

	// Fill to limit
	for i := 0; i < 10; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("connection.id", strconv.Itoa(i)),
		}
		allowed := limiter.ShouldRecord(attrs)
		assert.True(t, allowed, "Should allow metrics up to limit")
	}

	// Add new combination - LRU will evict oldest
	attrs := []attribute.KeyValue{
		attribute.String("connection.id", "new-value"),
	}
	allowed := limiter.ShouldRecord(attrs)
	assert.True(t, allowed, "LRU should evict oldest and allow new entry")

	// Count should stay at limit
	assert.Equal(t, 10, limiter.Count(), "Should maintain limit via LRU eviction")
}

// TestCardinalityLimiter_SameLabels tests that duplicate labels are allowed
func TestCardinalityLimiter_SameLabels(t *testing.T) {
	limiter, err := NewCardinalityLimiter(100)
	require.NoError(t, err)

	attrs := []attribute.KeyValue{
		attribute.String("connection.id", "same-value"),
	}

	// Record same labels 100 times
	for i := 0; i < 100; i++ {
		allowed := limiter.ShouldRecord(attrs)
		assert.True(t, allowed, "Should allow duplicate label combinations")
	}

	// Should only count as 1 unique combination
	assert.Equal(t, 1, limiter.Count(), "Duplicate labels should count as 1")
}

// TestCardinalityLimiter_LRUEviction tests that LRU eviction works
func TestCardinalityLimiter_LRUEviction(t *testing.T) {
	limiter, err := NewCardinalityLimiter(3) // Very small limit
	require.NoError(t, err)

	// Add 3 combinations
	attrs1 := []attribute.KeyValue{attribute.String("id", "1")}
	attrs2 := []attribute.KeyValue{attribute.String("id", "2")}
	attrs3 := []attribute.KeyValue{attribute.String("id", "3")}

	assert.True(t, limiter.ShouldRecord(attrs1))
	assert.True(t, limiter.ShouldRecord(attrs2))
	assert.True(t, limiter.ShouldRecord(attrs3))
	assert.Equal(t, 3, limiter.Count())

	// Access attrs1 to make it recently used
	assert.True(t, limiter.ShouldRecord(attrs1))

	// Add new combination - LRU will evict attrs2 (least recently used)
	attrs4 := []attribute.KeyValue{attribute.String("id", "4")}
	assert.True(t, limiter.ShouldRecord(attrs4), "LRU should evict oldest and accept new entry")

	// Count should stay at 3
	assert.Equal(t, 3, limiter.Count())
}

// TestCardinalityLimiter_MultipleAttributes tests hashing multiple attributes
func TestCardinalityLimiter_MultipleAttributes(t *testing.T) {
	limiter, err := NewCardinalityLimiter(100)
	require.NoError(t, err)

	// Same values, different order = same hash (attributes are sorted)
	attrs1 := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key2", "value2"),
	}
	attrs2 := []attribute.KeyValue{
		attribute.String("key2", "value2"),
		attribute.String("key1", "value1"),
	}

	assert.True(t, limiter.ShouldRecord(attrs1))
	assert.True(t, limiter.ShouldRecord(attrs2))

	// Same attributes in different order should be recognized as same
	assert.True(t, limiter.ShouldRecord(attrs1), "Same attributes should be allowed")
	assert.Equal(t, 1, limiter.Count(), "Different order should hash to same value")
}

// TestCardinalityLimiter_EmptyAttributes tests handling empty attributes
func TestCardinalityLimiter_EmptyAttributes(t *testing.T) {
	limiter, err := NewCardinalityLimiter(100)
	require.NoError(t, err)

	// Empty attributes should work
	attrs := []attribute.KeyValue{}
	assert.True(t, limiter.ShouldRecord(attrs), "Empty attributes should be allowed")
	assert.Equal(t, 1, limiter.Count(), "Empty attributes count as 1 combination")

	// Second call with empty attributes
	assert.True(t, limiter.ShouldRecord(attrs), "Duplicate empty attributes allowed")
	assert.Equal(t, 1, limiter.Count(), "Still only 1 combination")
}

// TestCardinalityLimiter_InvalidLimit tests error handling for invalid limits
func TestCardinalityLimiter_InvalidLimit(t *testing.T) {
	// Zero limit
	limiter, err := NewCardinalityLimiter(0)
	assert.Error(t, err)
	assert.Nil(t, limiter)
	assert.Contains(t, err.Error(), "positive")

	// Negative limit
	limiter, err = NewCardinalityLimiter(-10)
	assert.Error(t, err)
	assert.Nil(t, limiter)
	assert.Contains(t, err.Error(), "positive")
}
