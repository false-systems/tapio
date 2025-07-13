package systemd

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
)

// DBusConnection manages the D-Bus connection with high-performance monitoring
type DBusConnection struct {
	// Connection management
	conn        *dbus.Conn
	connMu      sync.RWMutex
	isConnected atomic.Bool
	
	// Configuration
	config DBusConfig
	
	// Signal handling with efficient buffering
	signalChan    chan *dbus.Signal
	signalBuffer  *SignalBuffer
	subscriptions map[string]bool
	subMu         sync.RWMutex
	
	// Connection health
	lastHealthCheck time.Time
	healthCheckMu   sync.Mutex
	reconnectCount  uint32
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	
	// Metrics
	signalsReceived  uint64
	signalsProcessed uint64
	signalsDropped   uint64
	
	// Error tracking
	lastError     error
	lastErrorTime time.Time
	errorMu       sync.RWMutex
}

// DBusConfig configures the D-Bus connection
type DBusConfig struct {
	// Connection settings
	SystemBus         bool
	ConnectTimeout    time.Duration
	HealthCheckPeriod time.Duration
	
	// Signal handling
	SignalBufferSize    int
	SignalBatchSize     int
	SignalFlushInterval time.Duration
	
	// Reconnection
	ReconnectInterval    time.Duration
	MaxReconnectAttempts int
	BackoffMultiplier    float64
	
	// Performance
	MaxConcurrentSignals int
	SignalWorkers        int
}

// DefaultDBusConfig returns optimized configuration for high throughput
func DefaultDBusConfig() DBusConfig {
	return DBusConfig{
		SystemBus:            true,
		ConnectTimeout:       5 * time.Second,
		HealthCheckPeriod:    10 * time.Second,
		SignalBufferSize:     10000,
		SignalBatchSize:      100,
		SignalFlushInterval:  100 * time.Millisecond,
		ReconnectInterval:    1 * time.Second,
		MaxReconnectAttempts: 5,
		BackoffMultiplier:    1.5,
		MaxConcurrentSignals: 1000,
		SignalWorkers:        4,
	}
}

// SignalBuffer provides efficient buffering for D-Bus signals
type SignalBuffer struct {
	buffer    []SignalBatch
	current   *SignalBatch
	mu        sync.Mutex
	batchSize int
	flushCh   chan struct{}
}

// SignalBatch groups signals for efficient processing
type SignalBatch struct {
	Signals   []*dbus.Signal
	Timestamp time.Time
	Count     int
}

// NewDBusConnection creates a new high-performance D-Bus connection
func NewDBusConnection(config DBusConfig) (*DBusConnection, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	dbc := &DBusConnection{
		config:        config,
		signalChan:    make(chan *dbus.Signal, config.SignalBufferSize),
		subscriptions: make(map[string]bool),
		ctx:           ctx,
		cancel:        cancel,
		signalBuffer: &SignalBuffer{
			batchSize: config.SignalBatchSize,
			flushCh:   make(chan struct{}, 1),
		},
	}
	
	// Initialize connection
	if err := dbc.connect(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to establish D-Bus connection: %w", err)
	}
	
	// Start background workers
	dbc.wg.Add(2)
	go dbc.healthMonitor()
	go dbc.signalProcessor()
	
	return dbc, nil
}

// Connect establishes or re-establishes the D-Bus connection
func (dbc *DBusConnection) connect() error {
	dbc.connMu.Lock()
	defer dbc.connMu.Unlock()
	
	// Close existing connection if any
	if dbc.conn != nil {
		dbc.conn.Close()
	}
	
	// Create new connection
	var conn *dbus.Conn
	var err error
	
	ctx, cancel := context.WithTimeout(context.Background(), dbc.config.ConnectTimeout)
	defer cancel()
	
	done := make(chan struct{})
	go func() {
		if dbc.config.SystemBus {
			conn, err = dbus.ConnectSystemBus()
		} else {
			conn, err = dbus.ConnectSessionBus()
		}
		close(done)
	}()
	
	select {
	case <-ctx.Done():
		return fmt.Errorf("connection timeout")
	case <-done:
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}
	
	// Verify connection
	if err := dbc.verifyConnection(conn); err != nil {
		conn.Close()
		return fmt.Errorf("connection verification failed: %w", err)
	}
	
	dbc.conn = conn
	dbc.isConnected.Store(true)
	
	// Re-subscribe to signals
	if err := dbc.resubscribe(); err != nil {
		return fmt.Errorf("failed to resubscribe: %w", err)
	}
	
	// Setup signal channel
	conn.Signal(dbc.signalChan)
	
	return nil
}

