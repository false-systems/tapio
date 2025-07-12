package systemd

import (
	"context"
	"sync"
	"time"
)

// UnitWatcher watches systemd unit file changes
type UnitWatcher struct {
	config    *UnitWatcherConfig
	events    chan *UnitEvent
	ctx       context.Context
	cancel    context.CancelFunc
	isStarted bool
	mutex     sync.RWMutex
}

// UnitWatcherConfig configures the unit watcher
type UnitWatcherConfig struct {
	WatchedServices []string
	EventBufferSize int
}

// UnitEvent represents a unit file change event
type UnitEvent struct {
	Timestamp  time.Time
	UnitName   string
	EventType  int
	OldState   string
	NewState   string
	Reason     string
	Properties map[string]interface{}
}

// NewUnitWatcher creates a new unit watcher
func NewUnitWatcher(config *UnitWatcherConfig) (*UnitWatcher, error) {
	ctx, cancel := context.WithCancel(context.Background())

	return &UnitWatcher{
		config: config,
		events: make(chan *UnitEvent, config.EventBufferSize),
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Start begins unit watching
func (uw *UnitWatcher) Start(ctx context.Context) error {
	uw.mutex.Lock()
	defer uw.mutex.Unlock()
	uw.isStarted = true
	return nil
}

// Stop stops unit watching
func (uw *UnitWatcher) Stop() error {
	uw.mutex.Lock()
	defer uw.mutex.Unlock()
	if uw.isStarted {
		uw.cancel()
		close(uw.events)
		uw.isStarted = false
	}
	return nil
}

// GetEventChannel returns the event channel
func (uw *UnitWatcher) GetEventChannel() <-chan *UnitEvent {
	return uw.events
}

// GetUnitInfo returns unit information
func (uw *UnitWatcher) GetUnitInfo() (map[string]interface{}, error) {
	return map[string]interface{}{
		"watched_units": len(uw.config.WatchedServices),
	}, nil
}

// AddUnit adds a unit to watch
func (uw *UnitWatcher) AddUnit(unitName string) error {
	return nil
}

// RemoveUnit removes a unit from watching
func (uw *UnitWatcher) RemoveUnit(unitName string) error {
	return nil
}
