package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/observers/systemd"
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

	// Create systemd observer config
	config := &systemd.Config{
		BufferSize:           10000,
		EnableEBPF:           true,
		EnableJournal:        true,
		ServicePatterns:      []string{}, // Monitor all services
		MonitorServiceStates: true,
		MonitorCgroups:       true,
		RateLimitPerSecond:   1000,
		HealthCheckInterval:  30 * time.Second,
		Logger:               logger,
	}

	// Create and start observer
	observer, err := systemd.NewObserver("systemd-observer", config)
	if err != nil {
		logger.Fatal("Failed to create systemd observer", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start observer
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start systemd observer", zap.Error(err))
	}

	logger.Info("Systemd observer started successfully")

	// Process events
	go func() {
		events := observer.Events()
		for event := range events {
			// In production, send to correlation engine or storage
			logger.Debug("Systemd event",
				zap.String("service", event.EventID),
				zap.Time("timestamp", event.Timestamp),
			)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down systemd observer...")

	// Graceful shutdown
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping systemd observer", zap.Error(err))
	}

	logger.Info("Systemd observer stopped")
}
