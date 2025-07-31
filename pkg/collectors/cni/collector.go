package cni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yairfalse/tapio/pkg/collectors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Collector implements minimal CNI collection with auto-detection
type Collector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// CNI detection
	detectedCNI string
	strategy    CNIStrategy

	// K8s client for detection
	k8sClient *kubernetes.Clientset

	// File watchers
	watcher *fsnotify.Watcher

	// eBPF components (Linux only)
	ebpfCollection interface{}
	ebpfReader     interface{}
	ebpfLinks      []interface{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	healthy bool
}

// CNIStrategy defines how to collect for specific CNI plugins
type CNIStrategy interface {
	GetLogPaths() []string
	GetWatchPaths() []string
	GetName() string
}

// NewCollector creates a new CNI collector
func NewCollector(config collectors.CollectorConfig) (*Collector, error) {
	// Initialize K8s client for CNI detection
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		// Try out-of-cluster config for development
		k8sConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %w", err)
		}
	}

	k8sClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &Collector{
		config:    config,
		events:    make(chan collectors.RawEvent, config.BufferSize),
		k8sClient: k8sClient,
		healthy:   true,
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return "cni"
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return errors.New("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Detect CNI type
	if err := c.detectCNI(); err != nil {
		return fmt.Errorf("failed to detect CNI: %w", err)
	}

	// Initialize file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	c.watcher = watcher

	// Add paths to watch based on strategy
	for _, path := range c.strategy.GetWatchPaths() {
		if err := c.watcher.Add(path); err != nil {
			// Log but don't fail - path might not exist yet
			continue
		}
	}

	// Initialize eBPF if on Linux
	if runtime.GOOS == "linux" {
		// Try enhanced network policy monitoring first
		if err := c.EnhanceWithNetworkPolicy(); err != nil {
			// Fall back to basic eBPF
			if err := c.initEBPF(); err != nil {
				// Log but don't fail - eBPF is optional enhancement
				// In production, would use proper logging
			} else {
				c.wg.Add(1)
				go c.readEBPFEvents()
			}
		}
	}

	// Start collection goroutines
	c.wg.Add(2)
	go c.watchFiles()
	go c.watchLogs()

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.watcher != nil {
		c.watcher.Close()
	}

	// Cleanup eBPF resources
	if runtime.GOOS == "linux" {
		c.cleanupEBPF()
	}

	c.wg.Wait()
	close(c.events)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// detectCNI detects the CNI plugin from K8s DaemonSets
func (c *Collector) detectCNI() error {
	// Check DaemonSets in kube-system namespace
	daemonsets, err := c.k8sClient.AppsV1().DaemonSets("kube-system").List(c.ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list daemonsets: %w", err)
	}

	// Check for known CNI DaemonSets
	for _, ds := range daemonsets.Items {
		name := strings.ToLower(ds.Name)

		// Check containers for CNI images
		for _, container := range ds.Spec.Template.Spec.Containers {
			image := strings.ToLower(container.Image)

			// Calico detection
			if strings.Contains(name, "calico") || strings.Contains(image, "calico") {
				c.detectedCNI = "calico"
				c.strategy = &CalicoStrategy{}
				return nil
			}

			// Cilium detection
			if strings.Contains(name, "cilium") || strings.Contains(image, "cilium") {
				c.detectedCNI = "cilium"
				c.strategy = &CiliumStrategy{}
				return nil
			}

			// Flannel detection
			if strings.Contains(name, "flannel") || strings.Contains(image, "flannel") {
				c.detectedCNI = "flannel"
				c.strategy = &FlannelStrategy{}
				return nil
			}
		}
	}

	// Default to generic strategy
	c.detectedCNI = "generic"
	c.strategy = &GenericStrategy{}
	return nil
}

// watchFiles monitors CNI-related files
func (c *Collector) watchFiles() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}

			// Create raw event for file changes
			data, _ := json.Marshal(map[string]interface{}{
				"event": event.String(),
				"file":  event.Name,
				"op":    event.Op.String(),
			})

			rawEvent := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "cni",
				Data:      data,
				Metadata: map[string]string{
					"source":     "file_watch",
					"cni_plugin": c.detectedCNI,
					"file":       event.Name,
				},
			}

			select {
			case c.events <- rawEvent:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop event
			}
		}
	}
}

// watchLogs monitors CNI logs
func (c *Collector) watchLogs() {
	defer c.wg.Done()

	// Simple log monitoring - in production would use tail or journal
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return

		case <-ticker.C:
			// Emit heartbeat event
			data, _ := json.Marshal(map[string]interface{}{
				"type":       "heartbeat",
				"cni_plugin": c.detectedCNI,
				"timestamp":  time.Now().Unix(),
			})

			rawEvent := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "cni",
				Data:      data,
				Metadata: map[string]string{
					"source":     "heartbeat",
					"cni_plugin": c.detectedCNI,
				},
			}

			select {
			case c.events <- rawEvent:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop event
			}
		}
	}
}

// CNI Strategy implementations

// CalicoStrategy for Calico CNI
type CalicoStrategy struct{}

func (s *CalicoStrategy) GetName() string { return "calico" }

func (s *CalicoStrategy) GetLogPaths() []string {
	return []string{
		"/var/log/calico/cni/",
		"/var/log/calico/felix.log",
	}
}

func (s *CalicoStrategy) GetWatchPaths() []string {
	return []string{
		"/etc/cni/net.d/",
		"/var/lib/calico/",
	}
}

// CiliumStrategy for Cilium CNI
type CiliumStrategy struct{}

func (s *CiliumStrategy) GetName() string { return "cilium" }

func (s *CiliumStrategy) GetLogPaths() []string {
	return []string{
		"/var/run/cilium/cilium.log",
		"/var/log/cilium-cni.log",
	}
}

func (s *CiliumStrategy) GetWatchPaths() []string {
	return []string{
		"/etc/cni/net.d/",
		"/var/run/cilium/",
	}
}

// FlannelStrategy for Flannel CNI
type FlannelStrategy struct{}

func (s *FlannelStrategy) GetName() string { return "flannel" }

func (s *FlannelStrategy) GetLogPaths() []string {
	return []string{
		"/var/log/flanneld.log",
	}
}

func (s *FlannelStrategy) GetWatchPaths() []string {
	return []string{
		"/etc/cni/net.d/",
		"/run/flannel/",
	}
}

// GenericStrategy for unknown CNIs
type GenericStrategy struct{}

func (s *GenericStrategy) GetName() string { return "generic" }

func (s *GenericStrategy) GetLogPaths() []string {
	return []string{
		"/var/log/cni/",
	}
}

func (s *GenericStrategy) GetWatchPaths() []string {
	return []string{
		"/etc/cni/net.d/",
		"/var/lib/cni/",
	}
}
