//go:build !linux
// +build !linux

package dns

// startPlatform starts fallback DNS problem simulation
func (o *Observer) startPlatform() error {
	return o.startFallback()
}

// stopPlatform stops the fallback mode
func (o *Observer) stopPlatform() {
	o.logger.Info("Stopping DNS observer fallback mode")
}
