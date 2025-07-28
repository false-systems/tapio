package internal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sInformerMonitor monitors Kubernetes events using informers
type K8sInformerMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// Kubernetes client and informers
	clientset         *kubernetes.Clientset
	podInformer       cache.SharedIndexInformer
	serviceInformer   cache.SharedIndexInformer
	endpointInformer  cache.SharedIndexInformer
	netpolicyInformer cache.SharedIndexInformer
	nodeInformer      cache.SharedIndexInformer

	// Track processed events to avoid duplicates
	processedEvents sync.Map
}

// NewK8sInformerMonitor creates a new Kubernetes informer-based monitor
func NewK8sInformerMonitor(config core.Config) (*K8sInformerMonitor, error) {
	// Create Kubernetes client
	clientset, err := createK8sClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	monitor := &K8sInformerMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
		clientset: clientset,
	}

	// Create informers
	if err := monitor.createInformers(); err != nil {
		return nil, fmt.Errorf("failed to create informers: %w", err)
	}

	return monitor, nil
}

func createK8sClient(config core.Config) (*kubernetes.Clientset, error) {
	var k8sConfig *rest.Config
	var err error

	if config.KubeConfigPath != "" {
		// Use specified kubeconfig
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigPath)
	} else {
		// Try in-cluster config first
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			k8sConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				loadingRules, configOverrides).ClientConfig()
		}
	}

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(k8sConfig)
}

func (m *K8sInformerMonitor) createInformers() error {
	// Create label selector if namespace is specified
	var labelSelector string
	if m.config.LabelSelector != "" {
		labelSelector = m.config.LabelSelector
	}

	// Pod informer
	m.podInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = labelSelector
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Pods(m.config.Namespace).List(context.Background(), options)
				}
				return m.clientset.CoreV1().Pods("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelector
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Pods(m.config.Namespace).Watch(context.Background(), options)
				}
				return m.clientset.CoreV1().Pods("").Watch(context.Background(), options)
			},
		},
		&v1.Pod{},
		time.Minute,
		cache.Indexers{},
	)

	// Service informer
	m.serviceInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Services(m.config.Namespace).List(context.Background(), options)
				}
				return m.clientset.CoreV1().Services("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Services(m.config.Namespace).Watch(context.Background(), options)
				}
				return m.clientset.CoreV1().Services("").Watch(context.Background(), options)
			},
		},
		&v1.Service{},
		time.Minute,
		cache.Indexers{},
	)

	// Endpoints informer
	m.endpointInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Endpoints(m.config.Namespace).List(context.Background(), options)
				}
				return m.clientset.CoreV1().Endpoints("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if m.config.Namespace != "" {
					return m.clientset.CoreV1().Endpoints(m.config.Namespace).Watch(context.Background(), options)
				}
				return m.clientset.CoreV1().Endpoints("").Watch(context.Background(), options)
			},
		},
		&v1.Endpoints{},
		time.Minute,
		cache.Indexers{},
	)

	// NetworkPolicy informer
	m.netpolicyInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if m.config.Namespace != "" {
					return m.clientset.NetworkingV1().NetworkPolicies(m.config.Namespace).List(context.Background(), options)
				}
				return m.clientset.NetworkingV1().NetworkPolicies("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if m.config.Namespace != "" {
					return m.clientset.NetworkingV1().NetworkPolicies(m.config.Namespace).Watch(context.Background(), options)
				}
				return m.clientset.NetworkingV1().NetworkPolicies("").Watch(context.Background(), options)
			},
		},
		&networkingv1.NetworkPolicy{},
		time.Minute,
		cache.Indexers{},
	)

	// Node informer (cluster-wide)
	m.nodeInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.clientset.CoreV1().Nodes().List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.clientset.CoreV1().Nodes().Watch(context.Background(), options)
			},
		},
		&v1.Node{},
		time.Minute,
		cache.Indexers{},
	)

	return nil
}

