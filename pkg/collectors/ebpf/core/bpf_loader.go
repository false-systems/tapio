package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

// BPFLoader handles CO-RE based BPF program loading with BTF support
type BPFLoader struct {
	// BTF information
	kernelBTF *btf.Spec
	
	// Loaded programs and maps
	programs map[string]*BPFProgram
	maps     map[string]*ebpf.Map
	
	// Program management
	mu            sync.RWMutex
	collectionOpts *ebpf.CollectionOptions
	
	// Map persistence
	pinnedPath    string
	persistMaps   bool
	
	// Tail call support
	tailCallMaps  map[string]*ebpf.Map
}

// BPFProgram represents a loaded BPF program with its metadata
type BPFProgram struct {
	Name       string
	Type       ebpf.ProgramType
	Program    *ebpf.Program
	Link       link.Link
	TailCalls  []string // Programs this can tail call to
	Statistics *ebpf.ProgramInfo
}

// BPFLoaderConfig contains configuration for the BPF loader
type BPFLoaderConfig struct {
	// BTF options
	UseKernelBTF   bool   `json:"use_kernel_btf"`
	BTFPath        string `json:"btf_path"` // Custom BTF file path
	
	// Map persistence
	EnablePersistence bool   `json:"enable_persistence"`
	PinnedPath       string `json:"pinned_path"` // Path for pinned maps
	
	// Loading options
	VerifierLogLevel uint32 `json:"verifier_log_level"`
	VerifierLogSize  int    `json:"verifier_log_size"`
	
	// CO-RE options
	EnableCORE       bool     `json:"enable_core"`
	TargetKernels    []string `json:"target_kernels"` // Kernel versions to support
}

// NewBPFLoader creates a new CO-RE enabled BPF loader
func NewBPFLoader(config *BPFLoaderConfig) (*BPFLoader, error) {
	// Remove memory limit for BPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock limit: %w", err)
	}

	loader := &BPFLoader{
		programs:     make(map[string]*BPFProgram),
		maps:         make(map[string]*ebpf.Map),
		tailCallMaps: make(map[string]*ebpf.Map),
		persistMaps:  config.EnablePersistence,
		pinnedPath:   config.PinnedPath,
	}

	// Load kernel BTF if requested
	if config.UseKernelBTF {
		kernelBTF, err := btf.LoadKernelSpec()
		if err != nil {
			// Try custom BTF path
			if config.BTFPath != "" {
				kernelBTF, err = btf.LoadSpec(config.BTFPath)
				if err != nil {
					return nil, fmt.Errorf("failed to load BTF from %s: %w", config.BTFPath, err)
				}
			} else {
				return nil, fmt.Errorf("failed to load kernel BTF: %w", err)
			}
		}
		loader.kernelBTF = kernelBTF
	}

	// Set up collection options for CO-RE
	loader.collectionOpts = &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevel(config.VerifierLogLevel),
			LogSize:  config.VerifierLogSize,
		},
	}

	// Create pinned path if persistence is enabled
	if config.EnablePersistence && config.PinnedPath != "" {
		if err := os.MkdirAll(config.PinnedPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create pinned path: %w", err)
		}
	}

	return loader, nil
}

// LoadProgramSpec loads a BPF program specification with CO-RE support
func (l *BPFLoader) LoadProgramSpec(path string) (*ebpf.CollectionSpec, error) {
	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load BPF spec from %s: %w", path, err)
	}

	// Apply BTF relocations if kernel BTF is available
	if l.kernelBTF != nil {
		err = spec.RewriteConstants(map[string]interface{}{
			"KERNEL_BTF": true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to rewrite constants: %w", err)
		}

		// Apply CO-RE relocations
		for _, prog := range spec.Programs {
			if prog.BTF != nil {
				// CO-RE is automatically handled by cilium/ebpf
				prog.BTF = l.kernelBTF
			}
		}
	}

	return spec, nil
}

// LoadProgram loads a single BPF program with dynamic loading support
func (l *BPFLoader) LoadProgram(name string, spec *ebpf.ProgramSpec) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if already loaded
	if _, exists := l.programs[name]; exists {
		return fmt.Errorf("program %s already loaded", name)
	}

	// Load the program
	prog, err := ebpf.NewProgramWithOptions(spec, l.collectionOpts.Programs)
	if err != nil {
		return fmt.Errorf("failed to load program %s: %w", name, err)
	}

	// Store program metadata
	info, err := prog.Info()
	if err == nil {
		l.programs[name] = &BPFProgram{
			Name:       name,
			Type:       prog.Type(),
			Program:    prog,
			Statistics: info,
		}
	} else {
		l.programs[name] = &BPFProgram{
			Name:    name,
			Type:    prog.Type(),
			Program: prog,
		}
	}

	return nil
}

