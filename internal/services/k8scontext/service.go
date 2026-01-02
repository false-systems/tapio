package k8scontext

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Config for the K8sContext service.
type Config struct {
	NodeName string // This node's name (for pod filtering)
}

// Service provides in-memory K8s metadata lookup for eBPF enrichment.
type Service struct {
	client   kubernetes.Interface
	nodeName string
	store    *Store

	podFactory informers.SharedInformerFactory
	svcFactory informers.SharedInformerFactory

	ready atomic.Bool
}

// New creates a new K8sContext service.
func New(client kubernetes.Interface, cfg Config) *Service {
	return &Service{
		client:   client,
		nodeName: cfg.NodeName,
		store:    NewStore(),
	}
}

// Start initializes informers and begins watching.
func (s *Service) Start(ctx context.Context) error {
	// Pod factory with node filter (local pods only)
	s.podFactory = informers.NewSharedInformerFactoryWithOptions(
		s.client,
		0, // No resync
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.Set{"spec.nodeName": s.nodeName}.String()
		}),
	)

	// Service factory - cluster-wide (no filter)
	s.svcFactory = informers.NewSharedInformerFactory(s.client, 0)

	// Setup pod informer
	podInformer := s.podFactory.Core().V1().Pods().Informer()
	if _, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onPodAdd,
		UpdateFunc: s.onPodUpdate,
		DeleteFunc: s.onPodDelete,
	}); err != nil {
		return fmt.Errorf("add pod event handler: %w", err)
	}

	// Setup service informer
	svcInformer := s.svcFactory.Core().V1().Services().Informer()
	if _, err := svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onServiceAdd,
		UpdateFunc: s.onServiceUpdate,
		DeleteFunc: s.onServiceDelete,
	}); err != nil {
		return fmt.Errorf("add service event handler: %w", err)
	}

	// Start informers
	s.podFactory.Start(ctx.Done())
	s.svcFactory.Start(ctx.Done())

	// Wait for sync in background
	go func() {
		if !cache.WaitForCacheSync(ctx.Done(),
			podInformer.HasSynced,
			svcInformer.HasSynced) {
			return
		}
		s.ready.Store(true)
	}()

	return nil
}

// Ready returns true after initial sync is complete.
func (s *Service) Ready() bool {
	return s.ready.Load()
}

// PodByIP looks up a pod by its IP address.
func (s *Service) PodByIP(ip string) (*PodMeta, bool) {
	return s.store.PodByIP(ip)
}

// PodByContainerID looks up a pod by container ID.
func (s *Service) PodByContainerID(cid string) (*PodMeta, bool) {
	return s.store.PodByContainerID(cid)
}

// PodByName looks up a pod by namespace and name.
func (s *Service) PodByName(namespace, name string) (*PodMeta, bool) {
	return s.store.PodByName(namespace, name)
}

// ServiceByClusterIP looks up a service by its ClusterIP.
func (s *Service) ServiceByClusterIP(ip string) (*ServiceMeta, bool) {
	return s.store.ServiceByClusterIP(ip)
}

// ServiceByName looks up a service by namespace and name.
func (s *Service) ServiceByName(namespace, name string) (*ServiceMeta, bool) {
	return s.store.ServiceByName(namespace, name)
}

// PodCount returns the number of pods in the cache.
func (s *Service) PodCount() int {
	return s.store.PodCount()
}

// ServiceCount returns the number of services in the cache.
func (s *Service) ServiceCount() int {
	return s.store.ServiceCount()
}

// Event handlers

func (s *Service) onPodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	s.store.AddPod(TransformPod(pod))
}

func (s *Service) onPodUpdate(_, newObj interface{}) {
	pod, ok := newObj.(*corev1.Pod)
	if !ok {
		return
	}
	s.store.AddPod(TransformPod(pod))
}

func (s *Service) onPodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		// Handle DeletedFinalStateUnknown
		if stale, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			pod, ok = stale.Obj.(*corev1.Pod)
			if !ok {
				return
			}
		} else {
			return
		}
	}
	s.store.DeletePod(TransformPod(pod))
}

func (s *Service) onServiceAdd(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	s.store.AddService(TransformService(svc))
}

func (s *Service) onServiceUpdate(_, newObj interface{}) {
	svc, ok := newObj.(*corev1.Service)
	if !ok {
		return
	}
	s.store.AddService(TransformService(svc))
}

func (s *Service) onServiceDelete(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		if stale, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			svc, ok = stale.Obj.(*corev1.Service)
			if !ok {
				return
			}
		} else {
			return
		}
	}
	s.store.DeleteService(TransformService(svc))
}
