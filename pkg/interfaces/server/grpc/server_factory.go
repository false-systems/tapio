package grpc

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// ServerFactory provides convenient server creation and management
type ServerFactory struct {
	logger *zap.Logger
	config ServerConfig
}

// NewServerFactory creates a new server factory
func NewServerFactory(logger *zap.Logger, config ServerConfig) *ServerFactory {
	return &ServerFactory{
		logger: logger,
		config: config,
	}
}

// CreateProductionServer creates a fully configured production server
func (f *ServerFactory) CreateProductionServer() (*TapioGRPCServer, error) {
	// Production configuration
	config := f.config
	config.EnableAuth = true
	config.EnableRateLimit = true
	config.EnableHealthCheck = true
	config.EnableReflection = false              // Disable reflection in production
	config.MaxRequestsPerSec = 50000             // Higher throughput for production
	config.MaxRecvMessageSize = 16 * 1024 * 1024 // 16MB
	config.MaxSendMessageSize = 16 * 1024 * 1024 // 16MB
	config.MaxConcurrentStream = 10000

	server, err := NewTapioGRPCServer(config, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create production server: %w", err)
	}

	// Initialize all services
	if err := server.InitializeServices(); err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	// Configure integrations
	if err := server.ConfigureIntegrations(); err != nil {
		return nil, fmt.Errorf("failed to configure integrations: %w", err)
	}

	f.logger.Info("Production Tapio gRPC server created successfully",
		zap.Int("port", config.Port),
		zap.Bool("auth", config.EnableAuth),
		zap.Int("max_rps", config.MaxRequestsPerSec),
	)

	return server, nil
}

// CreateDevelopmentServer creates a development-friendly server
func (f *ServerFactory) CreateDevelopmentServer() (*TapioGRPCServer, error) {
	// Development configuration
	config := f.config
	config.EnableAuth = false      // Disable auth for easier development
	config.EnableRateLimit = false // Disable rate limiting
	config.EnableHealthCheck = true
	config.EnableReflection = true // Enable reflection for debugging
	config.MaxRequestsPerSec = 1000
	config.MaxRecvMessageSize = 4 * 1024 * 1024 // 4MB
	config.MaxSendMessageSize = 4 * 1024 * 1024 // 4MB
	config.MaxConcurrentStream = 1000

	server, err := NewTapioGRPCServer(config, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create development server: %w", err)
	}

	// Initialize all services
	if err := server.InitializeServices(); err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	// Configure integrations
	if err := server.ConfigureIntegrations(); err != nil {
		return nil, fmt.Errorf("failed to configure integrations: %w", err)
	}

	f.logger.Info("Development Tapio gRPC server created successfully",
		zap.Int("port", config.Port),
		zap.Bool("reflection", config.EnableReflection),
		zap.Bool("auth_disabled", !config.EnableAuth),
	)

	return server, nil
}

// CreateTestServer creates a minimal server for testing
func (f *ServerFactory) CreateTestServer() (*TapioGRPCServer, error) {
	// Test configuration
	config := f.config
	config.Port = 0 // Use random port for tests
	config.EnableAuth = false
	config.EnableRateLimit = false
	config.EnableHealthCheck = true
	config.EnableReflection = true
	config.MaxRequestsPerSec = 100
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	config.IdleTimeout = 10 * time.Second

	server, err := NewTapioGRPCServer(config, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create test server: %w", err)
	}

	// Initialize all services
	if err := server.InitializeServices(); err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	f.logger.Info("Test Tapio gRPC server created successfully")
	return server, nil
}

