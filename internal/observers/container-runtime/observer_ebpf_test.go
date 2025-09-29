//go:build linux
// +build linux

package containerruntime

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestStartEBPF tests the eBPF initialization process
func TestStartEBPF(t *testing.T) {
	config := NewDefaultConfig("test-ebpf")
	config.EnableOOMKill = true
	config.EnableMemoryPressure = true
	config.EnableProcessExit = true

	observer, err := NewObserver("test-ebpf", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	// Mock runtime client
	observer.runtimeClient = &mockRuntimeClient{
		containers: []Container{
			{ID: "test-container", PID: 1234, CgroupID: 5678},
		},
	}

	t.Run("Load eBPF programs", func(t *testing.T) {
		err := observer.loadEBPFPrograms()
		// May fail without proper privileges
		if err != nil {
			t.Skip("Cannot load eBPF programs without proper privileges")
		}

		assert.NotNil(t, observer.ebpfState)
		state := observer.ebpfState.(*ebpfStateImpl)
		assert.NotNil(t, state.objs)
	})

	t.Run("Attach programs", func(t *testing.T) {
		// Skip if loadEBPFPrograms failed
		if observer.ebpfState == nil {
			t.Skip("eBPF programs not loaded")
		}

		err := observer.attachPrograms()
		// May fail without proper privileges
		if err != nil {
			t.Skip("Cannot attach eBPF programs without proper privileges")
		}

		state := observer.ebpfState.(*ebpfStateImpl)
		assert.Greater(t, len(state.links), 0)
	})

	t.Run("Cleanup", func(t *testing.T) {
		// Should not panic even if not initialized
		observer.cleanup()
		assert.Nil(t, observer.ebpfState)
	})
}

// TestConvertToObserverEvent tests BPF event conversion
func TestConvertToObserverEvent(t *testing.T) {
	observer := &Observer{
		BaseObserver: &domain.BaseObserver{},
	}
	observer.SetName("test-observer")

	tests := []struct {
		name          string
		bpfEvent      *BPFContainerExitEvent
		expectedType  domain.EventType
		expectedSev   domain.EventSeverity
		checkExitCode bool
	}{
		{
			name: "Normal process exit",
			bpfEvent: &BPFContainerExitEvent{
				PID:         1234,
				ExitCode:    0,
				ContainerID: stringToInt8Array("container-123"),
				Comm:        stringToInt8Array("nginx"),
				CgroupID:    5678,
			},
			expectedType:  domain.EventTypeKernelProcess,
			expectedSev:   domain.EventSeverityInfo,
			checkExitCode: true,
		},
		{
			name: "OOM killed process",
			bpfEvent: &BPFContainerExitEvent{
				PID:         2345,
				ExitCode:    137,
				OOMKilled:   1,
				ContainerID: stringToInt8Array("container-456"),
				Comm:        stringToInt8Array("java"),
				CgroupID:    6789,
			},
			expectedType:  domain.EventTypeContainerOOM,
			expectedSev:   domain.EventSeverityCritical,
			checkExitCode: true,
		},
		{
			name: "Failed process exit",
			bpfEvent: &BPFContainerExitEvent{
				PID:         3456,
				ExitCode:    1,
				ContainerID: stringToInt8Array("container-789"),
				Comm:        stringToInt8Array("python"),
				CgroupID:    7890,
			},
			expectedType:  domain.EventTypeContainerExit,
			expectedSev:   domain.EventSeverityWarning,
			checkExitCode: true,
		},
		{
			name: "Memory pressure",
			bpfEvent: &BPFContainerExitEvent{
				PID:         4567,
				ExitCode:    0,
				MemoryUsage: 900000000,  // 900MB
				MemoryLimit: 1000000000, // 1GB
				ContainerID: stringToInt8Array("container-mem"),
				Comm:        stringToInt8Array("redis"),
			},
			expectedType:  domain.EventTypeMemoryPressure,
			expectedSev:   domain.EventSeverityWarning,
			checkExitCode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := observer.convertToObserverEvent(tt.bpfEvent)
			require.NoError(t, err)
			assert.NotNil(t, event)

			assert.Equal(t, tt.expectedType, event.Type)
			assert.Equal(t, tt.expectedSev, event.Severity)
			assert.Equal(t, "test-observer", event.Source)

			// Check container data
			containerData, ok := event.EventData.(domain.EventDataContainer)
			assert.True(t, ok)
			assert.NotNil(t, containerData.Container)

			if tt.checkExitCode {
				assert.NotNil(t, containerData.Container.ExitCode)
				assert.Equal(t, tt.bpfEvent.ExitCode, *containerData.Container.ExitCode)
			}

			// Check correlation hints
			assert.NotNil(t, event.CorrelationHints)
			expectedID := CStringToGo(tt.bpfEvent.ContainerID[:])
			assert.Equal(t, expectedID, event.CorrelationHints.ContainerID)
		})
	}
}

