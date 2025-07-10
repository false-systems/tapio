package sniffer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// K8sSniffer implements the Sniffer interface for Kubernetes API monitoring
type K8sSniffer struct {
	// Core components
	client    kubernetes.Interface
	eventChan chan Event
	ctx       context.Context
	cancel    context.CancelFunc
	config    Config

	// Health tracking
	mu              sync.RWMutex
	lastEventTime   time.Time
	eventsProcessed uint64
	eventsDropped   uint64
	isRunning       bool

	// Watchers
	podInformer   cache.SharedIndexInformer
	eventInformer cache.SharedIndexInformer
	nodeInformer  cache.SharedIndexInformer

	// State tracking
	podStates    map[string]*PodState
	nodeStates   map[string]*NodeState
	stateMutex   sync.RWMutex
}

// PodState tracks pod state for change detection
type PodState struct {
	Pod            *corev1.Pod
	LastRestarts   map[string]int32 // container -> restart count
	LastPhase      corev1.PodPhase
	LastConditions map[corev1.PodConditionType]corev1.ConditionStatus
	CrashLoopCount int
	LastCrashTime  time.Time
}

// NodeState tracks node state
type NodeState struct {
	Node           *corev1.Node
	LastConditions map[corev1.NodeConditionType]corev1.ConditionStatus
}

// NewK8sSniffer creates a new Kubernetes API sniffer
func NewK8sSniffer(client kubernetes.Interface) *K8sSniffer {
	return &K8sSniffer{
		client:     client,
		podStates:  make(map[string]*PodState),
		nodeStates: make(map[string]*NodeState),
	}
}

// Name returns the unique name of this sniffer
func (s *K8sSniffer) Name() string {
	return "k8s-api"
}

// Events returns the event channel
func (s *K8sSniffer) Events() <-chan Event {
	return s.eventChan
}

// Start begins Kubernetes API monitoring
func (s *K8sSniffer) Start(ctx context.Context, config Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return fmt.Errorf("K8s sniffer already running")
	}

	s.config = config
	s.eventChan = make(chan Event, config.EventBufferSize)
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.isRunning = true
	s.lastEventTime = time.Now()

	// Start informers
	if err := s.startInformers(); err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	// Start periodic checks
	go s.performPeriodicChecks()

	return nil
}

// Health returns the current health status
func (s *K8sSniffer) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := HealthStatusHealthy
	message := "K8s API monitoring active"

	if !s.isRunning {
		status = HealthStatusUnhealthy
		message = "K8s sniffer not running"
	} else if time.Since(s.lastEventTime) > 5*time.Minute {
		status = HealthStatusDegraded
		message = "No events in last 5 minutes"
	}

	metrics := map[string]interface{}{
		"pods_tracked":  len(s.podStates),
		"nodes_tracked": len(s.nodeStates),
	}

	return Health{
		Status:          status,
		Message:         message,
		LastEventTime:   s.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&s.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&s.eventsDropped),
		Metrics:         metrics,
	}
}

// startInformers sets up Kubernetes watchers
func (s *K8sSniffer) startInformers() error {
	// Pod informer
	s.podInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(s.client.CoreV1().RESTClient(), "pods", "", fields.Everything()),
		&corev1.Pod{},
		time.Minute,
		cache.Indexers{},
	)

	s.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onPodAdd,
		UpdateFunc: s.onPodUpdate,
		DeleteFunc: s.onPodDelete,
	})

	// Event informer
	s.eventInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(s.client.CoreV1().RESTClient(), "events", "", fields.Everything()),
		&corev1.Event{},
		time.Minute,
		cache.Indexers{},
	)

	s.eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: s.onEventAdd,
	})

	// Node informer
	s.nodeInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(s.client.CoreV1().RESTClient(), "nodes", "", fields.Everything()),
		&corev1.Node{},
		time.Minute,
		cache.Indexers{},
	)

	s.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: s.onNodeUpdate,
	})

	// Start informers
	go s.podInformer.Run(s.ctx.Done())
	go s.eventInformer.Run(s.ctx.Done())
	go s.nodeInformer.Run(s.ctx.Done())

	// Wait for sync
	if !cache.WaitForCacheSync(s.ctx.Done(), 
		s.podInformer.HasSynced,
		s.eventInformer.HasSynced,
		s.nodeInformer.HasSynced) {
		return fmt.Errorf("failed to sync caches")
	}

	return nil
}

