package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// collector implements the core.Collector interface
type collector struct {
	// Configuration
	config core.Config
	
	// Kubernetes client
	clientset  kubernetes.Interface
	restConfig *rest.Config
	
	// State management
	started atomic.Bool
	stopped atomic.Bool
	
	// Event processing
	eventChan chan domain.Event
	processor core.EventProcessor
	
	// Resource watchers
	watchers []core.ResourceWatcher
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	
	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		apiCalls        atomic.Uint64
		apiErrors       atomic.Uint64
		reconnectCount  atomic.Uint64
	}
	
	// Health tracking
	lastEventTime atomic.Value // time.Time
	connectedAt   atomic.Value // time.Time
	startTime     time.Time
	
	// Connection state
	connected     atomic.Bool
	clusterInfo   atomic.Value // core.ClusterInfo
}

// NewCollector creates a new Kubernetes collector
func NewCollector(config core.Config) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	c := &collector{
		config:    config,
		eventChan: make(chan domain.Event, config.EventBufferSize),
		startTime: time.Now(),
		processor: newEventProcessor(),
		watchers:  make([]core.ResourceWatcher, 0),
	}
	
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})
	
	return c, nil
}

// Start begins event collection
func (c *collector) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return fmt.Errorf("collector is disabled")
	}
	
	if c.started.Load() {
		return core.ErrAlreadyStarted
	}
	
	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)
	
	// Initialize Kubernetes client
	if err := c.initializeClient(); err != nil {
		return fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}
	
	// Get cluster information
	if err := c.fetchClusterInfo(); err != nil {
		// Non-fatal, log and continue
		c.clusterInfo.Store(core.ClusterInfo{
			Name:        "unknown",
			Version:     "unknown",
			ConnectedAt: time.Now(),
		})
	}
	
	// Mark as connected
	c.connected.Store(true)
	c.connectedAt.Store(time.Now())
	
	// Create watchers based on configuration
	if err := c.createWatchers(); err != nil {
		return fmt.Errorf("failed to create watchers: %w", err)
	}
	
	// Start watchers
	for _, watcher := range c.watchers {
		if err := watcher.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start watcher for %s: %w", watcher.ResourceType(), err)
		}
	}
	
	// Mark as started
	c.started.Store(true)
	
	// Start event processing
	c.wg.Add(1)
	go c.processEvents()
	
	// Start connection monitor
	c.wg.Add(1)
	go c.monitorConnection()
	
	return nil
}

// Stop gracefully stops the collector
func (c *collector) Stop() error {
	if !c.started.Load() {
		return core.ErrNotStarted
	}
	
	if c.stopped.Load() {
		return nil
	}
	
	// Mark as stopping
	c.stopped.Store(true)
	
	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}
	
	// Stop all watchers
	for _, watcher := range c.watchers {
		if err := watcher.Stop(); err != nil {
			// Log error but continue stopping other watchers
		}
	}
	
	// Wait for goroutines
	c.wg.Wait()
	
	// Close event channel
	close(c.eventChan)
	
	// Mark as disconnected
	c.connected.Store(false)
	
	return nil
}

// Events returns the event channel
func (c *collector) Events() <-chan domain.Event {
	return c.eventChan
}

// Health returns the current health status
func (c *collector) Health() core.Health {
	status := core.HealthStatusHealthy
	message := "Kubernetes collector is healthy"
	
	if !c.started.Load() {
		status = core.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if !c.connected.Load() {
		status = core.HealthStatusUnhealthy
		message = "Not connected to Kubernetes API"
	} else if c.stats.apiErrors.Load() > 100 {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("High API error count: %d", c.stats.apiErrors.Load())
	}
	
	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = core.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}
	
	clusterInfo := c.clusterInfo.Load().(core.ClusterInfo)
	
	return core.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.apiErrors.Load(),
		Connected:       c.connected.Load(),
		ClusterInfo:     clusterInfo,
		Metrics: map[string]float64{
			"watchers_active":    float64(len(c.watchers)),
			"api_calls_total":    float64(c.stats.apiCalls.Load()),
			"api_errors":         float64(c.stats.apiErrors.Load()),
			"reconnect_count":    float64(c.stats.reconnectCount.Load()),
			"events_per_second":  c.getEventsPerSecond(),
		},
	}
}

// Statistics returns runtime statistics
func (c *collector) Statistics() core.Statistics {
	resourcesWatched := make(map[string]int)
	if c.config.WatchPods {
		resourcesWatched["pods"] = 1
	}
	if c.config.WatchNodes {
		resourcesWatched["nodes"] = 1
	}
	if c.config.WatchServices {
		resourcesWatched["services"] = 1
	}
	if c.config.WatchDeployments {
		resourcesWatched["deployments"] = 1
	}
	if c.config.WatchEvents {
		resourcesWatched["events"] = 1
	}
	if c.config.WatchConfigMaps {
		resourcesWatched["configmaps"] = 1
	}
	if c.config.WatchSecrets {
		resourcesWatched["secrets"] = 1
	}
	
	uptime := time.Since(c.startTime)
	
	return core.Statistics{
		StartTime:        c.startTime,
		EventsCollected:  c.stats.eventsCollected.Load(),
		EventsDropped:    c.stats.eventsDropped.Load(),
		ResourcesWatched: resourcesWatched,
		WatchersActive:   len(c.watchers),
		APICallsTotal:    c.stats.apiCalls.Load(),
		APIErrors:        c.stats.apiErrors.Load(),
		ReconnectCount:   c.stats.reconnectCount.Load(),
		Custom: map[string]interface{}{
			"uptime_seconds":    uptime.Seconds(),
			"events_per_second": c.getEventsPerSecond(),
			"connected":         c.connected.Load(),
		},
	}
}

