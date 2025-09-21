//go:build !linux
// +build !linux

package services

import (
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// startEBPF starts fallback connection simulation (non-Linux)
func (t *ConnectionTracker) startEBPF() error {
	t.logger.Info("Starting services observer in fallback mode (simulated connections)")

	// Start mock connection generator
	go t.generateMockConnections()

	return nil
}

// stopEBPF stops fallback mode
func (t *ConnectionTracker) stopEBPF() {
	t.logger.Info("Stopping services observer fallback mode")
}

// generateMockConnections generates simulated connection events for testing
func (t *ConnectionTracker) generateMockConnections() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Mock service endpoints
	services := []struct {
		name string
		ip   string
		port uint16
	}{
		{"web-frontend", "10.244.1.10", 8080},
		{"api-backend", "10.244.2.20", 3000},
		{"postgres-db", "10.244.3.30", 5432},
		{"redis-cache", "10.244.4.40", 6379},
		{"kafka-broker", "10.244.5.50", 9092},
	}

	// Mock client pods
	clients := []struct {
		name string
		ip   string
		pid  uint32
	}{
		{"web-pod-1", "10.244.1.5", 1001},
		{"web-pod-2", "10.244.1.6", 1002},
		{"api-pod-1", "10.244.2.5", 2001},
		{"worker-pod-1", "10.244.6.5", 3001},
	}

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			// Generate random connections
			for i := 0; i < 3; i++ {
				client := clients[rand.Intn(len(clients))]
				service := services[rand.Intn(len(services))]

				// Create connection event
				event := &ConnectionEvent{
					Timestamp: uint64(time.Now().UnixNano()),
					EventType: ConnectionConnect,
					Direction: 0, // Outbound
					SrcPort:   uint16(30000 + rand.Intn(10000)),
					DstPort:   service.port,
					Family:    2, // AF_INET
					PID:       client.pid,
					TID:       client.pid,
					UID:       1000,
					GID:       1000,
					CgroupID:  uint64(client.pid * 100),
				}

				// Set IPs
				copy(event.SrcIP[:], []byte(client.ip))
				copy(event.DstIP[:], []byte(service.ip))
				copy(event.Comm[:], []byte(client.name))

				// Send event
				select {
				case t.eventCh <- event:
					t.logger.Debug("Sent mock connection event",
						zap.String("client", client.name),
						zap.String("service", service.name),
						zap.Uint16("port", service.port))
				default:
					t.logger.Warn("Event channel full in fallback mode")
				}
			}
		}
	}
}

// ipStringToBytes converts IP string to byte array for mock data
func ipStringToBytes(ip string) [16]byte {
	var result [16]byte
	copy(result[:], []byte(ip))
	return result
}
