package journald

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reader provides real-time journal streaming
type Reader struct {
	config  *ReaderConfig
	cmd     *exec.Cmd
	scanner *bufio.Scanner
	entries chan *LogEntry

	// State management
	mutex     sync.RWMutex
	isStarted bool
	isHealthy bool

	// Performance tracking
	entriesRead    uint64
	bytesRead      uint64
	reconnectCount int
	lastReconnect  time.Time
	lastEntry      time.Time

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// ReaderConfig configures the journald reader
type ReaderConfig struct {
	JournalPath       string
	SeekToEnd         bool
	FollowMode        bool
	ReadBatchSize     int
	ReadTimeout       time.Duration
	ReconnectInterval time.Duration
	OutputFormat      string // json, short, verbose
	Fields            []string
	Since             string
	Until             string
}

// LogEntry represents a journald log entry
type LogEntry struct {
	Timestamp      time.Time
	Service        string
	Unit           string
	Priority       int
	PriorityName   string
	Message        string
	Hostname       string
	ProcessID      int
	ThreadID       int
	BootID         string
	MachineID      string
	SystemdUnit    string
	SystemdSlice   string
	SelinuxContext string
	CommandLine    string
	Executable     string
	UserID         int
	GroupID        int
	Fields         map[string]interface{}
	RawData        map[string]interface{}
}

// LogEvent represents a processed log event
type LogEvent struct {
	Timestamp       time.Time
	Service         string
	Priority        int
	Message         string
	Fields          map[string]interface{}
	MatchedPatterns []string
	Classification  *EventClassification
}

// EventClassification represents event classification results
type EventClassification struct {
	Category   string
	Severity   string
	Confidence float64
	Tags       []string
	Metadata   map[string]interface{}
}

// NewReader creates a new journald reader
func NewReader(config *ReaderConfig) (*Reader, error) {
	if config == nil {
		config = DefaultReaderConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	reader := &Reader{
		config:  config,
		entries: make(chan *LogEntry, 10000),
		ctx:     ctx,
		cancel:  cancel,
	}

	return reader, nil
}

// DefaultReaderConfig returns the default reader configuration
func DefaultReaderConfig() *ReaderConfig {
	return &ReaderConfig{
		JournalPath:       "/var/log/journal",
		SeekToEnd:         true,
		FollowMode:        true,
		ReadBatchSize:     1000,
		ReadTimeout:       1 * time.Second,
		ReconnectInterval: 5 * time.Second,
		OutputFormat:      "json",
		Fields: []string{
			"__REALTIME_TIMESTAMP",
			"__MONOTONIC_TIMESTAMP",
			"_SYSTEMD_UNIT",
			"_SYSTEMD_SLICE",
			"SYSLOG_IDENTIFIER",
			"PRIORITY",
			"MESSAGE",
			"_PID",
			"_GID",
			"_UID",
			"_COMM",
			"_EXE",
			"_CMDLINE",
			"_HOSTNAME",
			"_BOOT_ID",
			"_MACHINE_ID",
		},
	}
}

// IsAvailable checks if journald is available
func (r *Reader) IsAvailable() bool {
	// Check if journalctl command exists
	_, err := exec.LookPath("journalctl")
	if err != nil {
		return false
	}

	// Check if we can read from journal
	cmd := exec.Command("journalctl", "--version")
	err = cmd.Run()
	return err == nil
}

// Start begins reading from journald
func (r *Reader) Start(ctx context.Context) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.isStarted {
		return fmt.Errorf("reader already started")
	}

	if err := r.startJournalctl(); err != nil {
		return fmt.Errorf("failed to start journalctl: %w", err)
	}

	go r.readEntries()
	go r.monitorHealth()

	r.isStarted = true
	r.isHealthy = true

	return nil
}

// Stop stops reading from journald
func (r *Reader) Stop() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if !r.isStarted {
		return nil
	}

	r.cancel()

	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
		r.cmd.Wait()
	}

	close(r.entries)
	r.isStarted = false
	r.isHealthy = false

	return nil
}

// GetEntryChannel returns the channel for receiving log entries
func (r *Reader) GetEntryChannel() <-chan *LogEntry {
	return r.entries
}