func (m *K8sInformerMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Add event handlers
	m.addEventHandlers()

	// Start informers
	m.wg.Add(5)
	go func() {
		defer m.wg.Done()
		m.podInformer.Run(m.ctx.Done())
	}()
	go func() {
		defer m.wg.Done()
		m.serviceInformer.Run(m.ctx.Done())
	}()
	go func() {
		defer m.wg.Done()
		m.endpointInformer.Run(m.ctx.Done())
	}()
	go func() {
		defer m.wg.Done()
		m.netpolicyInformer.Run(m.ctx.Done())
	}()
	go func() {
		defer m.wg.Done()
		m.nodeInformer.Run(m.ctx.Done())
	}()

	// Wait for initial sync
	if !cache.WaitForCacheSync(m.ctx.Done(),
		m.podInformer.HasSynced,
		m.serviceInformer.HasSynced,
		m.endpointInformer.HasSynced,
		m.netpolicyInformer.HasSynced,
		m.nodeInformer.HasSynced) {
		return fmt.Errorf("failed to sync informer caches")
	}

	// Start cleanup routine for processed events
	m.wg.Add(1)
	go m.cleanupProcessedEvents()

	return nil
}

func (m *K8sInformerMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for goroutines
	m.wg.Wait()

	close(m.eventChan)
	return nil
}

func (m *K8sInformerMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *K8sInformerMonitor) MonitorType() string {
	return "k8s-informer"
}

func (m *K8sInformerMonitor) addEventHandlers() {
	// Pod event handlers
	m.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if pod, ok := obj.(*v1.Pod); ok {
				m.handlePodAdd(pod)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if oldPod, ok := oldObj.(*v1.Pod); ok {
				if newPod, ok := newObj.(*v1.Pod); ok {
					m.handlePodUpdate(oldPod, newPod)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			if pod, ok := obj.(*v1.Pod); ok {
				m.handlePodDelete(pod)
			}
		},
	})

	// Service event handlers
	m.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if svc, ok := obj.(*v1.Service); ok {
				m.handleServiceChange(svc, "created")
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if svc, ok := newObj.(*v1.Service); ok {
				m.handleServiceChange(svc, "updated")
			}
		},
		DeleteFunc: func(obj interface{}) {
			if svc, ok := obj.(*v1.Service); ok {
				m.handleServiceChange(svc, "deleted")
			}
		},
	})

	// Endpoints event handlers
	m.endpointInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if ep, ok := obj.(*v1.Endpoints); ok {
				m.handleEndpointsChange(ep, "created")
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if ep, ok := newObj.(*v1.Endpoints); ok {
				m.handleEndpointsChange(ep, "updated")
			}
		},
		DeleteFunc: func(obj interface{}) {
			if ep, ok := obj.(*v1.Endpoints); ok {
				m.handleEndpointsChange(ep, "deleted")
			}
		},
	})

	// NetworkPolicy event handlers
	m.netpolicyInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if np, ok := obj.(*networkingv1.NetworkPolicy); ok {
				m.handleNetworkPolicyChange(np, "created")
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if np, ok := newObj.(*networkingv1.NetworkPolicy); ok {
				m.handleNetworkPolicyChange(np, "updated")
			}
		},
		DeleteFunc: func(obj interface{}) {
			if np, ok := obj.(*networkingv1.NetworkPolicy); ok {
				m.handleNetworkPolicyChange(np, "deleted")
			}
		},
	})

	// Node event handlers
	m.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if oldNode, ok := oldObj.(*v1.Node); ok {
				if newNode, ok := newObj.(*v1.Node); ok {
					m.handleNodeUpdate(oldNode, newNode)
				}
			}
		},
	})
}

func (m *K8sInformerMonitor) handlePodAdd(pod *v1.Pod) {
	// Check if pod has IP allocated
	if pod.Status.PodIP == "" {
		return
	}

	eventID := fmt.Sprintf("pod_add_%s_%s", pod.Namespace, pod.Name)
	if m.isEventProcessed(eventID) {
		return
	}

	event := &core.CNIRawEvent{
		ID:           eventID,
		Timestamp:    time.Now(),
		Source:       "k8s-informer",
		Operation:    core.CNIOperationAdd,
		Success:      pod.Status.Phase != v1.PodFailed,
		PodName:      pod.Name,
		PodNamespace: pod.Namespace,
		AssignedIP:   pod.Status.PodIP,
		PluginName:   m.detectCNIPlugin(pod),
		Annotations: map[string]string{
			"event_type": "pod_ip_allocated",
			"pod_uid":    string(pod.UID),
			"node_name":  pod.Spec.NodeName,
			"phase":      string(pod.Status.Phase),
		},
	}

	// Add container network info
	if len(pod.Status.PodIPs) > 0 {
		ips := make([]string, 0, len(pod.Status.PodIPs))
		for _, podIP := range pod.Status.PodIPs {
			ips = append(ips, podIP.IP)
		}
		event.Annotations["pod_ips"] = strings.Join(ips, ",")
	}

	m.sendEvent(event)
}

