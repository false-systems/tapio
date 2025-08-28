//go:build !linux
// +build !linux

package dns

// eBPFState is a stub type for non-Linux platforms
type eBPFState struct {
	// Empty struct for non-Linux platforms
}
