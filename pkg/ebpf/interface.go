//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

// All types and interfaces are defined in types.go
// This file only contains the stub implementation for non-Linux platforms

// Note: NewMonitor is implemented in monitor_stub.go to avoid duplication
