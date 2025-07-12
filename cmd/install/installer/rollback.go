package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

// commandHistory implements CommandHistory interface
type commandHistory struct {
	mu       sync.Mutex
	commands []Command
	maxSize  int
}

// NewCommandHistory creates a new command history
func NewCommandHistory() CommandHistory {
	return &commandHistory{
		commands: make([]Command, 0),
		maxSize:  100, // Limit history size
	}
}

// Push adds a command to history
func (h *commandHistory) Push(cmd Command) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.commands = append(h.commands, cmd)
	
	// Trim history if it exceeds max size
	if len(h.commands) > h.maxSize {
		h.commands = h.commands[len(h.commands)-h.maxSize:]
	}
}

// Pop removes and returns the last command
func (h *commandHistory) Pop() (Command, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	if len(h.commands) == 0 {
		return nil, false
	}
	
	cmd := h.commands[len(h.commands)-1]
	h.commands = h.commands[:len(h.commands)-1]
	
	return cmd, true
}

// Clear removes all commands
func (h *commandHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.commands = h.commands[:0]
}

// Rollback undoes all commands in reverse order
func (h *commandHistory) Rollback(ctx context.Context) error {
	h.mu.Lock()
	commands := make([]Command, len(h.commands))
	copy(commands, h.commands)
	h.mu.Unlock()
	
	var errors []error
	
	// Execute rollback in reverse order
	for i := len(commands) - 1; i >= 0; i-- {
		cmd := commands[i]
		
		if !cmd.CanUndo() {
			continue
		}
		
		if err := cmd.Undo(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to undo %T: %w", cmd, err))
		}
	}
	
	// Clear history after rollback
	h.Clear()
	
	if len(errors) > 0 {
		return fmt.Errorf("rollback completed with %d errors: %v", len(errors), errors)
	}
	
	return nil
}

// baseCommand provides common command functionality
type baseCommand struct {
	name        string
	description string
	canUndo     bool
	executed    bool
	mu          sync.Mutex
}

// CanUndo returns if the command can be undone
func (c *baseCommand) CanUndo() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canUndo && c.executed
}

// Common installation commands

// CreateDirectoryCommand creates a directory
type CreateDirectoryCommand struct {
	baseCommand
	path string
	mode os.FileMode
}

// NewCreateDirectoryCommand creates a new directory creation command
func NewCreateDirectoryCommand(path string, mode os.FileMode) Command {
	return &CreateDirectoryCommand{
		baseCommand: baseCommand{
			name:        "create-directory",
			description: fmt.Sprintf("Create directory %s", path),
			canUndo:     true,
		},
		path: path,
		mode: mode,
	}
}

func (c *CreateDirectoryCommand) Execute(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if err := os.MkdirAll(c.path, c.mode); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", c.path, err)
	}
	
	c.executed = true
	return nil
}

func (c *CreateDirectoryCommand) Undo(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.executed {
		return nil
	}
	
	// Only remove if directory is empty
	if err := os.Remove(c.path); err != nil {
		// If directory is not empty, don't fail
		if !os.IsExist(err) {
			return fmt.Errorf("failed to remove directory %s: %w", c.path, err)
		}
	}
	
	c.executed = false
	return nil
}

// CopyFileCommand copies a file
type CopyFileCommand struct {
	baseCommand
	src      string
	dst      string
	mode     os.FileMode
	backup   string
}

// NewCopyFileCommand creates a new file copy command
func NewCopyFileCommand(src, dst string, mode os.FileMode) Command {
	return &CopyFileCommand{
		baseCommand: baseCommand{
			name:        "copy-file",
			description: fmt.Sprintf("Copy %s to %s", src, dst),
			canUndo:     true,
		},
		src:  src,
		dst:  dst,
		mode: mode,
	}
}

