package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// AutoConfigDetector detects common configuration scenarios
type AutoConfigDetector struct {
	detectedEnvironment string
	recommendations     []string
	warnings            []string
}

// NewAutoConfigDetector creates a new auto-configuration detector
func NewAutoConfigDetector() *AutoConfigDetector {
	return &AutoConfigDetector{
		recommendations: make([]string, 0),
		warnings:        make([]string, 0),
	}
}

// DetectAndConfigure automatically detects the environment and configures appropriately
func (d *AutoConfigDetector) DetectAndConfigure() (*Config, error) {
	// Start with zero config
	config := ZeroConfig()

	// Detect environment type
	d.detectEnvironment()

	// Apply environment-specific configurations
	switch d.detectedEnvironment {
	case "development":
		d.configureForDevelopment(config)
	case "ci":
		d.configureForCI(config)
	case "production":
		d.configureForProduction(config)
	case "minikube":
		d.configureForMinikube(config)
	case "kind":
		d.configureForKind(config)
	case "docker-desktop":
		d.configureForDockerDesktop(config)
	default:
		d.configureDefault(config)
	}

	// Detect and configure Kubernetes settings
	d.configureKubernetes(config)

	// Detect and configure eBPF settings
	d.configureEBPF(config)

	// Detect and configure resource limits
	d.configureResources(config)

	return config, nil
}

// detectEnvironment determines what kind of environment we're running in
func (d *AutoConfigDetector) detectEnvironment() {
	// Check for CI environments first
	if d.isCIEnvironment() {
		d.detectedEnvironment = "ci"
		return
	}

	// Check for local development clusters
	if d.isMinikube() {
		d.detectedEnvironment = "minikube"
		return
	}

	if d.isKind() {
		d.detectedEnvironment = "kind"
		return
	}

	if d.isDockerDesktop() {
		d.detectedEnvironment = "docker-desktop"
		return
	}

	// Check if we're in a container (likely production)
	if d.isInContainer() {
		d.detectedEnvironment = "production"
		return
	}

	// Default to development
	d.detectedEnvironment = "development"
}

// isCIEnvironment detects common CI environments
func (d *AutoConfigDetector) isCIEnvironment() bool {
	ciEnvVars := []string{
		"CI", "CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL",
		"TRAVIS", "CIRCLECI", "BUILDKITE",
	}

	for _, envVar := range ciEnvVars {
		if os.Getenv(envVar) != "" {
			d.recommendations = append(d.recommendations, fmt.Sprintf("Detected CI environment (%s)", envVar))
			return true
		}
	}

	return false
}

// isMinikube detects if we're running against minikube
func (d *AutoConfigDetector) isMinikube() bool {
	// Check kubectl context
	if context := getCurrentKubeContext(); context != "" {
		if strings.Contains(context, "minikube") {
			d.recommendations = append(d.recommendations, "Detected minikube cluster")
			return true
		}
	}

	// Check for minikube binary
	if findExecutable("minikube") != "" {
		d.recommendations = append(d.recommendations, "Minikube available")
		return true
	}

	return false
}

// isKind detects if we're running against kind
func (d *AutoConfigDetector) isKind() bool {
	// Check kubectl context
	if context := getCurrentKubeContext(); context != "" {
		if strings.Contains(context, "kind") {
			d.recommendations = append(d.recommendations, "Detected kind cluster")
			return true
		}
	}

	// Check for kind binary
	if findExecutable("kind") != "" {
		d.recommendations = append(d.recommendations, "Kind available")
		return true
	}

	return false
}

// isDockerDesktop detects Docker Desktop Kubernetes
func (d *AutoConfigDetector) isDockerDesktop() bool {
	if context := getCurrentKubeContext(); context != "" {
		if strings.Contains(context, "docker-desktop") || strings.Contains(context, "docker-for-desktop") {
			d.recommendations = append(d.recommendations, "Detected Docker Desktop Kubernetes")
			return true
		}
	}

	return false
}

