package context

import (
	"context"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// watchPods watches pod events and updates NATS KV
func (s *Service) watchPods(ctx context.Context) error {
	watcher, err := s.k8sClient.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch pods: %w", err)
	}
	s.podWatch = watcher

	go func() {
		for event := range watcher.ResultChan() {
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				if err := s.storePodMetadata(pod); err != nil {
					log.Printf("failed to store pod metadata: %v", err)
				}
			case watch.Deleted:
				if err := s.deletePodMetadata(pod); err != nil {
					log.Printf("failed to delete pod metadata: %v", err)
				}
			}
		}
	}()

	return nil
}

// watchServices watches service events and updates NATS KV
func (s *Service) watchServices(ctx context.Context) error {
	watcher, err := s.k8sClient.CoreV1().Services("").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch services: %w", err)
	}
	s.svcWatch = watcher

	go func() {
		for event := range watcher.ResultChan() {
			svc, ok := event.Object.(*corev1.Service)
			if !ok {
				continue
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				if err := s.storeServiceMetadata(svc); err != nil {
					log.Printf("failed to store service metadata: %v", err)
				}
			case watch.Deleted:
				if err := s.deleteServiceMetadata(svc); err != nil {
					log.Printf("failed to delete service metadata: %v", err)
				}
			}
		}
	}()

	return nil
}
