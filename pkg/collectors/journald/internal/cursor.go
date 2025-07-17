package internal

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
)

// fileCursorManager implements cursor persistence to disk
type fileCursorManager struct {
	filePath string
	mutex    sync.RWMutex
}

// newFileCursorManager creates a new file-based cursor manager
func newFileCursorManager(filePath string) core.CursorManager {
	return &fileCursorManager{
		filePath: filePath,
	}
}

// SaveCursor saves the cursor to disk
func (f *fileCursorManager) SaveCursor(cursor string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	
	if cursor == "" {
		return fmt.Errorf("empty cursor")
	}
	
	// Ensure directory exists
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cursor directory: %w", err)
	}
	
	// Write cursor to temporary file first
	tempFile := f.filePath + ".tmp"
	if err := ioutil.WriteFile(tempFile, []byte(cursor), 0644); err != nil {
		return fmt.Errorf("failed to write cursor to temp file: %w", err)
	}
	
	// Atomic rename
	if err := os.Rename(tempFile, f.filePath); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename cursor file: %w", err)
	}
	
	return nil
}

// LoadCursor loads the cursor from disk
func (f *fileCursorManager) LoadCursor() (string, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	
	data, err := ioutil.ReadFile(f.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", core.ErrCursorNotFound
		}
		return "", fmt.Errorf("failed to read cursor file: %w", err)
	}
	
	cursor := strings.TrimSpace(string(data))
	if cursor == "" {
		return "", core.ErrCursorFileCorrupt
	}
	
	return cursor, nil
}

// HasCursor checks if a cursor file exists
func (f *fileCursorManager) HasCursor() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	
	_, err := os.Stat(f.filePath)
	return err == nil
}

// ClearCursor removes the cursor file
func (f *fileCursorManager) ClearCursor() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	
	err := os.Remove(f.filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cursor file: %w", err)
	}
	
	return nil
}

// memoryCursorManager implements in-memory cursor storage
type memoryCursorManager struct {
	cursor string
	mutex  sync.RWMutex
}

// newMemoryCursorManager creates a new memory-based cursor manager
func newMemoryCursorManager() core.CursorManager {
	return &memoryCursorManager{}
}

// SaveCursor saves the cursor in memory
func (m *memoryCursorManager) SaveCursor(cursor string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.cursor = cursor
	return nil
}

// LoadCursor loads the cursor from memory
func (m *memoryCursorManager) LoadCursor() (string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if m.cursor == "" {
		return "", core.ErrCursorNotFound
	}
	
	return m.cursor, nil
}

// HasCursor checks if a cursor exists in memory
func (m *memoryCursorManager) HasCursor() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	return m.cursor != ""
}

// ClearCursor clears the cursor from memory
func (m *memoryCursorManager) ClearCursor() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.cursor = ""
	return nil
}