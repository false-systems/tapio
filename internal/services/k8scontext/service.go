package k8scontext

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/internal/base"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Service watches K8s API and populates NATS KV with metadata
type Service struct {
	config    Config
	k8sClient *kubernetes.Clientset
	kv        nats.KeyValue
	logger    zerolog.Logger

	// Informer factory (shared across all informers)
	informerFactory informers.SharedInformerFactory

	// Event buffer for async NATS KV writes
	eventBuffer chan func() error

	// Lifecycle management
	ctx      context.Context
	cancel   context.CancelFunc
	workerWG sync.WaitGroup
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

	// 7. Create logger
	logger := base.NewLogger("k8scontext")

	return &Service{
		config:          config,
		k8sClient:       clientset,
		kv:              kv,
		logger:          logger,
		informerFactory: informerFactory,
		eventBuffer:     eventBuffer,
	}, nil
}

// startInformers registers event handlers for all K8s resources
func (s *Service) startInformers() error {
	// Pod informer
	podInformer := s.informerFactory.Core().V1().Pods().Informer()
	if _, err := podInformer.AddEventHandler(&podEventHandler{service: s}); err != nil {
		return fmt.Errorf("failed to add pod event handler: %w", err)
	}

	// Service informer
	serviceInformer := s.informerFactory.Core().V1().Services().Informer()
	if _, err := serviceInformer.AddEventHandler(&serviceEventHandler{service: s}); err != nil {
		return fmt.Errorf("failed to add service event handler: %w", err)
	}

	// Deployment informer
	deploymentInformer := s.informerFactory.Apps().V1().Deployments().Informer()
	if _, err := deploymentInformer.AddEventHandler(&deploymentEventHandler{service: s}); err != nil {
		return fmt.Errorf("failed to add deployment event handler: %w", err)
	}

	// ReplicaSet informer
	replicaSetInformer := s.informerFactory.Apps().V1().ReplicaSets().Informer()
	if _, err := replicaSetInformer.AddEventHandler(&replicaSetEventHandler{service: s}); err != nil {
		return fmt.Errorf("failed to add replicaset event handler: %w", err)
	}

	// Node informer
	nodeInformer := s.informerFactory.Core().V1().Nodes().Informer()
	if _, err := nodeInformer.AddEventHandler(&nodeEventHandler{service: s}); err != nil {
		return fmt.Errorf("failed to add node event handler: %w", err)
	}

	// Events informer (for FailedScheduling events) - TDD: will implement with tests
	// eventsInformer := s.informerFactory.Core().V1().Events().Informer()
	// if _, err := eventsInformer.AddEventHandler(&eventEventHandler{service: s}); err != nil {
	// 	return fmt.Errorf("failed to add events event handler: %w", err)
	// }

	return nil
}

// Start begins watching K8s resources
func (s *Service) Start(ctx context.Context) error {
	// Create cancellable context for workers
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Start event processing worker with WaitGroup
	s.workerWG.Add(1)
	go func() {
		defer s.workerWG.Done()
		s.processEvents(s.ctx)
	}()

	// Start Prometheus scraper worker if enabled - TDD: will implement with tests
	// if s.promScraper != nil {
	// 	s.workerWG.Add(1)
	// 	go func() {
	// 		defer s.workerWG.Done()
	// 		s.scrapeSchedulerMetrics(s.ctx)
	// 	}()
	// }

	// Register event handlers
	if err := s.startInformers(); err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	// Start all informers
	s.informerFactory.Start(s.ctx.Done())

	// Wait for cache sync
	s.informerFactory.WaitForCacheSync(s.ctx.Done())

	// Perform initial sync to populate KV with existing resources
	if err := s.initialSync(s.ctx); err != nil {
		return fmt.Errorf("initial sync failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the service
func (s *Service) Stop() error {
	// Cancel context to stop informers
	if s.cancel != nil {
		s.cancel()
	}

	// Close event buffer to signal worker to finish
	close(s.eventBuffer)

	// Wait for worker to drain remaining events
	s.workerWG.Wait()

	s.logger.Info().Msg("k8scontext service stopped gracefully")
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

// deploymentEventHandler wraps Service to implement cache.ResourceEventHandler
type deploymentEventHandler struct {
	service *Service
}

func (h *deploymentEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	h.service.handleDeploymentAdd(obj)
}

func (h *deploymentEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.service.handleDeploymentUpdate(oldObj, newObj)
}

func (h *deploymentEventHandler) OnDelete(obj interface{}) {
	h.service.handleDeploymentDelete(obj)
}

// replicaSetEventHandler wraps Service to implement cache.ResourceEventHandler
type replicaSetEventHandler struct {
	service *Service
}

func (h *replicaSetEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	h.service.handleReplicaSetAdd(obj)
}

func (h *replicaSetEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.service.handleReplicaSetUpdate(oldObj, newObj)
}

func (h *replicaSetEventHandler) OnDelete(obj interface{}) {
	h.service.handleReplicaSetDelete(obj)
}

// nodeEventHandler wraps Service to implement cache.ResourceEventHandler
type nodeEventHandler struct {
	service *Service
}

func (h *nodeEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	h.service.handleNodeAdd(obj)
}

func (h *nodeEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.service.handleNodeUpdate(oldObj, newObj)
}

func (h *nodeEventHandler) OnDelete(obj interface{}) {
	h.service.handleNodeDelete(obj)
}
