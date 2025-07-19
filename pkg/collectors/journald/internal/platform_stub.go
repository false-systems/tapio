//go:build !linux

package internal

import (
	"github.com/yairfalse/tapio/pkg/collectors/journald/stub"
)

// createPlatformImpl creates the stub implementation for non-Linux platforms
func createPlatformImpl() (platformImpl, error) {
	impl, err := stub.NewPlatformImpl()
	if err != nil {
		return nil, err
	}
	return impl, nil
}
