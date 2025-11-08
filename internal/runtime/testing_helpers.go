package runtime

import (
	"fmt"
	"os"
	"time"
)

// splitLines splits a string into lines, handling both \n and \r\n line endings.
// Returns empty slice for empty input.
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	lines := []string{}
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			if current != "" {
				lines = append(lines, current)
			}
			current = ""
		} else if ch != '\r' {
			current += string(ch)
		}
	}
	// Add last line if not terminated with newline
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// waitForFileReady polls for a file to exist, preventing flaky tests from
// fixed sleep durations. Returns error if timeout is exceeded.
func waitForFileReady(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := os.Stat(path)
		if err == nil && info.Size() >= 0 {
			// File exists (size may be zero before first event, but file is created)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for file %s to be created", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForQueueDrain waits for the event queue to drain by polling file size.
// This is more reliable than fixed sleep durations for integration tests.
func waitForQueueDrain(path string, expectedLines int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := splitLines(string(data))
			if len(lines) >= expectedLines {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %d events (got %d)", expectedLines, len(splitLines(string(data))))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