// UnloadProgram dynamically unloads a BPF program
func (l *BPFLoader) UnloadProgram(name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	prog, exists := l.programs[name]
	if !exists {
		return fmt.Errorf("program %s not found", name)
	}

	// Detach link if exists
	if prog.Link != nil {
		if err := prog.Link.Close(); err != nil {
			return fmt.Errorf("failed to close link for %s: %w", name, err)
		}
	}

	// Close program
	if err := prog.Program.Close(); err != nil {
		return fmt.Errorf("failed to close program %s: %w", name, err)
	}

	delete(l.programs, name)
	return nil
}

// LoadMap loads or retrieves a pinned BPF map with persistence support
func (l *BPFLoader) LoadMap(name string, spec *ebpf.MapSpec) (*ebpf.Map, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if already loaded
	if m, exists := l.maps[name]; exists {
		return m, nil
	}

	var m *ebpf.Map
	var err error

	// Try to load pinned map if persistence is enabled
	if l.persistMaps && l.pinnedPath != "" {
		pinnedPath := filepath.Join(l.pinnedPath, name)
		m, err = ebpf.LoadPinnedMap(pinnedPath, &ebpf.LoadPinOptions{})
		if err == nil {
			l.maps[name] = m
			return m, nil
		}
	}

	// Create new map
	m, err = ebpf.NewMapWithOptions(spec, ebpf.MapOptions{
		PinPath: l.getPinPath(name),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create map %s: %w", name, err)
	}

	// Pin map if persistence is enabled
	if l.persistMaps && l.pinnedPath != "" {
		pinnedPath := filepath.Join(l.pinnedPath, name)
		if err := m.Pin(pinnedPath); err != nil {
			m.Close()
			return nil, fmt.Errorf("failed to pin map %s: %w", name, err)
		}
	}

	l.maps[name] = m
	return m, nil
}

// SetupTailCallMap sets up a tail call map for program chaining
func (l *BPFLoader) SetupTailCallMap(name string, programs map[int32]string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create tail call map if not exists
	if _, exists := l.tailCallMaps[name]; !exists {
		spec := &ebpf.MapSpec{
			Type:       ebpf.ProgramArray,
			KeySize:    4,
			ValueSize:  4,
			MaxEntries: uint32(len(programs)),
		}

		m, err := l.LoadMap(name+"_tail_calls", spec)
		if err != nil {
			return fmt.Errorf("failed to create tail call map: %w", err)
		}
		l.tailCallMaps[name] = m
	}

	// Populate tail call map
	tailMap := l.tailCallMaps[name]
	for idx, progName := range programs {
		prog, exists := l.programs[progName]
		if !exists {
			return fmt.Errorf("program %s not found for tail call", progName)
		}

		if err := tailMap.Put(idx, prog.Program); err != nil {
			return fmt.Errorf("failed to add program %s to tail call map: %w", progName, err)
		}

		// Update program metadata
		prog.TailCalls = append(prog.TailCalls, name)
	}

	return nil
}

// AttachProgram attaches a program to a kernel hook
func (l *BPFLoader) AttachProgram(name string, attachType AttachType, attachPoint string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	prog, exists := l.programs[name]
	if !exists {
		return fmt.Errorf("program %s not found", name)
	}

	// Attach based on type
	var attachLink link.Link
	var err error

	switch attachType {
	case AttachTypeKprobe:
		attachLink, err = link.Kprobe(attachPoint, prog.Program, nil)
	case AttachTypeKretprobe:
		attachLink, err = link.Kretprobe(attachPoint, prog.Program, nil)
	case AttachTypeTracepoint:
		parts := strings.Split(attachPoint, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid tracepoint format: %s", attachPoint)
		}
		attachLink, err = link.Tracepoint(parts[0], parts[1], prog.Program, nil)
	case AttachTypeRawTracepoint:
		attachLink, err = link.AttachRawTracepoint(link.RawTracepointOptions{
			Name:    attachPoint,
			Program: prog.Program,
		})
	case AttachTypeCgroup:
		// Cgroup attachment requires cgroup fd
		return errors.New("cgroup attachment not implemented")
	case AttachTypeXDP:
		// XDP attachment requires interface index
		return errors.New("XDP attachment not implemented")
	default:
		return fmt.Errorf("unsupported attach type: %v", attachType)
	}

	if err != nil {
		return fmt.Errorf("failed to attach program %s: %w", name, err)
	}

	prog.Link = attachLink
	return nil
}

// GetProgramStats retrieves runtime statistics for a program
func (l *BPFLoader) GetProgramStats(name string) (*ProgramStats, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	prog, exists := l.programs[name]
	if !exists {
		return nil, fmt.Errorf("program %s not found", name)
	}

	info, err := prog.Program.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to get program info: %w", err)
	}

	runCount, _ := info.RunCount()
	runTime, _ := info.Runtime()

	return &ProgramStats{
		Name:        name,
		Type:        prog.Type.String(),
		RunCount:    runCount,
		RunTime:     runTime,
		ID:          info.ID,
		Tag:         fmt.Sprintf("%x", info.Tag),
		LoadTime:    info.CreatedAt,
		MemoryUsage: info.MemoryUsage,
		Verified:    info.Verified,
	}, nil
}

