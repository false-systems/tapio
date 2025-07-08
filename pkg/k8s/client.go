package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Clientset kubernetes.Interface
	Config    *rest.Config
}

func NewClient(kubeconfigPath string) (*Client, error) {
	config, err := getConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	client := &Client{
		Clientset: clientset,
		Config:    config,
	}

	// Test connectivity immediately
	if err := client.TestConnection(); err != nil {
		return nil, fmt.Errorf("kubernetes cluster not accessible: %w", err)
	}

	return client, nil
}

// TestConnection verifies the Kubernetes cluster is reachable
func (c *Client) TestConnection() error {
	// Try to get server version as a connectivity test
	_, err := c.Clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("cannot reach Kubernetes API server: %w", err)
	}

	return nil
}

func getConfig(kubeconfigPath string) (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// If a specific kubeconfig path is provided, use it
	if kubeconfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		return config, nil
	}

	// Use the same kubeconfig resolution as kubectl
	// This respects KUBECONFIG env var and uses ~/.kube/config as fallback
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	return kubeConfig.ClientConfig()
}
