//go:build linux

package systemd

// JournalEntry represents a minimal journal entry
// We only extract what we need - no business logic
type JournalEntry struct {
	RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
	Message           string `json:"MESSAGE"`
	SystemdUnit       string `json:"_SYSTEMD_UNIT"`
	Hostname          string `json:"_HOSTNAME"`
	PID               string `json:"_PID"`
	UID               string `json:"_UID"`
}
