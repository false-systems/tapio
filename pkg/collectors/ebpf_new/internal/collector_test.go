package internal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Helper functions
func int32Ptr(v int32) *int32 {
	return &v
}

// Mock implementations for testing

type mockProgramLoader struct {
	mu       sync.Mutex
	programs map[string]core.Program
	loadErr  error
	unloadErr error
}

func newMockProgramLoader() *mockProgramLoader {
	return &mockProgramLoader{
		programs: make(map[string]core.Program),
	}
}

func (m *mockProgramLoader) Load(ctx context.Context, spec core.ProgramSpec) (core.Program, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.loadErr != nil {
		return core.Program{}, m.loadErr
	}

	program := core.Program{
		ID:           uint32(len(m.programs) + 1),
		Name:         spec.Name,
		Type:         spec.Type,
		AttachTarget: spec.AttachTarget,
		LoadTime:     time.Now(),
	}

	m.programs[spec.Name] = program
	return program, nil
}

func (m *mockProgramLoader) Unload(program core.Program) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.unloadErr != nil {
		return m.unloadErr
	}

	delete(m.programs, program.Name)
	return nil
}

func (m *mockProgramLoader) List() ([]core.Program, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	programs := make([]core.Program, 0, len(m.programs))
	for _, p := range m.programs {
		programs = append(programs, p)
	}
	return programs, nil
}

type mockEventParser struct {
	parseErr error
}

func (m *mockEventParser) Parse(data []byte, eventType core.EventType) (domain.Event, error) {
	if m.parseErr != nil {
		return domain.Event{}, m.parseErr
	}

	return domain.Event{
		ID:        "test-event-1",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Severity:  domain.SeverityInfo,
		Payload: domain.SystemEventPayload{
			Syscall:    "test_syscall",
			ReturnCode: 0,
			Arguments: map[string]string{
				"test": "value",
			},
		},
		Context: domain.EventContext{
			PID: int32Ptr(1234),
			Labels: domain.Labels{
				"process_name": "test-process",
			},
		},
	}, nil
}

func (m *mockEventParser) CanParse(eventType core.EventType) bool {
	return true
}

type mockMapManager struct {
	mu   sync.Mutex
	maps map[string]core.Map
}

func newMockMapManager() *mockMapManager {
	return &mockMapManager{
		maps: make(map[string]core.Map),
	}
}

func (m *mockMapManager) CreateMap(spec core.MapSpec) (core.Map, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mockMap := &mockMap{name: spec.Name}
	m.maps[spec.Name] = mockMap
	return mockMap, nil
}

func (m *mockMapManager) GetMap(name string) (core.Map, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mp, ok := m.maps[name]; ok {
		return mp, nil
	}
	return nil, errors.New("map not found")
}

func (m *mockMapManager) DeleteMap(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.maps, name)
	return nil
}

func (m *mockMapManager) ListMaps() ([]core.MapInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	infos := make([]core.MapInfo, 0, len(m.maps))
	for name := range m.maps {
		infos = append(infos, core.MapInfo{Name: name})
	}
	return infos, nil
}

type mockMap struct {
	name string
}

func (m *mockMap) Lookup(key []byte) ([]byte, error) {
	return []byte("value"), nil
}

func (m *mockMap) Update(key, value []byte) error {
	return nil
}

func (m *mockMap) Delete(key []byte) error {
	return nil
}

func (m *mockMap) Iterate(fn func(key, value []byte) error) error {
	return nil
}

func (m *mockMap) Close() error {
	return nil
}

type mockRingBufferReader struct {
	events [][]byte
	index  int
	closed bool
	mu     sync.Mutex
}

func (m *mockRingBufferReader) Read() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("reader closed")
	}

	if m.index >= len(m.events) {
		return nil, errors.New("no more events")
	}

	event := m.events[m.index]
	m.index++
	return event, nil
}

func (m *mockRingBufferReader) ReadBatch(maxEvents int) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.New("reader closed")
	}

	var batch [][]byte
	for i := 0; i < maxEvents && m.index < len(m.events); i++ {
		batch = append(batch, m.events[m.index])
		m.index++
	}

	return batch, nil
}

func (m *mockRingBufferReader) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// Tests

func TestNewCollector(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	if collector == nil {
		t.Fatal("NewCollector() returned nil collector")
	}

	// Test with invalid config
	invalidConfig := core.Config{}
	_, err = NewCollector(invalidConfig, loader, parser, manager)
	if err == nil {
		t.Error("NewCollector() with invalid config should return error")
	}
}