// TestHandleRingBufferEvent tests ring buffer event processing
func TestHandleRingBufferEvent(t *testing.T) {
	config := NewDefaultConfig("test-ring")
	observer, err := NewObserver("test-ring", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()

	// Initialize channel
	eventCh := make(chan *domain.CollectorEvent, 10)
	observer.EventChannelManager = &domain.EventChannelManager{
		EventChannel: eventCh,
	}
	observer.BaseObserver = &domain.BaseObserver{}

	t.Run("Valid event", func(t *testing.T) {
		event := BPFContainerExitEvent{
			PID:         1234,
			ExitCode:    0,
			ContainerID: stringToInt8Array("test-container"),
			Comm:        stringToInt8Array("test-app"),
		}

		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.LittleEndian, &event)
		require.NoError(t, err)

		observer.handleRingBufferEvent(buf.Bytes())

		// Check event was sent
		select {
		case receivedEvent := <-eventCh:
			assert.NotNil(t, receivedEvent)
			assert.Equal(t, domain.EventTypeKernelProcess, receivedEvent.Type)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("No event received")
		}
	})

	t.Run("Invalid event size", func(t *testing.T) {
		// Send data that's too small
		observer.handleRingBufferEvent([]byte{1, 2, 3})

		// Should not send any event
		select {
		case <-eventCh:
			t.Fatal("Should not receive event for invalid data")
		case <-time.After(100 * time.Millisecond):
			// Expected
		}
	})

	t.Run("OOM event", func(t *testing.T) {
		event := BPFContainerExitEvent{
			PID:         2345,
			ExitCode:    137,
			OOMKilled:   1,
			ContainerID: stringToInt8Array("oom-container"),
			Comm:        stringToInt8Array("memory-hog"),
		}

		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.LittleEndian, &event)
		require.NoError(t, err)

		observer.handleRingBufferEvent(buf.Bytes())

		// Check OOM event was sent
		select {
		case receivedEvent := <-eventCh:
			assert.NotNil(t, receivedEvent)
			assert.Equal(t, domain.EventTypeContainerOOM, receivedEvent.Type)
			assert.Equal(t, domain.EventSeverityCritical, receivedEvent.Severity)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("No OOM event received")
		}
	})
}

// TestUpdateEBPFMapsWithContainers tests eBPF map updates
func TestUpdateEBPFMapsWithContainers(t *testing.T) {
	config := NewDefaultConfig("test-maps")
	observer, err := NewObserver("test-maps", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Mock eBPF maps
	mockContainerMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}
	mockCgroupMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}

	// Set up mock eBPF state
	observer.ebpfState = &ebpfStateImpl{
		objs: &crimonitorObjects{
			ContainerMap: mockContainerMap,
			CgroupMap:    mockCgroupMap,
		},
	}

	containers := []Container{
		{ID: "container-1", PID: 1001, CgroupID: 10001},
		{ID: "container-2", PID: 1002, CgroupID: 10002},
		{ID: "container-3", PID: 1003, CgroupID: 10003},
	}

	err = observer.updateEBPFMapsWithContainers(containers)
	assert.NoError(t, err)

	// Verify maps were updated
	assert.Equal(t, 3, len(mockContainerMap.data))
	assert.Equal(t, 3, len(mockCgroupMap.data))

	// Verify cache was updated
	assert.Equal(t, 3, len(observer.containerCache))
	for _, container := range containers {
		cached, exists := observer.containerCache[container.ID]
		assert.True(t, exists)
		assert.Equal(t, container.ID, cached.ContainerID)
		assert.Equal(t, container.CgroupID, cached.CgroupID)
	}
}

