//go:build linux
// +build linux

package linux

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
)

// programLoader implements core.ProgramLoader for Linux
type programLoader struct {
	mu       sync.RWMutex
	programs map[string]*loadedProgram
	links    map[string]link.Link
}

type loadedProgram struct {
	spec       *ebpf.ProgramSpec
	program    *ebpf.Program
	coreProgram core.Program
	loadTime   time.Time
}

// NewProgramLoader creates a new Linux eBPF program loader
func NewProgramLoader() (core.ProgramLoader, error) {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock limit: %w", err)
	}

	return &programLoader{
		programs: make(map[string]*loadedProgram),
		links:    make(map[string]link.Link),
	}, nil
}

// Load implements core.ProgramLoader
func (pl *programLoader) Load(ctx context.Context, spec core.ProgramSpec) (core.Program, error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Check if already loaded
	if _, exists := pl.programs[spec.Name]; exists {
		return core.Program{}, fmt.Errorf("program %s already loaded", spec.Name)
	}

	// Load bytecode
	bytecode, err := pl.loadBytecode(spec)
	if err != nil {
		return core.Program{}, fmt.Errorf("failed to load bytecode: %w", err)
	}

	// Create program spec
	progType, err := convertProgramType(spec.Type)
	if err != nil {
		return core.Program{}, err
	}

	ebpfSpec := &ebpf.ProgramSpec{
		Name:    spec.Name,
		Type:    progType,
		License: "GPL",
	}

	// Parse bytecode if it's ELF format
	// For now, we'll create a simple program
	// In a real implementation, we'd parse the bytecode
	coll, err := ebpf.NewCollection(&ebpf.CollectionSpec{
		Programs: map[string]*ebpf.ProgramSpec{
			spec.Name: ebpfSpec,
		},
	})
	if err != nil {
		return core.Program{}, fmt.Errorf("failed to create collection: %w", err)
	}

	prog := coll.Programs[spec.Name]
	if prog == nil {
		return core.Program{}, fmt.Errorf("program %s not found in collection", spec.Name)
	}

	// Attach the program
	l, err := pl.attachProgram(prog, spec)
	if err != nil {
		prog.Close()
		return core.Program{}, core.AttachError{
			ProgramName:  spec.Name,
			AttachTarget: spec.AttachTarget,
			Cause:        err,
		}
	}

	// Create core.Program
	coreProgram := core.Program{
		ID:           uint32(prog.FD()),
		Name:         spec.Name,
		Type:         spec.Type,
		AttachTarget: spec.AttachTarget,
		LoadTime:     time.Now(),
		Stats: core.ProgramStats{
			RunCount: 0,
			RunTime:  0,
			LastRun:  time.Time{},
		},
	}

	// Store the loaded program
	pl.programs[spec.Name] = &loadedProgram{
		spec:        ebpfSpec,
		program:     prog,
		coreProgram: coreProgram,
		loadTime:    coreProgram.LoadTime,
	}
	pl.links[spec.Name] = l

	_ = bytecode // Use bytecode to avoid unused variable error

	return coreProgram, nil
}

// Unload implements core.ProgramLoader
func (pl *programLoader) Unload(program core.Program) error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	loaded, exists := pl.programs[program.Name]
	if !exists {
		return fmt.Errorf("program %s not found", program.Name)
	}

	// Detach the program
	if l, exists := pl.links[program.Name]; exists {
		if err := l.Close(); err != nil {
			return fmt.Errorf("failed to detach program: %w", err)
		}
		delete(pl.links, program.Name)
	}

	// Close the program
	if err := loaded.program.Close(); err != nil {
		return fmt.Errorf("failed to close program: %w", err)
	}

	delete(pl.programs, program.Name)
	return nil
}

// List implements core.ProgramLoader
func (pl *programLoader) List() ([]core.Program, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	programs := make([]core.Program, 0, len(pl.programs))
	for _, loaded := range pl.programs {
		// Update stats from kernel
		info, err := loaded.program.Info()
		if err == nil {
			loaded.coreProgram.Stats.RunCount = info.RunCount
			loaded.coreProgram.Stats.RunTime = time.Duration(info.Runtime) * time.Nanosecond
		}
		programs = append(programs, loaded.coreProgram)
	}

	return programs, nil
}

// Close closes the program loader and all loaded programs
func (pl *programLoader) Close() error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	var errs []error

	// Close all links
	for name, l := range pl.links {
		if err := l.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close link %s: %w", name, err))
		}
	}

	// Close all programs
	for name, loaded := range pl.programs {
		if err := loaded.program.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close program %s: %w", name, err))
		}
	}

	pl.links = make(map[string]link.Link)
	pl.programs = make(map[string]*loadedProgram)

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	return nil
}

// Private methods

func (pl *programLoader) loadBytecode(spec core.ProgramSpec) ([]byte, error) {
	if len(spec.Code) > 0 {
		return spec.Code, nil
	}

	if spec.CodePath != "" {
		return os.ReadFile(spec.CodePath)
	}

	return nil, fmt.Errorf("no bytecode provided")
}

func (pl *programLoader) attachProgram(prog *ebpf.Program, spec core.ProgramSpec) (link.Link, error) {
	switch spec.Type {
	case core.ProgramTypeKprobe:
		return link.Kprobe(spec.AttachTarget, prog, nil)
	case core.ProgramTypeKretprobe:
		return link.Kretprobe(spec.AttachTarget, prog, nil)
	case core.ProgramTypeTracepoint:
		// Parse tracepoint target (format: "category/name")
		// For simplicity, we'll just use raw tracepoint
		return link.AttachRawTracepoint(link.RawTracepointOptions{
			Name:    spec.AttachTarget,
			Program: prog,
		})
	case core.ProgramTypeRawTracepoint:
		return link.AttachRawTracepoint(link.RawTracepointOptions{
			Name:    spec.AttachTarget,
			Program: prog,
		})
	default:
		return nil, fmt.Errorf("unsupported program type: %s", spec.Type)
	}
}

func convertProgramType(t core.ProgramType) (ebpf.ProgramType, error) {
	switch t {
	case core.ProgramTypeKprobe:
		return ebpf.Kprobe, nil
	case core.ProgramTypeTracepoint:
		return ebpf.TracePoint, nil
	case core.ProgramTypeRawTracepoint:
		return ebpf.RawTracepoint, nil
	case core.ProgramTypeXDP:
		return ebpf.XDP, nil
	case core.ProgramTypeTC:
		return ebpf.SchedCLS, nil
	case core.ProgramTypePerfEvent:
		return ebpf.PerfEvent, nil
	default:
		return 0, fmt.Errorf("unsupported program type: %s", t)
	}
}