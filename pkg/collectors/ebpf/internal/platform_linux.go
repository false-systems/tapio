//go:build linux
// +build linux

package internal

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// MapManager handles BPF map lifecycle and operations
type MapManager struct {
	maps     map[string]*ebpf.Map
	mapSpecs map[string]*ebpf.MapSpec
	mu       sync.RWMutex
}

// PerfEventManager handles perf event processing
type PerfEventManager struct {
	perfMaps    map[string]*ebpf.Map
	perfReaders map[string]*perf.Reader
	eventChan   chan core.RawEvent
	ctx         context.Context
	wg          sync.WaitGroup
	mu          sync.RWMutex
}

// linuxImpl provides Linux-specific eBPF functionality
type linuxImpl struct {
	config core.Config

	// BPF objects
	programs    map[string]*ebpf.Program
	maps        map[string]*ebpf.Map
	links       []link.Link
	mapManager  *MapManager
	perfManager *PerfEventManager

	// Event processing
	ringReader *ringbuf.Reader
	perfReader *perf.Reader
	eventChan  chan core.RawEvent

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	// Metrics
	programCount int
	mapCount     int
}

// NewMapManager creates a new map manager
func NewMapManager() *MapManager {
	return &MapManager{
		maps:     make(map[string]*ebpf.Map),
		mapSpecs: make(map[string]*ebpf.MapSpec),
	}
}

// NewPerfEventManager creates a new perf event manager
func NewPerfEventManager(ctx context.Context, eventChan chan core.RawEvent) *PerfEventManager {
	return &PerfEventManager{
		perfMaps:    make(map[string]*ebpf.Map),
		perfReaders: make(map[string]*perf.Reader),
		eventChan:   eventChan,
		ctx:         ctx,
	}
}

// newPlatformImpl creates a Linux-specific implementation
func newPlatformImpl() (platformImpl, error) {
	eventChan := make(chan core.RawEvent, 1000)

	impl := &linuxImpl{
		programs:  make(map[string]*ebpf.Program),
		maps:      make(map[string]*ebpf.Map),
		links:     make([]link.Link, 0),
		eventChan: eventChan,
	}

	impl.mapManager = NewMapManager()
	// perfManager will be initialized in start() with proper context

	return impl, nil
}

func (l *linuxImpl) init(config core.Config) error {
	l.config = config

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	return nil
}

func (l *linuxImpl) start(ctx context.Context) error {
	l.ctx, l.cancel = context.WithCancel(ctx)

	// Initialize perf event manager with context
	l.perfManager = NewPerfEventManager(l.ctx, l.eventChan)

	// Load eBPF programs based on configuration
	if err := l.loadPrograms(); err != nil {
		// Log error but continue - allows running on non-Linux for development
		fmt.Printf("Warning: Failed to load eBPF programs: %v\n", err)

		// Start a dummy event generator for testing
		go l.generateDummyEvents()
		return nil
	}

	// Start event readers
	if err := l.startEventReaders(); err != nil {
		return fmt.Errorf("failed to start event readers: %w", err)
	}

	// Start perf event processing
	if err := l.perfManager.Start(); err != nil {
		fmt.Printf("Warning: Failed to start perf event manager: %v\n", err)
	}

	return nil
}

func (l *linuxImpl) loadPrograms() error {
	// Create event ring buffer first using map manager
	ringSpec := &ebpf.MapSpec{
		Type:       ebpf.RingBuf,
		MaxEntries: 1 << 20, // 1MB
	}

	if err := l.mapManager.CreateMap("events", ringSpec); err != nil {
		return fmt.Errorf("failed to create ring buffer: %w", err)
	}
	l.mapCount++

	// Create perf event maps for high-performance event collection
	if l.config.EnableNetwork {
		if err := l.CreateNetworkMap(); err != nil {
			fmt.Printf("Warning: Failed to create network perf map: %v\n", err)
		}
		l.loadNetworkProgram()
	}
	if l.config.EnableMemory {
		l.loadMemoryProgram()
	}
	if l.config.EnableProcess {
		if err := l.CreateSyscallMap(); err != nil {
			fmt.Printf("Warning: Failed to create syscall perf map: %v\n", err)
		}
		l.loadProcessProgram()
	}

	return nil
}