func (m *K8sInformerMonitor) handlePodUpdate(oldPod, newPod *v1.Pod) {
	// Check for IP allocation changes
	if oldPod.Status.PodIP == "" && newPod.Status.PodIP != "" {
		// IP was allocated
		m.handlePodAdd(newPod)
		return
	}

	// Check for significant network-related changes
	if oldPod.Status.PodIP != newPod.Status.PodIP {
		eventID := fmt.Sprintf("pod_ip_change_%s_%s_%d", newPod.Namespace, newPod.Name, time.Now().UnixNano())
		event := &core.CNIRawEvent{
			ID:           eventID,
			Timestamp:    time.Now(),
			Source:       "k8s-informer",
			Operation:    core.CNIOperationOther,
			Success:      true,
			PodName:      newPod.Name,
			PodNamespace: newPod.Namespace,
			AssignedIP:   newPod.Status.PodIP,
			PluginName:   m.detectCNIPlugin(newPod),
			Annotations: map[string]string{
				"event_type": "pod_ip_changed",
				"old_ip":     oldPod.Status.PodIP,
				"new_ip":     newPod.Status.PodIP,
				"pod_uid":    string(newPod.UID),
			},
		}
		m.sendEvent(event)
	}
}

func (m *K8sInformerMonitor) handlePodDelete(pod *v1.Pod) {
	if pod.Status.PodIP == "" {
		return
	}

	eventID := fmt.Sprintf("pod_delete_%s_%s", pod.Namespace, pod.Name)
	if m.isEventProcessed(eventID) {
		return
	}

	event := &core.CNIRawEvent{
		ID:           eventID,
		Timestamp:    time.Now(),
		Source:       "k8s-informer",
		Operation:    core.CNIOperationDel,
		Success:      true,
		PodName:      pod.Name,
		PodNamespace: pod.Namespace,
		AssignedIP:   pod.Status.PodIP,
		PluginName:   m.detectCNIPlugin(pod),
		Annotations: map[string]string{
			"event_type": "pod_ip_deallocated",
			"pod_uid":    string(pod.UID),
			"node_name":  pod.Spec.NodeName,
		},
	}

	m.sendEvent(event)
}

func (m *K8sInformerMonitor) handleServiceChange(svc *v1.Service, changeType string) {
	eventID := fmt.Sprintf("service_%s_%s_%s", changeType, svc.Namespace, svc.Name)
	if m.isEventProcessed(eventID) {
		return
	}

	event := &core.CNIRawEvent{
		ID:           eventID,
		Timestamp:    time.Now(),
		Source:       "k8s-informer",
		Operation:    core.CNIOperationOther,
		Success:      true,
		PodNamespace: svc.Namespace,
		Annotations: map[string]string{
			"event_type":    "service_" + changeType,
			"service_name":  svc.Name,
			"service_type":  string(svc.Spec.Type),
			"cluster_ip":    svc.Spec.ClusterIP,
			"external_name": svc.Spec.ExternalName,
		},
	}

	// Add load balancer IPs if available
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		var lbIPs []string
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				lbIPs = append(lbIPs, ingress.IP)
			}
		}
		if len(lbIPs) > 0 {
			event.Annotations["load_balancer_ips"] = strings.Join(lbIPs, ",")
		}
	}

	m.sendEvent(event)
}

func (m *K8sInformerMonitor) handleEndpointsChange(ep *v1.Endpoints, changeType string) {
	eventID := fmt.Sprintf("endpoints_%s_%s_%s_%d", changeType, ep.Namespace, ep.Name, time.Now().UnixNano())

	// Count endpoint addresses
	addressCount := 0
	for _, subset := range ep.Subsets {
		addressCount += len(subset.Addresses)
	}

	event := &core.CNIRawEvent{
		ID:           eventID,
		Timestamp:    time.Now(),
		Source:       "k8s-informer",
		Operation:    core.CNIOperationOther,
		Success:      true,
		PodNamespace: ep.Namespace,
		Annotations: map[string]string{
			"event_type":     "endpoints_" + changeType,
			"endpoints_name": ep.Name,
			"address_count":  fmt.Sprintf("%d", addressCount),
		},
	}

	m.sendEvent(event)
}

