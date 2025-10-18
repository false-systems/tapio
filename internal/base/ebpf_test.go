//go:build linux
// +build linux

package base

import (
	"context"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEBPF_FileNotFound(t *testing.T) {
	_, err := LoadEBPF("/nonexistent/path/program.o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eBPF object file not found")
}

func TestEBPFManager_AttachKprobe_ProgramNotFound(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.AttachKprobe("nonexistent_program", "sys_execve")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "program nonexistent_program not found")
}

func TestEBPFManager_AttachKretprobe_ProgramNotFound(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.AttachKretprobe("nonexistent_program", "sys_execve")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "program nonexistent_program not found")
}

func TestEBPFManager_AttachTracepoint_ProgramNotFound(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.AttachTracepoint("nonexistent_program", "syscalls", "sys_enter_execve")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "program nonexistent_program not found")
}

func TestEBPFManager_OpenRingBuffer_MapNotFound(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.OpenRingBuffer("nonexistent_map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ring buffer map nonexistent_map not found")
}

func TestEBPFManager_ReadEvents_RingBufferNotOpened(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := mgr.ReadEvents(ctx, func(data []byte) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ring buffer not opened")
}

func TestEBPFManager_GetMap_MapNotFound(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	_, err := mgr.GetMap("nonexistent_map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "map nonexistent_map not found")
}

func TestEBPFManager_Close_NoResources(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.Close()
	require.NoError(t, err)
}

func TestEBPFManager_WaitForReady_Timeout(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.WaitForReady(100 * time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for eBPF programs to be ready")
}

func TestNewEBPFManagerFromCollection_NilCollection(t *testing.T) {
	mgr := NewEBPFManagerFromCollection(nil)
	require.NotNil(t, mgr)
	assert.Nil(t, mgr.collection)
	assert.NotNil(t, mgr.links)
}

func TestNewEBPFManagerFromCollection_ValidCollection(t *testing.T) {
	// Create minimal collection for testing
	coll := &ebpf.Collection{}
	mgr := NewEBPFManagerFromCollection(coll)

	require.NotNil(t, mgr)
	assert.Equal(t, coll, mgr.collection)
	assert.NotNil(t, mgr.links)
	assert.Len(t, mgr.links, 0)
}

func TestEBPFManager_AttachTracepointWithProgram_NilProgram(t *testing.T) {
	mgr := &EBPFManager{
		links: make([]link.Link, 0),
	}

	err := mgr.AttachTracepointWithProgram(nil, "sock", "inet_sock_set_state")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "program cannot be nil")
}
