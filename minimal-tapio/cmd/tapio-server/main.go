package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yairfalse/tapio-minimal/pkg/domain"
	"github.com/yairfalse/tapio-minimal/pkg/server"
)

func main() {
	var (
		port     = flag.Int("port", 8080, "Server port")
		logLevel = flag.String("log-level", "info", "Log level")
	)
	
	flag.Parse()
	
	// Create configuration
	config := &domain.Config{
		ServerPort: *port,
		LogLevel:   *logLevel,
	}
	
	// Create server
	srv := server.NewServer(config)
	
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		log.Println("Shutdown signal received")
		cancel()
	}()
	
	// Start server
	log.Printf("Starting Tapio server on port %d", *port)
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
	
	log.Println("Server stopped")
}