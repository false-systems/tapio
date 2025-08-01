package correlation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// K8sRelationshipLoader loads and maintains K8s resource relationships
type K8sRelationshipLoader struct {
	logger    *zap.Logger
	clientset kubernetes.Interface

	// Caches with actual data
	ownerCache    *OwnershipCache
	selectorCache *SelectorCache
	nodeCache     *NodeResourceCache

	// Informers for watching K8s resources
	deploymentInformer cache.SharedIndexInformer
	replicaSetInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	serviceInformer    cache.SharedIndexInformer
	endpointInformer   cache.SharedIndexInformer

	// Control
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NodeResourceCache tracks which pods run on which nodes
type NodeResourceCache struct {
	nodePods map[string][]ResourceRef // node -> pods
	podNode  map[string]string        // pod -> node
	mu       sync.RWMutex
}

// GetPodNode returns the node a pod is running on
func (c *NodeResourceCache) GetPodNode(podUID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.podNode[podUID]
}

// GetNodePods returns all pods on a node
func (c *NodeResourceCache) GetNodePods(nodeName string) []ResourceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodePods[nodeName]
}

// NewK8sRelationshipLoader creates a new loader with K8s client
func NewK8sRelationshipLoader(logger *zap.Logger, clientset kubernetes.Interface) *K8sRelationshipLoader {
	return &K8sRelationshipLoader{
		logger:    logger,
		clientset: clientset,
		ownerCache: &OwnershipCache{
			owners: make(map[string][]*ResourceRef),
			owned:  make(map[string]*ResourceRef),
		},
		selectorCache: &SelectorCache{
			selectors: make(map[string][]*ResourceRef),
			matches:   make(map[string][]string),
		},
		nodeCache: &NodeResourceCache{
			nodePods: make(map[string][]ResourceRef),
			podNode:  make(map[string]string),
		},
		stopCh: make(chan struct{}),
	}
}

// Start begins watching K8s resources and populating caches
func (l *K8sRelationshipLoader) Start(ctx context.Context) error {
	l.logger.Info("Starting K8s relationship loader")

	// Create informers
	if err := l.createInformers(); err != nil {
		return fmt.Errorf("failed to create informers: %w", err)
	}

	// Start informers
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.deploymentInformer.Run(l.stopCh)
	}()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.replicaSetInformer.Run(l.stopCh)
	}()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.podInformer.Run(l.stopCh)
	}()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.serviceInformer.Run(l.stopCh)
	}()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.endpointInformer.Run(l.stopCh)
	}()

	// Wait for caches to sync
	l.logger.Info("Waiting for K8s caches to sync")
	if !cache.WaitForCacheSync(l.stopCh,
		l.deploymentInformer.HasSynced,
		l.replicaSetInformer.HasSynced,
		l.podInformer.HasSynced,
		l.serviceInformer.HasSynced,
		l.endpointInformer.HasSynced) {
		return fmt.Errorf("failed to sync K8s caches")
	}

	l.logger.Info("K8s relationship loader started successfully")
	return nil
}

// Stop gracefully shuts down the loader
func (l *K8sRelationshipLoader) Stop() {
	l.logger.Info("Stopping K8s relationship loader")
	close(l.stopCh)
	l.wg.Wait()
}

// createInformers sets up K8s informers with event handlers
func (l *K8sRelationshipLoader) createInformers() error {
	// Deployment informer
	l.deploymentInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(
			l.clientset.AppsV1().RESTClient(),
			"deployments",
			metav1.NamespaceAll,
			fields.Everything(),
		),
		&v1.Deployment{},
		time.Minute*10,
		cache.Indexers{},
	)

	l.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    l.handleDeploymentAdd,
		UpdateFunc: l.handleDeploymentUpdate,
		DeleteFunc: l.handleDeploymentDelete,
	})

	// ReplicaSet informer
	l.replicaSetInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(
			l.clientset.AppsV1().RESTClient(),
			"replicasets",
			metav1.NamespaceAll,
			fields.Everything(),
		),
		&v1.ReplicaSet{},
		time.Minute*10,
		cache.Indexers{},
	)

	l.replicaSetInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    l.handleReplicaSetAdd,
		UpdateFunc: l.handleReplicaSetUpdate,
		DeleteFunc: l.handleReplicaSetDelete,
	})

	// Pod informer
	l.podInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(
			l.clientset.CoreV1().RESTClient(),
			"pods",
			metav1.NamespaceAll,
			fields.Everything(),
		),
		&corev1.Pod{},
		time.Minute*10,
		cache.Indexers{},
	)

	l.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    l.handlePodAdd,
		UpdateFunc: l.handlePodUpdate,
		DeleteFunc: l.handlePodDelete,
	})

	// Service informer
	l.serviceInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(
			l.clientset.CoreV1().RESTClient(),
			"services",
			metav1.NamespaceAll,
			fields.Everything(),
		),
		&corev1.Service{},
		time.Minute*10,
		cache.Indexers{},
	)

	l.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    l.handleServiceAdd,
		UpdateFunc: l.handleServiceUpdate,
		DeleteFunc: l.handleServiceDelete,
	})

	// Endpoints informer
	l.endpointInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(
			l.clientset.CoreV1().RESTClient(),
			"endpoints",
			metav1.NamespaceAll,
			fields.Everything(),
		),
		&corev1.Endpoints{},
		time.Minute*10,
		cache.Indexers{},
	)

	l.endpointInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    l.handleEndpointAdd,
		UpdateFunc: l.handleEndpointUpdate,
		DeleteFunc: l.handleEndpointDelete,
	})

	return nil
}