// onPodAdd handles new pod events
func (s *K8sSniffer) onPodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	s.stateMutex.Lock()
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	s.podStates[key] = s.createPodState(pod)
	s.stateMutex.Unlock()

	// Check for immediate issues
	if pod.Status.Phase == corev1.PodFailed {
		s.emitPodFailedEvent(pod)
	}
}

// onPodUpdate handles pod updates
func (s *K8sSniffer) onPodUpdate(oldObj, newObj interface{}) {
	oldPod, ok1 := oldObj.(*corev1.Pod)
	newPod, ok2 := newObj.(*corev1.Pod)
	if !ok1 || !ok2 {
		return
	}

	s.stateMutex.Lock()
	key := fmt.Sprintf("%s/%s", newPod.Namespace, newPod.Name)
	oldState, exists := s.podStates[key]
	if !exists {
		oldState = s.createPodState(oldPod)
	}
	newState := s.createPodState(newPod)
	s.podStates[key] = newState
	s.stateMutex.Unlock()

	// Detect changes
	s.detectPodChanges(oldState, newState, newPod)
}

// onPodDelete handles pod deletion
func (s *K8sSniffer) onPodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		// Handle deleted final state unknown
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		pod, ok = deletedState.Obj.(*corev1.Pod)
		if !ok {
			return
		}
	}

	s.stateMutex.Lock()
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	delete(s.podStates, key)
	s.stateMutex.Unlock()

	// Emit deletion event if unexpected
	if pod.DeletionGracePeriodSeconds != nil && *pod.DeletionGracePeriodSeconds == 0 {
		s.emitPodTerminatedEvent(pod, "Force deleted")
	}
}

// onEventAdd handles new Kubernetes events
func (s *K8sSniffer) onEventAdd(obj interface{}) {
	event, ok := obj.(*corev1.Event)
	if !ok {
		return
	}

	// Filter interesting events
	if s.isInterestingEvent(event) {
		s.emitK8sEvent(event)
	}
}

// onNodeUpdate handles node updates
func (s *K8sSniffer) onNodeUpdate(oldObj, newObj interface{}) {
	oldNode, ok1 := oldObj.(*corev1.Node)
	newNode, ok2 := newObj.(*corev1.Node)
	if !ok1 || !ok2 {
		return
	}

	s.detectNodeChanges(oldNode, newNode)
}

// detectPodChanges detects significant pod changes
func (s *K8sSniffer) detectPodChanges(oldState, newState *PodState, pod *corev1.Pod) {
	// Detect container restarts
	for container, newRestarts := range newState.LastRestarts {
		oldRestarts := oldState.LastRestarts[container]
		if newRestarts > oldRestarts {
			s.emitContainerRestartEvent(pod, container, newRestarts)
			
			// Check for crash loop
			if newRestarts > 3 {
				newState.CrashLoopCount++
				newState.LastCrashTime = time.Now()
				
				if newState.CrashLoopCount > 5 {
					s.emitCrashLoopEvent(pod, container, newRestarts)
				}
			}
		}
	}

	// Detect phase changes
	if oldState.LastPhase != newState.LastPhase {
		if newState.LastPhase == corev1.PodFailed {
			s.emitPodFailedEvent(pod)
		} else if newState.LastPhase == corev1.PodPending && 
			time.Since(pod.CreationTimestamp.Time) > 5*time.Minute {
			s.emitPodStuckEvent(pod)
		}
	}

	// Detect condition changes
	for condType, newStatus := range newState.LastConditions {
		if oldStatus, exists := oldState.LastConditions[condType]; exists && oldStatus != newStatus {
			if condType == corev1.PodReady && newStatus == corev1.ConditionFalse {
				s.emitPodNotReadyEvent(pod, condType)
			}
		}
	}
}

