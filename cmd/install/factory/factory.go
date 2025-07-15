package factory

import (
	"fmt"

	"github.com/yairfalse/tapio/cmd/install/common"
	"github.com/yairfalse/tapio/cmd/install/installer"
)

// Factory creates platform-specific installers
type Factory struct {
	platform common.PlatformInfo
}

// NewFactory creates a new installer factory
func NewFactory(platform common.PlatformInfo) *Factory {
	return &Factory{
		platform: platform,
	}
}

// Create creates an installer for the given strategy
func (f *Factory) Create(strategy installer.InstallStrategy) (installer.Installer, error) {
	switch strategy {
	case installer.StrategyBinary:
		return installer.NewBinaryInstaller(f.platform), nil

	case installer.StrategyDocker:
		return installer.NewDockerInstaller(f.platform), nil

	case installer.StrategyKubernetes:
		return installer.NewKubernetesInstaller(f.platform), nil

	default:
		return nil, fmt.Errorf("unknown installation strategy: %s", strategy)
	}
}

// GetAvailableStrategies returns supported strategies for the platform
func (f *Factory) GetAvailableStrategies() []installer.InstallStrategy {
	strategies := []installer.InstallStrategy{
		installer.StrategyBinary,
	}

	// Docker is available on all platforms
	strategies = append(strategies, installer.StrategyDocker)

	// Kubernetes is typically available on Linux and with Docker Desktop
	if f.platform.OS == "linux" || f.platform.OS == "darwin" || f.platform.OS == "windows" {
		strategies = append(strategies, installer.StrategyKubernetes)
	}

	return strategies
}

// IsSupported checks if the platform is supported
func (f *Factory) IsSupported() bool {
	supportedPlatforms := map[string][]string{
		"linux":   {"amd64", "arm64", "arm"},
		"darwin":  {"amd64", "arm64"},
		"windows": {"amd64"},
	}

	arches, ok := supportedPlatforms[f.platform.OS]
	if !ok {
		return false
	}

	for _, a := range arches {
		if a == f.platform.Arch {
			return true
		}
	}

	return false
}