//go:build !linux
// +build !linux

package cni

import (
	"fmt"
	"net"
)

// Stub implementations for non-Linux platforms

// EnhanceWithNetworkPolicy stub for non-Linux
func (c *Collector) EnhanceWithNetworkPolicy() error {
	return fmt.Errorf("network policy monitoring not supported on this platform")
}

// PolicyMetrics stub
type PolicyMetrics struct {
	PacketsAllowed uint64
	PacketsDropped uint64
	PacketsLogged  uint64
	PolicyMatches  uint64
	PolicyMisses   uint64
}

// PolicyRule stub
type PolicyRule struct {
	ID         uint32
	Name       string
	SourceCIDR net.IP
	SourceMask net.IP
	DestCIDR   net.IP
	DestMask   net.IP
	Port       uint16
	Protocol   uint8
	Action     uint8
}
