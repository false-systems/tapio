package test

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/internal/runtime"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestObserver is a minimal observer for testing ObserverRuntime.
// It implements runtime.EventProcessor interface.
type TestObserver struct {
	name string
}

// NewTestObserver creates a new test observer.
func NewTestObserver(name string) (*TestObserver, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return &TestObserver{
		name: name,
	}, nil
}

// Name returns the observer name.
func (o *TestObserver) Name() string {
	return o.name
}

// Setup initializes the observer (no-op for test observer).
func (o *TestObserver) Setup(ctx context.Context, cfg runtime.Config) error {
	return nil
}

// Process processes a raw event and returns a domain event (no-op for test observer).
func (o *TestObserver) Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
	return nil, nil
}

// Teardown cleans up resources (no-op for test observer).
func (o *TestObserver) Teardown(ctx context.Context) error {
	return nil
}
