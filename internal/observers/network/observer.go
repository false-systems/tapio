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
