package context

import (
	"context"
	"encoding/json"
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
		resultChan := watcher.ResultChan()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-resultChan:
				if !ok {
					return
				}
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
		resultChan := watcher.ResultChan()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-resultChan:
				if !ok {
					return
				}
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
		}
	}()

	return nil
}

// storePodMetadata writes pod metadata to NATS KV
func (s *Service) storePodMetadata(pod *corev1.Pod) error {
	if pod.Status.PodIP == "" {
		return nil // Skip pods without IP
	}

	labels := pod.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	podInfo := PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		PodIP:     pod.Status.PodIP,
		HostIP:    pod.Status.HostIP,
		Labels:    labels,
	}

	data, err := json.Marshal(podInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal pod info: %w", err)
	}

	key := fmt.Sprintf("pod.ip.%s", pod.Status.PodIP)
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to put pod metadata in KV: %w", err)
	}

	return nil
}

// deletePodMetadata removes pod metadata from NATS KV
func (s *Service) deletePodMetadata(pod *corev1.Pod) error {
	if pod.Status.PodIP == "" {
		return nil
	}

	key := fmt.Sprintf("pod.ip.%s", pod.Status.PodIP)
	if err := s.kv.Delete(key); err != nil {
		return fmt.Errorf("failed to delete pod metadata from KV: %w", err)
	}

	return nil
}

// storeServiceMetadata writes service metadata to NATS KV
func (s *Service) storeServiceMetadata(svc *corev1.Service) error {
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
		return nil // Skip headless services
	}

	serviceInfo := ServiceInfo{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		ClusterIP: svc.Spec.ClusterIP,
		Type:      string(svc.Spec.Type),
		Labels:    svc.Labels,
	}

	data, err := json.Marshal(serviceInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal service info: %w", err)
	}

	key := fmt.Sprintf("service.ip.%s", svc.Spec.ClusterIP)
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to put service metadata in KV: %w", err)
	}

	return nil
}

// deleteServiceMetadata removes service metadata from NATS KV
func (s *Service) deleteServiceMetadata(svc *corev1.Service) error {
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
		return nil
	}

	key := fmt.Sprintf("service.ip.%s", svc.Spec.ClusterIP)
	if err := s.kv.Delete(key); err != nil {
		return fmt.Errorf("failed to delete service metadata from KV: %w", err)
	}

	return nil
}
