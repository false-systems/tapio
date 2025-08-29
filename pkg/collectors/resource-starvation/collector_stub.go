//go:build !linux
// +build !linux

package resourcestarvation

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

type LinuxCollector struct {
	*Collector
}

type CollectorStats struct {
	Platform      string `json:"platform"`
	EBPFEnabled   bool   `json:"ebpf_enabled"`
	LinksAttached int    `json:"links_attached"`
}

func NewLinuxCollector(config *Config, logger *zap.Logger) (*LinuxCollector, error) {
	baseCollector, err := NewCollector(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create base collector: %w", err)
	}

	return &LinuxCollector{
		Collector: baseCollector,
	}, nil
}

func (c *LinuxCollector) Start(ctx context.Context) error {
	c.logger.Warn("Resource starvation collector is not supported on this platform",
		zap.String("platform", "non-linux"))
	return fmt.Errorf("resource starvation collector requires Linux with eBPF support")
}

func (c *LinuxCollector) Stop() error {
	return nil
}

func (c *LinuxCollector) GetStats() CollectorStats {
	return CollectorStats{
		Platform:      "non-linux",
		EBPFEnabled:   false,
		LinksAttached: 0,
	}
}
