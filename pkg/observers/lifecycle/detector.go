package lifecycle

import (
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// TransitionDetector detects lifecycle transitions that matter
type TransitionDetector struct {
	// No config, no scoring, just K8s mechanics
}

// NewTransitionDetector creates a lean detector
func NewTransitionDetector() *TransitionDetector {
	return &TransitionDetector{}
}

// DetectTransition identifies lifecycle changes that can break things
func (td *TransitionDetector) DetectTransition(
	kind string,
	old, new runtime.Object,
) *LifecycleTransition {

	// Quick noise filter - Pods and Nodes need special handling for status changes
	significant := td.hasSignificantChange(old, new)
	if kind != "Pod" && kind != "Node" && !significant {
		return nil
	}

	switch kind {
	case "Deployment":
		return td.detectDeploymentTransition(old, new)
	case "StatefulSet":
		return td.detectStatefulSetTransition(old, new)
	case "Pod":
		return td.detectPodTransition(old, new)
	case "Node":
		return td.detectNodeTransition(old, new)
	case "Service":
		return td.detectServiceTransition(old, new)
	default:
		return nil
	}
}

// detectDeploymentTransition - Scale downs, resource cuts, rollouts
func (td *TransitionDetector) detectDeploymentTransition(
	old, new runtime.Object,
) *LifecycleTransition {

	// Handle deletion - check for nil more carefully
	if new == nil || (new != nil && reflect.ValueOf(new).IsNil()) {
		if old == nil || (old != nil && reflect.ValueOf(old).IsNil()) {
			return nil // Both nil - no transition
		}
		oldDep := old.(*appsv1.Deployment)
		return &LifecycleTransition{
			Type: TransitionDeletion,
			State: StateChange{
				Resource:       td.extractResourceID(old),
				FromState:      "active",
				ToState:        "deleted",
				ReplicasBefore: int(*oldDep.Spec.Replicas),
				ReplicasAfter:  0,
			},
			Resources: AffectedResources{
				DirectCount: int(*oldDep.Spec.Replicas),
			},
			Cascade: []CascadeEffect{
				{Effect: "pods_terminating", Count: int(*oldDep.Spec.Replicas)},
				{Effect: "service_endpoints_removed", Count: int(*oldDep.Spec.Replicas)},
			},
		}
	}

	oldDep := old.(*appsv1.Deployment)
	newDep := new.(*appsv1.Deployment)

	oldReplicas := int(*oldDep.Spec.Replicas)
	newReplicas := int(*newDep.Spec.Replicas)

	// Scale to zero - SERVICE DOWN
	if newReplicas == 0 && oldReplicas > 0 {
		return &LifecycleTransition{
			Type: TransitionScaleToZero,
			State: StateChange{
				Resource:       td.extractResourceID(new),
				FromState:      "running",
				ToState:        "scaled_to_zero",
				ReplicasBefore: oldReplicas,
				ReplicasAfter:  0,
			},
			Resources: AffectedResources{
				DirectCount: oldReplicas,
			},
			Cascade: []CascadeEffect{
				{Effect: "service_unavailable", Count: 1},
				{Effect: "pods_terminating", Count: oldReplicas},
			},
		}
	}

	// Significant scale down
	if newReplicas < oldReplicas && (oldReplicas-newReplicas) > oldReplicas/2 {
		return &LifecycleTransition{
			Type: TransitionScaleDown,
			State: StateChange{
				Resource:       td.extractResourceID(new),
				FromState:      "scaled",
				ToState:        "reduced",
				ReplicasBefore: oldReplicas,
				ReplicasAfter:  newReplicas,
			},
			Resources: AffectedResources{
				DirectCount: oldReplicas - newReplicas,
			},
			Cascade: []CascadeEffect{
				{Effect: "capacity_halved", Count: 1},
				{Effect: "pods_terminating", Count: oldReplicas - newReplicas},
			},
		}
	}

	// Resource cuts
	if td.hasResourceCut(oldDep, newDep) {
		return &LifecycleTransition{
			Type:  TransitionResourceCut,
			State: td.extractResourceCut(oldDep, newDep),
			Resources: AffectedResources{
				DirectCount: newReplicas,
			},
			Cascade: []CascadeEffect{
				{Effect: "potential_oom", Count: newReplicas},
				{Effect: "potential_throttling", Count: newReplicas},
			},
		}
	}

	// Image update - rolling update
	if td.hasImageChange(oldDep, newDep) {
		return &LifecycleTransition{
			Type: TransitionImageUpdate,
			State: StateChange{
				Resource:  td.extractResourceID(new),
				FromState: "stable",
				ToState:   "updating",
			},
			Resources: AffectedResources{
				DirectCount: newReplicas,
			},
			Cascade: []CascadeEffect{
				{Effect: "rolling_update", Count: newReplicas},
			},
		}
	}

	return nil
}

// detectPodTransition - OOM kills, evictions, crash loops
func (td *TransitionDetector) detectPodTransition(
	old, new runtime.Object,
) *LifecycleTransition {

	// Check for nil more carefully - handle typed nils
	if new == nil || reflect.ValueOf(new).IsNil() {
		// Pod deleted/evicted
		if old == nil {
			return nil // Both nil - no transition
		}
		oldPod := old.(*corev1.Pod)
		transType := TransitionDeletion

		// Check if evicted
		if oldPod.Status.Reason == "Evicted" {
			transType = TransitionEviction
		}

		return &LifecycleTransition{
			Type: transType,
			State: StateChange{
				Resource:  td.extractResourceID(old),
				FromState: string(oldPod.Status.Phase),
				ToState:   "deleted",
			},
		}
	}

	if old == nil {
		return nil // Creation - not a breaking transition
	}

	oldPod, ok := old.(*corev1.Pod)
	if !ok || oldPod == nil {
		return nil
	}
	newPod, ok := new.(*corev1.Pod)
	if !ok || newPod == nil {
		return nil
	}

	// Check for OOMKill
	for i, container := range newPod.Status.ContainerStatuses {
		if container.LastTerminationState.Terminated != nil &&
			container.LastTerminationState.Terminated.Reason == "OOMKilled" {
			return &LifecycleTransition{
				Type: TransitionOOMKill,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: "running",
					ToState:   "oom_killed",
				},
				Cascade: []CascadeEffect{
					{Effect: "pod_restarting", Count: 1},
					{Effect: "memory_limit_too_low", Count: 1},
				},
			}
		}

		// Check for crash loop
		oldRestartCount := int32(0)
		if len(oldPod.Status.ContainerStatuses) > i {
			oldRestartCount = oldPod.Status.ContainerStatuses[i].RestartCount
		}

		if container.RestartCount > 5 && container.RestartCount > oldRestartCount {
			return &LifecycleTransition{
				Type: TransitionCrashLoop,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: "restarting",
					ToState:   "crash_loop",
				},
				Cascade: []CascadeEffect{
					{Effect: "service_degraded", Count: 1},
				},
			}
		}
	}

	// Phase transitions
	if oldPod.Status.Phase != newPod.Status.Phase {
		if newPod.Status.Phase == corev1.PodFailed {
			return &LifecycleTransition{
				Type: TransitionNotReady,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: string(oldPod.Status.Phase),
					ToState:   string(newPod.Status.Phase),
				},
			}
		}
	}

	return nil
}

