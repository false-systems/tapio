package internal

import (
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// eventStream implements domain.EventStream
type eventStream struct {
	eventChan chan domain.Event
	errorChan chan error
	closeChan chan struct{}
	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex
}

// newEventStream creates a new event stream
func newEventStream(bufferSize int) *eventStream {
	return &eventStream{
		eventChan: make(chan domain.Event, bufferSize),
		errorChan: make(chan error, 10),
		closeChan: make(chan struct{}),
	}
}

// Events implements domain.EventStream
func (s *eventStream) Events() <-chan domain.Event {
	return s.eventChan
}

// Errors implements domain.EventStream
func (s *eventStream) Errors() <-chan error {
	return s.errorChan
}

// Close implements domain.EventStream
func (s *eventStream) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		close(s.closeChan)
		close(s.eventChan)
		close(s.errorChan)
	})
	return nil
}

// sendEvent sends an event to the stream
func (s *eventStream) sendEvent(event domain.Event) bool {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return false
	}
	s.mu.RUnlock()

	select {
	case s.eventChan <- event:
		return true
	case <-s.closeChan:
		return false
	default:
		// Channel full, send error
		select {
		case s.errorChan <- fmt.Errorf("event buffer full"):
		default:
			// Error channel also full, drop error
		}
		return false
	}
}

// sendError sends an error to the stream
func (s *eventStream) sendError(err error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	select {
	case s.errorChan <- err:
	case <-s.closeChan:
	default:
		// Error channel full, drop error
	}
}

// isClosed returns true if the stream is closed
func (s *eventStream) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}
