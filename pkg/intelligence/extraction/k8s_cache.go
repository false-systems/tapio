package extraction

import (
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// Custom index names
	containerIDIndex = "containerID"
	podIPIndex       = "podIP"
)

// NewK8sCache creates a new K8s resource cache
func NewK8sCache(k8sClient kubernetes.Interface) (*K8sCache, error) {
	factory := informers.NewSharedInformerFactory(k8sClient, 30*time.Second)

	k8sCache := &K8sCache{
		podInformer:         factory.Core().V1().Pods().Informer(),
		serviceInformer:     factory.Core().V1().Services().Informer(),
		endpointInformer:    factory.Core().V1().Endpoints().Informer(),
		nodeInformer:        factory.Core().V1().Nodes().Informer(),
		deploymentInformer:  factory.Apps().V1().Deployments().Informer(),
		replicaSetInformer:  factory.Apps().V1().ReplicaSets().Informer(),
		statefulSetInformer: factory.Apps().V1().StatefulSets().Informer(),
		daemonSetInformer:   factory.Apps().V1().DaemonSets().Informer(),
		configMapInformer:   factory.Core().V1().ConfigMaps().Informer(),
		secretInformer:      factory.Core().V1().Secrets().Informer(),
		containerIDIndex:    containerIDIndex,
		podIPIndex:          podIPIndex,
	}

	// Add custom indexes
	if err := k8sCache.addIndexes(); err != nil {
		return nil, fmt.Errorf("failed to add indexes: %w", err)
	}

	// Start informers
	factory.Start(nil)

	// Wait for caches to sync
	factory.WaitForCacheSync(nil)

	return k8sCache, nil
}

// addIndexes adds custom indexes for fast lookups
func (c *K8sCache) addIndexes() error {
	// Index pods by container ID
	err := c.podInformer.AddIndexers(cache.Indexers{
		containerIDIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return nil, nil
			}

			var containerIDs []string
			// Running containers
			for _, status := range pod.Status.ContainerStatuses {
				if status.ContainerID != "" {
					// ContainerID format: docker://abc123...
					parts := strings.Split(status.ContainerID, "://")
					if len(parts) == 2 {
						containerIDs = append(containerIDs, parts[1])
					}
				}
			}
			// Init containers
			for _, status := range pod.Status.InitContainerStatuses {
				if status.ContainerID != "" {
					parts := strings.Split(status.ContainerID, "://")
					if len(parts) == 2 {
						containerIDs = append(containerIDs, parts[1])
					}
				}
			}
			return containerIDs, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add container ID index: %w", err)
	}

	// Index pods by IP
	err = c.podInformer.AddIndexers(cache.Indexers{
		podIPIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return nil, nil
			}

			var ips []string
			if pod.Status.PodIP != "" {
				ips = append(ips, pod.Status.PodIP)
			}
			for _, podIP := range pod.Status.PodIPs {
				ips = append(ips, podIP.IP)
			}
			return ips, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add pod IP index: %w", err)
	}

	return nil
}

// GetPod retrieves a pod by namespace and name
func (c *K8sCache) GetPod(namespace, name string) (*corev1.Pod, error) {
	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}

	obj, exists, err := c.podInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}

	return pod, nil
}

// GetPodByContainerID retrieves a pod by container ID
func (c *K8sCache) GetPodByContainerID(containerID string) (*corev1.Pod, error) {
	objs, err := c.podInformer.GetIndexer().ByIndex(containerIDIndex, containerID)
	if err != nil {
		return nil, err
	}

	if len(objs) == 0 {
		return nil, nil
	}

	// Should only be one pod per container ID
	pod, ok := objs[0].(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", objs[0])
	}

	return pod, nil
}

// GetPodByIP retrieves a pod by IP address
func (c *K8sCache) GetPodByIP(ip string) (*corev1.Pod, error) {
	objs, err := c.podInformer.GetIndexer().ByIndex(podIPIndex, ip)
	if err != nil {
		return nil, err
	}

	if len(objs) == 0 {
		return nil, nil
	}

	pod, ok := objs[0].(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", objs[0])
	}

	return pod, nil
}

