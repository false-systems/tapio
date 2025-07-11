package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

type WatchManager struct {
	client      kubernetes.Interface
	streams     map[string]*WatchStream
	eventMerger *EventMerger
	reconnector *Reconnector
	mu          sync.RWMutex
	closed      bool
	config      *WatchConfig
}

type WatchConfig struct {
	ReconnectInterval time.Duration
	ReconnectTimeout  time.Duration
	EventBufferSize   int
	MaxRetries        int
	BackoffDuration   time.Duration
	BackoffFactor     float64
	HeartbeatInterval time.Duration
}

type WatchStream struct {
	id                  string
	namespace           string
	resource            string
	watcher             watch.Interface
	eventChan           chan watch.Event
	errorChan           chan error
	stopChan            chan struct{}
	lastResourceVersion string
	mu                  sync.RWMutex
	active              bool
	reconnectCount      int
	lastHeartbeat       time.Time
}

type EventMerger struct {
	eventQueue   *EventQueue
	deduplicator *EventDeduplicator
	sequencer    *EventSequencer
	mu           sync.RWMutex
}

type EventQueue struct {
	events   []watch.Event
	capacity int
	mu       sync.Mutex
}

type EventDeduplicator struct {
	recentEvents map[string]time.Time
	window       time.Duration
	mu           sync.RWMutex
}

type EventSequencer struct {
	sequences map[string]uint64
	mu        sync.RWMutex
}

type Reconnector struct {
	watchManager *WatchManager
	config       *WatchConfig
	stopChan     chan struct{}
}

type WatchEvent struct {
	Type     watch.EventType
	Object   runtime.Object
	Sequence uint64
	Source   string
	Time     time.Time
}

func DefaultWatchConfig() *WatchConfig {
	return &WatchConfig{
		ReconnectInterval: 5 * time.Second,
		ReconnectTimeout:  30 * time.Second,
		EventBufferSize:   1000,
		MaxRetries:        10,
		BackoffDuration:   100 * time.Millisecond,
		BackoffFactor:     2.0,
		HeartbeatInterval: 30 * time.Second,
	}
}

func NewWatchManager(client kubernetes.Interface, config *WatchConfig) *WatchManager {
	if config == nil {
		config = DefaultWatchConfig()
	}

	wm := &WatchManager{
		client:  client,
		streams: make(map[string]*WatchStream),
		eventMerger: &EventMerger{
			eventQueue: &EventQueue{
				events:   make([]watch.Event, 0, config.EventBufferSize),
				capacity: config.EventBufferSize,
			},
			deduplicator: &EventDeduplicator{
				recentEvents: make(map[string]time.Time),
				window:       5 * time.Second,
			},
			sequencer: &EventSequencer{
				sequences: make(map[string]uint64),
			},
		},
		config: config,
	}

	wm.reconnector = &Reconnector{
		watchManager: wm,
		config:       config,
		stopChan:     make(chan struct{}),
	}

	go wm.reconnector.Start()
	go wm.eventMerger.deduplicator.cleanupLoop()

	return wm
}

func (wm *WatchManager) WatchPods(ctx context.Context, namespace string) (<-chan WatchEvent, error) {
	return wm.WatchResource(ctx, namespace, "pods")
}

func (wm *WatchManager) WatchServices(ctx context.Context, namespace string) (<-chan WatchEvent, error) {
	return wm.WatchResource(ctx, namespace, "services")
}

func (wm *WatchManager) WatchDeployments(ctx context.Context, namespace string) (<-chan WatchEvent, error) {
	return wm.WatchResource(ctx, namespace, "deployments")
}

func (wm *WatchManager) WatchResource(ctx context.Context, namespace, resource string) (<-chan WatchEvent, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.closed {
		return nil, fmt.Errorf("watch manager is closed")
	}

	streamID := fmt.Sprintf("%s/%s", namespace, resource)
	if stream, exists := wm.streams[streamID]; exists && stream.active {
		return wm.createEventChannel(stream), nil
	}

	stream, err := wm.createWatchStream(ctx, namespace, resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create watch stream: %w", err)
	}

	wm.streams[streamID] = stream
	go wm.manageStream(stream)

	return wm.createEventChannel(stream), nil
}

func (wm *WatchManager) createWatchStream(ctx context.Context, namespace, resource string) (*WatchStream, error) {
	var watcher watch.Interface
	var err error

	listOptions := metav1.ListOptions{
		Watch: true,
	}

	switch resource {
	case "pods":
		watcher, err = wm.client.CoreV1().Pods(namespace).Watch(ctx, listOptions)
	case "services":
		watcher, err = wm.client.CoreV1().Services(namespace).Watch(ctx, listOptions)
	case "deployments":
		watcher, err = wm.client.AppsV1().Deployments(namespace).Watch(ctx, listOptions)
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to start watch for %s: %w", resource, err)
	}

	stream := &WatchStream{
		id:            fmt.Sprintf("%s/%s", namespace, resource),
		namespace:     namespace,
		resource:      resource,
		watcher:       watcher,
		eventChan:     make(chan watch.Event, wm.config.EventBufferSize),
		errorChan:     make(chan error, 10),
		stopChan:      make(chan struct{}),
		active:        true,
		lastHeartbeat: time.Now(),
	}

	return stream, nil
}

