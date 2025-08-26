//go:build !linux

package bpf

import "errors"

// GetEBPFProgram returns an error on non-Linux platforms
func GetEBPFProgram() ([]byte, error) {
	return nil, errors.New("eBPF is only supported on Linux")
}

// GetProgramSpecs returns an error on non-Linux platforms
func GetProgramSpecs() (interface{}, error) {
	return nil, errors.New("eBPF is only supported on Linux")
}

// IsArchitectureSupported returns false on non-Linux platforms
func IsArchitectureSupported() bool {
	return false
}
