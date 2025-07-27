package security

import "github.com/yairfalse/tapio/pkg/interfaces/logging"

// createTestLogger creates a simple logger for testing
func createTestLogger() *logging.Logger {
	config := logging.DefaultConfig()
	config.Output = "stdout"
	return logging.NewLogger(config)
}
