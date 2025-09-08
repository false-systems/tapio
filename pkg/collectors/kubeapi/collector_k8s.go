//go:build linux || darwin || windows
// +build linux darwin windows

package kubeapi

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// Platform-specific K8s client initialization
// This is essentially the same across all platforms since we're using the K8s API

// initK8sClient initializes the Kubernetes client (platform-specific)
func (c *Collector) initK8sClient() error {
	if c.clientset != nil {
		return nil // Already initialized
	}

	k8sConfig, err := getK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to get k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	c.clientset = clientset
	return nil
}

// startPlatformSpecific starts platform-specific components
func (c *Collector) startPlatformSpecific(ctx context.Context) error {
	// K8s API collector doesn't have platform-specific components
	// All functionality is through the K8s API which is platform-agnostic
	return nil
}

// stopPlatformSpecific stops platform-specific components
func (c *Collector) stopPlatformSpecific() {
	// K8s API collector doesn't have platform-specific components to stop
}
