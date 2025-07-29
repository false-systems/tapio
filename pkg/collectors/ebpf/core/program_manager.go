package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/ringbuf"
)

// ProgramManager handles dynamic loading/unloading of BPF programs
type ProgramManager struct {
	loader   *BPFLoader
	programs map[string]*ManagedProgram
	mu       sync.RWMutex
	
	// Event handling
	eventHandlers map[string]EventHandler
	readers       map[string]EventReader
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ManagedProgram represents a dynamically managed BPF program
type ManagedProgram struct {
	Name        string
	Type        string
	Spec        *ebpf.CollectionSpec
	Collection  *ebpf.Collection
	Attachments []Attachment
	Maps        map[string]*ebpf.Map
	State       ProgramState
	LoadedAt    time.Time
	Statistics  *ProgramStats
	Config      *ProgramConfig
}

// Attachment represents how a program is attached to the kernel
type Attachment struct {
	Type        AttachType
	Point       string
	Priority    int
	Attached    bool
	AttachedAt  time.Time
}

// ProgramState represents the current state of a program
type ProgramState int

const (
	StateUnloaded ProgramState = iota
	StateLoading
	StateLoaded
	StateAttaching
	StateAttached
	StateDetaching
	StateError
)

// ProgramConfig contains configuration for a BPF program
type ProgramConfig struct {
	// Loading options
	AutoAttach       bool              `json:"auto_attach"`
	AttachPoints     []AttachmentSpec  `json:"attach_points"`
	
	// Resource limits
	MaxMemory        uint64            `json:"max_memory"`
	MaxMaps          int               `json:"max_maps"`
	
	// Event handling
	EventBufferSize  int               `json:"event_buffer_size"`
	EventHandler     string            `json:"event_handler"`
	
	// Map configuration
	MapConfigs       map[string]MapConfig `json:"map_configs"`
	
	// Tail call configuration
	TailCalls        map[int32]string  `json:"tail_calls"`
}

// AttachmentSpec specifies how to attach a program
type AttachmentSpec struct {
	Type     string `json:"type"`
	Point    string `json:"point"`
	Priority int    `json:"priority"`
}

// MapConfig contains configuration for a BPF map
type MapConfig struct {
	Persist     bool   `json:"persist"`
	PinPath     string `json:"pin_path"`
	MaxEntries  uint32 `json:"max_entries"`
}

// EventHandler processes events from BPF programs
type EventHandler interface {
	HandleEvent(event []byte) error
	Close() error
}

// EventReader reads events from BPF maps
type EventReader interface {
	Read() ([]byte, error)
	Close() error
}

// NewProgramManager creates a new BPF program manager
func NewProgramManager(loader *BPFLoader) *ProgramManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ProgramManager{
		loader:        loader,
		programs:      make(map[string]*ManagedProgram),
		eventHandlers: make(map[string]EventHandler),
		readers:       make(map[string]EventReader),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// LoadProgram dynamically loads a BPF program
func (pm *ProgramManager) LoadProgram(name string, elfPath string, config *ProgramConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	// Check if already loaded
	if _, exists := pm.programs[name]; exists {
		return fmt.Errorf("program %s already loaded", name)
	}
	
	// Create managed program
	managed := &ManagedProgram{
		Name:        name,
		Maps:        make(map[string]*ebpf.Map),
		State:       StateLoading,
		LoadedAt:    time.Now(),
		Config:      config,
		Attachments: make([]Attachment, 0),
	}
	
	// Load program specification
	spec, err := pm.loader.LoadProgramSpec(elfPath)
	if err != nil {
		managed.State = StateError
		return fmt.Errorf("failed to load program spec: %w", err)
	}
	managed.Spec = spec
	
	// Apply map configurations
	for mapName, mapConfig := range config.MapConfigs {
		if mapSpec, exists := spec.Maps[mapName]; exists {
			if mapConfig.MaxEntries > 0 {
				mapSpec.MaxEntries = mapConfig.MaxEntries
			}
		}
	}
	
	// Load collection
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		managed.State = StateError
		return fmt.Errorf("failed to load collection: %w", err)
	}
	managed.Collection = coll
	
	// Setup maps
	for mapName, m := range coll.Maps {
		// Handle map persistence
		if mapConfig, exists := config.MapConfigs[mapName]; exists && mapConfig.Persist {
			// Map will be persisted by loader
		}
		managed.Maps[mapName] = m
	}
	
	// Setup tail calls if configured
	if len(config.TailCalls) > 0 {
		if err := pm.setupTailCalls(managed); err != nil {
			coll.Close()
			return fmt.Errorf("failed to setup tail calls: %w", err)
		}
	}
	
	// Setup event handling
	if config.EventHandler != "" {
		if err := pm.setupEventHandling(managed); err != nil {
			coll.Close()
			return fmt.Errorf("failed to setup event handling: %w", err)
		}
	}
	
	managed.State = StateLoaded
	pm.programs[name] = managed
	
	// Auto-attach if configured
	if config.AutoAttach {
		if err := pm.AttachProgram(name); err != nil {
			pm.UnloadProgram(name)
			return fmt.Errorf("failed to auto-attach: %w", err)
		}
	}
	
	return nil
}

// UnloadProgram dynamically unloads a BPF program
func (pm *ProgramManager) UnloadProgram(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	managed, exists := pm.programs[name]
	if !exists {
		return fmt.Errorf("program %s not found", name)
	}
	
	// Detach if attached
	if managed.State == StateAttached {
		if err := pm.detachProgram(managed); err != nil {
			return fmt.Errorf("failed to detach: %w", err)
		}
	}
	
	// Close event handling
	if reader, exists := pm.readers[name]; exists {
		reader.Close()
		delete(pm.readers, name)
	}
	
	if handler, exists := pm.eventHandlers[name]; exists {
		handler.Close()
		delete(pm.eventHandlers, name)
	}
	
	// Close collection
	if managed.Collection != nil {
		managed.Collection.Close()
	}
	
	managed.State = StateUnloaded
	delete(pm.programs, name)
	
	return nil
}

// AttachProgram attaches a loaded program to kernel hooks
func (pm *ProgramManager) AttachProgram(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	managed, exists := pm.programs[name]
	if !exists {
		return fmt.Errorf("program %s not found", name)
	}
	
	if managed.State != StateLoaded {
		return fmt.Errorf("program %s not in loaded state", name)
	}
	
	managed.State = StateAttaching
	
	// Attach based on configuration
	for _, attachSpec := range managed.Config.AttachPoints {
		attachType, err := parseAttachType(attachSpec.Type)
		if err != nil {
			managed.State = StateError
			return err
		}
		
		// Find the program in collection
		var prog *ebpf.Program
		for _, p := range managed.Collection.Programs {
			// Match by type or name
			prog = p
			break
		}
		
		if prog == nil {
			managed.State = StateError
			return fmt.Errorf("no program found for attachment")
		}
		
		// Use loader to attach
		if err := pm.loader.AttachProgram(name, attachType, attachSpec.Point); err != nil {
			managed.State = StateError
			return fmt.Errorf("failed to attach to %s: %w", attachSpec.Point, err)
		}
		
		managed.Attachments = append(managed.Attachments, Attachment{
			Type:       attachType,
			Point:      attachSpec.Point,
			Priority:   attachSpec.Priority,
			Attached:   true,
			AttachedAt: time.Now(),
		})
	}
	
	managed.State = StateAttached
	return nil
}

// DetachProgram detaches a program from kernel hooks
func (pm *ProgramManager) DetachProgram(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	managed, exists := pm.programs[name]
	if !exists {
		return fmt.Errorf("program %s not found", name)
	}
	
	return pm.detachProgram(managed)
}

// GetProgramState returns the current state of a program
func (pm *ProgramManager) GetProgramState(name string) (ProgramState, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	managed, exists := pm.programs[name]
	if !exists {
		return StateUnloaded, fmt.Errorf("program %s not found", name)
	}
	
	return managed.State, nil
}

// GetProgramStats returns statistics for a program
func (pm *ProgramManager) GetProgramStats(name string) (*ProgramStats, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	managed, exists := pm.programs[name]
	if !exists {
		return nil, fmt.Errorf("program %s not found", name)
	}
	
	// Get fresh stats from loader
	stats, err := pm.loader.GetProgramStats(name)
	if err != nil {
		return managed.Statistics, err
	}
	
	managed.Statistics = stats
	return stats, nil
}

// ListPrograms returns a list of all managed programs
func (pm *ProgramManager) ListPrograms() []ProgramInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	programs := make([]ProgramInfo, 0, len(pm.programs))
	for name, managed := range pm.programs {
		info := ProgramInfo{
			Name:        name,
			Type:        managed.Type,
			State:       managed.State.String(),
			LoadedAt:    managed.LoadedAt,
			Attachments: len(managed.Attachments),
			Maps:        len(managed.Maps),
		}
		programs = append(programs, info)
	}
	
	return programs
}

