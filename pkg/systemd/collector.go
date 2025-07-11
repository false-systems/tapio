package systemd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Collector manages D-Bus connection to systemd
type Collector struct {
	conn   *dbus.Conn
	config *CollectorConfig
	
	// Connection management
	mutex           sync.RWMutex
	isConnected     bool
	reconnectAttempts int
	lastReconnect   time.Time
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// CollectorConfig configures the systemd collector
type CollectorConfig struct {
	SystemBusTimeout     time.Duration
	ReconnectInterval    time.Duration
	MaxReconnectAttempts int
}

// DefaultCollectorConfig returns the default configuration
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		SystemBusTimeout:     10 * time.Second,
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 3,
	}
}

// NewCollector creates a new systemd collector
func NewCollector(config *CollectorConfig) (*Collector, error) {
	if config == nil {
		config = DefaultCollectorConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	collector := &Collector{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
	
	// Initial connection
	if err := collector.connect(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to systemd: %w", err)
	}
	
	return collector, nil
}

// IsAvailable checks if systemd is available
func (c *Collector) IsAvailable() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isConnected
}

// Start begins systemd monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Start connection monitoring
	go c.monitorConnection()
	return nil
}

// Stop stops systemd monitoring
func (c *Collector) Stop() error {
	c.cancel()
	
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.isConnected = false
	}
	
	return nil
}

// GetConnection returns the D-Bus connection
func (c *Collector) GetConnection() *dbus.Conn {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	if c.isConnected {
		return c.conn
	}
	return nil
}

// connect establishes a connection to systemd via D-Bus
func (c *Collector) connect() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	
	// Test the connection by calling a simple method
	var version string
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, 
		"org.freedesktop.systemd1.Manager", "Version").Store(&version)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to test systemd connection: %w", err)
	}
	
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.conn != nil {
		c.conn.Close()
	}
	
	c.conn = conn
	c.isConnected = true
	c.reconnectAttempts = 0
	
	return nil
}

// monitorConnection monitors the D-Bus connection and reconnects if needed
func (c *Collector) monitorConnection() {
	ticker := time.NewTicker(c.config.ReconnectInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.isConnectionHealthy() {
				c.handleConnectionLoss()
			}
		}
	}
}

// isConnectionHealthy checks if the D-Bus connection is healthy
func (c *Collector) isConnectionHealthy() bool {
	c.mutex.RLock()
	conn := c.conn
	connected := c.isConnected
	c.mutex.RUnlock()
	
	if !connected || conn == nil {
		return false
	}
	
	// Test the connection with a quick call
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.systemd1.Manager", "Version")
	
	return call.Err == nil
}

// handleConnectionLoss handles D-Bus connection loss
func (c *Collector) handleConnectionLoss() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if !c.isConnected {
		return // Already handling
	}
	
	c.isConnected = false
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	
	// Attempt reconnection
	go c.attemptReconnection()
}

// attemptReconnection attempts to reconnect to systemd
func (c *Collector) attemptReconnection() {
	for c.reconnectAttempts < c.config.MaxReconnectAttempts {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.config.ReconnectInterval):
			c.reconnectAttempts++
			
			if err := c.connect(); err == nil {
				c.lastReconnect = time.Now()
				return // Successfully reconnected
			}
		}
	}
	
	// Max attempts reached, give up
	c.mutex.Lock()
	c.isConnected = false
	c.mutex.Unlock()
}

// GetUnitProperty gets a property of a systemd unit
func (c *Collector) GetUnitProperty(unitName, property string) (interface{}, error) {
	conn := c.GetConnection()
	if conn == nil {
		return nil, fmt.Errorf("not connected to systemd")
	}
	
	unitPath, err := c.getUnitPath(unitName)
	if err != nil {
		return nil, fmt.Errorf("failed to get unit path: %w", err)
	}
	
	obj := conn.Object("org.freedesktop.systemd1", unitPath)
	variant, err := obj.GetProperty("org.freedesktop.systemd1.Unit." + property)
	if err != nil {
		return nil, fmt.Errorf("failed to get property %s: %w", property, err)
	}
	
	return variant.Value(), nil
}