func (m *K8sInformerMonitor) handleNetworkPolicyChange(np *networkingv1.NetworkPolicy, changeType string) {
	eventID := fmt.Sprintf("netpol_%s_%s_%s", changeType, np.Namespace, np.Name)
	if m.isEventProcessed(eventID) {
		return
	}

	event := &core.CNIRawEvent{
		ID:           eventID,
		Timestamp:    time.Now(),
		Source:       "k8s-informer",
		Operation:    core.CNIOperationOther,
		Success:      true,
		PodNamespace: np.Namespace,
		Annotations: map[string]string{
			"event_type":          "network_policy_" + changeType,
			"network_policy_name": np.Name,
		},
	}

	// Add policy type info
	if len(np.Spec.PolicyTypes) > 0 {
		policyTypes := make([]string, len(np.Spec.PolicyTypes))
		for i, pt := range np.Spec.PolicyTypes {
			policyTypes[i] = string(pt)
		}
		event.Annotations["policy_types"] = strings.Join(policyTypes, ",")
	}

	m.sendEvent(event)
}

func (m *K8sInformerMonitor) handleNodeUpdate(oldNode, newNode *v1.Node) {
	// Check for network-related condition changes
	oldReady := m.isNodeReady(oldNode)
	newReady := m.isNodeReady(newNode)
	oldNetworkUnavailable := m.isNodeNetworkUnavailable(oldNode)
	newNetworkUnavailable := m.isNodeNetworkUnavailable(newNode)

	if oldReady != newReady || oldNetworkUnavailable != newNetworkUnavailable {
		eventID := fmt.Sprintf("node_network_change_%s_%d", newNode.Name, time.Now().UnixNano())

		event := &core.CNIRawEvent{
			ID:        eventID,
			Timestamp: time.Now(),
			Source:    "k8s-informer",
			Operation: core.CNIOperationOther,
			Success:   true,
			Annotations: map[string]string{
				"event_type":          "node_network_status_change",
				"node_name":           newNode.Name,
				"ready":               fmt.Sprintf("%v", newReady),
				"network_unavailable": fmt.Sprintf("%v", newNetworkUnavailable),
			},
		}

		// Add node addresses
		var addresses []string
		for _, addr := range newNode.Status.Addresses {
			addresses = append(addresses, fmt.Sprintf("%s:%s", addr.Type, addr.Address))
		}
		if len(addresses) > 0 {
			event.Annotations["node_addresses"] = strings.Join(addresses, ",")
		}

		m.sendEvent(event)
	}
}

func (m *K8sInformerMonitor) detectCNIPlugin(pod *v1.Pod) string {
	// Try to detect CNI plugin from pod annotations
	annotations := pod.GetAnnotations()

	// Common CNI plugin annotations
	cniAnnotations := map[string]string{
		"cilium.io/global-ip":       "cilium",
		"cni.projectcalico.org/":    "calico",
		"flannel.alpha.coreos.com/": "flannel",
		"weave.works/":              "weave",
	}

	for annotation, plugin := range cniAnnotations {
		for key := range annotations {
			if strings.HasPrefix(key, annotation) {
				return plugin
			}
		}
	}

	// Check container runtime annotations
	if runtime, ok := annotations["io.kubernetes.cri.container-runtime"]; ok {
		if strings.Contains(runtime, "containerd") {
			// Could be any CNI plugin with containerd
			return "containerd-cni"
		}
	}

	return "unknown"
}

func (m *K8sInformerMonitor) isNodeReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func (m *K8sInformerMonitor) isNodeNetworkUnavailable(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeNetworkUnavailable {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func (m *K8sInformerMonitor) sendEvent(event *core.CNIRawEvent) {
	select {
	case m.eventChan <- *event:
		m.markEventProcessed(event.ID)
	case <-m.ctx.Done():
		return
	}
}

func (m *K8sInformerMonitor) isEventProcessed(eventID string) bool {
	_, exists := m.processedEvents.Load(eventID)
	return exists
}

func (m *K8sInformerMonitor) markEventProcessed(eventID string) {
	m.processedEvents.Store(eventID, time.Now())
}

func (m *K8sInformerMonitor) cleanupProcessedEvents() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-10 * time.Minute)
			m.processedEvents.Range(func(key, value interface{}) bool {
				if t, ok := value.(time.Time); ok && t.Before(cutoff) {
					m.processedEvents.Delete(key)
				}
				return true
			})
		}
	}
}
