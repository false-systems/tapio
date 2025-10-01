// Test program to demonstrate multi-output functionality
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/observers/deployments"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	logger.Info("Starting multi-output test")

	// Create observer with both stdout and OTEL outputs enabled
	config := &deployments.Config{
		Name:                    "test-observer",
		BufferSize:              1000,
		ResyncPeriod:            30 * time.Second,
		TrackConfigMaps:         true,
		TrackSecrets:            true,
		IgnoreSystemDeployments: true,
		DeduplicationWindow:     5 * time.Minute,
		MockMode:                true, // Use mock mode for testing without K8s
		EnableStdout:            true, // Enable JSON output to stdout
		EnableOTEL:              true, // Enable domain metrics to OTEL
	}

	observer, err := deployments.NewObserver("test-observer", config)
	if err != nil {
		logger.Fatal("Failed to create observer", zap.Error(err))
	}

	// Start observer
	ctx := context.Background()
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start observer", zap.Error(err))
	}

	logger.Info("Observer started in mock mode with multi-output enabled")
	logger.Info("Configuration",
		zap.Bool("stdout_enabled", config.EnableStdout),
		zap.Bool("otel_enabled", config.EnableOTEL))

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	fmt.Println("\n✅ Observer running with multi-output support:")
	fmt.Println("   - Stdout: JSON events will be printed")
	fmt.Println("   - OTEL: Domain metrics will be exported")
	fmt.Println("\nPress Ctrl+C to stop...")

	<-sigCh

	logger.Info("Shutting down...")
	if err := observer.Stop(); err != nil {
		logger.Error("Failed to stop observer", zap.Error(err))
	}

	logger.Info("Test completed successfully")
}
