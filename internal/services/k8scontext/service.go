package k8scontext

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Service watches K8s API and populates NATS KV with metadata
type Service struct {
	config    Config
	k8sClient *kubernetes.Clientset
	kv        nats.KeyValue

	// Informer factory (shared across all informers)
	informerFactory informers.SharedInformerFactory

	// Event buffer for async NATS KV writes
	eventBuffer chan func() error
}

// NewService creates a new K8s Context Service
func NewService(config Config) (*Service, error) {
	// 1. Validate required config
	if config.NATSConn == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}
	if config.KVBucket == "" {
		return nil, fmt.Errorf("KV bucket name is required")
	}

	// 2. Apply default values
	if config.EventBufferSize == 0 {
		config.EventBufferSize = 1000
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 1 * time.Second
	}

	// 3. Create K8s client
	var k8sConfig *rest.Config
	var err error
	if config.K8sConfig != nil {
		// Use provided config (for testing)
		k8sConfig = config.K8sConfig
	} else {
		// Use in-cluster config (production)
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster K8s config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	// 4. Get or create NATS KV bucket
	js, err := config.NATSConn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	kv, err := js.KeyValue(config.KVBucket)
	if err != nil {
		// Bucket doesn't exist, create it
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket: config.KVBucket,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get/create KV bucket %s: %w", config.KVBucket, err)
		}
	}

	// 5. Create informer factory (not started yet)
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	// 6. Create event buffer
	eventBuffer := make(chan func() error, config.EventBufferSize)

	return &Service{
		config:          config,
		k8sClient:       clientset,
		kv:              kv,
		informerFactory: informerFactory,
		eventBuffer:     eventBuffer,
	}, nil
}

// startInformers registers event handlers for all K8s resources
func (s *Service) startInformers() {
	// Pod informer
	podInformer := s.informerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(&podEventHandler{service: s})

	// Service informer
	serviceInformer := s.informerFactory.Core().V1().Services().Informer()
	serviceInformer.AddEventHandler(&serviceEventHandler{service: s})
}

// Start begins watching K8s resources
func (s *Service) Start(ctx context.Context) error {
	// Register event handlers
	s.startInformers()

	// Start all informers
	s.informerFactory.Start(ctx.Done())

	// Wait for cache sync
	s.informerFactory.WaitForCacheSync(ctx.Done())

	return nil
}

// Stop gracefully stops the service
func (s *Service) Stop() error {
	// Informers will stop when context is cancelled
	// Close event buffer
	close(s.eventBuffer)
	return nil
}

// podEventHandler wraps Service to implement cache.ResourceEventHandler
type podEventHandler struct {
	service *Service
}

func (h *podEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	h.service.handlePodAdd(obj)
}

func (h *podEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.service.handlePodUpdate(oldObj, newObj)
}

func (h *podEventHandler) OnDelete(obj interface{}) {
	h.service.handlePodDelete(obj)
}

// serviceEventHandler wraps Service to implement cache.ResourceEventHandler
type serviceEventHandler struct {
	service *Service
}

func (h *serviceEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	h.service.handleServiceAdd(obj)
}

func (h *serviceEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.service.handleServiceUpdate(oldObj, newObj)
}

func (h *serviceEventHandler) OnDelete(obj interface{}) {
	h.service.handleServiceDelete(obj)
}
