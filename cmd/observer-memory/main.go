package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/observers/memory"
	"go.uber.org/zap"
)

func main() {
	var (
		logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		mode     = flag.String("mode", "growth", "Operation mode (growth, targeted, debugging)")
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

	// Map mode string to OperationMode
	var opMode memory.OperationMode
	switch *mode {
	case "targeted":
		opMode = memory.ModeTargeted
	case "debugging":
		opMode = memory.ModeDebugging
	default:
		opMode = memory.ModeGrowthDetection
	}

	// Create memory observer config
	config := &memory.Config{
		Name:               "memory-observer",
		BufferSize:         10000,
		EnableEBPF:         true,
		Mode:               opMode,
		MinAllocationSize:  10 * 1024, // 10KB minimum
		MinUnfreedAge:      30 * time.Second,
		SamplingRate:       1,
		MaxEventsPerSec:    1000,
		StackDedupWindow:   5 * time.Second,
		TargetPID:          0, // Track all processes
		TargetDuration:     0,
		TargetCGroupID:     0,
		RSSGrowthThreshold: 100 * 1024 * 1024, // 100MB
		RSSCheckInterval:   10 * time.Second,
		LibCPath:           "/lib/x86_64-linux-gnu/libc.so.6",
	}

	// Create and start observer
	observer, err := memory.NewObserver("memory-observer", config, logger)
	if err != nil {
		logger.Fatal("Failed to create memory observer", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start observer
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start memory observer", zap.Error(err))
	}

	logger.Info("Memory observer started successfully", zap.String("mode", string(opMode)))

	// Process events
	go func() {
		events := observer.Events()
		for event := range events {
			// In production, send to correlation engine or storage
			logger.Debug("Memory event",
				zap.String("allocation", event.EventID),
				zap.Time("timestamp", event.Timestamp),
			)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down memory observer...")

	// Graceful shutdown
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping memory observer", zap.Error(err))
	}

	logger.Info("Memory observer stopped")
}
