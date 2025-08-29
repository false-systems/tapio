//go:build !linux
// +build !linux

package bpf

import "fmt"

// Stub for non-Linux platforms

// IsSupported always returns false on non-Linux platforms
func IsSupported() bool {
	return false
}

// Stub types for non-Linux platforms
type StarvationmonitorObjects struct{}
type StarvationmonitorMaps struct{}
type StarvationmonitorPrograms struct{}

// Stub functions for non-Linux platforms
func LoadStarvationmonitor() (*StarvationmonitorObjects, error) {
	return nil, ErrNotSupported
}

func LoadStarvationmonitorObjects(*StarvationmonitorObjects, interface{}) error {
	return ErrNotSupported
}

// ErrNotSupported is returned when eBPF is not supported
var ErrNotSupported = fmt.Errorf("eBPF not supported on this platform")
