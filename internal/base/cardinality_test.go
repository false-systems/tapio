package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

// TestCardinalityLimiter_UnderLimit tests that metrics are recorded when under the limit
func TestCardinalityLimiter_UnderLimit(t *testing.T) {
	limiter := NewCardinalityLimiter(100)

	// Add 50 unique label combinations (under limit)
	for i := 0; i < 50; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("connection.id", string(rune(i))),
		}
		allowed := limiter.ShouldRecord(attrs)
		assert.True(t, allowed, "Should allow metrics under limit")
	}

	// Verify count
	assert.Equal(t, 50, limiter.Count(), "Should track 50 unique combinations")
}

// TestCardinalityLimiter_AtLimit tests that metrics are dropped when limit reached
func TestCardinalityLimiter_AtLimit(t *testing.T) {
	limiter := NewCardinalityLimiter(10) // Small limit

	// Fill to limit
	for i := 0; i < 10; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("connection.id", string(rune(i))),
		}
		allowed := limiter.ShouldRecord(attrs)
		assert.True(t, allowed, "Should allow metrics up to limit")
	}

	// Exceed limit - should drop
	attrs := []attribute.KeyValue{
		attribute.String("connection.id", "new-value"),
	}
	allowed := limiter.ShouldRecord(attrs)
	assert.False(t, allowed, "Should drop metrics when limit reached")

	// Count should still be at limit
	assert.Equal(t, 10, limiter.Count(), "Should not exceed limit")
}

// TestCardinalityLimiter_SameLabels tests that duplicate labels are allowed
func TestCardinalityLimiter_SameLabels(t *testing.T) {
	limiter := NewCardinalityLimiter(100)

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
	limiter := NewCardinalityLimiter(3) // Very small limit

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

	// Add new combination - should evict attrs2 (least recently used)
	attrs4 := []attribute.KeyValue{attribute.String("id", "4")}
	assert.False(t, limiter.ShouldRecord(attrs4), "Should drop when limit reached (no LRU eviction yet)")

	// In future: LRU should evict oldest, allowing new entries
	// For now, we just block at limit
}

// TestCardinalityLimiter_MultipleAttributes tests hashing multiple attributes
func TestCardinalityLimiter_MultipleAttributes(t *testing.T) {
	limiter := NewCardinalityLimiter(100)

	// Same values, different order = different hash
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

	// Different order might count as different (implementation detail)
	// But same exact attributes should be recognized
	assert.True(t, limiter.ShouldRecord(attrs1), "Same attributes should be allowed")
}

// TestCardinalityLimiter_EmptyAttributes tests handling empty attributes
func TestCardinalityLimiter_EmptyAttributes(t *testing.T) {
	limiter := NewCardinalityLimiter(100)

	// Empty attributes should work
	attrs := []attribute.KeyValue{}
	assert.True(t, limiter.ShouldRecord(attrs), "Empty attributes should be allowed")
	assert.Equal(t, 1, limiter.Count(), "Empty attributes count as 1 combination")

	// Second call with empty attributes
	assert.True(t, limiter.ShouldRecord(attrs), "Duplicate empty attributes allowed")
	assert.Equal(t, 1, limiter.Count(), "Still only 1 combination")
}