// detectNodeChanges detects significant node changes
func (s *K8sSniffer) detectNodeChanges(oldNode, newNode *corev1.Node) {
	// Check for node pressure conditions
	for _, condition := range newNode.Status.Conditions {
		switch condition.Type {
		case corev1.NodeMemoryPressure:
			if condition.Status == corev1.ConditionTrue {
				s.emitNodePressureEvent(newNode, "memory")
			}
		case corev1.NodeDiskPressure:
			if condition.Status == corev1.ConditionTrue {
				s.emitNodePressureEvent(newNode, "disk")
			}
		case corev1.NodePIDPressure:
			if condition.Status == corev1.ConditionTrue {
				s.emitNodePressureEvent(newNode, "pid")
			}
		}
	}
}

// Event emission methods

func (s *K8sSniffer) emitContainerRestartEvent(pod *corev1.Pod, container string, restarts int32) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "container_restart",
		Severity:  SeverityMedium,
		Data: map[string]interface{}{
			"container":     container,
			"restart_count": restarts,
			"reason":        s.getContainerTerminationReason(pod, container),
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Container: container,
			Labels:    pod.Labels,
		},
		Actionable: &ActionableItem{
			Title:       fmt.Sprintf("Container %s restarted %d times", container, restarts),
			Description: "Container is restarting frequently, investigate logs",
			Commands: []string{
				fmt.Sprintf("kubectl logs %s -n %s -c %s --previous", pod.Name, pod.Namespace, container),
				fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		},
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitCrashLoopEvent(pod *corev1.Pod, container string, restarts int32) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "crash_loop_backoff",
		Severity:  SeverityCritical,
		Data: map[string]interface{}{
			"container":     container,
			"restart_count": restarts,
			"reason":        s.getContainerTerminationReason(pod, container),
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Container: container,
			Labels:    pod.Labels,
		},
		Actionable: &ActionableItem{
			Title:       "Container in CrashLoopBackOff",
			Description: fmt.Sprintf("Container %s has restarted %d times and is in a crash loop", container, restarts),
			Commands: []string{
				fmt.Sprintf("kubectl logs %s -n %s -c %s --previous", pod.Name, pod.Namespace, container),
				fmt.Sprintf("kubectl get events --field-selector involvedObject.name=%s -n %s", pod.Name, pod.Namespace),
				fmt.Sprintf("kubectl rollout undo deployment/%s -n %s", s.getDeploymentName(pod), pod.Namespace),
			},
			Risk:            "medium",
			EstimatedImpact: "Will rollback to previous deployment version",
		},
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitPodFailedEvent(pod *corev1.Pod) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "pod_failed",
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"phase":   pod.Status.Phase,
			"reason":  pod.Status.Reason,
			"message": pod.Status.Message,
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Node:      pod.Spec.NodeName,
			Labels:    pod.Labels,
		},
		Actionable: &ActionableItem{
			Title:       "Pod Failed",
			Description: fmt.Sprintf("Pod %s has failed: %s", pod.Name, pod.Status.Reason),
			Commands: []string{
				fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
				fmt.Sprintf("kubectl delete pod %s -n %s", pod.Name, pod.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Pod will be recreated by its controller",
		},
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitPodStuckEvent(pod *corev1.Pod) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "pod_stuck_pending",
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"phase":         pod.Status.Phase,
			"reason":        pod.Status.Reason,
			"pending_time":  time.Since(pod.CreationTimestamp.Time).Minutes(),
			"scheduled":     pod.Spec.NodeName != "",
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		},
		Actionable: s.getPodStuckActionable(pod),
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitNodePressureEvent(node *corev1.Node, pressureType string) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      fmt.Sprintf("node_%s_pressure", pressureType),
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"pressure_type": pressureType,
			"allocatable":   node.Status.Allocatable,
			"capacity":      node.Status.Capacity,
		},
		Context: &EventContext{
			Node: node.Name,
		},
		Actionable: &ActionableItem{
			Title:       fmt.Sprintf("Node %s Pressure", strings.Title(pressureType)),
			Description: fmt.Sprintf("Node %s is experiencing %s pressure", node.Name, pressureType),
			Commands: []string{
				fmt.Sprintf("kubectl describe node %s", node.Name),
				fmt.Sprintf("kubectl top node %s", node.Name),
				fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data", node.Name),
			},
			Risk:            "high",
			EstimatedImpact: "Will evict pods from the node",
		},
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitK8sEvent(k8sEvent *corev1.Event) {
	severity := SeverityLow
	if k8sEvent.Type == "Warning" {
		severity = SeverityMedium
	}
	if strings.Contains(strings.ToLower(k8sEvent.Reason), "failed") ||
		strings.Contains(strings.ToLower(k8sEvent.Reason), "error") {
		severity = SeverityHigh
	}

	event := Event{
		ID:        uuid.New().String(),
		Timestamp: k8sEvent.FirstTimestamp.Time,
		Source:    "k8s-api",
		Type:      fmt.Sprintf("k8s_event_%s", strings.ToLower(k8sEvent.Reason)),
		Severity:  severity,
		Data: map[string]interface{}{
			"reason":  k8sEvent.Reason,
			"message": k8sEvent.Message,
			"count":   k8sEvent.Count,
			"type":    k8sEvent.Type,
		},
		Context: &EventContext{
			Pod:       k8sEvent.InvolvedObject.Name,
			Namespace: k8sEvent.InvolvedObject.Namespace,
		},
	}

	s.emitEvent(&event)
}