// isInContainer detects if we're running inside a container
func (d *AutoConfigDetector) isInContainer() bool {
	// Check for container indicators
	if _, err := os.Stat("/.dockerenv"); err == nil {
		d.recommendations = append(d.recommendations, "Running in Docker container")
		return true
	}

	// Check cgroup
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		if strings.Contains(string(data), "docker") || strings.Contains(string(data), "kubepods") {
			d.recommendations = append(d.recommendations, "Running in container environment")
			return true
		}
	}

	// Check for Kubernetes service account
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err == nil {
		d.recommendations = append(d.recommendations, "Running in Kubernetes pod")
		return true
	}

	return false
}

// getCurrentKubeContext gets the current Kubernetes context
func getCurrentKubeContext() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return ""
	}

	return rawConfig.CurrentContext
}

// Configuration methods for different environments

func (d *AutoConfigDetector) configureForDevelopment(config *Config) {
	config.LogLevel = "debug"
	config.Output.Color = true
	config.Output.Verbose = true
	config.Features.EnablePrediction = true
	config.Advanced.DebugMode = true

	// More relaxed resource limits for development
	config.Resources.MaxMemoryUsage = 1024
	config.Resources.MaxCPUPercent = 50

	d.recommendations = append(d.recommendations, "Configured for development environment")
}

func (d *AutoConfigDetector) configureForCI(config *Config) {
	config.LogLevel = "info"
	config.LogFormat = "json"
	config.Output.Color = false
	config.Output.Verbose = false
	config.Features.EnableEBPF = false // Usually not available in CI
	config.Features.EnableMetrics = false

	// Conservative resource limits for CI
	config.Resources.MaxMemoryUsage = 256
	config.Resources.MaxCPUPercent = 25
	config.UpdateInterval = 30 * time.Second // Less frequent updates

	d.recommendations = append(d.recommendations, "Configured for CI environment - eBPF disabled, conservative resources")
}

func (d *AutoConfigDetector) configureForProduction(config *Config) {
	config.LogLevel = "warn"
	config.LogFormat = "json"
	config.Features.EnableEBPF = true
	config.Features.EnableMetrics = true
	config.Features.EnableCorrelation = true
	config.Metrics.Enabled = true

	// Production resource limits
	config.Resources.MaxMemoryUsage = 2048
	config.Resources.MaxCPUPercent = 75

	d.recommendations = append(d.recommendations, "Configured for production environment")
}

func (d *AutoConfigDetector) configureForMinikube(config *Config) {
	config.LogLevel = "info"
	config.Features.EnableEBPF = false // Often not available in minikube
	config.Output.Color = true

	// Minikube-friendly resource limits
	config.Resources.MaxMemoryUsage = 512
	config.Resources.MaxCPUPercent = 40

	// Minikube-specific namespace filtering
	config.Kubernetes.Namespaces.Exclude = append(config.Kubernetes.Namespaces.Exclude,
		"kube-system", "kube-public", "kube-node-lease")

	d.recommendations = append(d.recommendations, "Configured for minikube - eBPF disabled, moderate resources")
}

func (d *AutoConfigDetector) configureForKind(config *Config) {
	config.LogLevel = "info"
	config.Features.EnableEBPF = false // eBPF usually not available in kind
	config.Output.Color = true

	// Kind-friendly resource limits
	config.Resources.MaxMemoryUsage = 512
	config.Resources.MaxCPUPercent = 30

	d.recommendations = append(d.recommendations, "Configured for kind cluster - eBPF disabled")
}

func (d *AutoConfigDetector) configureForDockerDesktop(config *Config) {
	config.LogLevel = "info"
	config.Features.EnableEBPF = false // eBPF not available on Docker Desktop
	config.Output.Color = true

	// Docker Desktop resource limits
	config.Resources.MaxMemoryUsage = 512
	config.Resources.MaxCPUPercent = 35

	d.recommendations = append(d.recommendations, "Configured for Docker Desktop - eBPF disabled")
}

func (d *AutoConfigDetector) configureDefault(config *Config) {
	// Keep zero-config defaults
	d.recommendations = append(d.recommendations, "Using default configuration")
}

