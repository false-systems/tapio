package domain

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test that NewEventID generates valid ULID
func TestNewEventID_IsValidULID(t *testing.T) {
	id := NewEventID() // ❌ Doesn't exist yet - test will FAIL

	// Should be valid ULID format
	_, err := ulid.Parse(id)
	require.NoError(t, err, "NewEventID should generate valid ULID")
}

// RED: Test that sequential IDs are lexicographically ordered
func TestNewEventID_Sortable(t *testing.T) {
	id1 := NewEventID() // ❌ Doesn't exist yet
	time.Sleep(2 * time.Millisecond)
	id2 := NewEventID()

	// Later ID should be lexicographically greater
	assert.True(t, id2 > id1, "Later ULID should be greater than earlier ULID")
}

// RED: Test ULID generation with specific timestamp
func TestNewEventIDWithTime(t *testing.T) {
	now := time.Now()
	future := now.Add(1 * time.Hour)

	id1 := NewEventIDWithTime(now)    // ❌ Doesn't exist yet
	id2 := NewEventIDWithTime(future) // ❌ Doesn't exist yet

	// Future ID should be lexicographically greater
	assert.True(t, id2 > id1, "Future timestamp ULID should be greater")
}

// RED: Test extracting timestamp from ULID
func TestParseEventID_ExtractsTimestamp(t *testing.T) {
	now := time.Now()
	id := NewEventIDWithTime(now) // ❌ Doesn't exist yet

	extractedTime, err := ParseEventID(id) // ❌ Doesn't exist yet
	require.NoError(t, err, "ParseEventID should extract timestamp from ULID")

	// ULID timestamp has millisecond precision
	expected := now.Truncate(time.Millisecond)
	actual := extractedTime.Truncate(time.Millisecond)
	assert.True(t, expected.Equal(actual), "Extracted timestamp should match original")
}

// RED: Test parsing invalid ULID
func TestParseEventID_InvalidULID(t *testing.T) {
	_, err := ParseEventID("not-a-ulid") // ❌ Doesn't exist yet
	assert.Error(t, err, "ParseEventID should fail on invalid ULID")
}

// RED: Test ULID uniqueness (no collisions)
func TestNewEventID_Uniqueness(t *testing.T) {
	// Generate 1000 ULIDs in tight loop
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewEventID() // ❌ Doesn't exist yet
		assert.False(t, ids[id], "ULID should be unique (collision detected)")
		ids[id] = true
	}
}

// RED: Test ULID length (26 characters)
func TestNewEventID_Length(t *testing.T) {
	id := NewEventID() // ❌ Doesn't exist yet
	assert.Len(t, id, 26, "ULID should be 26 characters")
}

// RED: Test ULID is URL-safe (Base32 encoding)
func TestNewEventID_URLSafe(t *testing.T) {
	id := NewEventID() // ❌ Doesn't exist yet

	// ULID uses Crockford Base32 (0-9, A-Z, no I, L, O, U)
	for _, char := range id {
		assert.True(t,
			(char >= '0' && char <= '9') || (char >= 'A' && char <= 'Z'),
			"ULID should only contain Base32 characters")
	}
}