// detectNodeTransition - Node pressure, draining, not ready
func (td *TransitionDetector) detectNodeTransition(
	old, new runtime.Object,
) *LifecycleTransition {

	// Check for nil more carefully - handle typed nils
	if new == nil || reflect.ValueOf(new).IsNil() {
		return &LifecycleTransition{
			Type: TransitionDeletion,
			State: StateChange{
				Resource:  td.extractResourceID(old),
				FromState: "ready",
				ToState:   "deleted",
			},
		}
	}

	newNode := new.(*corev1.Node)

	// Check node conditions
	for _, condition := range newNode.Status.Conditions {
		// Node NotReady
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return &LifecycleTransition{
				Type: TransitionNotReady,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: "ready",
					ToState:   "not_ready",
				},
				Cascade: []CascadeEffect{
					{Effect: "pods_may_be_evicted", Count: 1},
				},
			}
		}

		// Memory pressure
		if condition.Type == corev1.NodeMemoryPressure && condition.Status == corev1.ConditionTrue {
			return &LifecycleTransition{
				Type: TransitionNodePressure,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: "no_pressure",
					ToState:   "memory_pressure",
				},
				Cascade: []CascadeEffect{
					{Effect: "pods_may_be_evicted", Count: 1},
				},
			}
		}

		// Disk pressure
		if condition.Type == corev1.NodeDiskPressure && condition.Status == corev1.ConditionTrue {
			return &LifecycleTransition{
				Type: TransitionNodePressure,
				State: StateChange{
					Resource:  td.extractResourceID(new),
					FromState: "no_pressure",
					ToState:   "disk_pressure",
				},
				Cascade: []CascadeEffect{
					{Effect: "pods_may_be_evicted", Count: 1},
				},
			}
		}
	}

	return nil
}

// detectServiceTransition - Endpoints lost
func (td *TransitionDetector) detectServiceTransition(
	old, new runtime.Object,
) *LifecycleTransition {

	if new == nil || reflect.ValueOf(new).IsNil() {
		return &LifecycleTransition{
			Type: TransitionDeletion,
			State: StateChange{
				Resource:  td.extractResourceID(old),
				FromState: "active",
				ToState:   "deleted",
			},
			Cascade: []CascadeEffect{
				{Effect: "service_unavailable", Count: 1},
			},
		}
	}

	// Service itself doesn't have many breaking transitions
	// The important ones come from EndpointSlice changes
	return nil
}

// detectStatefulSetTransition - Similar to deployment but ordered
func (td *TransitionDetector) detectStatefulSetTransition(
	old, new runtime.Object,
) *LifecycleTransition {
	// Similar logic to deployment
	// StatefulSets have ordered deletion which is more impactful
	return td.detectDeploymentTransition(old, new)
}