// startJournalctl starts the journalctl process
func (r *Reader) startJournalctl() error {
	args := []string{}

	// Output format
	args = append(args, "--output", r.config.OutputFormat)

	// Follow mode
	if r.config.FollowMode {
		args = append(args, "--follow")
	}

	// Seek to end
	if r.config.SeekToEnd {
		args = append(args, "--since", "now")
	} else if r.config.Since != "" {
		args = append(args, "--since", r.config.Since)
	}

	// Until time
	if r.config.Until != "" {
		args = append(args, "--until", r.config.Until)
	}

	// Fields
	if len(r.config.Fields) > 0 {
		for _, field := range r.config.Fields {
			args = append(args, "--output-fields", field)
		}
	}

	// No pager
	args = append(args, "--no-pager")

	// Create command
	r.cmd = exec.CommandContext(r.ctx, "journalctl", args...)

	// Get stdout pipe
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Start the command
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start journalctl: %w", err)
	}

	// Create scanner
	r.scanner = bufio.NewScanner(stdout)

	return nil
}

// readEntries reads entries from journalctl
func (r *Reader) readEntries() {
	defer func() {
		if p := recover(); p != nil {
			// Handle panic and attempt reconnection
			r.handleReaderError(fmt.Errorf("reader panic: %v", p))
		}
	}()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			if !r.scanner.Scan() {
				// Check for scanner error
				if err := r.scanner.Err(); err != nil {
					r.handleReaderError(fmt.Errorf("scanner error: %w", err))
				} else {
					// EOF - normal termination if not in follow mode
					if !r.config.FollowMode {
						return
					}
					r.handleReaderError(fmt.Errorf("unexpected EOF"))
				}
				return
			}

			line := r.scanner.Text()
			if line == "" {
				continue
			}

			entry, err := r.parseLogEntry(line)
			if err != nil {
				// Skip malformed entries but don't stop reading
				continue
			}

			r.updateStatistics(entry, len(line))

			select {
			case r.entries <- entry:
			default:
				// Drop entry if buffer is full
			}
		}
	}
}

// parseLogEntry parses a journalctl output line into a LogEntry
func (r *Reader) parseLogEntry(line string) (*LogEntry, error) {

	switch r.config.OutputFormat {
	case "json":
		return r.parseJSONEntry(line)
	case "short":
		return r.parseShortEntry(line)
	case "verbose":
		return r.parseVerboseEntry(line)
	default:
		return r.parseJSONEntry(line)
	}
}

// parseJSONEntry parses a JSON format log entry
func (r *Reader) parseJSONEntry(line string) (*LogEntry, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	entry := &LogEntry{
		Fields:  make(map[string]interface{}),
		RawData: raw,
	}

	// Extract timestamp
	if ts, ok := raw["__REALTIME_TIMESTAMP"]; ok {
		if tsStr, ok := ts.(string); ok {
			if usec, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
				entry.Timestamp = time.Unix(usec/1000000, (usec%1000000)*1000)
			}
		}
	}

	// Extract service/unit information
	if unit, ok := raw["_SYSTEMD_UNIT"]; ok {
		if unitStr, ok := unit.(string); ok {
			entry.SystemdUnit = unitStr
			entry.Service = strings.TrimSuffix(unitStr, ".service")
		}
	}

	if syslogId, ok := raw["SYSLOG_IDENTIFIER"]; ok {
		if syslogIdStr, ok := syslogId.(string); ok {
			if entry.Service == "" {
				entry.Service = syslogIdStr
			}
		}
	}

	// Extract priority
	if priority, ok := raw["PRIORITY"]; ok {
		if priorityStr, ok := priority.(string); ok {
			if p, err := strconv.Atoi(priorityStr); err == nil {
				entry.Priority = p
				entry.PriorityName = getPriorityName(p)
			}
		}
	}

	// Extract message
	if message, ok := raw["MESSAGE"]; ok {
		if messageStr, ok := message.(string); ok {
			entry.Message = messageStr
		}
	}

	// Extract process information
	if pid, ok := raw["_PID"]; ok {
		if pidStr, ok := pid.(string); ok {
			if p, err := strconv.Atoi(pidStr); err == nil {
				entry.ProcessID = p
			}
		}
	}

	if uid, ok := raw["_UID"]; ok {
		if uidStr, ok := uid.(string); ok {
			if u, err := strconv.Atoi(uidStr); err == nil {
				entry.UserID = u
			}
		}
	}

	if gid, ok := raw["_GID"]; ok {
		if gidStr, ok := gid.(string); ok {
			if g, err := strconv.Atoi(gidStr); err == nil {
				entry.GroupID = g
			}
		}
	}

	// Extract command line and executable
	if cmdline, ok := raw["_CMDLINE"]; ok {
		if cmdlineStr, ok := cmdline.(string); ok {
			entry.CommandLine = cmdlineStr
		}
	}

	if exe, ok := raw["_EXE"]; ok {
		if exeStr, ok := exe.(string); ok {
			entry.Executable = exeStr
		}
	}

	// Extract system information
	if hostname, ok := raw["_HOSTNAME"]; ok {
		if hostnameStr, ok := hostname.(string); ok {
			entry.Hostname = hostnameStr
		}
	}

	if bootId, ok := raw["_BOOT_ID"]; ok {
		if bootIdStr, ok := bootId.(string); ok {
			entry.BootID = bootIdStr
		}
	}

	if machineId, ok := raw["_MACHINE_ID"]; ok {
		if machineIdStr, ok := machineId.(string); ok {
			entry.MachineID = machineIdStr
		}
	}

	// Copy all fields for later use
	for k, v := range raw {
		entry.Fields[k] = v
	}

	return entry, nil
}

