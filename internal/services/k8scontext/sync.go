package k8scontext

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

// initialSync loads existing K8s resources into NATS KV on startup
func (s *Service) initialSync(ctx context.Context) error {
	// Sync existing pods
	if err := s.syncPods(ctx); err != nil {
		return fmt.Errorf("failed to sync pods: %w", err)
	}

	// Sync existing services
	if err := s.syncServices(ctx); err != nil {
		return fmt.Errorf("failed to sync services: %w", err)
	}

	return nil
}

// syncPods syncs all existing pods from informer cache to NATS KV
func (s *Service) syncPods(ctx context.Context) error {
	podLister := s.informerFactory.Core().V1().Pods().Lister()
	pods, err := podLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods {
		if shouldSkipPod(pod) {
			continue
		}

		podCopy := pod.DeepCopy()
		s.enqueueEvent(func() error {
			return s.storePodMetadata(podCopy)
		})
	}

	return nil
}

// syncServices syncs all existing services from informer cache to NATS KV
func (s *Service) syncServices(ctx context.Context) error {
	serviceLister := s.informerFactory.Core().V1().Services().Lister()
	services, err := serviceLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services {
		if shouldSkipService(svc) {
			continue
		}

		svcCopy := svc.DeepCopy()
		s.enqueueEvent(func() error {
			return s.storeServiceMetadata(svcCopy)
		})
	}

	return nil
}
