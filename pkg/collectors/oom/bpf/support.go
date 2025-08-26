//go:build linux

package bpf

import (
	"fmt"
	"runtime"

	"github.com/cilium/ebpf"
)

// GetEBPFProgram returns the appropriate eBPF program for the current architecture
func GetEBPFProgram() ([]byte, error) {
	switch runtime.GOARCH {
	case "amd64":
		return _OommonitorBytes, nil
	case "arm64":
		return _OommonitorBytes, nil
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
}

// GetProgramSpecs returns eBPF program specifications
func GetProgramSpecs() (*ebpf.CollectionSpec, error) {
	return loadOommonitor()
}

// Objects wraps the internal oommonitorObjects for external use
type Objects struct {
	objs *oommonitorObjects
}

// LoadObjects creates and loads all eBPF objects
func LoadObjects() (*Objects, error) {
	objs := &oommonitorObjects{}
	if err := loadOommonitorObjects(objs, nil); err != nil {
		return nil, err
	}
	return &Objects{objs: objs}, nil
}

// Close cleans up all eBPF resources
func (o *Objects) Close() error {
	if o.objs != nil {
		return o.objs.Close()
	}
	return nil
}

// GetOomEventsMap returns the ring buffer map for OOM events
func (o *Objects) GetOomEventsMap() *ebpf.Map {
	return o.objs.OomEvents
}

// GetConfigMap returns the configuration map
func (o *Objects) GetConfigMap() *ebpf.Map {
	return o.objs.ConfigMap
}

// GetTraceOomKillProcessProgram returns the OOM kill tracepoint program
func (o *Objects) GetTraceOomKillProcessProgram() *ebpf.Program {
	return o.objs.TraceOomKillProcess
}

// GetTraceMemoryPressureProgram returns the memory pressure tracepoint program
func (o *Objects) GetTraceMemoryPressureProgram() *ebpf.Program {
	return o.objs.TraceMemoryPressure
}

// GetTraceProcessExitProgram returns the process exit tracepoint program
func (o *Objects) GetTraceProcessExitProgram() *ebpf.Program {
	return o.objs.TraceProcessExit
}

// IsArchitectureSupported returns true if the current architecture is supported
func IsArchitectureSupported() bool {
	return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
}