// Helper methods

func (td *TransitionDetector) hasSignificantChange(old, new runtime.Object) bool {
	// Deletion is always significant - handle typed nils
	if new == nil || (new != nil && reflect.ValueOf(new).IsNil()) {
		return old != nil && (old == nil || !reflect.ValueOf(old).IsNil())
	}

	// Creation is always significant - handle typed nils
	if old == nil || (old != nil && reflect.ValueOf(old).IsNil()) {
		return true
	}

	// Both exist - check spec changes only, ignore status
	oldValue := reflect.ValueOf(old)
	newValue := reflect.ValueOf(new)

	// Make sure we have valid pointers
	if !oldValue.IsValid() || oldValue.IsNil() || !newValue.IsValid() || newValue.IsNil() {
		return false
	}

	// Dereference pointers to get the struct
	if oldValue.Kind() == reflect.Ptr {
		oldValue = oldValue.Elem()
	}
	if newValue.Kind() == reflect.Ptr {
		newValue = newValue.Elem()
	}

	// Get the Spec field if it exists
	oldSpec := oldValue.FieldByName("Spec")
	newSpec := newValue.FieldByName("Spec")

	if oldSpec.IsValid() && newSpec.IsValid() {
		return !reflect.DeepEqual(oldSpec.Interface(), newSpec.Interface())
	}

	return false
}

func (td *TransitionDetector) hasResourceCut(old, new *appsv1.Deployment) bool {
	if len(old.Spec.Template.Spec.Containers) == 0 ||
		len(new.Spec.Template.Spec.Containers) == 0 {
		return false
	}

	oldC := old.Spec.Template.Spec.Containers[0]
	newC := new.Spec.Template.Spec.Containers[0]

	// Check CPU
	if oldC.Resources.Limits != nil && newC.Resources.Limits != nil {
		oldCPU := oldC.Resources.Limits.Cpu()
		newCPU := newC.Resources.Limits.Cpu()
		if oldCPU != nil && newCPU != nil && newCPU.Cmp(*oldCPU) < 0 {
			return true
		}

		oldMem := oldC.Resources.Limits.Memory()
		newMem := newC.Resources.Limits.Memory()
		if oldMem != nil && newMem != nil && newMem.Cmp(*oldMem) < 0 {
			return true
		}
	}

	return false
}

func (td *TransitionDetector) extractResourceCut(old, new *appsv1.Deployment) StateChange {
	state := StateChange{
		Resource:  td.extractResourceID(new),
		FromState: "normal_resources",
		ToState:   "reduced_resources",
	}

	if len(old.Spec.Template.Spec.Containers) > 0 &&
		len(new.Spec.Template.Spec.Containers) > 0 {
		oldC := old.Spec.Template.Spec.Containers[0]
		newC := new.Spec.Template.Spec.Containers[0]

		if oldC.Resources.Limits != nil {
			if cpu := oldC.Resources.Limits.Cpu(); cpu != nil {
				state.CPUBefore = cpu.String()
			}
			if mem := oldC.Resources.Limits.Memory(); mem != nil {
				state.MemoryBefore = mem.String()
			}
		}

		if newC.Resources.Limits != nil {
			if cpu := newC.Resources.Limits.Cpu(); cpu != nil {
				state.CPUAfter = cpu.String()
			}
			if mem := newC.Resources.Limits.Memory(); mem != nil {
				state.MemoryAfter = mem.String()
			}
		}
	}

	return state
}

func (td *TransitionDetector) hasImageChange(old, new *appsv1.Deployment) bool {
	if len(old.Spec.Template.Spec.Containers) == 0 ||
		len(new.Spec.Template.Spec.Containers) == 0 {
		return false
	}

	return old.Spec.Template.Spec.Containers[0].Image !=
		new.Spec.Template.Spec.Containers[0].Image
}

func (td *TransitionDetector) extractResourceID(obj runtime.Object) ResourceIdentifier {
	metaObj, _ := meta.Accessor(obj)
	gvk := obj.GetObjectKind().GroupVersionKind()

	return ResourceIdentifier{
		Kind:       gvk.Kind,
		Name:       metaObj.GetName(),
		Namespace:  metaObj.GetNamespace(),
		UID:        metaObj.GetUID(),
		APIVersion: gvk.Version,
	}
}

// IsBreaking determines if this transition breaks availability
func (td *TransitionDetector) IsBreaking(transition *LifecycleTransition) bool {
	switch transition.Type {
	case TransitionScaleToZero,
		TransitionDeletion,
		TransitionOOMKill,
		TransitionCrashLoop,
		TransitionEviction,
		TransitionNotReady:
		return true
	case TransitionScaleDown:
		// Breaking if >50% reduction
		return transition.State.ReplicasBefore > 0 &&
			transition.State.ReplicasAfter < transition.State.ReplicasBefore/2
	case TransitionResourceCut:
		// Always breaking - can cause OOM
		return true
	default:
		return false
	}
}
