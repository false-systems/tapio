package context

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Service watches Kubernetes resources and populates NATS KV with metadata
type Service struct {
	k8sClient *kubernetes.Clientset
	kv        nats.KeyValue
	podWatch  watch.Interface
	svcWatch  watch.Interface
}

// NewService creates a Context Service
func NewService(cfg Config) (*Service, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	js, err := cfg.NATSConn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	kv, err := js.KeyValue(cfg.KVBucket)
	if err != nil {
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket: cfg.KVBucket,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get/create KV bucket: %w", err)
		}
	}

	return &Service{
		k8sClient: clientset,
		kv:        kv,
	}, nil
}

// Start begins watching Kubernetes resources
func (s *Service) Start(ctx context.Context) error {
	if err := s.watchPods(ctx); err != nil {
		return fmt.Errorf("failed to start pod watcher: %w", err)
	}

	if err := s.watchServices(ctx); err != nil {
		return fmt.Errorf("failed to start service watcher: %w", err)
	}

	<-ctx.Done()
	return ctx.Err()
}

// Stop gracefully stops the service
func (s *Service) Stop() error {
	if s.podWatch != nil {
		s.podWatch.Stop()
	}
	if s.svcWatch != nil {
		s.svcWatch.Stop()
	}
	return nil
}