// parseShortEntry parses a short format log entry
func (r *Reader) parseShortEntry(line string) (*LogEntry, error) {
	// Simple parsing for short format: timestamp hostname service: message
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid short format line")
	}

	entry := &LogEntry{
		Fields: make(map[string]interface{}),
	}

	// Parse timestamp (assuming systemd short format)
	if ts, err := time.Parse("Jan 02 15:04:05", parts[0]+" "+parts[1]); err == nil {
		entry.Timestamp = ts
	}

	entry.Hostname = parts[2]

	// Extract service and message
	servicePart := parts[3]
	if colonIdx := strings.Index(servicePart, ":"); colonIdx > 0 {
		entry.Service = servicePart[:colonIdx]
		if colonIdx+2 < len(servicePart) {
			entry.Message = servicePart[colonIdx+2:]
		}
	} else {
		entry.Message = servicePart
	}

	return entry, nil
}

// parseVerboseEntry parses a verbose format log entry
func (r *Reader) parseVerboseEntry(line string) (*LogEntry, error) {
	// For now, fallback to simple parsing
	// In a full implementation, this would handle the verbose multi-line format
	return r.parseShortEntry(line)
}

// getPriorityName converts numeric priority to name
func getPriorityName(priority int) string {
	switch priority {
	case 0:
		return "emergency"
	case 1:
		return "alert"
	case 2:
		return "critical"
	case 3:
		return "error"
	case 4:
		return "warning"
	case 5:
		return "notice"
	case 6:
		return "info"
	case 7:
		return "debug"
	default:
		return "unknown"
	}
}

// handleReaderError handles reader errors and attempts reconnection
func (r *Reader) handleReaderError(err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.isHealthy = false

	// Attempt reconnection if in follow mode
	if r.config.FollowMode {
		go r.attemptReconnection()
	}
}

// attemptReconnection attempts to reconnect to journald
func (r *Reader) attemptReconnection() {
	time.Sleep(r.config.ReconnectInterval)

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Kill existing process
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
		r.cmd.Wait()
	}

	// Restart journalctl
	if err := r.startJournalctl(); err != nil {
		// Failed to reconnect, try again later
		go r.attemptReconnection()
		return
	}

	// Start reading again
	go r.readEntries()

	r.isHealthy = true
	r.reconnectCount++
	r.lastReconnect = time.Now()
}

// monitorHealth monitors the health of the reader
func (r *Reader) monitorHealth() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.mutex.RLock()
			lastEntry := r.lastEntry
			isHealthy := r.isHealthy
			r.mutex.RUnlock()

			// Check if we haven't received entries in a while
			if isHealthy && r.config.FollowMode {
				if time.Since(lastEntry) > 5*time.Minute {
					// No entries for 5 minutes, might be an issue
					r.handleReaderError(fmt.Errorf("no entries received for 5 minutes"))
				}
			}
		}
	}
}

// updateStatistics updates reader statistics
func (r *Reader) updateStatistics(entry *LogEntry, lineSize int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.entriesRead++
	r.bytesRead += uint64(lineSize)
	r.lastEntry = time.Now()
}

// GetStatistics returns reader statistics
func (r *Reader) GetStatistics() map[string]interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return map[string]interface{}{
		"entries_read":    r.entriesRead,
		"bytes_read":      r.bytesRead,
		"reconnect_count": r.reconnectCount,
		"last_reconnect":  r.lastReconnect,
		"last_entry":      r.lastEntry,
		"is_healthy":      r.isHealthy,
		"is_started":      r.isStarted,
	}
}
