//go:build linux

package linux

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
)

// platformImpl implements the platform-specific journald functionality for Linux
type platformImpl struct {
	config  core.Config
	journal *sdjournal.Journal
	reader  core.LogReader
}

// NewPlatformImpl creates a new Linux platform implementation
func NewPlatformImpl() (*platformImpl, error) {
	return &platformImpl{}, nil
}

// Init initializes the platform implementation
func (p *platformImpl) Init(config core.Config) error {
	p.config = config

	// Create journal reader
	reader, err := newJournalReader(config)
	if err != nil {
		return fmt.Errorf("failed to create journal reader: %w", err)
	}

	p.reader = reader
	return nil
}

// Start starts the platform implementation
func (p *platformImpl) Start(ctx context.Context) error {
	if p.reader == nil {
		return fmt.Errorf("reader not initialized")
	}

	if err := p.reader.Open(); err != nil {
		return fmt.Errorf("failed to open journal: %w", err)
	}

	return nil
}

// Stop stops the platform implementation
func (p *platformImpl) Stop() error {
	if p.reader != nil {
		return p.reader.Close()
	}
	return nil
}

// Reader returns the log reader
func (p *platformImpl) Reader() core.LogReader {
	return p.reader
}

// IsOpen checks if the journal is open
func (p *platformImpl) IsOpen() bool {
	if p.reader == nil {
		return false
	}
	return p.reader.IsOpen()
}

// BootID returns the current boot ID
func (p *platformImpl) BootID() string {
	if p.reader == nil {
		return ""
	}
	return p.reader.GetBootID()
}

// MachineID returns the machine ID
func (p *platformImpl) MachineID() string {
	if p.reader == nil {
		return ""
	}
	return p.reader.GetMachineID()
}

// CurrentCursor returns the current cursor
func (p *platformImpl) CurrentCursor() string {
	if p.reader == nil {
		return ""
	}
	cursor, _ := p.reader.GetCursor()
	return cursor
}

// journalReader implements core.LogReader using systemd journal
type journalReader struct {
	config    core.Config
	journal   *sdjournal.Journal
	bootID    string
	machineID string
}

// newJournalReader creates a new journal reader
func newJournalReader(config core.Config) (core.LogReader, error) {
	return &journalReader{
		config: config,
	}, nil
}

// Open opens the systemd journal
func (r *journalReader) Open() error {
	var err error

	// Open the journal
	if len(r.config.Units) > 0 {
		// Open with unit filters
		r.journal, err = sdjournal.NewJournal()
		if err != nil {
			return core.NewCollectorError(core.ErrorTypeJournal, "failed to open journal", err)
		}

		// Add unit filters
		for _, unit := range r.config.Units {
			err = r.journal.AddMatch("_SYSTEMD_UNIT=" + unit)
			if err != nil {
				r.journal.Close()
				return core.NewCollectorError(core.ErrorTypeJournal, "failed to add unit filter", err)
			}
		}
	} else {
		// Open without filters
		r.journal, err = sdjournal.NewJournal()
		if err != nil {
			return core.NewCollectorError(core.ErrorTypeJournal, "failed to open journal", err)
		}
	}

	// Add priority filters
	if len(r.config.Priorities) > 0 {
		priorityMatches := make([]string, 0, len(r.config.Priorities))
		for _, priority := range r.config.Priorities {
			priorityMatches = append(priorityMatches, fmt.Sprintf("PRIORITY=%d", int(priority)))
		}

		// Add OR-ed priority matches
		for _, match := range priorityMatches {
			err = r.journal.AddMatch(match)
			if err != nil {
				r.journal.Close()
				return core.NewCollectorError(core.ErrorTypeJournal, "failed to add priority filter", err)
			}
		}

		// Use OR logic for priorities
		err = r.journal.AddDisjunction()
		if err != nil {
			r.journal.Close()
			return core.NewCollectorError(core.ErrorTypeJournal, "failed to add priority disjunction", err)
		}
	}

	// Add boot ID filter if specified
	if r.config.BootID != "" {
		err = r.journal.AddMatch("_BOOT_ID=" + r.config.BootID)
		if err != nil {
			r.journal.Close()
			return core.NewCollectorError(core.ErrorTypeJournal, "failed to add boot ID filter", err)
		}
	}

	// Get system information
	r.bootID = r.getBootIDFromJournal()
	r.machineID = r.getMachineIDFromJournal()

	return nil
}

// Close closes the journal
func (r *journalReader) Close() error {
	if r.journal != nil {
		err := r.journal.Close()
		r.journal = nil
		if err != nil {
			return core.NewCollectorError(core.ErrorTypeJournal, "failed to close journal", err)
		}
	}
	return nil
}

// IsOpen checks if the journal is open
func (r *journalReader) IsOpen() bool {
	return r.journal != nil
}

// ReadEntry reads the next journal entry
func (r *journalReader) ReadEntry() (*core.LogEntry, error) {
	if r.journal == nil {
		return nil, core.ErrJournalNotOpen
	}

	// Move to next entry
	ret, err := r.journal.Next()
	if err != nil {
		return nil, core.NewCollectorError(core.ErrorTypeRead, "failed to read next entry", err)
	}

	if ret == 0 {
		return nil, core.ErrNoMoreEntries
	}

	// Get all entry data
	entry, err := r.journal.GetEntry()
	if err != nil {
		return nil, core.NewCollectorError(core.ErrorTypeRead, "failed to get entry data", err)
	}

	// Convert to our LogEntry format
	logEntry := r.convertEntry(entry)

	return logEntry, nil
}