func (wm *WatchManager) createEventChannel(stream *WatchStream) <-chan WatchEvent {
	eventChan := make(chan WatchEvent, wm.config.EventBufferSize)

	go func() {
		defer close(eventChan)

		for {
			select {
			case event, ok := <-stream.eventChan:
				if !ok {
					return
				}

				if !wm.eventMerger.deduplicator.IsDuplicate(event) {
					watchEvent := WatchEvent{
						Type:     event.Type,
						Object:   event.Object,
						Sequence: wm.eventMerger.sequencer.NextSequence(stream.id),
						Source:   stream.id,
						Time:     time.Now(),
					}

					select {
					case eventChan <- watchEvent:
					default:
						// Buffer full, drop oldest events
					}
				}

			case <-stream.stopChan:
				return
			}
		}
	}()

	return eventChan
}

func (wm *WatchManager) manageStream(stream *WatchStream) {
	defer func() {
		stream.mu.Lock()
		stream.active = false
		stream.mu.Unlock()

		if stream.watcher != nil {
			stream.watcher.Stop()
		}
		close(stream.eventChan)
	}()

	for {
		select {
		case event, ok := <-stream.watcher.ResultChan():
			if !ok {
				stream.errorChan <- fmt.Errorf("watch stream closed unexpectedly")
				return
			}

			stream.mu.Lock()
			stream.lastHeartbeat = time.Now()
			if event.Object != nil {
				if accessor, err := meta.Accessor(event.Object); err == nil {
					stream.lastResourceVersion = accessor.GetResourceVersion()
				}
			}
			stream.mu.Unlock()

			select {
			case stream.eventChan <- event:
			case <-stream.stopChan:
				return
			default:
				// Event channel full, apply backpressure
				wm.eventMerger.eventQueue.AddWithBackpressure(event)
			}

		case err := <-stream.errorChan:
			if wm.shouldReconnect(err) {
				stream.mu.Lock()
				stream.reconnectCount++
				stream.mu.Unlock()
				continue
			}
			return

		case <-stream.stopChan:
			return
		}
	}
}

func (wm *WatchManager) shouldReconnect(err error) bool {
	if errors.IsTimeout(err) {
		return true
	}
	if errors.IsServerTimeout(err) {
		return true
	}
	if errors.IsServiceUnavailable(err) {
		return true
	}
	return false
}

func (wm *WatchManager) StopWatch(namespace, resource string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	streamID := fmt.Sprintf("%s/%s", namespace, resource)
	if stream, exists := wm.streams[streamID]; exists {
		close(stream.stopChan)
		delete(wm.streams, streamID)
	}

	return nil
}

func (wm *WatchManager) Close() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.closed {
		return nil
	}

	wm.closed = true
	close(wm.reconnector.stopChan)

	for _, stream := range wm.streams {
		close(stream.stopChan)
	}

	return nil
}

func (r *Reconnector) Start() {
	ticker := time.NewTicker(r.config.ReconnectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.checkAndReconnect()
		case <-r.stopChan:
			return
		}
	}
}

func (r *Reconnector) checkAndReconnect() {
	r.watchManager.mu.RLock()
	streams := make([]*WatchStream, 0, len(r.watchManager.streams))
	for _, stream := range r.watchManager.streams {
		streams = append(streams, stream)
	}
	r.watchManager.mu.RUnlock()

	for _, stream := range streams {
		stream.mu.RLock()
		needsReconnect := !stream.active ||
			time.Since(stream.lastHeartbeat) > r.config.HeartbeatInterval ||
			stream.reconnectCount > 0
		stream.mu.RUnlock()

		if needsReconnect {
			r.reconnectStream(stream)
		}
	}
}

func (r *Reconnector) reconnectStream(stream *WatchStream) {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	if stream.reconnectCount >= r.config.MaxRetries {
		return
	}

	backoff := wait.Backoff{
		Duration: r.config.BackoffDuration,
		Factor:   r.config.BackoffFactor,
		Steps:    r.config.MaxRetries,
		Cap:      r.config.ReconnectTimeout,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), r.config.ReconnectTimeout)
		defer cancel()

		newStream, err := r.watchManager.createWatchStream(ctx, stream.namespace, stream.resource)
		if err != nil {
			return false, nil // Retry
		}

		if stream.watcher != nil {
			stream.watcher.Stop()
		}

		stream.watcher = newStream.watcher
		stream.active = true
		stream.reconnectCount = 0
		stream.lastHeartbeat = time.Now()

		return true, nil
	})

	if err != nil {
		stream.active = false
	}
}

func (eq *EventQueue) AddWithBackpressure(event watch.Event) {
	eq.mu.Lock()
	defer eq.mu.Unlock()

	if len(eq.events) >= eq.capacity {
		// Remove oldest event
		eq.events = eq.events[1:]
	}

	eq.events = append(eq.events, event)
}

func (ed *EventDeduplicator) IsDuplicate(event watch.Event) bool {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	key := ed.generateKey(event)
	now := time.Now()

	if lastSeen, exists := ed.recentEvents[key]; exists {
		if now.Sub(lastSeen) < ed.window {
			return true
		}
	}

	ed.recentEvents[key] = now
	return false
}

func (ed *EventDeduplicator) generateKey(event watch.Event) string {
	if event.Object == nil {
		return string(event.Type)
	}

	if accessor, err := meta.Accessor(event.Object); err == nil {
		return fmt.Sprintf("%s:%s:%s:%s",
			event.Type,
			accessor.GetNamespace(),
			accessor.GetName(),
			accessor.GetResourceVersion())
	}

	return fmt.Sprintf("%s:%v", event.Type, event.Object)
}

func (ed *EventDeduplicator) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ed.cleanup()
	}
}

func (ed *EventDeduplicator) cleanup() {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	now := time.Now()
	for key, lastSeen := range ed.recentEvents {
		if now.Sub(lastSeen) > ed.window*2 {
			delete(ed.recentEvents, key)
		}
	}
}

func (es *EventSequencer) NextSequence(source string) uint64 {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.sequences[source]++
	return es.sequences[source]
}
