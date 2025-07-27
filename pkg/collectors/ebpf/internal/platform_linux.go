//go:build linux
// +build linux

package internal

import (
	"context"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// newPlatformImpl creates a Linux-specific implementation
func newPlatformImpl() (platformImpl, error) {
	return &linuxImpl{
		eventChan: make(chan core.RawEvent, 1000),
		links:     make([]link.Link, 0),
	}, nil
}

// linuxImpl provides Linux-specific eBPF functionality
type linuxImpl struct {
	config core.Config

	// Event processing
	perfReader *perf.Reader
	eventChan  chan core.RawEvent

	// State
	ctx    context.Context
	cancel context.CancelFunc
	links  []link.Link
}

func (l *linuxImpl) init(config core.Config) error {
	l.config = config

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	return nil
}

func (l *linuxImpl) start(ctx context.Context) error {
	l.ctx, l.cancel = context.WithCancel(ctx)

	// For now, just start without loading programs
	// This is a stub implementation to fix the import cycle
	go func() {
		<-l.ctx.Done()
		close(l.eventChan)
	}()

	return nil
}

func (l *linuxImpl) stop() error {
	if l.cancel != nil {
		l.cancel()
	}

	// Close all links
	for _, link := range l.links {
		link.Close()
	}

	// Close perf reader
	if l.perfReader != nil {
		l.perfReader.Close()
	}

	return nil
}

func (l *linuxImpl) events() <-chan core.RawEvent {
	return l.eventChan
}

func (l *linuxImpl) programsLoaded() int {
	// Stub implementation
	return 0
}

func (l *linuxImpl) mapsCreated() int {
	// Stub implementation
	return 0
}
