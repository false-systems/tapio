//go:build !linux
// +build !linux

package services

import (
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// ebpfState stub for non-Linux platforms (always nil)
type ebpfState struct{}

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

// generateMockIP generates a random IP in the 10.244.x.x range (K8s pod CIDR)
func generateMockIP() string {
	return fmt.Sprintf("10.244.%d.%d", rand.Intn(256), rand.Intn(256))
}

// generateMockConnections generates simulated connection events for testing
func (t *ConnectionTracker) generateMockConnections() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Mock service endpoints
	services := []struct {
		name string
		port uint16
	}{
		{"web-frontend", 8080},
		{"api-backend", 3000},
		{"postgres-db", 5432},
		{"redis-cache", 6379},
		{"kafka-broker", 9092},
	}

	// Mock client pods
	clients := []struct {
		name string
		pid  uint32
	}{
		{"web-pod-1", 1001},
		{"web-pod-2", 1002},
		{"api-pod-1", 2001},
		{"worker-pod-1", 3001},
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

				// Generate random IPs
				clientIP := generateMockIP()
				serviceIP := generateMockIP()

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
				copy(event.SrcIP[:], []byte(clientIP))
				copy(event.DstIP[:], []byte(serviceIP))
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