// verifyConnection tests the D-Bus connection
func (dbc *DBusConnection) verifyConnection(conn *dbus.Conn) error {
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	
	var version string
	err := obj.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.systemd1.Manager", "Version").Store(&version)
	
	if err != nil {
		return fmt.Errorf("systemd not accessible: %w", err)
	}
	
	return nil
}

// Subscribe adds a signal subscription
func (dbc *DBusConnection) Subscribe(rule string) error {
	dbc.subMu.Lock()
	defer dbc.subMu.Unlock()
	
	if dbc.subscriptions[rule] {
		return nil // Already subscribed
	}
	
	dbc.connMu.RLock()
	conn := dbc.conn
	dbc.connMu.RUnlock()
	
	if conn == nil {
		return fmt.Errorf("not connected")
	}
	
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule).Err; err != nil {
		return fmt.Errorf("failed to add match rule: %w", err)
	}
	
	dbc.subscriptions[rule] = true
	return nil
}

// SubscribeToSystemdSignals subscribes to all relevant systemd signals
func (dbc *DBusConnection) SubscribeToSystemdSignals() error {
	rules := []string{
		// Unit lifecycle events
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='UnitNew'",
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='UnitRemoved'",
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='JobNew'",
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='JobRemoved'",
		
		// Property changes on any systemd unit
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path_namespace='/org/freedesktop/systemd1/unit'",
		
		// Manager state changes
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='Reloading'",
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='StartupFinished'",
	}
	
	for _, rule := range rules {
		if err := dbc.Subscribe(rule); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", rule, err)
		}
	}
	
	return nil
}

// resubscribe re-establishes all subscriptions after reconnection
func (dbc *DBusConnection) resubscribe() error {
	dbc.subMu.RLock()
	rules := make([]string, 0, len(dbc.subscriptions))
	for rule := range dbc.subscriptions {
		rules = append(rules, rule)
	}
	dbc.subMu.RUnlock()
	
	// Clear subscriptions as we'll re-add them
	dbc.subMu.Lock()
	dbc.subscriptions = make(map[string]bool)
	dbc.subMu.Unlock()
	
	// Re-subscribe to all rules
	for _, rule := range rules {
		if err := dbc.Subscribe(rule); err != nil {
			return err
		}
	}
	
	return nil
}

// GetConnection returns the current D-Bus connection if healthy
func (dbc *DBusConnection) GetConnection() (*dbus.Conn, error) {
	if !dbc.isConnected.Load() {
		return nil, fmt.Errorf("not connected")
	}
	
	dbc.connMu.RLock()
	conn := dbc.conn
	dbc.connMu.RUnlock()
	
	if conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	
	return conn, nil
}

// GetSignals returns the signal channel for receiving D-Bus signals
func (dbc *DBusConnection) GetSignals() <-chan *dbus.Signal {
	return dbc.signalChan
}

// healthMonitor continuously monitors connection health
func (dbc *DBusConnection) healthMonitor() {
	defer dbc.wg.Done()
	
	ticker := time.NewTicker(dbc.config.HealthCheckPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-dbc.ctx.Done():
			return
		case <-ticker.C:
			if !dbc.checkHealth() {
				dbc.handleDisconnection()
			}
		}
	}
}

// checkHealth verifies the D-Bus connection is healthy
func (dbc *DBusConnection) checkHealth() bool {
	if !dbc.isConnected.Load() {
		return false
	}
	
	dbc.connMu.RLock()
	conn := dbc.conn
	dbc.connMu.RUnlock()
	
	if conn == nil {
		return false
	}
	
	// Quick health check with timeout
	ctx, cancel := context.WithTimeout(dbc.ctx, 2*time.Second)
	defer cancel()
	
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.Peer.Ping", 0)
	
	if call.Err != nil {
		dbc.recordError(fmt.Errorf("health check failed: %w", call.Err))
		return false
	}
	
	dbc.healthCheckMu.Lock()
	dbc.lastHealthCheck = time.Now()
	dbc.healthCheckMu.Unlock()
	
	return true
}

