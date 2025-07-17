package internal

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
)

// platformImpl defines the interface that platform-specific implementations must satisfy
type platformImpl interface {
	Init(config core.Config) error
	Start(ctx context.Context) error
	Stop() error
	Reader() core.LogReader
	IsOpen() bool
	BootID() string
	MachineID() string
	CurrentCursor() string
}

// newPlatformImpl creates a new platform-specific implementation
// This function is implemented in platform-specific files
func newPlatformImpl() (platformImpl, error) {
	return createPlatformImpl()
}