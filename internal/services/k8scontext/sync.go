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

	// Sync existing deployments
	if err := s.syncDeployments(ctx); err != nil {
		return fmt.Errorf("failed to sync deployments: %w", err)
	}

	// Sync existing nodes
	if err := s.syncNodes(ctx); err != nil {
		return fmt.Errorf("failed to sync nodes: %w", err)
	}

	return nil
}

// syncPods syncs all existing pods from informer cache to NATS KV
func (s *Service) syncPods(_ context.Context) error {
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
func (s *Service) syncServices(_ context.Context) error {
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

// syncDeployments syncs all existing deployments from informer cache to NATS KV
func (s *Service) syncDeployments(_ context.Context) error {
	deploymentLister := s.informerFactory.Apps().V1().Deployments().Lister()
	deployments, err := deploymentLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, deployment := range deployments {
		deploymentCopy := deployment.DeepCopy()
		s.enqueueEvent(func() error {
			return s.storeDeploymentMetadata(deploymentCopy)
		})
	}

	return nil
}

// syncNodes syncs all existing nodes from informer cache to NATS KV
func (s *Service) syncNodes(_ context.Context) error {
	nodeLister := s.informerFactory.Core().V1().Nodes().Lister()
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodes {
		nodeCopy := node.DeepCopy()
		s.enqueueEvent(func() error {
			return s.storeNodeMetadata(nodeCopy)
		})
	}

	return nil
}