// handleDisconnection manages reconnection with exponential backoff
func (dbc *DBusConnection) handleDisconnection() {
	dbc.isConnected.Store(false)
	
	// Attempt reconnection with backoff
	backoff := dbc.config.ReconnectInterval
	attempts := 0
	
	for attempts < dbc.config.MaxReconnectAttempts {
		select {
		case <-dbc.ctx.Done():
			return
		case <-time.After(backoff):
			attempts++
			
			if err := dbc.connect(); err == nil {
				atomic.AddUint32(&dbc.reconnectCount, 1)
				return // Successfully reconnected
			}
			
			// Exponential backoff
			backoff = time.Duration(float64(backoff) * dbc.config.BackoffMultiplier)
		}
	}
	
	dbc.recordError(fmt.Errorf("max reconnection attempts reached"))
}

// signalProcessor efficiently processes incoming signals in batches
func (dbc *DBusConnection) signalProcessor() {
	defer dbc.wg.Done()
	
	// Start signal workers
	workerWg := sync.WaitGroup{}
	for i := 0; i < dbc.config.SignalWorkers; i++ {
		workerWg.Add(1)
		go dbc.signalWorker(&workerWg)
	}
	
	// Batch signals for efficient processing
	ticker := time.NewTicker(dbc.config.SignalFlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-dbc.ctx.Done():
			workerWg.Wait()
			return
			
		case signal := <-dbc.signalChan:
			if signal != nil {
				atomic.AddUint64(&dbc.signalsReceived, 1)
				dbc.signalBuffer.Add(signal)
			}
			
		case <-ticker.C:
			dbc.signalBuffer.Flush()
			
		case <-dbc.signalBuffer.flushCh:
			dbc.signalBuffer.Flush()
		}
	}
}

// signalWorker processes signal batches
func (dbc *DBusConnection) signalWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	
	for {
		select {
		case <-dbc.ctx.Done():
			return
		default:
			// Process signals from buffer
			// Implementation depends on specific signal handling needs
		}
	}
}

// recordError records the last error
func (dbc *DBusConnection) recordError(err error) {
	dbc.errorMu.Lock()
	dbc.lastError = err
	dbc.lastErrorTime = time.Now()
	dbc.errorMu.Unlock()
}

// GetStats returns connection statistics
func (dbc *DBusConnection) GetStats() DBusStats {
	return DBusStats{
		IsConnected:      dbc.isConnected.Load(),
		SignalsReceived:  atomic.LoadUint64(&dbc.signalsReceived),
		SignalsProcessed: atomic.LoadUint64(&dbc.signalsProcessed),
		SignalsDropped:   atomic.LoadUint64(&dbc.signalsDropped),
		ReconnectCount:   atomic.LoadUint32(&dbc.reconnectCount),
		LastHealthCheck:  dbc.lastHealthCheck,
		LastError:        dbc.lastError,
		LastErrorTime:    dbc.lastErrorTime,
	}
}

// DBusStats contains D-Bus connection statistics
type DBusStats struct {
	IsConnected      bool
	SignalsReceived  uint64
	SignalsProcessed uint64
	SignalsDropped   uint64
	ReconnectCount   uint32
	LastHealthCheck  time.Time
	LastError        error
	LastErrorTime    time.Time
}

// Close cleanly shuts down the D-Bus connection
func (dbc *DBusConnection) Close() error {
	dbc.cancel()
	dbc.wg.Wait()
	
	dbc.connMu.Lock()
	defer dbc.connMu.Unlock()
	
	if dbc.conn != nil {
		dbc.conn.Close()
		dbc.conn = nil
	}
	
	dbc.isConnected.Store(false)
	close(dbc.signalChan)
	
	return nil
}

// Signal buffer methods

// Add adds a signal to the buffer
func (sb *SignalBuffer) Add(signal *dbus.Signal) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if sb.current == nil {
		sb.current = &SignalBatch{
			Signals:   make([]*dbus.Signal, 0, sb.batchSize),
			Timestamp: time.Now(),
		}
	}
	
	sb.current.Signals = append(sb.current.Signals, signal)
	sb.current.Count++
	
	if sb.current.Count >= sb.batchSize {
		sb.buffer = append(sb.buffer, *sb.current)
		sb.current = nil
		
		// Trigger flush
		select {
		case sb.flushCh <- struct{}{}:
		default:
		}
	}
}

// Flush flushes the current batch
func (sb *SignalBuffer) Flush() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if sb.current != nil && sb.current.Count > 0 {
		sb.buffer = append(sb.buffer, *sb.current)
		sb.current = nil
	}
}

// GetBatch retrieves and removes a batch from the buffer
func (sb *SignalBuffer) GetBatch() *SignalBatch {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if len(sb.buffer) == 0 {
		return nil
	}
	
	batch := sb.buffer[0]
	sb.buffer = sb.buffer[1:]
	
	return &batch
}