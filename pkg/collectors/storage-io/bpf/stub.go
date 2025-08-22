//go:build !linux
// +build !linux

package bpf

import "fmt"

// Stub implementations for non-Linux platforms

type StoragemonitorObjects struct{}

func (o *StoragemonitorObjects) Close() error {
	return nil
}

func LoadStoragemonitorObjects(obj *StoragemonitorObjects, opts interface{}) error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

func CheckKernelCompatibility() error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

func ValidateEBPFProgram() error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

func CheckEBPFSupport() bool {
	return false
}
