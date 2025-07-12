package platform

import (
	"fmt"
	
	"tapio/cmd/install/installer"
)

// Factory creates platform-specific installers
type factory struct {
	platform Info
}

// NewFactory creates a new installer factory
func NewFactory(platform Info) installer.Factory {
	return &factory{
		platform: platform,
	}
}

// Create creates an installer for the given strategy
func (f *factory) Create(strategy installer.InstallStrategy) (installer.Installer, error) {
	// Check if platform is supported
	detector := NewDetector()
	if !detector.IsSupported(f.platform.OS, f.platform.Arch) {
		return nil, &UnsupportedPlatformError{
			OS:   f.platform.OS,
			Arch: f.platform.Arch,
		}
	}
	
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
func (f *factory) GetAvailableStrategies() []installer.InstallStrategy {
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