func TestCollectorSubscribe(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now().Add(-time.Hour),
			End:   time.Now().Add(time.Hour),
		},
	}
	options := domain.SubscriptionOptions{
		BufferSize: 100,
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if eventChan == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	// Test invalid criteria
	invalidCriteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(-time.Hour), // End before start
		},
	}

	_, err = collector.Subscribe(ctx, invalidCriteria, options)
	if err == nil {
		t.Error("Subscribe() with invalid criteria should return error")
	}
}

func TestCollectorQuery(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now().Add(-time.Hour),
			End:   time.Now().Add(time.Hour),
		},
	}

	events, err := collector.Query(ctx, criteria)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	// Since we're using a mock that doesn't generate events,
	// we expect an empty result
	if events == nil {
		t.Error("Query() returned nil events slice")
	}
}

func TestCollectorLoadPrograms(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test_prog1",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func1",
			Code:         []byte{1},
		},
		{
			Name:         "test_prog2",
			Type:         core.ProgramTypeTracepoint,
			AttachTarget: "test_func2",
			Code:         []byte{2},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	err = collector.LoadPrograms(ctx)
	if err != nil {
		t.Fatalf("LoadPrograms() error = %v", err)
	}

	// Verify programs were loaded
	programs, err := collector.GetLoadedPrograms()
	if err != nil {
		t.Fatalf("GetLoadedPrograms() error = %v", err)
	}

	if len(programs) != 2 {
		t.Errorf("GetLoadedPrograms() returned %d programs, want 2", len(programs))
	}

	// Test loading with error
	loader.loadErr = errors.New("load failed")
	collector2, _ := NewCollector(config, loader, parser, manager)
	err = collector2.LoadPrograms(ctx)
	if err == nil {
		t.Error("LoadPrograms() should return error when loader fails")
	}
}

func TestCollectorUnloadPrograms(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test_prog",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	ctx := context.Background()
	
	// Load programs first
	err = collector.LoadPrograms(ctx)
	if err != nil {
		t.Fatalf("LoadPrograms() error = %v", err)
	}

	// Unload programs
	err = collector.UnloadPrograms()
	if err != nil {
		t.Fatalf("UnloadPrograms() error = %v", err)
	}

	// Verify programs were unloaded
	programs, err := collector.GetLoadedPrograms()
	if err != nil {
		t.Fatalf("GetLoadedPrograms() error = %v", err)
	}

	if len(programs) != 0 {
		t.Errorf("GetLoadedPrograms() returned %d programs after unload, want 0", len(programs))
	}
}

func TestCollectorSetFilter(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	filter := core.Filter{
		EventTypes: []core.EventType{core.EventTypeSyscall},
		ProcessIDs: []uint32{1234, 5678},
		MinSeverity: domain.SeverityMedium,
	}

	err = collector.SetFilter(filter)
	if err != nil {
		t.Fatalf("SetFilter() error = %v", err)
	}
}

func TestCollectorGetStats(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	stats, err := collector.GetStats()
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	// Verify initial stats
	if stats.EventsCollected != 0 {
		t.Errorf("GetStats() EventsCollected = %d, want 0", stats.EventsCollected)
	}

	if stats.StartTime.IsZero() {
		t.Error("GetStats() StartTime should not be zero")
	}
}

func TestCollectorHealth(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	health := collector.Health()

	// Verify initial health
	if health.Status != core.HealthStatusHealthy {
		t.Errorf("Health() Status = %v, want %v", health.Status, core.HealthStatusHealthy)
	}

	if health.Message == "" {
		t.Error("Health() Message should not be empty")
	}

	if health.LastCheck.IsZero() {
		t.Error("Health() LastCheck should not be zero")
	}
}

func TestCollectorStartStop(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	defer collector.Close()

	ctx := context.Background()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Try to start again - should fail
	err = collector.Start(ctx)
	if err == nil {
		t.Error("Start() should return error when already started")
	}

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Try to stop again - should fail
	err = collector.Stop()
	if err == nil {
		t.Error("Stop() should return error when not started")
	}
}

func TestCollectorClose(t *testing.T) {
	config := core.DefaultConfig()
	config.Programs = []core.ProgramSpec{
		{
			Name:         "test",
			Type:         core.ProgramTypeKprobe,
			AttachTarget: "test_func",
			Code:         []byte{1},
		},
	}

	loader := newMockProgramLoader()
	parser := &mockEventParser{}
	manager := newMockMapManager()

	collector, err := NewCollector(config, loader, parser, manager)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Close collector
	err = collector.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Close again - should be idempotent
	err = collector.Close()
	if err != nil {
		t.Error("Close() should be idempotent")
	}

	// Operations after close should fail
	ctx := context.Background()
	_, err = collector.Subscribe(ctx, domain.QueryCriteria{}, domain.SubscriptionOptions{})
	if err == nil {
		t.Error("Subscribe() should return error after Close()")
	}
}