// GetNode retrieves a node by name
func (c *K8sCache) GetNode(name string) (*corev1.Node, error) {
	obj, exists, err := c.nodeInformer.GetStore().GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}

	return node, nil
}

// GetServicesForPod returns all services that select a pod
func (c *K8sCache) GetServicesForPod(namespace, podName string) []*corev1.Service {
	pod, err := c.GetPod(namespace, podName)
	if err != nil || pod == nil {
		return nil
	}

	var services []*corev1.Service

	// Get all services in the namespace
	for _, obj := range c.serviceInformer.GetStore().List() {
		svc, ok := obj.(*corev1.Service)
		if !ok || svc.Namespace != namespace {
			continue
		}

		// Check if service selector matches pod labels
		if svc.Spec.Selector != nil && len(svc.Spec.Selector) > 0 {
			selector := labels.SelectorFromSet(svc.Spec.Selector)
			if selector.Matches(labels.Set(pod.Labels)) {
				services = append(services, svc)
			}
		}
	}

	return services
}

// GetDeploymentForReplicaSet finds the deployment that owns a replicaset
func (c *K8sCache) GetDeploymentForReplicaSet(namespace, rsName string) *appsv1.Deployment {
	// Get the ReplicaSet
	rsKey := namespace + "/" + rsName
	obj, exists, err := c.replicaSetInformer.GetStore().GetByKey(rsKey)
	if err != nil || !exists {
		return nil
	}

	rs, ok := obj.(*appsv1.ReplicaSet)
	if !ok {
		return nil
	}

	// Find the deployment owner
	for _, owner := range rs.OwnerReferences {
		if owner.Kind == "Deployment" {
			deployKey := namespace + "/" + owner.Name
			obj, exists, err := c.deploymentInformer.GetStore().GetByKey(deployKey)
			if err != nil || !exists {
				continue
			}

			deployment, ok := obj.(*appsv1.Deployment)
			if ok {
				return deployment
			}
		}
	}

	return nil
}

// GetEndpointsForService returns endpoints for a service
func (c *K8sCache) GetEndpointsForService(namespace, serviceName string) (*corev1.Endpoints, error) {
	key := namespace + "/" + serviceName
	obj, exists, err := c.endpointInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	endpoints, ok := obj.(*corev1.Endpoints)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}

	return endpoints, nil
}

// GetConfigMap retrieves a ConfigMap
func (c *K8sCache) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {
	key := namespace + "/" + name
	obj, exists, err := c.configMapInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}

	return cm, nil
}

// GetSecret retrieves a Secret
func (c *K8sCache) GetSecret(namespace, name string) (*corev1.Secret, error) {
	key := namespace + "/" + name
	obj, exists, err := c.secretInformer.GetStore().GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T", obj)
	}

	return secret, nil
}

// GetWorkloadForPod finds the workload controller for a pod
func (c *K8sCache) GetWorkloadForPod(pod *corev1.Pod) (runtime.Object, error) {
	if pod == nil {
		return nil, nil
	}

	// Check owner references
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			switch owner.Kind {
			case "ReplicaSet":
				// Check if it's owned by a Deployment
				if deployment := c.GetDeploymentForReplicaSet(pod.Namespace, owner.Name); deployment != nil {
					return deployment, nil
				}
				// Otherwise return the ReplicaSet
				key := pod.Namespace + "/" + owner.Name
				obj, exists, _ := c.replicaSetInformer.GetStore().GetByKey(key)
				if exists {
					return obj.(runtime.Object), nil
				}

			case "StatefulSet":
				key := pod.Namespace + "/" + owner.Name
				obj, exists, _ := c.statefulSetInformer.GetStore().GetByKey(key)
				if exists {
					return obj.(runtime.Object), nil
				}

			case "DaemonSet":
				key := pod.Namespace + "/" + owner.Name
				obj, exists, _ := c.daemonSetInformer.GetStore().GetByKey(key)
				if exists {
					return obj.(runtime.Object), nil
				}
			}
		}
	}

	return nil, nil
}