// Helper methods

func (s *K8sSniffer) createPodState(pod *corev1.Pod) *PodState {
	state := &PodState{
		Pod:            pod,
		LastPhase:      pod.Status.Phase,
		LastRestarts:   make(map[string]int32),
		LastConditions: make(map[corev1.PodConditionType]corev1.ConditionStatus),
	}

	// Track container restarts
	for _, cs := range pod.Status.ContainerStatuses {
		state.LastRestarts[cs.Name] = cs.RestartCount
	}

	// Track conditions
	for _, cond := range pod.Status.Conditions {
		state.LastConditions[cond.Type] = cond.Status
	}

	return state
}

func (s *K8sSniffer) getContainerTerminationReason(pod *corev1.Pod, container string) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == container && cs.LastTerminationState.Terminated != nil {
			return cs.LastTerminationState.Terminated.Reason
		}
	}
	return "Unknown"
}

func (s *K8sSniffer) getDeploymentName(pod *corev1.Pod) string {
	// Extract deployment name from pod
	if owner := metav1.GetControllerOf(pod); owner != nil {
		if owner.Kind == "ReplicaSet" {
			// Extract deployment name from RS
			parts := strings.Split(owner.Name, "-")
			if len(parts) > 1 {
				return strings.Join(parts[:len(parts)-1], "-")
			}
		}
	}
	return pod.Name
}

func (s *K8sSniffer) getPodStuckActionable(pod *corev1.Pod) *ActionableItem {
	// Determine why pod is stuck
	conditions := pod.Status.Conditions
	
	for _, cond := range conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			return &ActionableItem{
				Title:       "Pod Cannot Be Scheduled",
				Description: cond.Message,
				Commands: []string{
					"kubectl get nodes",
					"kubectl top nodes",
					fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
				},
				Risk:            "low",
				EstimatedImpact: "Diagnostic only",
			}
		}
	}

	// Check for image pull issues
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && strings.Contains(cs.State.Waiting.Reason, "ImagePull") {
			return &ActionableItem{
				Title:       "Image Pull Issue",
				Description: cs.State.Waiting.Message,
				Commands: []string{
					fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
					fmt.Sprintf("kubectl get events --field-selector involvedObject.name=%s -n %s", pod.Name, pod.Namespace),
				},
				Risk:            "low",
				EstimatedImpact: "Diagnostic only",
			}
		}
	}

	return &ActionableItem{
		Title:       "Pod Stuck in Pending State",
		Description: "Pod has been pending for over 5 minutes",
		Commands: []string{
			fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
			fmt.Sprintf("kubectl get events --field-selector involvedObject.name=%s -n %s", pod.Name, pod.Namespace),
		},
		Risk:            "low",
		EstimatedImpact: "Diagnostic only",
	}
}

