//go:build linux

package containerruntime

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// loadBPFSpec loads BPF object file and returns CollectionSpec
func loadBPFSpec(path string) (*ebpf.CollectionSpec, error) {
	if path == "" {
		return nil, fmt.Errorf("failed to load BPF spec: empty path")
	}

	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load BPF spec: %w", err)
	}

	return spec, nil
}