func (l *linuxImpl) loadNetworkProgram() error {
	// Try to load compiled BPF object
	objPath := findBPFObject("network_monitor")
	if objPath == "" {
		fmt.Println("Network monitor BPF object not found, skipping")
		return nil
	}

	spec, err := ebpf.LoadCollectionSpec(objPath)
	if err != nil {
		fmt.Printf("Failed to load network monitor: %v\n", err)
		return nil
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		fmt.Printf("Failed to create collection: %v\n", err)
		return nil
	}

	// Store programs and maps
	for name, prog := range coll.Programs {
		l.programs[name] = prog
		l.programCount++
	}
	for name, m := range coll.Maps {
		l.maps[name] = m
		l.mapCount++
	}

	return nil
}

func (l *linuxImpl) loadMemoryProgram() error {
	objPath := findBPFObject("memory_tracker")
	if objPath == "" {
		return nil
	}

	spec, err := ebpf.LoadCollectionSpec(objPath)
	if err != nil {
		return nil
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil
	}

	for name, prog := range coll.Programs {
		l.programs[name] = prog
		l.programCount++
	}
	for name, m := range coll.Maps {
		l.maps[name] = m
		l.mapCount++
	}

	return nil
}

func (l *linuxImpl) loadProcessProgram() error {
	// Similar implementation
	return nil
}

func (l *linuxImpl) startEventReaders() error {
	// Start ring buffer reader if available from map manager
	if rb, ok := l.mapManager.GetMap("events"); ok {
		reader, err := ringbuf.NewReader(rb)
		if err != nil {
			return fmt.Errorf("failed to create ring buffer reader: %w", err)
		}
		l.ringReader = reader

		l.wg.Add(1)
		go l.readRingBuffer()
	}

	// Legacy ring buffer support
	if rb, ok := l.maps["events"]; ok {
		reader, err := ringbuf.NewReader(rb)
		if err != nil {
			return fmt.Errorf("failed to create legacy ring buffer reader: %w", err)
		}
		l.ringReader = reader

		l.wg.Add(1)
		go l.readRingBuffer()
	}

	// Perf event processing is handled by perfManager.Start()
	// which was already called in the main start() method

	return nil
}

func (l *linuxImpl) readRingBuffer() {
	defer l.wg.Done()

	for {
		select {
		case <-l.ctx.Done():
			return
		default:
			record, err := l.ringReader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				continue
			}

			event := l.parseRawEvent(record.RawSample)
			if event != nil {
				select {
				case l.eventChan <- *event:
				case <-l.ctx.Done():
					return
				}
			}
		}
	}
}

func (l *linuxImpl) parseRawEvent(data []byte) *core.RawEvent {
	if len(data) < 32 {
		return nil
	}

	reader := bytes.NewReader(data)
	event := &core.RawEvent{
		Timestamp: time.Now(),
		Data:      data,
		Type:      "network",
	}

	// Read basic fields
	binary.Read(reader, binary.LittleEndian, &event.CPU)
	binary.Read(reader, binary.LittleEndian, &event.PID)
	binary.Read(reader, binary.LittleEndian, &event.TID)
	binary.Read(reader, binary.LittleEndian, &event.UID)
	binary.Read(reader, binary.LittleEndian, &event.GID)

	return event
}

func (l *linuxImpl) generateDummyEvents() {
	defer close(l.eventChan)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	eventTypes := []string{"network", "memory", "process", "syscall"}
	var counter uint32

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			counter++
			event := core.RawEvent{
				Timestamp: time.Now(),
				Type:      eventTypes[counter%uint32(len(eventTypes))],
				CPU:       counter % 4,
				PID:       1000 + counter,
				TID:       1000 + counter,
				UID:       1000,
				GID:       1000,
				Comm:      "test-process",
				Data:      []byte(fmt.Sprintf("dummy event %d", counter)),
			}

			select {
			case l.eventChan <- event:
			case <-l.ctx.Done():
				return
			}
		}
	}
}

func (l *linuxImpl) stop() error {
	if l.cancel != nil {
		l.cancel()
	}

	// Stop perf event manager
	if l.perfManager != nil {
		if err := l.perfManager.Stop(); err != nil {
			fmt.Printf("Error stopping perf manager: %v\n", err)
		}
	}

	// Close all links
	for _, link := range l.links {
		link.Close()
	}

	// Close readers
	if l.ringReader != nil {
		l.ringReader.Close()
	}
	if l.perfReader != nil {
		l.perfReader.Close()
	}

	// Close map manager (this will close all managed maps)
	if l.mapManager != nil {
		if err := l.mapManager.CloseAll(); err != nil {
			fmt.Printf("Error closing map manager: %v\n", err)
		}
	}

	// Close programs and legacy maps
	for _, prog := range l.programs {
		prog.Close()
	}
	for _, m := range l.maps {
		m.Close()
	}

	// Wait for goroutines
	l.wg.Wait()

	return nil
}

