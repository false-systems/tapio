//go:build !linux
// +build !linux

package etcd

import (
	"fmt"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// NewEBPFCollector returns an error on non-Linux systems
func NewEBPFCollector(config collectors.CollectorConfig) (*EBPFCollector, error) {
	return nil, fmt.Errorf("eBPF collector is only supported on Linux")
}

// EBPFCollector is not available on non-Linux systems
type EBPFCollector struct{}
