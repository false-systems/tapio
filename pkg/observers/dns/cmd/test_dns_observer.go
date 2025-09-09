package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/observers/dns"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Create DNS observer config with fallback mode
	config := dns.DefaultConfig()
	config.EnableEBPF = false // Disable eBPF due to kernel compatibility issues
	config.BufferSize = 1000

	logger.Info("Starting DNS observer test",
		zap.Bool("ebpf_enabled", config.EnableEBPF),
		zap.Bool("circuit_breaker_enabled", config.CircuitBreakerConfig.Enabled))

	// Create the observer
	observer, err := dns.NewObserver("dns-test", config)
	if err != nil {
		logger.Fatal("Failed to create DNS observer", zap.Error(err))
	}

	// Start the observer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start DNS observer", zap.Error(err))
	}

	logger.Info("DNS observer started successfully")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start goroutine to read events
	go func() {
		eventCount := 0
		for event := range observer.Events() {
			eventCount++
			logger.Info("DNS Event Captured",
				zap.Int("event_count", eventCount),
				zap.String("event_id", event.EventID),
				zap.String("type", string(event.Type)),
				zap.Time("timestamp", event.Timestamp),
				zap.String("source", event.Source))

			if event.EventData.DNS != nil {
				dnsData := event.EventData.DNS
				logger.Info("DNS Details",
					zap.String("query_name", dnsData.QueryName),
					zap.String("query_type", dnsData.QueryType),
					zap.Int("response_code", dnsData.ResponseCode),
					zap.Duration("duration", dnsData.Duration),
					zap.String("client_ip", dnsData.ClientIP),
					zap.String("server_ip", dnsData.ServerIP))
			}

			// Print metadata
			if len(event.Metadata.Attributes) > 0 {
				logger.Info("Event Metadata",
					zap.Any("attributes", event.Metadata.Attributes))
			}
		}
	}()

	// Print observer stats periodically
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for range ticker.C {
			if observer.IsHealthy() {
				logger.Info("Collector Status: HEALTHY")
			} else {
				logger.Warn("Collector Status: UNHEALTHY")
			}
		}
	}()
	defer ticker.Stop()

	logger.Info("DNS observer is running. Press Ctrl+C to stop.")
	logger.Info("To generate DNS traffic, try running: dig google.com or nslookup github.com")

	// Wait for signal
	<-sigChan

	logger.Info("Shutting down DNS observer...")

	// Stop the observer
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping observer", zap.Error(err))
	}

	logger.Info("DNS observer stopped")
}