// GetServiceProperty gets a property of a systemd service
func (c *Collector) GetServiceProperty(serviceName, property string) (interface{}, error) {
	conn := c.GetConnection()
	if conn == nil {
		return nil, fmt.Errorf("not connected to systemd")
	}
	
	unitPath, err := c.getUnitPath(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get unit path: %w", err)
	}
	
	obj := conn.Object("org.freedesktop.systemd1", unitPath)
	variant, err := obj.GetProperty("org.freedesktop.systemd1.Service." + property)
	if err != nil {
		return nil, fmt.Errorf("failed to get property %s: %w", property, err)
	}
	
	return variant.Value(), nil
}

// ListUnits lists all loaded units
func (c *Collector) ListUnits() ([]UnitStatus, error) {
	conn := c.GetConnection()
	if conn == nil {
		return nil, fmt.Errorf("not connected to systemd")
	}
	
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := obj.Call("org.freedesktop.systemd1.Manager.ListUnits", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("failed to list units: %w", call.Err)
	}
	
	var unitStatusList [][]interface{}
	if err := call.Store(&unitStatusList); err != nil {
		return nil, fmt.Errorf("failed to parse unit list: %w", err)
	}
	
	var units []UnitStatus
	for _, unitData := range unitStatusList {
		if len(unitData) >= 10 {
			units = append(units, UnitStatus{
				Name:        unitData[0].(string),
				Description: unitData[1].(string),
				LoadState:   unitData[2].(string),
				ActiveState: unitData[3].(string),
				SubState:    unitData[4].(string),
				Following:   unitData[5].(string),
				UnitPath:    dbus.ObjectPath(unitData[6].(string)),
				JobID:       unitData[7].(uint32),
				JobType:     unitData[8].(string),
				JobPath:     dbus.ObjectPath(unitData[9].(string)),
			})
		}
	}
	
	return units, nil
}

// GetUnitStatus gets the status of a specific unit
func (c *Collector) GetUnitStatus(unitName string) (*UnitStatus, error) {
	units, err := c.ListUnits()
	if err != nil {
		return nil, err
	}
	
	for _, unit := range units {
		if unit.Name == unitName {
			return &unit, nil
		}
	}
	
	return nil, fmt.Errorf("unit %s not found", unitName)
}

// getUnitPath gets the D-Bus object path for a unit
func (c *Collector) getUnitPath(unitName string) (dbus.ObjectPath, error) {
	conn := c.GetConnection()
	if conn == nil {
		return "", fmt.Errorf("not connected to systemd")
	}
	
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := obj.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unitName)
	if call.Err != nil {
		return "", fmt.Errorf("failed to get unit path: %w", call.Err)
	}
	
	var unitPath dbus.ObjectPath
	if err := call.Store(&unitPath); err != nil {
		return "", fmt.Errorf("failed to parse unit path: %w", err)
	}
	
	return unitPath, nil
}

// GetManagerProperty gets a property of the systemd manager
func (c *Collector) GetManagerProperty(property string) (interface{}, error) {
	conn := c.GetConnection()
	if conn == nil {
		return nil, fmt.Errorf("not connected to systemd")
	}
	
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	variant, err := obj.GetProperty("org.freedesktop.systemd1.Manager." + property)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager property %s: %w", property, err)
	}
	
	return variant.Value(), nil
}

// SubscribeToSignals subscribes to systemd D-Bus signals
func (c *Collector) SubscribeToSignals() (<-chan *dbus.Signal, error) {
	conn := c.GetConnection()
	if conn == nil {
		return nil, fmt.Errorf("not connected to systemd")
	}
	
	// Subscribe to unit state changes
	rules := []string{
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='UnitNew'",
		"type='signal',interface='org.freedesktop.systemd1.Manager',member='UnitRemoved'",
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
	}
	
	for _, rule := range rules {
		if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule).Err; err != nil {
			return nil, fmt.Errorf("failed to add match rule: %w", err)
		}
	}
	
	signalChan := make(chan *dbus.Signal, 100)
	conn.Signal(signalChan)
	
	return signalChan, nil
}

// UnitStatus represents the status of a systemd unit
type UnitStatus struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
	Following   string
	UnitPath    dbus.ObjectPath
	JobID       uint32
	JobType     string
	JobPath     dbus.ObjectPath
}

// GetStatistics returns collector statistics
func (c *Collector) GetStatistics() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return map[string]interface{}{
		"is_connected":       c.isConnected,
		"reconnect_attempts": c.reconnectAttempts,
		"last_reconnect":     c.lastReconnect,
	}
}