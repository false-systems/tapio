package shutdown

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Handler manages graceful shutdown
type Handler struct {
	mu          sync.Mutex
	shutdownFns []func(context.Context) error
	timeout     time.Duration
	signals     []os.Signal
	done        chan struct{}
}

// NewHandler creates a new shutdown handler
func NewHandler(timeout time.Duration) *Handler {
	return &Handler{
		shutdownFns: make([]func(context.Context) error, 0),
		timeout:     timeout,
		signals: []os.Signal{
			os.Interrupt,    // Ctrl+C
			syscall.SIGTERM, // Kubernetes pod termination
			syscall.SIGQUIT, // Quit
		},
		done: make(chan struct{}),
	}
}

// Register registers a cleanup function
func (h *Handler) Register(name string, fn func(context.Context) error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	wrappedFn := func(ctx context.Context) error {
		fmt.Printf("🧹 Cleaning up %s...\n", name)
		start := time.Now()
		err := fn(ctx)
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("⚠️  Failed to cleanup %s: %v (took %v)\n", name, err, duration)
			return err
		}
		fmt.Printf("✅ Cleaned up %s (took %v)\n", name, duration)
		return nil
	}

	h.shutdownFns = append(h.shutdownFns, wrappedFn)
}

// Start begins listening for shutdown signals
func (h *Handler) Start() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, h.signals...)

	go func() {
		sig := <-sigChan
		fmt.Printf("\n🛑 Received signal: %s\n", sig)
		fmt.Println("🔄 Starting graceful shutdown...")

		h.executeShutdown()
		close(h.done)
	}()
}

// Wait blocks until shutdown is complete
func (h *Handler) Wait() {
	<-h.done
}

// Shutdown triggers manual shutdown
func (h *Handler) Shutdown() {
	fmt.Println("🔄 Starting graceful shutdown...")
	h.executeShutdown()
	close(h.done)
}

// executeShutdown runs all cleanup functions
func (h *Handler) executeShutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	// Track completion
	start := time.Now()
	errors := 0

	// Copy functions to avoid holding lock
	h.mu.Lock()
	fns := make([]func(context.Context) error, len(h.shutdownFns))
	copy(fns, h.shutdownFns)
	h.mu.Unlock()

	// Execute cleanup functions in reverse order (LIFO)
	for i := len(fns) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			fmt.Printf("⚠️  Shutdown timeout exceeded, some cleanup may be incomplete\n")
			break
		}

		if err := fns[i](ctx); err != nil {
			errors++
		}
	}

	duration := time.Since(start)
	if errors > 0 {
		fmt.Printf("⚠️  Shutdown completed with %d errors (took %v)\n", errors, duration)
	} else {
		fmt.Printf("✅ Graceful shutdown completed (took %v)\n", duration)
	}
}

// GracefulShutdown is a convenience function for simple use cases
func GracefulShutdown(timeout time.Duration, cleanupFns ...func() error) {
	handler := NewHandler(timeout)

	// Register cleanup functions
	for i, fn := range cleanupFns {
		fnCopy := fn // Capture loop variable
		handler.Register(fmt.Sprintf("cleanup-%d", i+1), func(ctx context.Context) error {
			return fnCopy()
		})
	}

	handler.Start()
	handler.Wait()
}

// Context creates a context that is cancelled on shutdown signals
func Context() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		fmt.Println("\n🛑 Shutting down...")
		cancel()
	}()

	return ctx, cancel
}

// RegisterGlobalHandler sets up a global shutdown handler
var globalHandler *Handler
var globalOnce sync.Once

// RegisterGlobal registers a cleanup function with the global handler
func RegisterGlobal(name string, fn func(context.Context) error) {
	globalOnce.Do(func() {
		globalHandler = NewHandler(30 * time.Second)
		globalHandler.Start()
	})

	globalHandler.Register(name, fn)
}

// WaitForShutdown waits for the global shutdown handler
func WaitForShutdown() {
	if globalHandler != nil {
		globalHandler.Wait()
	}
}
