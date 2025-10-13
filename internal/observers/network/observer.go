package network

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/base"
)

// Config holds network observer configuration
type Config struct {
	Output           base.OutputConfig
	EventChannelSize int // Ring buffer → processor channel size (default: 1000)
}

// NetworkObserver tracks TCP/UDP/DNS network events using eBPF
type NetworkObserver struct {
	*base.BaseObserver
	config Config
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

// stateToEventType maps TCP state transitions to domain event types
func stateToEventType(oldState, newState uint8) string {
	switch newState {
	case TCP_ESTABLISHED:
		return "connection_established"
	case TCP_LISTEN:
		if oldState == TCP_CLOSE {
			return "listen_started"
		}
	case TCP_CLOSE:
		if oldState == TCP_LISTEN {
			return "listen_stopped"
		}
		return "connection_closed"
	}
	return "tcp_state_change"
}

// convertIPv4 converts uint32 IPv4 (little-endian) to string
func convertIPv4(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

// convertIPv6 converts [16]byte IPv6 to string
func convertIPv6(ip [16]byte) string {
	return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
		uint16(ip[0])<<8|uint16(ip[1]),
		uint16(ip[2])<<8|uint16(ip[3]),
		uint16(ip[4])<<8|uint16(ip[5]),
		uint16(ip[6])<<8|uint16(ip[7]),
		uint16(ip[8])<<8|uint16(ip[9]),
		uint16(ip[10])<<8|uint16(ip[11]),
		uint16(ip[12])<<8|uint16(ip[13]),
		uint16(ip[14])<<8|uint16(ip[15]))
}

// extractComm extracts null-terminated process name
func extractComm(comm [16]byte) string {
	for i, b := range comm {
		if b == 0 {
			return string(comm[:i])
		}
	}
	return string(comm[:])
}