// Pod event handlers
func (l *K8sRelationshipLoader) handlePodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	podRef := ResourceRef{
		Kind:      "Pod",
		Namespace: pod.Namespace,
		Name:      pod.Name,
		UID:       string(pod.UID),
		Labels:    pod.Labels,
	}

	// Update ownership
	for _, owner := range pod.OwnerReferences {
		ownerRef := ResourceRef{
			Kind:      owner.Kind,
			Namespace: pod.Namespace,
			Name:      owner.Name,
			UID:       string(owner.UID),
		}
		l.ownerCache.AddOwnership(&ownerRef, &podRef)
	}

	// Update node cache
	if pod.Spec.NodeName != "" {
		l.nodeCache.AddPodToNode(pod.Spec.NodeName, podRef)
	}

	// Update selector cache
	if pod.Labels != nil {
		l.selectorCache.UpdateResourceLabels(&podRef, pod.Labels)
	}

	l.logger.Debug("Added pod to relationship cache",
		zap.String("pod", pod.Name),
		zap.String("namespace", pod.Namespace))
}

func (l *K8sRelationshipLoader) handlePodUpdate(oldObj, newObj interface{}) {
	// For now, just remove and re-add
	l.handlePodDelete(oldObj)
	l.handlePodAdd(newObj)
}

func (l *K8sRelationshipLoader) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	podRef := ResourceRef{
		Kind:      "Pod",
		Namespace: pod.Namespace,
		Name:      pod.Name,
		UID:       string(pod.UID),
	}

	// Remove from ownership
	l.ownerCache.RemoveResource(&podRef)

	// Remove from node cache
	if pod.Spec.NodeName != "" {
		l.nodeCache.RemovePodFromNode(pod.Spec.NodeName, podRef)
	}

	// Remove from selector cache
	l.selectorCache.RemoveResource(&podRef)
}

// Deployment event handlers
func (l *K8sRelationshipLoader) handleDeploymentAdd(obj interface{}) {
	deployment, ok := obj.(*v1.Deployment)
	if !ok {
		return
	}

	deployRef := ResourceRef{
		Kind:      "Deployment",
		Namespace: deployment.Namespace,
		Name:      deployment.Name,
		UID:       string(deployment.UID),
		Labels:    deployment.Labels,
	}

	// Update selector cache with deployment's pod selector
	if deployment.Spec.Selector != nil && deployment.Spec.Selector.MatchLabels != nil {
		l.selectorCache.AddSelector(&deployRef, deployment.Spec.Selector.MatchLabels)
	}

	l.logger.Debug("Added deployment to relationship cache",
		zap.String("deployment", deployment.Name),
		zap.String("namespace", deployment.Namespace))
}

func (l *K8sRelationshipLoader) handleDeploymentUpdate(oldObj, newObj interface{}) {
	l.handleDeploymentDelete(oldObj)
	l.handleDeploymentAdd(newObj)
}

func (l *K8sRelationshipLoader) handleDeploymentDelete(obj interface{}) {
	deployment, ok := obj.(*v1.Deployment)
	if !ok {
		return
	}

	deployRef := ResourceRef{
		Kind:      "Deployment",
		Namespace: deployment.Namespace,
		Name:      deployment.Name,
		UID:       string(deployment.UID),
	}

	l.selectorCache.RemoveResource(&deployRef)
}

