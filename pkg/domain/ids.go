package domain

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// NewEventID generates a new ULID for an event.
// ULIDs are:
// - Time-sortable (first 48 bits = timestamp in milliseconds)
// - Lexicographically ordered (can sort as strings)
// - Globally unique (80 bits of entropy)
// - URL-safe (Crockford Base32 encoding)
//
// Example: "01HQZX5M7K2QY3P8R9N6W4V1T0"
func NewEventID() string {
	return ulid.Make().String()
}

// NewEventIDWithTime generates a ULID with a specific timestamp.
// Useful for:
// - Deterministic IDs in tests
// - Backfilling historical events
// - Ensuring correct time ordering
//
// The timestamp is truncated to millisecond precision (ULID requirement).
func NewEventIDWithTime(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), ulid.DefaultEntropy()).String()
}

// ParseEventID extracts timestamp from a ULID event ID.
// Returns error if the ID is not a valid ULID format.
//
// Note: ULID timestamps have millisecond precision.
func ParseEventID(id string) (time.Time, error) {
	parsed, err := ulid.Parse(id)
	if err != nil {
		return time.Time{}, err
	}
	return ulid.Time(parsed.Time()), nil
}