// RunServerWithShutdown runs a server with graceful shutdown handling
func (f *ServerFactory) RunServerWithShutdown(server *TapioGRPCServer, shutdownTimeout time.Duration) error {
	// Start the server
	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	f.logger.Info("Server is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	sig := <-sigChan
	f.logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Perform graceful shutdown
	if err := server.Stop(ctx); err != nil {
		f.logger.Error("Failed to shutdown server gracefully", zap.Error(err))
		return err
	}

	f.logger.Info("Server shutdown completed")
	return nil
}

// QuickStart provides a one-line server startup for common scenarios
func QuickStartProductionServer(port int, logger *zap.Logger) error {
	config := DefaultServerConfig()
	config.Port = port

	factory := NewServerFactory(logger, config)
	server, err := factory.CreateProductionServer()
	if err != nil {
		return err
	}

	return factory.RunServerWithShutdown(server, 30*time.Second)
}

// QuickStartDevelopmentServer provides a one-line development server startup
func QuickStartDevelopmentServer(port int, logger *zap.Logger) error {
	config := DefaultServerConfig()
	config.Port = port

	factory := NewServerFactory(logger, config)
	server, err := factory.CreateDevelopmentServer()
	if err != nil {
		return err
	}

	return factory.RunServerWithShutdown(server, 10*time.Second)
}

// ServerHealthMonitor provides continuous health monitoring
type ServerHealthMonitor struct {
	server   *TapioGRPCServer
	logger   *zap.Logger
	interval time.Duration
	stopChan chan struct{}
}

// NewServerHealthMonitor creates a health monitor
func NewServerHealthMonitor(server *TapioGRPCServer, logger *zap.Logger, interval time.Duration) *ServerHealthMonitor {
	return &ServerHealthMonitor{
		server:   server,
		logger:   logger,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

// Start begins health monitoring
func (m *ServerHealthMonitor) Start() {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		m.logger.Info("Started server health monitoring", zap.Duration("interval", m.interval))

		for {
			select {
			case <-ticker.C:
				if err := m.server.HealthCheck(); err != nil {
					m.logger.Error("Server health check failed", zap.Error(err))
					// In production, this might trigger alerts or restart logic
				} else {
					m.logger.Debug("Server health check passed")
				}

			case <-m.stopChan:
				m.logger.Info("Stopped server health monitoring")
				return
			}
		}
	}()
}

// Stop stops health monitoring
func (m *ServerHealthMonitor) Stop() {
	close(m.stopChan)
}

// GetStats returns detailed server statistics
func (m *ServerHealthMonitor) GetStats() map[string]interface{} {
	return m.server.GetServiceStats()
}

// Example usage patterns:

// ExampleProductionSetup shows how to set up a production server
func ExampleProductionSetup() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Create and run production server
	if err := QuickStartProductionServer(8080, logger); err != nil {
		logger.Fatal("Failed to start production server", zap.Error(err))
	}
}

// ExampleDevelopmentSetup shows how to set up a development server
func ExampleDevelopmentSetup() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create and run development server
	if err := QuickStartDevelopmentServer(8080, logger); err != nil {
		logger.Fatal("Failed to start development server", zap.Error(err))
	}
}

// ExampleCustomSetup shows how to create a custom server configuration
func ExampleCustomSetup() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Custom configuration
	config := ServerConfig{
		Port:                9090,
		EnableReflection:    false,
		EnableHealthCheck:   true,
		EnableAuth:          true,
		EnableRateLimit:     true,
		MaxRequestsPerSec:   25000,
		ReadTimeout:         45 * time.Second,
		WriteTimeout:        45 * time.Second,
		IdleTimeout:         300 * time.Second,
		MaxRecvMessageSize:  32 * 1024 * 1024, // 32MB
		MaxSendMessageSize:  32 * 1024 * 1024, // 32MB
		MaxConcurrentStream: 5000,
	}

	factory := NewServerFactory(logger, config)
	server, err := factory.CreateProductionServer()
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}

	// Start health monitoring
	monitor := NewServerHealthMonitor(server, logger, 30*time.Second)
	monitor.Start()
	defer monitor.Stop()

	// Run with custom shutdown timeout
	if err := factory.RunServerWithShutdown(server, 60*time.Second); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}