// configureKubernetes auto-detects and configures Kubernetes settings
func (d *AutoConfigDetector) configureKubernetes(config *Config) {
	// Check if we're running in-cluster
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err == nil {
		config.Kubernetes.InCluster = true
		d.recommendations = append(d.recommendations, "Detected in-cluster mode")
		return
	}

	// Try to detect kubeconfig
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	if kubeconfigPath != "" && fileExists(kubeconfigPath) {
		config.Kubernetes.Kubeconfig = kubeconfigPath
		d.recommendations = append(d.recommendations, fmt.Sprintf("Using kubeconfig: %s", kubeconfigPath))

		// Test connectivity
		if d.testKubernetesConnectivity(kubeconfigPath) {
			d.recommendations = append(d.recommendations, "Kubernetes connectivity verified")
		} else {
			d.warnings = append(d.warnings, "Kubernetes connectivity test failed")
		}
	} else {
		d.warnings = append(d.warnings, "No kubeconfig found - Kubernetes features may not work")
	}
}

// testKubernetesConnectivity tests if we can connect to Kubernetes
func (d *AutoConfigDetector) testKubernetesConnectivity(kubeconfigPath string) bool {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return false
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = clientset.Discovery().ServerVersion()
	// Note: ServerVersion() doesn't take context in older k8s client versions
	_ = ctx
	return err == nil
}

// configureEBPF auto-detects and configures eBPF settings
func (d *AutoConfigDetector) configureEBPF(config *Config) {
	if runtime.GOOS != "linux" {
		config.Features.EnableEBPF = false
		d.warnings = append(d.warnings, fmt.Sprintf("eBPF not supported on %s", runtime.GOOS))
		return
	}

	// Check if eBPF is likely available
	if d.isEBPFAvailable() {
		config.Features.EnableEBPF = true
		d.recommendations = append(d.recommendations, "eBPF appears to be available")
	} else {
		config.Features.EnableEBPF = false
		d.warnings = append(d.warnings, "eBPF may not be available - requires Linux kernel 4.15+ and proper permissions")
	}
}

// isEBPFAvailable performs a basic check for eBPF availability
func (d *AutoConfigDetector) isEBPFAvailable() bool {
	// Check if running as root
	if os.Geteuid() == 0 {
		return true
	}

	// Check for bpf filesystem
	if _, err := os.Stat("/sys/fs/bpf"); err == nil {
		return true
	}

	// Check for common eBPF tools
	ebpfTools := []string{"bpftool", "bcc-tools"}
	for _, tool := range ebpfTools {
		if findExecutable(tool) != "" {
			return true
		}
	}

	return false
}

// configureResources auto-detects and configures resource limits
func (d *AutoConfigDetector) configureResources(config *Config) {
	// Adjust based on available system resources
	cpuCount := runtime.NumCPU()

	if cpuCount <= 2 {
		// Low-resource system
		config.Resources.MaxMemoryUsage = 256
		config.Resources.MaxCPUPercent = 20
		config.Resources.ParallelWorkers = 1
		d.recommendations = append(d.recommendations, "Configured for low-resource system")
	} else if cpuCount <= 4 {
		// Medium-resource system
		config.Resources.MaxMemoryUsage = 512
		config.Resources.MaxCPUPercent = 30
		config.Resources.ParallelWorkers = 2
		d.recommendations = append(d.recommendations, "Configured for medium-resource system")
	} else {
		// High-resource system
		config.Resources.MaxMemoryUsage = 1024
		config.Resources.MaxCPUPercent = 50
		config.Resources.ParallelWorkers = min(cpuCount/2, 8)
		d.recommendations = append(d.recommendations, "Configured for high-resource system")
	}
}

// GetRecommendations returns the auto-configuration recommendations
func (d *AutoConfigDetector) GetRecommendations() []string {
	return d.recommendations
}

// GetWarnings returns any warnings from auto-configuration
func (d *AutoConfigDetector) GetWarnings() []string {
	return d.warnings
}

// GetDetectedEnvironment returns the detected environment type
func (d *AutoConfigDetector) GetDetectedEnvironment() string {
	return d.detectedEnvironment
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
