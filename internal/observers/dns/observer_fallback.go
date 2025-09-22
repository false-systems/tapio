//go:build !linux
// +build !linux

package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startPlatform starts fallback DNS problem simulation
func (o *Observer) startPlatform() error {
	return o.startFallback()
}

// startFallback generates simulated DNS problems for testing
func (o *Observer) startFallback() error {
	o.logger.Info("Starting DNS observer in fallback mode (simulated problems)")

	o.LifecycleManager.Start("mock-generator", func() {
		o.generateMockProblems(context.Background())
	})

	return nil
}

// generateMockProblems generates fake DNS problems for testing
func (o *Observer) generateMockProblems(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	problems := []struct {
		query       string
		problemType DNSProblemType
		latencyMs   float64
	}{
		{"slow-service.example.com", DNSProblemSlow, 250},
		{"nonexistent.domain.local", DNSProblemNXDOMAIN, 5},
		{"timeout.service.cluster.local", DNSProblemTimeout, 5000},
		{"broken-dns.internal", DNSProblemSERVFAIL, 10},
	}

	eventCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate a mock problem
			problem := problems[eventCount%len(problems)]
			eventCount++

			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("mock-dns-problem-%d", eventCount),
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    o.name,
				Severity:  domain.EventSeverityWarning,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName:    problem.query,
						QueryType:    "A",
						Duration:     time.Duration(problem.latencyMs * float64(time.Millisecond)),
						ResponseCode: getResponseCode(problem.problemType),
						Error:        true,
						ErrorMessage: getMockErrorMessage(problem.problemType),
						ClientIP:     "10.0.1.5",
						ServerIP:     "10.0.0.53",
					},
					Process: &domain.ProcessData{
						PID:     12345,
						Command: "mock-app",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer":     "dns",
						"version":      "1.0.0",
						"mode":         "fallback",
						"problem_type": problem.problemType.String(),
					},
				},
			}

			if o.EventChannelManager.SendEvent(event) {
				o.BaseObserver.RecordEvent()
				o.logger.Debug("Sent mock DNS problem event",
					zap.String("query", problem.query),
					zap.String("problem", problem.problemType.String()))
			} else {
				o.BaseObserver.RecordDrop()
			}
		}
	}
}

// stopPlatform stops the fallback mode
func (o *Observer) stopPlatform() {
	o.logger.Info("Stopping DNS observer fallback mode")
}

// Helper functions for fallback mode
func getResponseCode(problemType DNSProblemType) int {
	switch problemType {
	case DNSProblemNXDOMAIN:
		return 3 // NXDOMAIN
	case DNSProblemSERVFAIL:
		return 2 // SERVFAIL
	case DNSProblemRefused:
		return 5 // REFUSED
	default:
		return 0 // NOERROR (but slow/timeout)
	}
}

func getMockErrorMessage(problemType DNSProblemType) string {
	switch problemType {
	case DNSProblemSlow:
		return "Query exceeded latency threshold"
	case DNSProblemTimeout:
		return "DNS query timed out"
	case DNSProblemNXDOMAIN:
		return "Domain does not exist"
	case DNSProblemSERVFAIL:
		return "DNS server failure"
	case DNSProblemRefused:
		return "Query refused by server"
	default:
		return "Unknown DNS problem"
	}
}
