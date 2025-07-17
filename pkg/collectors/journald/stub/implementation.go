//go:build !linux

package stub

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
)

// platformImpl provides a stub implementation for non-Linux platforms
type platformImpl struct{}

// NewPlatformImpl creates a stub platform implementation
func NewPlatformImpl() (*platformImpl, error) {
	return &platformImpl{}, nil
}

// Init returns platform not supported error
func (p *platformImpl) Init(config core.Config) error {
	return core.ErrPlatformNotSupported
}

// Start returns platform not supported error
func (p *platformImpl) Start(ctx context.Context) error {
	return core.ErrLinuxOnly
}

// Stop returns nil (no-op)
func (p *platformImpl) Stop() error {
	return nil
}

// Reader returns a stub reader
func (p *platformImpl) Reader() core.LogReader {
	return &stubLogReader{}
}

// IsOpen always returns false
func (p *platformImpl) IsOpen() bool {
	return false
}

// BootID returns empty string
func (p *platformImpl) BootID() string {
	return ""
}

// MachineID returns empty string
func (p *platformImpl) MachineID() string {
	return ""
}

// CurrentCursor returns empty string
func (p *platformImpl) CurrentCursor() string {
	return ""
}

// stubLogReader provides a stub implementation of LogReader
type stubLogReader struct{}

// Open returns platform not supported error
func (r *stubLogReader) Open() error {
	return core.ErrLinuxOnly
}

// Close returns nil (no-op)
func (r *stubLogReader) Close() error {
	return nil
}

// IsOpen always returns false
func (r *stubLogReader) IsOpen() bool {
	return false
}

// ReadEntry returns platform not supported error
func (r *stubLogReader) ReadEntry() (*core.LogEntry, error) {
	return nil, core.ErrLinuxOnly
}

// SeekCursor returns platform not supported error
func (r *stubLogReader) SeekCursor(cursor string) error {
	return core.ErrLinuxOnly
}

// SeekTime returns platform not supported error
func (r *stubLogReader) SeekTime(timestamp time.Time) error {
	return core.ErrLinuxOnly
}

// GetCursor returns platform not supported error
func (r *stubLogReader) GetCursor() (string, error) {
	return "", core.ErrLinuxOnly
}

// WaitForEntries returns platform not supported error
func (r *stubLogReader) WaitForEntries(timeout time.Duration) error {
	return core.ErrLinuxOnly
}

// GetBootID returns empty string
func (r *stubLogReader) GetBootID() string {
	return ""
}

// GetMachineID returns empty string
func (r *stubLogReader) GetMachineID() string {
	return ""
}