func (s *K8sSniffer) isInterestingEvent(event *corev1.Event) bool {
	// Filter for interesting event types
	interestingReasons := []string{
		"Failed", "FailedScheduling", "FailedMount", "FailedAttachVolume",
		"BackOff", "CrashLoopBackOff", "OOMKilled", "Evicted",
		"NodeNotReady", "Unhealthy", "FailedCreate",
	}

	reason := event.Reason
	for _, interesting := range interestingReasons {
		if strings.Contains(reason, interesting) {
			return true
		}
	}

	return event.Type == "Warning" && event.Count > 3
}

func (s *K8sSniffer) emitEvent(event *Event) {
	select {
	case s.eventChan <- *event:
		atomic.AddUint64(&s.eventsProcessed, 1)
		s.mu.Lock()
		s.lastEventTime = time.Now()
		s.mu.Unlock()
	default:
		atomic.AddUint64(&s.eventsDropped, 1)
	}
}

func (s *K8sSniffer) emitPodNotReadyEvent(pod *corev1.Pod, condType corev1.PodConditionType) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "pod_not_ready",
		Severity:  SeverityMedium,
		Data: map[string]interface{}{
			"condition": string(condType),
			"phase":     pod.Status.Phase,
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Node:      pod.Spec.NodeName,
			Labels:    pod.Labels,
		},
		Actionable: &ActionableItem{
			Title:       "Pod Not Ready",
			Description: fmt.Sprintf("Pod %s is not ready", pod.Name),
			Commands: []string{
				fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace),
				fmt.Sprintf("kubectl logs %s -n %s --all-containers", pod.Name, pod.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		},
	}

	s.emitEvent(&event)
}

func (s *K8sSniffer) emitPodTerminatedEvent(pod *corev1.Pod, reason string) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "pod_terminated",
		Severity:  SeverityMedium,
		Data: map[string]interface{}{
			"reason": reason,
		},
		Context: &EventContext{
			Pod:       pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		},
	}

	s.emitEvent(&event)
}

// performPeriodicChecks runs periodic health checks
func (s *K8sSniffer) performPeriodicChecks() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkForStuckPods()
			s.checkForNodeIssues()
		}
	}
}

func (s *K8sSniffer) checkForStuckPods() {
	// Get all pods from informer cache
	items := s.podInformer.GetStore().List()
	
	for _, item := range items {
		pod, ok := item.(*corev1.Pod)
		if !ok {
			continue
		}

		// Check for pods pending too long
		if pod.Status.Phase == corev1.PodPending && 
			time.Since(pod.CreationTimestamp.Time) > 10*time.Minute {
			s.emitPodStuckEvent(pod)
		}
	}
}

func (s *K8sSniffer) checkForNodeIssues() {
	// Get all nodes from informer cache
	items := s.nodeInformer.GetStore().List()
	
	for _, item := range items {
		node, ok := item.(*corev1.Node)
		if !ok {
			continue
		}

		// Check node conditions
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
				s.emitNodeNotReadyEvent(node)
			}
		}
	}
}

func (s *K8sSniffer) emitNodeNotReadyEvent(node *corev1.Node) {
	event := Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Type:      "node_not_ready",
		Severity:  SeverityCritical,
		Data: map[string]interface{}{
			"node": node.Name,
		},
		Context: &EventContext{
			Node: node.Name,
		},
		Actionable: &ActionableItem{
			Title:       "Node Not Ready",
			Description: fmt.Sprintf("Node %s is not ready", node.Name),
			Commands: []string{
				fmt.Sprintf("kubectl describe node %s", node.Name),
				fmt.Sprintf("kubectl get pods -o wide --field-selector spec.nodeName=%s", node.Name),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		},
	}

	s.emitEvent(&event)
}

// Stop stops the K8s sniffer
func (s *K8sSniffer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	// Cancel context to stop informers
	if s.cancel != nil {
		s.cancel()
	}

	// Close event channel
	close(s.eventChan)

	s.isRunning = false
	return nil
}