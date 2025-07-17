package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryCursorManager(t *testing.T) {
	manager := newMemoryCursorManager()

	// Test initial state
	if manager.HasCursor() {
		t.Error("Expected no cursor initially")
	}

	_, err := manager.LoadCursor()
	if err == nil {
		t.Error("Expected error when loading non-existent cursor")
	}

	// Test save and load
	testCursor := "test-cursor-123"
	err = manager.SaveCursor(testCursor)
	if err != nil {
		t.Errorf("Failed to save cursor: %v", err)
	}

	if !manager.HasCursor() {
		t.Error("Expected cursor to exist after save")
	}

	loaded, err := manager.LoadCursor()
	if err != nil {
		t.Errorf("Failed to load cursor: %v", err)
	}

	if loaded != testCursor {
		t.Errorf("Expected cursor %q, got %q", testCursor, loaded)
	}

	// Test clear
	err = manager.ClearCursor()
	if err != nil {
		t.Errorf("Failed to clear cursor: %v", err)
	}

	if manager.HasCursor() {
		t.Error("Expected no cursor after clear")
	}
}

func TestFileCursorManager(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "journald-cursor-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cursorFile := filepath.Join(tempDir, "cursor")
	manager := newFileCursorManager(cursorFile)

	// Test initial state
	if manager.HasCursor() {
		t.Error("Expected no cursor initially")
	}

	_, err = manager.LoadCursor()
	if err == nil {
		t.Error("Expected error when loading non-existent cursor")
	}

	// Test save and load
	testCursor := "test-cursor-456"
	err = manager.SaveCursor(testCursor)
	if err != nil {
		t.Errorf("Failed to save cursor: %v", err)
	}

	if !manager.HasCursor() {
		t.Error("Expected cursor to exist after save")
	}

	// Verify file exists
	if _, err := os.Stat(cursorFile); os.IsNotExist(err) {
		t.Error("Expected cursor file to exist")
	}

	loaded, err := manager.LoadCursor()
	if err != nil {
		t.Errorf("Failed to load cursor: %v", err)
	}

	if loaded != testCursor {
		t.Errorf("Expected cursor %q, got %q", testCursor, loaded)
	}

	// Test persistence across manager instances
	newManager := newFileCursorManager(cursorFile)
	if !newManager.HasCursor() {
		t.Error("Expected cursor to persist across manager instances")
	}

	persistedCursor, err := newManager.LoadCursor()
	if err != nil {
		t.Errorf("Failed to load persisted cursor: %v", err)
	}

	if persistedCursor != testCursor {
		t.Errorf("Expected persisted cursor %q, got %q", testCursor, persistedCursor)
	}

	// Test clear
	err = manager.ClearCursor()
	if err != nil {
		t.Errorf("Failed to clear cursor: %v", err)
	}

	if manager.HasCursor() {
		t.Error("Expected no cursor after clear")
	}

	// Verify file is removed
	if _, err := os.Stat(cursorFile); !os.IsNotExist(err) {
		t.Error("Expected cursor file to be removed")
	}
}

func TestFileCursorManagerDirectoryCreation(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "journald-cursor-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Use nested path that doesn't exist
	cursorFile := filepath.Join(tempDir, "nested", "deep", "cursor")
	manager := newFileCursorManager(cursorFile)

	// Test that directory is created automatically
	testCursor := "test-cursor-nested"
	err = manager.SaveCursor(testCursor)
	if err != nil {
		t.Errorf("Failed to save cursor with nested path: %v", err)
	}

	// Verify the nested directories were created
	if _, err := os.Stat(filepath.Dir(cursorFile)); os.IsNotExist(err) {
		t.Error("Expected nested directories to be created")
	}

	// Verify cursor was saved correctly
	loaded, err := manager.LoadCursor()
	if err != nil {
		t.Errorf("Failed to load cursor from nested path: %v", err)
	}

	if loaded != testCursor {
		t.Errorf("Expected cursor %q, got %q", testCursor, loaded)
	}
}

func TestFileCursorManagerEmptyCursor(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "journald-cursor-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cursorFile := filepath.Join(tempDir, "cursor")
	manager := newFileCursorManager(cursorFile)

	// Test that empty cursor returns error
	err = manager.SaveCursor("")
	if err == nil {
		t.Error("Expected error when saving empty cursor")
	}
}