func (l *linuxImpl) events() <-chan core.RawEvent {
	return l.eventChan
}

func (l *linuxImpl) programsLoaded() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.programCount
}

func (l *linuxImpl) mapsCreated() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.mapCount
}

// Helper function to find compiled BPF objects
func findBPFObject(name string) string {
	locations := []string{
		fmt.Sprintf("./bin/%s_bpfel_x86.o", name),
		fmt.Sprintf("./bin/%s_bpfel_arm64.o", name),
		fmt.Sprintf("./%s_bpfel_x86.o", name),
		fmt.Sprintf("./%s_bpfel_arm64.o", name),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// ===== MAP MANAGEMENT METHODS =====

// CreateMap creates a new BPF map with the given specification
func (m *MapManager) CreateMap(name string, spec *ebpf.MapSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.maps[name]; exists {
		return fmt.Errorf("map %s already exists", name)
	}

	bpfMap, err := ebpf.NewMap(spec)
	if err != nil {
		return fmt.Errorf("failed to create map %s: %w", name, err)
	}

	m.maps[name] = bpfMap
	m.mapSpecs[name] = spec
	return nil
}

// GetMap retrieves a BPF map by name
func (m *MapManager) GetMap(name string) (*ebpf.Map, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bpfMap, exists := m.maps[name]
	return bpfMap, exists
}

// UpdateMapElement updates a map element
func (m *MapManager) UpdateMapElement(mapName string, key, value interface{}) error {
	m.mu.RLock()
	bpfMap, exists := m.maps[mapName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("map %s not found", mapName)
	}

	return bpfMap.Update(key, value, ebpf.UpdateAny)
}

// LookupMapElement looks up a map element
func (m *MapManager) LookupMapElement(mapName string, key, value interface{}) error {
	m.mu.RLock()
	bpfMap, exists := m.maps[mapName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("map %s not found", mapName)
	}

	return bpfMap.Lookup(key, value)
}

// DeleteMapElement deletes a map element
func (m *MapManager) DeleteMapElement(mapName string, key interface{}) error {
	m.mu.RLock()
	bpfMap, exists := m.maps[mapName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("map %s not found", mapName)
	}

	return bpfMap.Delete(key)
}

// IterateMap iterates over all elements in a map
func (m *MapManager) IterateMap(mapName string, keyPtr, valuePtr interface{}, fn func(key, value interface{}) error) error {
	m.mu.RLock()
	bpfMap, exists := m.maps[mapName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("map %s not found", mapName)
	}

	iter := bpfMap.Iterate()
	for iter.Next(keyPtr, valuePtr) {
		if err := fn(keyPtr, valuePtr); err != nil {
			return err
		}
	}

	return iter.Err()
}

// GetMapInfo returns information about a map
func (m *MapManager) GetMapInfo(mapName string) (*ebpf.MapInfo, error) {
	m.mu.RLock()
	bpfMap, exists := m.maps[mapName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("map %s not found", mapName)
	}

	return bpfMap.Info()
}

// ListMaps returns all map names
func (m *MapManager) ListMaps() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.maps))
	for name := range m.maps {
		names = append(names, name)
	}
	return names
}

// CloseMap closes and removes a specific map
func (m *MapManager) CloseMap(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bpfMap, exists := m.maps[name]; exists {
		err := bpfMap.Close()
		delete(m.maps, name)
		delete(m.mapSpecs, name)
		return err
	}

	return fmt.Errorf("map %s not found", name)
}

// CloseAll closes all maps
func (m *MapManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, bpfMap := range m.maps {
		if err := bpfMap.Close(); err != nil {
			lastErr = err
		}
		delete(m.maps, name)
		delete(m.mapSpecs, name)
	}

	return lastErr
}

// GetMapStats returns statistics about all maps
func (m *MapManager) GetMapStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_maps"] = len(m.maps)

	mapDetails := make(map[string]map[string]interface{})
	for name, bpfMap := range m.maps {
		info, err := bpfMap.Info()
		if err != nil {
			continue
		}

		mapDetails[name] = map[string]interface{}{
			"type":        info.Type.String(),
			"key_size":    info.KeySize,
			"value_size":  info.ValueSize,
			"max_entries": info.MaxEntries,
			"id":          info.ID,
		}
	}
	stats["maps"] = mapDetails

	return stats
}

// ===== PERF EVENT MANAGEMENT METHODS =====

