package systemd

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

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

// startJournalReader starts reading systemd journal logs
func (c *Collector) startJournalReader() error {
	// Simple journalctl command - just follow and output JSON
	cmd := exec.CommandContext(c.ctx, "journalctl", "--follow", "--output=json", "--lines=0")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Read journal entries in a goroutine
	go func() {
		defer cmd.Process.Kill()
		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			// Parse JSON entry
			var entry JournalEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue // Skip malformed entries
			}

			// Convert to RawEvent - just raw data, no processing
			jsonData, _ := json.Marshal(entry)

			// Parse timestamp (microseconds since epoch)
			var timestamp time.Time
			if entry.RealtimeTimestamp != "" {
				// journald timestamp is microseconds since epoch as string
				if usec, err := strconv.ParseInt(entry.RealtimeTimestamp, 10, 64); err == nil {
					timestamp = time.Unix(0, usec*1000) // Convert microseconds to nanoseconds
				} else {
					timestamp = time.Now()
				}
			} else {
				timestamp = time.Now()
			}

			event := collectors.RawEvent{
				Timestamp: timestamp,
				Type:      "journal",
				Data:      jsonData,
				Metadata: map[string]string{
					"collector":    "systemd",
					"source":       "journal",
					"systemd_unit": entry.SystemdUnit,
					"hostname":     entry.Hostname,
					"pid":          entry.PID,
					"uid":          entry.UID,
				},
				TraceID: collectors.GenerateTraceID(),
				SpanID:  collectors.GenerateSpanID(),
			}

			// Send event
			select {
			case c.events <- event:
			case <-c.ctx.Done():
				return
			default:
				// Drop if buffer full
			}
		}
	}()

	return nil
}
