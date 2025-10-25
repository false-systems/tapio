package k8scontext

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Service watches K8s API and populates NATS KV with metadata
// DUAL RESPONSIBILITY:
// 1. Metadata store - Store K8s resources in NATS KV for lookups
// 2. Event emission - Emit diagnostic events for K8s resource changes
type Service struct {
	config    Config
	k8sClient *kubernetes.Clientset
	kv        nats.KeyValue
	logger    zerolog.Logger

	// Informer factory (shared across all informers)
	informerFactory informers.SharedInformerFactory

	// Event buffer for async NATS KV writes
	eventBuffer chan func() error

	// Event emission (optional - if Output configured)
	emitter base.Emitter // OTLP emitter for community tier

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

	// 8. Create event emitter (if event emission enabled)
	var emitter base.Emitter
	if config.Output.OTEL || config.Output.Tapio || config.Output.Stdout {
		emitter, err = base.CreateEmitters(base.OutputConfig{
			OTEL:   config.Output.OTEL,
			Tapio:  config.Output.Tapio,
			Stdout: config.Output.Stdout,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create event emitters: %w", err)
		}
		logger.Info().Msg("Event emission enabled (OTLP/NATS/stdout)")
	} else {
		logger.Info().Msg("Event emission disabled (metadata-only mode)")
	}

	return &Service{
		config:          config,
		k8sClient:       clientset,
		kv:              kv,
		logger:          logger,
		informerFactory: informerFactory,
		eventBuffer:     eventBuffer,
		emitter:         emitter,
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

	// Events informer - TDD: Future implementation to watch K8s Events API
	// Will monitor FailedScheduling events to track pod scheduling failures and correlate
	// with scheduler metrics. Events will be stored in NATS KV for historical analysis.
	// Example: Reason="FailedScheduling", Message="0/3 nodes available: insufficient memory"
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

	// Note: Scheduler observability is now handled by standalone SchedulerObserver
	// in internal/observers/scheduler/ using BaseObserver pattern

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

	// Close emitter if configured
	if s.emitter != nil {
		if err := s.emitter.Close(); err != nil {
			s.logger.Error().Err(err).Msg("failed to close event emitter")
		}
	}

	s.logger.Info().Msg("k8scontext service stopped gracefully")
	return nil
}

// emitDomainEvent emits diagnostic events for K8s resource changes
// Follows same pattern as NetworkObserver.emitDomainEvent()
func (s *Service) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) {
	// Skip if event emission not configured
	if s.emitter == nil {
		return
	}

	// Validate event has K8s data
	if evt.K8sData == nil {
		s.logger.Warn().
			Str("event_id", evt.ID).
			Str("event_type", evt.Type).
			Msg("missing K8s data in event")
		return
	}

	// Set source if not already set
	if evt.Source == "" {
		evt.Source = "k8scontext"
	}

	// Emit to OTLP (Community tier - structured logs)
	if err := s.emitter.Emit(ctx, evt); err != nil {
		s.logger.Error().
			Err(err).
			Str("event_id", evt.ID).
			Str("event_type", evt.Type).
			Msg("failed to emit event")
		return
	}

	// Publish to NATS with graph enrichment (Enterprise tier)
	if s.config.Output.Tapio && s.config.Publisher != nil {
		s.enrichAndPublish(ctx, evt)
	}

	// Debug logging if stdout enabled
	if s.config.Output.Stdout {
		s.logger.Info().
			Str("type", evt.Type).
			Str("subtype", evt.Subtype).
			Str("resource", evt.K8sData.ResourceKind).
			Str("name", evt.K8sData.ResourceName).
			Str("action", evt.K8sData.Action).
			Msg("K8s event emitted")
	}
}

// enrichAndPublish enriches ObserverEvent with graph entities and publishes to NATS
func (s *Service) enrichAndPublish(ctx context.Context, evt *domain.ObserverEvent) {
	// Build K8s context for enrichment
	k8sCtx := &domain.K8sContext{
		ClusterID:    s.config.ClusterID,
		PodNamespace: evt.K8sData.ResourceName, // Will be refined based on resource type
	}

	// Enrich with graph entities
	tapioEvent, err := domain.EnrichWithK8sContext(evt, k8sCtx)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("event_id", evt.ID).
			Msg("failed to enrich event with K8s context")
		return
	}

	// Publish to NATS JetStream
	subject := fmt.Sprintf("tapio.events.%s.%s", tapioEvent.Type, tapioEvent.Subtype)
	if err := s.config.Publisher.Publish(ctx, subject, tapioEvent); err != nil {
		s.logger.Error().
			Err(err).
			Str("subject", subject).
			Msg("failed to publish to NATS")
	}
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
