//go:build !linux
// +build !linux

package k8s

import (
	"context"
	"fmt"
)

// K8sEBPFCollector stub for non-Linux platforms
type K8sEBPFCollector struct{}

// NewK8sEBPFCollector returns an error on non-Linux platforms
func NewK8sEBPFCollector() (*K8sEBPFCollector, error) {
	return nil, fmt.Errorf("eBPF collection is only supported on Linux")
}

// Start returns an error on non-Linux platforms
func (c *K8sEBPFCollector) Start(ctx context.Context) error {
	return fmt.Errorf("eBPF collection is only supported on Linux")
}

// Stop is a no-op on non-Linux platforms
func (c *K8sEBPFCollector) Stop() error {
	return nil
}

// Events returns nil channel on non-Linux platforms
func (c *K8sEBPFCollector) Events() <-chan K8sSyscallEvent {
	ch := make(chan K8sSyscallEvent)
	close(ch)
	return ch
}

// UpdatePodInfo is a no-op on non-Linux platforms
func (c *K8sEBPFCollector) UpdatePodInfo(cgroupID uint64, podUID, namespace, podName string) error {
	return fmt.Errorf("eBPF collection is only supported on Linux")
}
