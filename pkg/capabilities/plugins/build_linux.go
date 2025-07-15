//go:build linux
// +build linux

package plugins

// This file ensures eBPF plugins are only compiled on Linux
// Build optimization: eBPF code excluded from non-Linux builds

// Linux-specific plugin initialization happens in ebpf_memory_linux.go
// Other files with different build tags handle other platforms
