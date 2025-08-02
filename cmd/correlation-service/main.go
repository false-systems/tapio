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
	"github.com/yairfalse/tapio/pkg/intelligence/nats"
	"github.com/yairfalse/tapio/pkg/intelligence/storage"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create K8s client
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to get in-cluster config", zap.Error(err))
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create K8s client", zap.Error(err))
	}

	// 1. Create storage
	storageConfig := storage.DefaultMemoryStorageConfig()
	memStorage := storage.NewMemoryStorage(logger, storageConfig)

	// 2. Create correlation engine
	engineConfig := correlation.DefaultEngineConfig()
	engine, err := correlation.NewEngine(logger, engineConfig, clientset, memStorage)
	if err != nil {
		logger.Fatal("Failed to create correlation engine", zap.Error(err))
	}

	// Start the engine
	if err := engine.Start(ctx); err != nil {
		logger.Fatal("Failed to start correlation engine", zap.Error(err))
	}

	// 3. Create NATS subscriber
	natsConfig := nats.DefaultConfig()
	natsConfig.URL = getEnv("NATS_URL", "nats://nats.tapio-system.svc.cluster.local:4222")
	natsConfig.StreamName = getEnv("STREAM_NAME", "TRACES")
	natsConfig.ConsumerName = getEnv("CONSUMER_NAME", "correlation-service")
	natsConfig.Subject = getEnv("SUBJECT", "traces.>")

	subscriber, err := nats.NewSubscriber(logger, natsConfig, engine)
	if err != nil {
		logger.Fatal("Failed to create NATS subscriber", zap.Error(err))
	}

	// 4. Start processing correlation results
	go handleCorrelationResults(ctx, engine, logger)

	// 5. Start NATS subscriber in background
	go func() {
		if err := subscriber.Start(ctx); err != nil {
			logger.Error("NATS subscriber error", zap.Error(err))
		}
	}()

	logger.Info("Correlation service started",
		zap.String("nats_url", natsConfig.URL),
		zap.String("stream", natsConfig.StreamName),
		zap.String("subject", natsConfig.Subject),
		zap.Bool("k8s_enabled", engineConfig.EnableK8s),
		zap.Bool("temporal_enabled", engineConfig.EnableTemporal),
		zap.Bool("sequence_enabled", engineConfig.EnableSequence),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down correlation service...")

	// Cancel context to trigger graceful shutdown
	cancel()

	// Give components time to shut down
	time.Sleep(2 * time.Second)

	// Stop engine
	if err := engine.Stop(); err != nil {
		logger.Error("Failed to stop engine", zap.Error(err))
	}

	logger.Info("Correlation service stopped")
}

// handleCorrelationResults processes correlation results from the engine
func handleCorrelationResults(ctx context.Context, engine *correlation.Engine, logger *zap.Logger) {
	results := engine.Results()

	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			// Log significant correlations
			if result.Confidence >= 0.8 {
				logger.Info("High confidence correlation detected",
					zap.String("id", result.ID),
					zap.String("type", result.Type),
					zap.Float64("confidence", result.Confidence),
					zap.String("summary", result.Summary),
					zap.Int("events", len(result.Events)),
				)
			}

			// Here you could:
			// - Send to alerting system
			// - Store in time-series DB
			// - Publish to NATS for other services
			// - Update Kubernetes annotations
			// - Send to UI/dashboard
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