// ReplicaSet handlers
func (l *K8sRelationshipLoader) handleReplicaSetAdd(obj interface{}) {
	rs, ok := obj.(*v1.ReplicaSet)
	if !ok {
		return
	}

	rsRef := ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: rs.Namespace,
		Name:      rs.Name,
		UID:       string(rs.UID),
		Labels:    rs.Labels,
	}

	// Update ownership (RS -> Deployment)
	for _, owner := range rs.OwnerReferences {
		ownerRef := ResourceRef{
			Kind:      owner.Kind,
			Namespace: rs.Namespace,
			Name:      owner.Name,
			UID:       string(owner.UID),
		}
		l.ownerCache.AddOwnership(&ownerRef, &rsRef)
	}

	// Update selector cache
	if rs.Spec.Selector != nil && rs.Spec.Selector.MatchLabels != nil {
		l.selectorCache.AddSelector(&rsRef, rs.Spec.Selector.MatchLabels)
	}
}

func (l *K8sRelationshipLoader) handleReplicaSetUpdate(oldObj, newObj interface{}) {
	l.handleReplicaSetDelete(oldObj)
	l.handleReplicaSetAdd(newObj)
}

func (l *K8sRelationshipLoader) handleReplicaSetDelete(obj interface{}) {
	rs, ok := obj.(*v1.ReplicaSet)
	if !ok {
		return
	}

	rsRef := ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: rs.Namespace,
		Name:      rs.Name,
		UID:       string(rs.UID),
	}

	l.ownerCache.RemoveResource(&rsRef)
	l.selectorCache.RemoveResource(&rsRef)
}

// Service handlers
func (l *K8sRelationshipLoader) handleServiceAdd(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return
	}

	serviceRef := ResourceRef{
		Kind:      "Service",
		Namespace: service.Namespace,
		Name:      service.Name,
		UID:       string(service.UID),
		Labels:    service.Labels,
	}

	// Update selector cache with service's pod selector
	if service.Spec.Selector != nil {
		l.selectorCache.AddSelector(&serviceRef, service.Spec.Selector)
	}

	l.logger.Debug("Added service to relationship cache",
		zap.String("service", service.Name),
		zap.String("namespace", service.Namespace))
}

func (l *K8sRelationshipLoader) handleServiceUpdate(oldObj, newObj interface{}) {
	l.handleServiceDelete(oldObj)
	l.handleServiceAdd(newObj)
}

func (l *K8sRelationshipLoader) handleServiceDelete(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return
	}

	serviceRef := ResourceRef{
		Kind:      "Service",
		Namespace: service.Namespace,
		Name:      service.Name,
		UID:       string(service.UID),
	}

	l.selectorCache.RemoveResource(&serviceRef)
}

// Endpoint handlers
func (l *K8sRelationshipLoader) handleEndpointAdd(obj interface{}) {
	endpoints, ok := obj.(*corev1.Endpoints)
	if !ok {
		return
	}

	// Track service -> pod relationships through endpoints
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				// Record that this pod is part of this service's endpoints
				l.logger.Debug("Service endpoint relationship",
					zap.String("service", endpoints.Name),
					zap.String("pod", address.TargetRef.Name))
			}
		}
	}
}

func (l *K8sRelationshipLoader) handleEndpointUpdate(oldObj, newObj interface{}) {
	// For simplicity, just process the new state
	l.handleEndpointAdd(newObj)
}

func (l *K8sRelationshipLoader) handleEndpointDelete(obj interface{}) {
	// Endpoint relationships are transient, no cleanup needed
}

// GetOwnershipCache returns the ownership cache for correlations
func (l *K8sRelationshipLoader) GetOwnershipCache() *OwnershipCache {
	return l.ownerCache
}

// GetSelectorCache returns the selector cache for correlations
func (l *K8sRelationshipLoader) GetSelectorCache() *SelectorCache {
	return l.selectorCache
}

// GetNodeCache returns the node cache for correlations
func (l *K8sRelationshipLoader) GetNodeCache() *NodeResourceCache {
	return l.nodeCache
}

// OwnershipCache methods
func (c *OwnershipCache) AddOwnership(owner, owned *ResourceRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add to owners map
	c.owners[owner.UID] = append(c.owners[owner.UID], owned)

	// Add to owned map
	c.owned[owned.UID] = owner
}

func (c *OwnershipCache) RemoveResource(ref *ResourceRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove as owner
	delete(c.owners, ref.UID)

	// Remove as owned
	delete(c.owned, ref.UID)

	// Remove from other owners' lists
	for uid, ownedList := range c.owners {
		var filtered []*ResourceRef
		for _, owned := range ownedList {
			if owned.UID != ref.UID {
				filtered = append(filtered, owned)
			}
		}
		if len(filtered) != len(ownedList) {
			c.owners[uid] = filtered
		}
	}
}

func (c *OwnershipCache) GetOwner(resource *ResourceRef) *ResourceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.owned[resource.UID]
}

func (c *OwnershipCache) GetOwned(owner *ResourceRef) []*ResourceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.owners[owner.UID]
}

