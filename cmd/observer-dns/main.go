package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yairfalse/tapio/internal/observers/dns"
	"go.uber.org/zap"
)

func main() {
	var (
		logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	)
	flag.Parse()

	// Setup logger
	logConfig := zap.NewProductionConfig()
	if *logLevel == "debug" {
		logConfig = zap.NewDevelopmentConfig()
	}
	logger, err := logConfig.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Create DNS observer config
	config := &dns.Config{
		Name:                  "dns-observer",
		BufferSize:            10000,
		EnableEBPF:            true,
		CircuitBreakerConfig:  dns.DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	// Create and start observer
	observer, err := dns.NewObserver("dns-observer", *config)
	if err != nil {
		logger.Fatal("Failed to create DNS observer", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start observer
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start DNS observer", zap.Error(err))
	}

	logger.Info("DNS observer started successfully")

	// Process events
	go func() {
		events := observer.Events()
		for event := range events {
			// In production, send to correlation engine or storage
			logger.Debug("DNS event",
				zap.String("query", event.EventID),
				zap.Time("timestamp", event.Timestamp),
			)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down DNS observer...")

	// Graceful shutdown
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping DNS observer", zap.Error(err))
	}

	logger.Info("DNS observer stopped")
}