// ReloadProgram reloads a program with new configuration
func (pm *ProgramManager) ReloadProgram(name string, elfPath string, config *ProgramConfig) error {
	// Unload existing program
	if err := pm.UnloadProgram(name); err != nil {
		return fmt.Errorf("failed to unload existing program: %w", err)
	}
	
	// Load new version
	if err := pm.LoadProgram(name, elfPath, config); err != nil {
		return fmt.Errorf("failed to load new program: %w", err)
	}
	
	return nil
}

// Start begins the program manager
func (pm *ProgramManager) Start() error {
	// Start event processing for attached programs
	pm.mu.RLock()
	for name, managed := range pm.programs {
		if managed.State == StateAttached && managed.Config.EventHandler != "" {
			pm.wg.Add(1)
			go pm.processEvents(name)
		}
	}
	pm.mu.RUnlock()
	
	return nil
}

// Stop stops the program manager
func (pm *ProgramManager) Stop() error {
	pm.cancel()
	pm.wg.Wait()
	
	// Unload all programs
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	for name := range pm.programs {
		pm.UnloadProgram(name)
	}
	
	return nil
}

// Helper methods

func (pm *ProgramManager) setupTailCalls(managed *ManagedProgram) error {
	// Setup tail call map
	progArrayName := managed.Name + "_progs"
	
	// Find program array map
	progArray, exists := managed.Maps[progArrayName]
	if !exists {
		// Try to find any program array map
		for mapName, m := range managed.Maps {
			info, _ := m.Info()
			if info != nil && info.Type == ebpf.ProgramArray {
				progArray = m
				progArrayName = mapName
				break
			}
		}
	}
	
	if progArray == nil {
		return fmt.Errorf("no program array map found for tail calls")
	}
	
	// Populate tail call map
	for idx, progName := range managed.Config.TailCalls {
		prog, exists := managed.Collection.Programs[progName]
		if !exists {
			return fmt.Errorf("tail call program %s not found", progName)
		}
		
		if err := progArray.Put(idx, prog); err != nil {
			return fmt.Errorf("failed to setup tail call %d->%s: %w", idx, progName, err)
		}
	}
	
	return nil
}

