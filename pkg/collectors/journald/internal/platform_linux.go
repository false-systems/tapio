//go:build linux

package internal

import (
	"github.com/yairfalse/tapio/pkg/collectors/journald/linux"
)

// createPlatformImpl creates the Linux-specific implementation
func createPlatformImpl() (platformImpl, error) {
	impl, err := linux.NewPlatformImpl()
	if err != nil {
		return nil, err
	}
	return impl, nil
}
