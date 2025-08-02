package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Create K8s client
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to get in-cluster config", zap.Error(err))
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create K8s client", zap.Error(err))
	}

	// Start the real correlation system
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize K8s relationship loader
	k8sLoader := correlation.NewK8sRelationshipLoader(logger, clientset)
	if err := k8sLoader.Start(ctx); err != nil {
		logger.Fatal("Failed to start K8s loader", zap.Error(err))
	}

	// 2. Create the correlation system with all components
	correlationConfig := correlation.SimpleSystemConfig{
		EventBufferSize:     1000,
		MaxConcurrency:      10,
		EnableK8sNative:     true,
		EnableTemporal:      true,
		EnableSequence:      true,
		ProcessingTimeout:   30 * time.Second,
		CleanupInterval:     5 * time.Minute,
	}

	correlationSystem := correlation.NewSimpleCorrelationSystem(logger, correlationConfig, clientset)

	// Start correlation system
	if err := correlationSystem.Start(); err != nil {
		logger.Fatal("Failed to start correlation system", zap.Error(err))
	}

	// 3. Start NATS integration
	natsConfig := &correlation.NATSIntegrationConfig{
		NATSURL:           getEnv("NATS_URL", "nats://nats.tapio-system.svc.cluster.local:4222"),
		StreamName:        getEnv("STREAM_NAME", "TRACES"),
		ConsumerName:      getEnv("CONSUMER_NAME", "correlation-service"),
		TraceSubjects:     []string{"traces.>"},
		CorrelationSystem: correlationSystem,
		Logger:            logger,
	}

	natsIntegration, err := correlation.NewNATSCorrelationIntegration(natsConfig)
	if err != nil {
		logger.Fatal("Failed to create NATS integration", zap.Error(err))
	}

	// Start NATS processing in background
	go func() {
		if err := natsIntegration.Start(ctx); err != nil {
			logger.Error("NATS integration failed", zap.Error(err))
		}
	}()

	// 4. Start correlation results handler
	go handleCorrelationResults(ctx, correlationSystem, natsIntegration, logger)

	logger.Info("Correlation service started",
		zap.String("nats_url", natsConfig.NATSURL),
		zap.String("stream", natsConfig.StreamName),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down correlation service...")
	cancel()
	time.Sleep(2 * time.Second) // Give time for graceful shutdown
}

// handleCorrelationResults processes and publishes correlation results
func handleCorrelationResults(ctx context.Context, system *correlation.SimpleCorrelationSystem,
	nats *correlation.NATSCorrelationIntegration, logger *zap.Logger) {

	// This would typically subscribe to correlation results from the system
	// For now, we'll simulate periodic correlation summaries
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// In a real implementation, this would get actual correlation results
			// from the correlation system's output channel
			logger.Info("Checking for correlation results...")

			// Example: Get active correlations and publish them
			// results := system.GetActiveCorrelations()
			// for _, result := range results {
			//     if err := nats.PublishCorrelationResult(result); err != nil {
			//         logger.Error("Failed to publish result", zap.Error(err))
			//     }
			// }
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