// SeekCursor seeks to a specific cursor position
func (r *journalReader) SeekCursor(cursor string) error {
	if r.journal == nil {
		return core.ErrJournalNotOpen
	}

	err := r.journal.SeekCursor(cursor)
	if err != nil {
		return core.NewCollectorError(core.ErrorTypeSeek, "failed to seek to cursor", err)
	}

	return nil
}

// SeekTime seeks to a specific timestamp
func (r *journalReader) SeekTime(timestamp time.Time) error {
	if r.journal == nil {
		return core.ErrJournalNotOpen
	}

	// Convert to microseconds since epoch
	usec := uint64(timestamp.UnixNano() / 1000)

	err := r.journal.SeekRealtimeUsec(usec)
	if err != nil {
		return core.NewCollectorError(core.ErrorTypeSeek, "failed to seek to timestamp", err)
	}

	return nil
}

// GetCursor gets the current cursor position
func (r *journalReader) GetCursor() (string, error) {
	if r.journal == nil {
		return "", core.ErrJournalNotOpen
	}

	cursor, err := r.journal.GetCursor()
	if err != nil {
		return "", core.NewCollectorError(core.ErrorTypeCursor, "failed to get cursor", err)
	}

	return cursor, nil
}

// WaitForEntries waits for new entries to become available
func (r *journalReader) WaitForEntries(timeout time.Duration) error {
	if r.journal == nil {
		return core.ErrJournalNotOpen
	}

	// Convert timeout to microseconds
	timeoutUsec := int(timeout.Microseconds())

	ret := r.journal.Wait(timeoutUsec)
	switch ret {
	case sdjournal.SD_JOURNAL_NOP:
		return core.ErrReadTimeout
	case sdjournal.SD_JOURNAL_APPEND:
		return nil
	case sdjournal.SD_JOURNAL_INVALIDATE:
		return nil
	default:
		return core.NewCollectorError(core.ErrorTypeRead, "journal wait failed", nil)
	}
}

// GetBootID returns the boot ID
func (r *journalReader) GetBootID() string {
	return r.bootID
}

// GetMachineID returns the machine ID
func (r *journalReader) GetMachineID() string {
	return r.machineID
}

// Helper methods

// convertEntry converts a systemd journal entry to our LogEntry format
func (r *journalReader) convertEntry(entry *sdjournal.JournalEntry) *core.LogEntry {
	logEntry := &core.LogEntry{
		Fields: make(map[string]interface{}),
	}

	// Set timestamp
	logEntry.Timestamp = time.Unix(0, int64(entry.RealtimeTimestamp*1000))
	logEntry.MonotonicTime = entry.MonotonicTimestamp

	// Process all fields
	for key, value := range entry.Fields {
		// Store in fields map
		logEntry.Fields[key] = value

		// Map important fields to struct fields
		switch key {
		case "MESSAGE":
			logEntry.Message = value
		case "PRIORITY":
			if priority, err := strconv.Atoi(value); err == nil {
				logEntry.Priority = core.Priority(priority)
			}
		case "SYSLOG_FACILITY":
			logEntry.Facility = value
		case "SYSLOG_IDENTIFIER":
			logEntry.Identifier = value
		case "_PID":
			if pid, err := strconv.ParseInt(value, 10, 32); err == nil {
				logEntry.PID = int32(pid)
			}
		case "_UID":
			if uid, err := strconv.ParseInt(value, 10, 32); err == nil {
				logEntry.UID = int32(uid)
			}
		case "_GID":
			if gid, err := strconv.ParseInt(value, 10, 32); err == nil {
				logEntry.GID = int32(gid)
			}
		case "_COMM":
			logEntry.Comm = value
		case "_EXE":
			logEntry.Exe = value
		case "_CMDLINE":
			logEntry.Cmdline = value
		case "_SYSTEMD_UNIT":
			logEntry.Unit = value
		case "_SYSTEMD_USER_UNIT":
			logEntry.UserUnit = value
		case "_SYSTEMD_SESSION":
			logEntry.Session = value
		case "_HOSTNAME":
			logEntry.HostName = value
		case "_BOOT_ID":
			logEntry.BootID = value
		case "_MACHINE_ID":
			logEntry.MachineID = value
		case "__CURSOR":
			logEntry.Cursor = value
		}
	}

	// Set defaults for missing required fields
	if logEntry.BootID == "" {
		logEntry.BootID = r.bootID
	}
	if logEntry.MachineID == "" {
		logEntry.MachineID = r.machineID
	}

	// Get cursor if not in fields
	if logEntry.Cursor == "" {
		if cursor, err := r.journal.GetCursor(); err == nil {
			logEntry.Cursor = cursor
		}
	}

	return logEntry
}

// getBootIDFromJournal extracts boot ID from journal
func (r *journalReader) getBootIDFromJournal() string {
	if r.journal == nil {
		return ""
	}

	// Try to get boot ID from current entry or seek to first entry
	entry, err := r.journal.GetEntry()
	if err != nil {
		// Try seeking to first entry
		r.journal.SeekHead()
		r.journal.Next()
		entry, err = r.journal.GetEntry()
		if err != nil {
			return ""
		}
	}

	if bootID, exists := entry.Fields["_BOOT_ID"]; exists {
		return bootID
	}

	return ""
}

// getMachineIDFromJournal extracts machine ID from journal
func (r *journalReader) getMachineIDFromJournal() string {
	if r.journal == nil {
		return ""
	}

	// Try to get machine ID from current entry
	entry, err := r.journal.GetEntry()
	if err != nil {
		// Try seeking to first entry
		r.journal.SeekHead()
		r.journal.Next()
		entry, err = r.journal.GetEntry()
		if err != nil {
			return ""
		}
	}

	if machineID, exists := entry.Fields["_MACHINE_ID"]; exists {
		return machineID
	}

	return ""
}
