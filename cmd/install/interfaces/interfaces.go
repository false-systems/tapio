package interfaces

import (
	"context"
)

// Validator performs post-installation validation
type Validator interface {
	// ValidateInstallation checks if installation is valid
	ValidateInstallation(ctx context.Context, installPath string) error

	// ValidateBinary checks binary integrity
	ValidateBinary(ctx context.Context, binaryPath string, checksum string) error

	// ValidateConnectivity checks network connectivity
	ValidateConnectivity(ctx context.Context, endpoints []string) error

	// ValidatePermissions checks file permissions
	ValidatePermissions(ctx context.Context, paths []string) error
}