func (pm *ProgramManager) setupEventHandling(managed *ManagedProgram) error {
	// Find event map (ring buffer or perf array)
	var eventMap *ebpf.Map
	var mapType ebpf.MapType
	
	for mapName, m := range managed.Maps {
		info, _ := m.Info()
		if info != nil {
			if info.Type == ebpf.RingBuf || info.Type == ebpf.PerfEventArray {
				eventMap = m
				mapType = info.Type
				break
			}
		}
	}
	
	if eventMap == nil {
		return fmt.Errorf("no event map found")
	}
	
	// Create appropriate reader
	var reader EventReader
	var err error
	
	switch mapType {
	case ebpf.RingBuf:
		rd, err := ringbuf.NewReader(eventMap)
		if err != nil {
			return fmt.Errorf("failed to create ring buffer reader: %w", err)
		}
		reader = &ringBufReader{rd}
		
	case ebpf.PerfEventArray:
		rd, err := perf.NewReader(eventMap, managed.Config.EventBufferSize)
		if err != nil {
			return fmt.Errorf("failed to create perf reader: %w", err)
		}
		reader = &perfReader{rd}
		
	default:
		return fmt.Errorf("unsupported event map type: %v", mapType)
	}
	
	pm.readers[managed.Name] = reader
	
	// TODO: Create event handler based on config
	// For now, we'll use a dummy handler
	pm.eventHandlers[managed.Name] = &dummyEventHandler{}
	
	return nil
}

func (pm *ProgramManager) processEvents(name string) {
	defer pm.wg.Done()
	
	reader, exists := pm.readers[name]
	if !exists {
		return
	}
	
	handler, exists := pm.eventHandlers[name]
	if !exists {
		return
	}
	
	for {
		select {
		case <-pm.ctx.Done():
			return
		default:
			event, err := reader.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return
				}
				continue
			}
			
			if err := handler.HandleEvent(event); err != nil {
				// Log error
			}
		}
	}
}

func (pm *ProgramManager) detachProgram(managed *ManagedProgram) error {
	managed.State = StateDetaching
	
	// Detach all attachments
	for i := range managed.Attachments {
		managed.Attachments[i].Attached = false
	}
	
	managed.State = StateLoaded
	return nil
}

func parseAttachType(typeStr string) (AttachType, error) {
	switch typeStr {
	case "kprobe":
		return AttachTypeKprobe, nil
	case "kretprobe":
		return AttachTypeKretprobe, nil
	case "tracepoint":
		return AttachTypeTracepoint, nil
	case "raw_tracepoint":
		return AttachTypeRawTracepoint, nil
	case "xdp":
		return AttachTypeXDP, nil
	default:
		return 0, fmt.Errorf("unknown attach type: %s", typeStr)
	}
}

// ProgramInfo contains information about a managed program
type ProgramInfo struct {
	Name        string
	Type        string
	State       string
	LoadedAt    time.Time
	Attachments int
	Maps        int
}

// String returns string representation of ProgramState
func (ps ProgramState) String() string {
	switch ps {
	case StateUnloaded:
		return "unloaded"
	case StateLoading:
		return "loading"
	case StateLoaded:
		return "loaded"
	case StateAttaching:
		return "attaching"
	case StateAttached:
		return "attached"
	case StateDetaching:
		return "detaching"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// Reader implementations

type ringBufReader struct {
	rd *ringbuf.Reader
}

func (r *ringBufReader) Read() ([]byte, error) {
	record, err := r.rd.Read()
	if err != nil {
		return nil, err
	}
	return record.RawSample, nil
}

func (r *ringBufReader) Close() error {
	return r.rd.Close()
}

type perfReader struct {
	rd *perf.Reader
}

func (r *perfReader) Read() ([]byte, error) {
	record, err := r.rd.Read()
	if err != nil {
		return nil, err
	}
	return record.RawSample, nil
}

func (r *perfReader) Close() error {
	return r.rd.Close()
}

type dummyEventHandler struct{}

func (d *dummyEventHandler) HandleEvent(event []byte) error {
	// Process event
	return nil
}

func (d *dummyEventHandler) Close() error {
	return nil
}