// TestWatchContainerEvents tests container event watching
func TestWatchContainerEvents(t *testing.T) {
	config := NewDefaultConfig("test-watch")
	observer, err := NewObserver("test-watch", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Mock runtime client with events
	mockClient := &mockRuntimeClient{
		events: []ContainerEvent{
			{
				Type:      ContainerEventStart,
				Container: Container{ID: "new-container", PID: 5555},
			},
			{
				Type:      ContainerEventStop,
				Container: Container{ID: "old-container", PID: 6666},
			},
			{
				Type:      ContainerEventOOM,
				Container: Container{ID: "oom-container", PID: 7777},
			},
		},
	}
	observer.runtimeClient = mockClient

	// Mock eBPF state
	mockContainerMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}
	mockCgroupMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}
	observer.ebpfState = &ebpfStateImpl{
		objs: &crimonitorObjects{
			ContainerMap: mockContainerMap,
			CgroupMap:    mockCgroupMap,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start watching events
	go observer.watchContainerEvents(ctx)

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify start event added container to maps
	_, exists := mockContainerMap.data[uint32(5555)]
	assert.True(t, exists)

	// Context should cause watch to stop
	<-ctx.Done()
}

// TestHandleContainerEvent tests individual container event handling
func TestHandleContainerEvent(t *testing.T) {
	config := NewDefaultConfig("test-handle")
	observer, err := NewObserver("test-handle", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Mock eBPF state
	mockContainerMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}
	mockCgroupMap := &mockEBPFMap{
		data: make(map[interface{}]interface{}),
	}
	observer.ebpfState = &ebpfStateImpl{
		objs: &crimonitorObjects{
			ContainerMap: mockContainerMap,
			CgroupMap:    mockCgroupMap,
		},
	}

	t.Run("Container start event", func(t *testing.T) {
		event := ContainerEvent{
			Type: ContainerEventStart,
			Container: Container{
				ID:       "start-container",
				PID:      8888,
				CgroupID: 88888,
			},
		}

		observer.handleContainerEvent(event)

		// Check maps were updated
		_, exists := mockContainerMap.data[uint32(8888)]
		assert.True(t, exists)
		_, exists = mockCgroupMap.data[uint64(88888)]
		assert.True(t, exists)
	})

	t.Run("Container stop event", func(t *testing.T) {
		// First add a container
		mockContainerMap.data[uint32(9999)] = &BPFContainerMetadata{}
		mockCgroupMap.data[uint64(99999)] = &BPFContainerMetadata{}
		observer.containerCache["stop-container"] = &ContainerMetadata{
			ContainerID: "stop-container",
		}

		event := ContainerEvent{
			Type: ContainerEventStop,
			Container: Container{
				ID:       "stop-container",
				PID:      9999,
				CgroupID: 99999,
			},
		}

		observer.handleContainerEvent(event)

		// Check container was removed from maps
		_, exists := mockContainerMap.data[uint32(9999)]
		assert.False(t, exists)
		_, exists = mockCgroupMap.data[uint64(99999)]
		assert.False(t, exists)
		_, exists = observer.containerCache["stop-container"]
		assert.False(t, exists)
	})
}

// TestRemoveContainerFromMaps tests container removal from eBPF maps
func TestRemoveContainerFromMaps(t *testing.T) {
	config := NewDefaultConfig("test-remove")
	observer, err := NewObserver("test-remove", config)
	require.NoError(t, err)
	observer.logger = zap.NewNop()
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Mock eBPF maps with initial data
	mockContainerMap := &mockEBPFMap{
		data: map[interface{}]interface{}{
			uint32(3333): &BPFContainerMetadata{},
		},
	}
	mockCgroupMap := &mockEBPFMap{
		data: map[interface{}]interface{}{
			uint64(33333): &BPFContainerMetadata{},
		},
	}
	observer.ebpfState = &ebpfStateImpl{
		objs: &crimonitorObjects{
			ContainerMap: mockContainerMap,
			CgroupMap:    mockCgroupMap,
		},
	}

	// Add to cache
	observer.containerCache["remove-test"] = &ContainerMetadata{
		ContainerID: "remove-test",
	}

	container := Container{
		ID:       "remove-test",
		PID:      3333,
		CgroupID: 33333,
	}

	observer.removeContainerFromMaps(container)

	// Verify removed from all maps
	assert.Equal(t, 0, len(mockContainerMap.data))
	assert.Equal(t, 0, len(mockCgroupMap.data))
	assert.Equal(t, 0, len(observer.containerCache))
}

// Helper functions

func stringToInt8Array(s string) [64]int8 {
	var arr [64]int8
	for i, b := range []byte(s) {
		if i < 64 {
			arr[i] = int8(b)
		}
	}
	return arr
}

// mockEBPFMap is a mock implementation of ebpf.Map
type mockEBPFMap struct {
	data map[interface{}]interface{}
}

func (m *mockEBPFMap) Update(key, value interface{}, flags ebpf.MapUpdateFlags) error {
	// Extract the actual value from the pointer
	var keyVal interface{}
	switch k := key.(type) {
	case *uint32:
		keyVal = *k
	case *uint64:
		keyVal = *k
	default:
		keyVal = key
	}

	m.data[keyVal] = value
	return nil
}

func (m *mockEBPFMap) Delete(key interface{}) error {
	// Extract the actual value from the pointer
	var keyVal interface{}
	switch k := key.(type) {
	case *uint32:
		keyVal = *k
	case *uint64:
		keyVal = *k
	default:
		keyVal = key
	}

	delete(m.data, keyVal)
	return nil
}

func (m *mockEBPFMap) Lookup(key, value interface{}) error {
	return fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) LookupBytes(key interface{}) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) LookupAndDelete(key, value interface{}) error {
	return fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) LookupAndDeleteBytes(key interface{}) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) NextKey(key, nextKey interface{}) error {
	return fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) NextKeyBytes(key interface{}) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) Batch(keys, values interface{}, opts *ebpf.BatchOptions) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) Iterate() *ebpf.MapIterator {
	return nil
}

func (m *mockEBPFMap) Close() error {
	return nil
}

func (m *mockEBPFMap) FD() int {
	return 0
}

func (m *mockEBPFMap) Clone() (*ebpf.Map, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) Pin(path string) error {
	return nil
}

func (m *mockEBPFMap) Unpin() error {
	return nil
}

func (m *mockEBPFMap) IsPinned() bool {
	return false
}

func (m *mockEBPFMap) Freeze() error {
	return nil
}

func (m *mockEBPFMap) Info() (*ebpf.MapInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEBPFMap) Type() ebpf.MapType {
	return ebpf.Hash
}

func (m *mockEBPFMap) KeySize() uint32 {
	return 4
}

func (m *mockEBPFMap) ValueSize() uint32 {
	return uint32(unsafe.Sizeof(BPFContainerMetadata{}))
}

func (m *mockEBPFMap) MaxEntries() uint32 {
	return 1024
}

func (m *mockEBPFMap) Flags() uint32 {
	return 0
}

func (m *mockEBPFMap) Extra() *ebpf.MapExtra {
	return nil
}

func (m *mockEBPFMap) String() string {
	return "mockEBPFMap"
}
