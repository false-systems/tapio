//go:build linux

package network

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/base"
)

// Config holds network observer configuration
type Config struct {
	Output base.OutputConfig
}

// NetworkObserver tracks TCP/UDP/DNS network events using eBPF
type NetworkObserver struct {
	*base.BaseObserver
	config      Config
	ebpfManager *base.EBPFManager
}

// NewNetworkObserver creates a new network observer
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	return &NetworkObserver{
		BaseObserver: baseObs,
		config:       config,
	}, nil
}