// GetMapStats retrieves statistics for a map
func (l *BPFLoader) GetMapStats(name string) (*MapStats, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	m, exists := l.maps[name]
	if !exists {
		return nil, fmt.Errorf("map %s not found", name)
	}

	info, err := m.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to get map info: %w", err)
	}

	return &MapStats{
		Name:        name,
		Type:        info.Type.String(),
		KeySize:     info.KeySize,
		ValueSize:   info.ValueSize,
		MaxEntries:  info.MaxEntries,
		Flags:       info.Flags,
		ID:          info.ID,
	}, nil
}

// Close cleans up all loaded programs and maps
func (l *BPFLoader) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error

	// Unload all programs
	for name, prog := range l.programs {
		if prog.Link != nil {
			if err := prog.Link.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close link for %s: %w", name, err))
			}
		}
		if err := prog.Program.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close program %s: %w", name, err))
		}
	}

	// Close all maps (unless pinned)
	for name, m := range l.maps {
		if !l.persistMaps {
			if err := m.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close map %s: %w", name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errs)
	}

	return nil
}

// Helper methods

func (l *BPFLoader) getPinPath(name string) string {
	if l.persistMaps && l.pinnedPath != "" {
		return filepath.Join(l.pinnedPath, name)
	}
	return ""
}

// AttachType represents different BPF program attachment types
type AttachType int

const (
	AttachTypeKprobe AttachType = iota
	AttachTypeKretprobe
	AttachTypeTracepoint
	AttachTypeRawTracepoint
	AttachTypeCgroup
	AttachTypeXDP
	AttachTypeTC
	AttachTypePerfEvent
)

// ProgramStats contains runtime statistics for a BPF program
type ProgramStats struct {
	Name        string
	Type        string
	RunCount    uint64
	RunTime     uint64 // nanoseconds
	ID          uint32
	Tag         string
	LoadTime    uint64
	MemoryUsage uint64
	Verified    bool
}

// MapStats contains statistics for a BPF map
type MapStats struct {
	Name       string
	Type       string
	KeySize    uint32
	ValueSize  uint32
	MaxEntries uint32
	Flags      uint32
	ID         uint32
}

// GetLoadedPrograms returns a list of all loaded programs
func (l *BPFLoader) GetLoadedPrograms() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	programs := make([]string, 0, len(l.programs))
	for name := range l.programs {
		programs = append(programs, name)
	}
	return programs
}

// GetLoadedMaps returns a list of all loaded maps
func (l *BPFLoader) GetLoadedMaps() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	maps := make([]string, 0, len(l.maps))
	for name := range l.maps {
		maps = append(maps, name)
	}
	return maps
}

// GetProgram retrieves a loaded program by name
func (l *BPFLoader) GetProgram(name string) (*ebpf.Program, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	prog, exists := l.programs[name]
	if !exists {
		return nil, fmt.Errorf("program %s not found", name)
	}
	return prog.Program, nil
}

// GetMap retrieves a loaded map by name
func (l *BPFLoader) GetMap(name string) (*ebpf.Map, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	m, exists := l.maps[name]
	if !exists {
		return nil, fmt.Errorf("map %s not found", name)
	}
	return m, nil
}