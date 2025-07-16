//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

// All types and interfaces are defined in types.go
// This file exists to ensure the package builds on non-Linux platforms
// eBPF functionality is only available on Linux with eBPF build tags
