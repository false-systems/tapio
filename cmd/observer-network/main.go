package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/observers/network"
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

	// Create network observer config
	config := &network.Config{
		BufferSize:         10000,
		FlushInterval:      5 * time.Second,
		EnableIPv4:         true,
		EnableIPv6:         true,
		EnableTCP:          true,
		EnableUDP:          true,
		EnableHTTP:         true,
		EnableHTTPS:        true,
		EnableDNS:          true,
		HTTPPorts:          []int{80, 8080, 3000},
		HTTPSPorts:         []int{443, 8443},
		DNSPort:            53,
		MaxEventsPerSecond: 1000,
		SamplingRate:       1.0,
	}

	// Create and start observer
	observer, err := network.NewObserver("network-observer", config, logger)
	if err != nil {
		logger.Fatal("Failed to create network observer", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start observer
	if err := observer.Start(ctx); err != nil {
		logger.Fatal("Failed to start network observer", zap.Error(err))
	}

	logger.Info("Network observer started successfully")

	// Process events
	go func() {
		events := observer.Events()
		for event := range events {
			// In production, send to correlation engine or storage
			logger.Debug("Network event",
				zap.String("connection", event.EventID),
				zap.Time("timestamp", event.Timestamp),
			)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down network observer...")

	// Graceful shutdown
	if err := observer.Stop(); err != nil {
		logger.Error("Error stopping network observer", zap.Error(err))
	}

	logger.Info("Network observer stopped")
}
