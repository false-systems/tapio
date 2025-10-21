package k8scontext

import (
	"context"
	"time"
)

// enqueueEvent adds an event to the buffer for async processing
// If buffer is full, drops the oldest event (non-blocking)
func (s *Service) enqueueEvent(event func() error) {
	select {
	case s.eventBuffer <- event:
		// Event buffered successfully
	default:
		// Buffer full, drop oldest and add new
		select {
		case <-s.eventBuffer:
			// Dropped oldest
		default:
		}
		// Try to add new event
		select {
		case s.eventBuffer <- event:
		default:
			// Still couldn't add, drop it
			s.logger.Warn().
				Int("buffer_size", cap(s.eventBuffer)).
				Msg("event buffer full, dropping event")
		}
	}
}

// processEvents runs in a goroutine and processes buffered events with retry
func (s *Service) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.eventBuffer:
			if !ok {
				// Channel closed
				return
			}
			s.executeWithRetry(ctx, event)
		}
	}
}

// executeWithRetry executes an event with exponential backoff retry
func (s *Service) executeWithRetry(ctx context.Context, event func() error) {
	var err error
	backoff := s.config.RetryInterval

	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		// Try to execute
		err = event()
		if err == nil {
			// Success
			return
		}

		// Failed, check if we should retry
		if attempt < s.config.MaxRetries {
			// Wait with exponential backoff
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2 // Exponential backoff
			}
		}
	}

	// Max retries exceeded
	s.logger.Error().
		Int("max_retries", s.config.MaxRetries).
		Err(err).
		Msg("event failed after max retries")
}