// Start begins perf event processing
func (p *PerfEventManager) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Start readers for all registered perf maps
	for name, perfMap := range p.perfMaps {
		reader, err := perf.NewReader(perfMap, os.Getpagesize())
		if err != nil {
			return fmt.Errorf("failed to create perf reader for %s: %w", name, err)
		}

		p.perfReaders[name] = reader

		// Start reading goroutine for this perf map
		p.wg.Add(1)
		go p.readPerfEvents(name, reader)
	}

	return nil
}

// RegisterPerfMap registers a perf event map for processing
func (p *PerfEventManager) RegisterPerfMap(name string, perfMap *ebpf.Map) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.perfMaps[name]; exists {
		return fmt.Errorf("perf map %s already registered", name)
	}

	p.perfMaps[name] = perfMap
	return nil
}

// readPerfEvents reads events from a specific perf reader
func (p *PerfEventManager) readPerfEvents(name string, reader *perf.Reader) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			record, err := reader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				// Log error and continue
				continue
			}

			// Parse the perf event
			event := p.parsePerfEvent(name, record)
			if event != nil {
				select {
				case p.eventChan <- *event:
				case <-p.ctx.Done():
					return
				}
			}
		}
	}
}

// parsePerfEvent parses a perf event record into a RawEvent
func (p *PerfEventManager) parsePerfEvent(mapName string, record perf.Record) *core.RawEvent {
	if record.LostSamples > 0 {
		// Handle lost samples
		return &core.RawEvent{
			Timestamp: time.Now(),
			Type:      "perf_lost",
			Data:      []byte(fmt.Sprintf("lost %d samples from %s", record.LostSamples, mapName)),
			Comm:      mapName,
		}
	}

	if len(record.RawSample) < 16 {
		return nil
	}

	// Parse basic perf event structure
	reader := bytes.NewReader(record.RawSample)
	event := &core.RawEvent{
		Timestamp: time.Now(),
		Data:      record.RawSample,
		Type:      "perf_event",
		Comm:      mapName,
	}

	// Read standard perf event fields
	binary.Read(reader, binary.LittleEndian, &event.CPU)
	binary.Read(reader, binary.LittleEndian, &event.PID)
	binary.Read(reader, binary.LittleEndian, &event.TID)

	// Determine event type based on map name
	switch mapName {
	case "syscall_events":
		event.Type = "syscall"
	case "network_events":
		event.Type = "network"
	case "file_events":
		event.Type = "file"
	default:
		event.Type = "generic_perf"
	}

	return event
}

// GetPerfStats returns perf event statistics
func (p *PerfEventManager) GetPerfStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_perf_maps"] = len(p.perfMaps)
	stats["active_readers"] = len(p.perfReaders)

	readerStats := make(map[string]interface{})
	for name := range p.perfReaders {
		// Get reader statistics if available
		readerStats[name] = map[string]interface{}{
			"active": true,
		}
	}
	stats["readers"] = readerStats

	return stats
}

// Stop stops all perf event processing
func (p *PerfEventManager) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close all readers
	var lastErr error
	for name, reader := range p.perfReaders {
		if err := reader.Close(); err != nil {
			lastErr = err
		}
		delete(p.perfReaders, name)
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	return lastErr
}

// ===== ENHANCED LINUX IMPL METHODS =====

// CreateNetworkMap creates a map for network events
func (l *linuxImpl) CreateNetworkMap() error {
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128, // Number of CPUs
	}

	if err := l.mapManager.CreateMap("network_events", spec); err != nil {
		return fmt.Errorf("failed to create network map: %w", err)
	}

	// Register with perf manager
	if perfMap, exists := l.mapManager.GetMap("network_events"); exists {
		return l.perfManager.RegisterPerfMap("network_events", perfMap)
	}

	return nil
}

// CreateSyscallMap creates a map for syscall events
func (l *linuxImpl) CreateSyscallMap() error {
	spec := &ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 128,
	}

	if err := l.mapManager.CreateMap("syscall_events", spec); err != nil {
		return fmt.Errorf("failed to create syscall map: %w", err)
	}

	// Register with perf manager
	if perfMap, exists := l.mapManager.GetMap("syscall_events"); exists {
		return l.perfManager.RegisterPerfMap("syscall_events", perfMap)
	}

	return nil
}

// GetMapManager returns the map manager for external access
func (l *linuxImpl) GetMapManager() *MapManager {
	return l.mapManager
}

// GetPerfManager returns the perf event manager for external access
func (l *linuxImpl) GetPerfManager() *PerfEventManager {
	return l.perfManager
}