// Configure updates the collector configuration
func (c *collector) Configure(config core.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	
	c.config = config
	
	// If running, restart with new configuration
	if c.started.Load() && !c.stopped.Load() {
		// Note: In a production implementation, this would gracefully
		// reconfigure the watchers without full restart
	}
	
	return nil
}

// initializeClient initializes the Kubernetes client
func (c *collector) initializeClient() error {
	var err error
	
	if c.config.InCluster {
		// Use in-cluster configuration
		c.restConfig, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to create in-cluster config: %w", err)
		}
	} else {
		// Use kubeconfig file
		c.restConfig, err = clientcmd.BuildConfigFromFlags("", c.config.KubeConfig)
		if err != nil {
			return fmt.Errorf("failed to create config from kubeconfig: %w", err)
		}
	}
	
	// Create clientset
	c.clientset, err = kubernetes.NewForConfig(c.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}
	
	// Test connection
	c.stats.apiCalls.Add(1)
	_, err = c.clientset.Discovery().ServerVersion()
	if err != nil {
		c.stats.apiErrors.Add(1)
		return fmt.Errorf("failed to connect to API server: %w", err)
	}
	
	return nil
}

// fetchClusterInfo retrieves cluster information
func (c *collector) fetchClusterInfo() error {
	c.stats.apiCalls.Add(1)
	version, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		c.stats.apiErrors.Add(1)
		return err
	}
	
	info := core.ClusterInfo{
		Name:         c.restConfig.Host,
		Version:      version.String(),
		Platform:     version.Platform,
		ConnectedAt:  time.Now(),
		APIServerURL: c.restConfig.Host,
	}
	
	c.clusterInfo.Store(info)
	return nil
}

// createWatchers creates resource watchers based on configuration
func (c *collector) createWatchers() error {
	if c.config.WatchPods {
		watcher := newPodWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchNodes {
		watcher := newNodeWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchServices {
		watcher := newServiceWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchDeployments {
		watcher := newDeploymentWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchEvents {
		watcher := newEventWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchConfigMaps {
		watcher := newConfigMapWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if c.config.WatchSecrets {
		watcher := newSecretWatcher(c.clientset, c.config)
		c.watchers = append(c.watchers, watcher)
	}
	
	if len(c.watchers) == 0 {
		return fmt.Errorf("no watchers configured")
	}
	
	return nil
}

// processEvents processes events from all watchers
func (c *collector) processEvents() {
	defer c.wg.Done()
	
	// Create a merged channel for all watcher events
	cases := make([]<-chan core.RawEvent, len(c.watchers))
	for i, watcher := range c.watchers {
		cases[i] = watcher.Events()
	}
	
	for {
		select {
		case <-c.ctx.Done():
			return
			
		default:
			// Check each watcher's event channel
			for _, ch := range cases {
				select {
				case rawEvent, ok := <-ch:
					if !ok {
						continue
					}
					
					// Process the raw event
					event, err := c.processor.ProcessEvent(c.ctx, rawEvent)
					if err != nil {
						c.stats.apiErrors.Add(1)
						continue
					}
					
					// Update stats
					c.stats.eventsCollected.Add(1)
					c.lastEventTime.Store(time.Now())
					
					// Try to send event
					select {
					case c.eventChan <- event:
						// Event sent successfully
					default:
						// Buffer full, drop event
						c.stats.eventsDropped.Add(1)
					}
					
				default:
					// No event available, continue
				}
			}
			
			// Small sleep to prevent busy loop
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// monitorConnection monitors the API connection and reconnects if needed
func (c *collector) monitorConnection() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			return
			
		case <-ticker.C:
			// Check connection health
			c.stats.apiCalls.Add(1)
			_, err := c.clientset.Discovery().ServerVersion()
			if err != nil {
				c.stats.apiErrors.Add(1)
				c.connected.Store(false)
				
				// Attempt reconnection
				if err := c.reconnect(); err != nil {
					// Reconnection failed, will retry on next tick
				}
			} else {
				// Connection is healthy
				if !c.connected.Load() {
					c.connected.Store(true)
					c.connectedAt.Store(time.Now())
				}
			}
		}
	}
}

// reconnect attempts to reconnect to the Kubernetes API
func (c *collector) reconnect() error {
	c.stats.reconnectCount.Add(1)
	
	if err := c.initializeClient(); err != nil {
		return err
	}
	
	// Recreate watchers
	for _, watcher := range c.watchers {
		watcher.Stop()
	}
	c.watchers = c.watchers[:0]
	
	if err := c.createWatchers(); err != nil {
		return err
	}
	
	// Restart watchers
	for _, watcher := range c.watchers {
		if err := watcher.Start(c.ctx); err != nil {
			return err
		}
	}
	
	c.connected.Store(true)
	c.connectedAt.Store(time.Now())
	
	return nil
}

// Helper methods

func (c *collector) getEventsPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / uptime
}