// SelectorCache methods
func (c *SelectorCache) AddSelector(resource *ResourceRef, selector map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	selectorKey := makeSelectorKey(selector)
	c.selectors[selectorKey] = append(c.selectors[selectorKey], resource)
	c.matches[resource.UID] = append(c.matches[resource.UID], selectorKey)
}

func (c *SelectorCache) UpdateResourceLabels(resource *ResourceRef, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check which selectors this resource matches
	for selectorKey, resources := range c.selectors {
		selector := parseSelectorKey(selectorKey)
		if matchesSelector(labels, selector) {
			// Add to matching resources if not already there
			found := false
			for _, r := range resources {
				if r.UID == resource.UID {
					found = true
					break
				}
			}
			if !found {
				c.selectors[selectorKey] = append(resources, resource)
			}
		}
	}
}

func (c *SelectorCache) RemoveResource(ref *ResourceRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from matches
	delete(c.matches, ref.UID)

	// Remove from selectors
	for selectorKey, resources := range c.selectors {
		var filtered []*ResourceRef
		for _, r := range resources {
			if r.UID != ref.UID {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) != len(resources) {
			c.selectors[selectorKey] = filtered
		}
	}
}

func (c *SelectorCache) FindResourcesMatchingSelector(selector map[string]string) []*ResourceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	selectorKey := makeSelectorKey(selector)
	resources := c.selectors[selectorKey]
	if resources == nil {
		return []*ResourceRef{}
	}
	return resources
}

// NodeCache methods
func (nc *NodeResourceCache) AddPodToNode(nodeName string, pod ResourceRef) {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	nc.nodePods[nodeName] = append(nc.nodePods[nodeName], pod)
	nc.podNode[pod.UID] = nodeName
}

func (nc *NodeResourceCache) RemovePodFromNode(nodeName string, pod ResourceRef) {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Remove from node's pod list
	var filtered []ResourceRef
	for _, p := range nc.nodePods[nodeName] {
		if p.UID != pod.UID {
			filtered = append(filtered, p)
		}
	}
	nc.nodePods[nodeName] = filtered

	// Remove pod->node mapping
	delete(nc.podNode, pod.UID)
}

func (nc *NodeResourceCache) GetPodsOnNode(nodeName string) []ResourceRef {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	return nc.nodePods[nodeName]
}

func (nc *NodeResourceCache) GetNodeForPod(podUID string) string {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	return nc.podNode[podUID]
}

// K8sRelationshipMap provides compatibility interface for event_tracker.go
type K8sRelationshipMap struct {
	loader *K8sRelationshipLoader
}

// NewK8sRelationshipMap creates a compatibility wrapper
func NewK8sRelationshipMap(loader *K8sRelationshipLoader) *K8sRelationshipMap {
	return &K8sRelationshipMap{loader: loader}
}

// GetRelatedPods returns pod UIDs related to a resource
func (m *K8sRelationshipMap) GetRelatedPods(resource ResourceRef) []string {
	// If resource is a deployment/replicaset, get owned pods
	if resource.Kind == "Deployment" || resource.Kind == "ReplicaSet" {
		owned := m.loader.ownerCache.GetOwned(&resource)
		var podUIDs []string
		for _, o := range owned {
			if o.Kind == "Pod" {
				podUIDs = append(podUIDs, o.UID)
			}
		}
		return podUIDs
	}
	return nil
}

// AreRelated checks if two resources are related
func (m *K8sRelationshipMap) AreRelated(resA, resB ResourceRef) (bool, string) {
	// Check ownership
	ownerA := m.loader.ownerCache.GetOwner(&resA)
	ownerB := m.loader.ownerCache.GetOwner(&resB)

	if ownerA != nil && ownerB != nil && ownerA.UID == ownerB.UID {
		return true, "same_owner"
	}

	if ownerA != nil && ownerA.UID == resB.UID {
		return true, "owner_child"
	}

	if ownerB != nil && ownerB.UID == resA.UID {
		return true, "owner_child"
	}

	return false, ""
}

// Helper functions
func makeSelectorKey(selector map[string]string) string {
	// Create a deterministic key by sorting the keys
	keys := make([]string, 0, len(selector))
	for k := range selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	
	key := ""
	for _, k := range keys {
		key += k + "=" + selector[k] + ","
	}
	return key
}

func parseSelectorKey(key string) map[string]string {
	selector := make(map[string]string)
	if key == "" {
		return selector
	}

	// Remove trailing comma if present
	key = strings.TrimSuffix(key, ",")

	// Split by comma
	pairs := strings.Split(key, ",")
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		// Split by equals
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			selector[parts[0]] = parts[1]
		}
	}

	return selector
}

func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
