//go:build linux
// +build linux

package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock eBPF manager for testing
type mockEBPFManager struct {
	mu          sync.Mutex
	started     bool
	stopped     bool
	links       int
	ringbufOpen bool
	closeErr    error
}

func newMockEBPFManager() *mockEBPFManager {
	return &mockEBPFManager{}
}

func (m *mockEBPFManager) AttachKprobe(progName, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links++
	return nil
}

func (m *mockEBPFManager) AttachKretprobe(progName, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links++
	return nil
}

func (m *mockEBPFManager) AttachTracepoint(progName, group, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links++
	return nil
}

func (m *mockEBPFManager) AttachTracepointWithProgram(prog *ebpf.Program, group, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links++
	return nil
}

func (m *mockEBPFManager) OpenRingBuffer(mapName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ringbufOpen = true
	return nil
}

func (m *mockEBPFManager) ReadEvents(ctx context.Context, handler func([]byte) error) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	<-ctx.Done()

	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()

	return ctx.Err()
}

func (m *mockEBPFManager) GetMap(name string) (*ebpf.Map, error) {
	return nil, nil
}

func (m *mockEBPFManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return m.closeErr
}

func (m *mockEBPFManager) WaitForReady(timeout time.Duration) error {
	return nil
}

func (m *mockEBPFManager) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *mockEBPFManager) IsStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

func (m *mockEBPFManager) LinkCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.links
}

// RED: Test EBPFRuntime creation
func TestNewEBPFRuntime(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)
	require.NotNil(t, runtime)
}

// RED: Test Start begins reading events
func TestEBPFRuntime_Start(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eventCh := make(chan []byte, 10)
	handler := func(data []byte) error {
		eventCh <- data
		return nil
	}

	// Start should block until context cancelled
	go func() {
		_ = runtime.Start(ctx, handler) // Ignore: Test goroutine, error checked elsewhere
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Verify manager was started
	assert.True(t, mgr.IsStarted())

	// Wait for context cancellation
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	// Verify manager was stopped
	assert.True(t, mgr.IsStopped())
}

// RED: Test Stop closes resources
func TestEBPFRuntime_Stop(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	handler := func(data []byte) error {
		return nil
	}

	// Start runtime
	go func() {
		_ = runtime.Start(ctx, handler) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop runtime
	err := runtime.Stop()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify manager was stopped
	assert.True(t, mgr.IsStopped())
}

// RED: Test multiple Start calls fail
func TestEBPFRuntime_Start_AlreadyRunning(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()

	handler := func(data []byte) error {
		return nil
	}

	// Start first time
	go func() {
		_ = runtime.Start(ctx1, handler) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(50 * time.Millisecond)

	// Try to start again
	ctx2 := context.Background()
	err := runtime.Start(ctx2, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

// RED: Test Stop before Start
func TestEBPFRuntime_Stop_NotStarted(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)

	// Stop without starting
	err := runtime.Stop()
	assert.NoError(t, err) // Should be idempotent
}

// RED: Test IsHealthy
func TestEBPFRuntime_IsHealthy(t *testing.T) {
	mgr := newMockEBPFManager()
	runtime := NewEBPFRuntime(mgr)

	// Initially not healthy (not running)
	assert.False(t, runtime.IsHealthy())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	handler := func(data []byte) error {
		return nil
	}

	// Start runtime
	go func() {
		_ = runtime.Start(ctx, handler) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(50 * time.Millisecond)

	// Should be healthy when running
	assert.True(t, runtime.IsHealthy())

	// Stop
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Should be unhealthy after stop
	assert.False(t, runtime.IsHealthy())
}
