package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/integrations/k8sgrapher"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	serviceName = "k8s-grapher"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting K8sGrapher service", zap.String("version", "1.0.0"))

	// Initialize OpenTelemetry
	otelConfig := telemetry.DefaultConfig(serviceName)
	otelProvider, err := telemetry.NewProvider(context.Background(), otelConfig)
	if err != nil {
		logger.Fatal("Failed to setup OTEL provider", zap.Error(err))
	}
	defer func() {
		if err := otelProvider.Shutdown(context.Background()); err != nil {
			logger.Error("Failed to shutdown OTEL provider", zap.Error(err))
		}
	}()

	// Initialize instrumentation
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	if err != nil {
		logger.Fatal("Failed to create instrumentation", zap.Error(err))
	}

	// Create Kubernetes client
	kubeClient, err := createKubeClient()
	if err != nil {
		logger.Fatal("Failed to create Kubernetes client", zap.Error(err))
	}

	// Create Neo4j driver
	neo4jDriver, err := createNeo4jDriver()
	if err != nil {
		logger.Fatal("Failed to create Neo4j driver", zap.Error(err))
	}
	defer neo4jDriver.Close(context.Background())

	// Create K8sGrapher
	config := k8sgrapher.Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     neo4jDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
		Namespace:       os.Getenv("K8S_NAMESPACE"), // Empty means all namespaces
		ResyncPeriod:    30 * time.Minute,
	}

	grapher, err := k8sgrapher.NewK8sGrapher(config)
	if err != nil {
		logger.Fatal("Failed to create K8sGrapher", zap.Error(err))
	}

	// Start grapher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := grapher.Start(ctx); err != nil {
		logger.Fatal("Failed to start K8sGrapher", zap.Error(err))
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop the grapher
	grapher.Stop()

	// Wait for shutdown or timeout
	select {
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded")
	default:
		logger.Info("Graceful shutdown completed")
	}
}

// createKubeClient creates a Kubernetes client
func createKubeClient() (kubernetes.Interface, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kube config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %w", err)
	}

	return client, nil
}

// createNeo4jDriver creates a Neo4j driver
func createNeo4jDriver() (neo4j.DriverWithContext, error) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "neo4j://localhost:7687"
	}

	username := os.Getenv("NEO4J_USERNAME")
	if username == "" {
		username = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("NEO4J_PASSWORD environment variable is required")
	}

	driver, err := neo4j.NewDriverWithContext(
		uri,
		neo4j.BasicAuth(username, password, ""),
		func(c *neo4j.Config) {
			c.MaxConnectionPoolSize = 50
			c.MaxConnectionLifetime = 5 * time.Minute
			c.ConnectionAcquisitionTimeout = 30 * time.Second
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	return driver, nil
}