func (c *CopyFileCommand) Execute(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Check if destination exists and create backup
	if _, err := os.Stat(c.dst); err == nil {
		c.backup = c.dst + ".backup"
		if err := os.Rename(c.dst, c.backup); err != nil {
			return fmt.Errorf("failed to backup existing file: %w", err)
		}
	}
	
	// Copy file
	srcFile, err := os.Open(c.src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()
	
	dstFile, err := os.OpenFile(c.dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, c.mode)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()
	
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	
	c.executed = true
	return nil
}

func (c *CopyFileCommand) Undo(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.executed {
		return nil
	}
	
	// Remove copied file
	if err := os.Remove(c.dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove file: %w", err)
	}
	
	// Restore backup if exists
	if c.backup != "" {
		if err := os.Rename(c.backup, c.dst); err != nil {
			return fmt.Errorf("failed to restore backup: %w", err)
		}
	}
	
	c.executed = false
	return nil
}

// WriteFileCommand writes content to a file
type WriteFileCommand struct {
	baseCommand
	path    string
	content []byte
	mode    os.FileMode
	backup  []byte
}

// NewWriteFileCommand creates a new file write command
func NewWriteFileCommand(path string, content []byte, mode os.FileMode) Command {
	return &WriteFileCommand{
		baseCommand: baseCommand{
			name:        "write-file",
			description: fmt.Sprintf("Write file %s", path),
			canUndo:     true,
		},
		path:    path,
		content: content,
		mode:    mode,
	}
}

func (c *WriteFileCommand) Execute(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Read existing content for backup
	if existing, err := os.ReadFile(c.path); err == nil {
		c.backup = existing
	}
	
	// Write new content
	if err := os.WriteFile(c.path, c.content, c.mode); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	
	c.executed = true
	return nil
}

func (c *WriteFileCommand) Undo(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.executed {
		return nil
	}
	
	if c.backup != nil {
		// Restore original content
		if err := os.WriteFile(c.path, c.backup, c.mode); err != nil {
			return fmt.Errorf("failed to restore file: %w", err)
		}
	} else {
		// Remove file if it didn't exist before
		if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove file: %w", err)
		}
	}
	
	c.executed = false
	return nil
}

// SymlinkCommand creates a symbolic link
type SymlinkCommand struct {
	baseCommand
	src string
	dst string
}

// NewSymlinkCommand creates a new symlink command
func NewSymlinkCommand(src, dst string) Command {
	return &SymlinkCommand{
		baseCommand: baseCommand{
			name:        "create-symlink",
			description: fmt.Sprintf("Create symlink %s -> %s", dst, src),
			canUndo:     true,
		},
		src: src,
		dst: dst,
	}
}

func (c *SymlinkCommand) Execute(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Remove existing symlink if it exists
	if _, err := os.Lstat(c.dst); err == nil {
		if err := os.Remove(c.dst); err != nil {
			return fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}
	
	// Create symlink
	if err := os.Symlink(c.src, c.dst); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	
	c.executed = true
	return nil
}

func (c *SymlinkCommand) Undo(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.executed {
		return nil
	}
	
	// Remove symlink
	if err := os.Remove(c.dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}
	
	c.executed = false
	return nil
}

// CompositeCommand executes multiple commands as a single unit
type CompositeCommand struct {
	baseCommand
	commands []Command
}

// NewCompositeCommand creates a new composite command
func NewCompositeCommand(name, description string, commands ...Command) Command {
	return &CompositeCommand{
		baseCommand: baseCommand{
			name:        name,
			description: description,
			canUndo:     true,
		},
		commands: commands,
	}
}

func (c *CompositeCommand) Execute(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	executed := make([]Command, 0, len(c.commands))
	
	for _, cmd := range c.commands {
		if err := cmd.Execute(ctx); err != nil {
			// Rollback executed commands
			for i := len(executed) - 1; i >= 0; i-- {
				if executed[i].CanUndo() {
					executed[i].Undo(ctx)
				}
			}
			return err
		}
		executed = append(executed, cmd)
	}
	
	c.executed = true
	return nil
}

func (c *CompositeCommand) Undo(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.executed {
		return nil
	}
	
	var errors []error
	
	// Undo in reverse order
	for i := len(c.commands) - 1; i >= 0; i-- {
		if c.commands[i].CanUndo() {
			if err := c.commands[i].Undo(ctx); err != nil {
				errors = append(errors, err)
			}
		}
	}
	
	c.executed = false
	
	if len(errors) > 0 {
		return fmt.Errorf("composite undo had %d errors: %v", len(errors), errors)
	}
	
	return nil
}