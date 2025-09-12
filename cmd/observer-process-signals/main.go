package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	processsignals "github.com/yairfalse/tapio/pkg/observers/process-signals"
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

	// Create process-signals observer config
	config := &processsignals.Config{
		BufferSize:       10000,
		EnableEBPF:       true,
		EnableRingBuffer: true,
		RingBufferSize:   8192,
		BatchSize:        32,
		BatchTimeout:     10 * time.Millisecond,
		EnableFilters:    true,
		Logger:           logger,
	}

	// Create and start observer
	observer, err := processsignals.NewObserver("process-signals-observer", config)
	if err != nil {
		logger.Fatal("Failed to create process-signals observer", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start observer
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start process-signals observer", zap.Error(err))
	}

	logger.Info("Process-signals observer started successfully")

	// Process events
	go func() {
		events := observer.Events()
		for event := range events {
			// In production, send to correlation engine or storage
			logger.Debug("Process signal event",
				zap.String("signal", event.EventID),
				zap.Time("timestamp", event.Timestamp),
			)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down process-signals observer...")

	// Graceful shutdown
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping process-signals observer", zap.Error(err))
	}

	logger.Info("Process-signals observer stopped")
}
