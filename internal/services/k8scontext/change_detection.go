package k8scontext

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// detectDeploymentChanges compares old and new Deployment and emits diagnostic events
func (s *Service) detectDeploymentChanges(ctx context.Context, old, new *appsv1.Deployment) {
	// Image changed?
	oldImage := getContainerImage(old)
	newImage := getContainerImage(new)
	if oldImage != newImage && newImage != "" {
		s.emitDomainEvent(ctx, &domain.ObserverEvent{
			ID:        generateEventID(),
			Type:      "deployment",
			Subtype:   "image_changed",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			K8sData: &domain.K8sEventData{
				ResourceKind: "Deployment",
				ResourceName: new.Name,
				Action:       "updated",
				ImageChanged: true,
				OldImage:     oldImage,
				NewImage:     newImage,
			},
		})
	}

	// Replicas changed?
	if old.Spec.Replicas != nil && new.Spec.Replicas != nil {
		if *old.Spec.Replicas != *new.Spec.Replicas {
			s.emitDomainEvent(ctx, &domain.ObserverEvent{
				ID:        generateEventID(),
				Type:      "deployment",
				Subtype:   "scaled",
				Source:    "k8scontext",
				Timestamp: time.Now(),
				K8sData: &domain.K8sEventData{
					ResourceKind:    "Deployment",
					ResourceName:    new.Name,
					Action:          "updated",
					ReplicasChanged: true,
					OldReplicas:     *old.Spec.Replicas,
					NewReplicas:     *new.Spec.Replicas,
				},
			})
		}
	}

	// Rollout status changed?
	s.detectRolloutStatus(ctx, old, new)
}

// detectRolloutStatus detects deployment rollout status changes
func (s *Service) detectRolloutStatus(ctx context.Context, old, new *appsv1.Deployment) {
	// Check if rollout is progressing
	for _, cond := range new.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing {
			// Find old condition
			var oldCond *appsv1.DeploymentCondition
			for i := range old.Status.Conditions {
				if old.Status.Conditions[i].Type == appsv1.DeploymentProgressing {
					oldCond = &old.Status.Conditions[i]
					break
				}
			}

			// Status changed?
			if oldCond == nil || oldCond.Status != cond.Status || oldCond.Reason != cond.Reason {
				subtype := "rollout_progressing"
				if cond.Reason == "ProgressDeadlineExceeded" {
					subtype = "rollout_failed"
				} else if cond.Reason == "NewReplicaSetAvailable" {
					subtype = "rollout_complete"
				}

				s.emitDomainEvent(ctx, &domain.ObserverEvent{
					ID:        generateEventID(),
					Type:      "deployment",
					Subtype:   subtype,
					Source:    "k8scontext",
					Timestamp: time.Now(),
					K8sData: &domain.K8sEventData{
						ResourceKind: "Deployment",
						ResourceName: new.Name,
						Action:       "updated",
						Reason:       cond.Reason,
						Message:      cond.Message,
					},
				})
			}
		}
	}
}

// detectPodChanges compares old and new Pod and emits diagnostic events
func (s *Service) detectPodChanges(ctx context.Context, old, new *corev1.Pod) {
	// Phase changed?
	if old.Status.Phase != new.Status.Phase {
		s.emitDomainEvent(ctx, &domain.ObserverEvent{
			ID:        generateEventID(),
			Type:      "pod",
			Subtype:   "phase_changed",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			K8sData: &domain.K8sEventData{
				ResourceKind: "Pod",
				ResourceName: new.Name,
				Action:       "updated",
				Reason:       string(new.Status.Phase),
				Message:      string(old.Status.Phase) + " -> " + string(new.Status.Phase),
			},
		})
	}

	// Check for crash loops
	for _, cs := range new.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			// Check if this is a new crash loop (wasn't crashing before)
			wasCrashing := false
			for _, oldCS := range old.Status.ContainerStatuses {
				if oldCS.Name == cs.Name &&
					oldCS.State.Waiting != nil &&
					oldCS.State.Waiting.Reason == "CrashLoopBackOff" {
					wasCrashing = true
					break
				}
			}

			if !wasCrashing {
				s.emitDomainEvent(ctx, &domain.ObserverEvent{
					ID:        generateEventID(),
					Type:      "pod",
					Subtype:   "crash_loop",
					Source:    "k8scontext",
					Timestamp: time.Now(),
					K8sData: &domain.K8sEventData{
						ResourceKind: "Pod",
						ResourceName: new.Name,
						Action:       "updated",
						Reason:       "CrashLoopBackOff",
						Message:      "Container " + cs.Name + " in crash loop",
					},
					ContainerData: &domain.ContainerEventData{
						ContainerName: cs.Name,
						RestartCount:  cs.RestartCount,
					},
				})
			}
		}

		// Check for OOM kills
		if cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled" {
			// Check if this is a new OOM (wasn't OOMKilled before)
			wasOOM := false
			for _, oldCS := range old.Status.ContainerStatuses {
				if oldCS.Name == cs.Name &&
					oldCS.State.Terminated != nil &&
					oldCS.State.Terminated.Reason == "OOMKilled" {
					wasOOM = true
					break
				}
			}

			if !wasOOM {
				s.emitDomainEvent(ctx, &domain.ObserverEvent{
					ID:        generateEventID(),
					Type:      "pod",
					Subtype:   "oom_killed",
					Source:    "k8scontext",
					Timestamp: time.Now(),
					K8sData: &domain.K8sEventData{
						ResourceKind: "Pod",
						ResourceName: new.Name,
						Action:       "updated",
						Reason:       "OOMKilled",
						Message:      "Container " + cs.Name + " killed by OOM",
					},
					ContainerData: &domain.ContainerEventData{
						ContainerName: cs.Name,
						ExitCode:      cs.State.Terminated.ExitCode,
					},
				})
			}
		}
	}
}

// detectServiceChanges compares old and new Service and emits diagnostic events
func (s *Service) detectServiceChanges(ctx context.Context, old, new *corev1.Service) {
	// ClusterIP changed?
	if old.Spec.ClusterIP != new.Spec.ClusterIP && new.Spec.ClusterIP != "" && new.Spec.ClusterIP != "None" {
		s.emitDomainEvent(ctx, &domain.ObserverEvent{
			ID:        generateEventID(),
			Type:      "service",
			Subtype:   "ip_changed",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			K8sData: &domain.K8sEventData{
				ResourceKind: "Service",
				ResourceName: new.Name,
				Action:       "updated",
				Reason:       "ClusterIP changed",
				Message:      old.Spec.ClusterIP + " -> " + new.Spec.ClusterIP,
			},
		})
	}

	// Type changed?
	if old.Spec.Type != new.Spec.Type {
		s.emitDomainEvent(ctx, &domain.ObserverEvent{
			ID:        generateEventID(),
			Type:      "service",
			Subtype:   "type_changed",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			K8sData: &domain.K8sEventData{
				ResourceKind: "Service",
				ResourceName: new.Name,
				Action:       "updated",
				Reason:       "Type changed",
				Message:      string(old.Spec.Type) + " -> " + string(new.Spec.Type),
			},
		})
	}
}

// detectNodeChanges compares old and new Node and emits diagnostic events
func (s *Service) detectNodeChanges(ctx context.Context, old, new *corev1.Node) {
	// Check node conditions
	for _, cond := range new.Status.Conditions {
		// Find corresponding old condition
		var oldCond *corev1.NodeCondition
		for i := range old.Status.Conditions {
			if old.Status.Conditions[i].Type == cond.Type {
				oldCond = &old.Status.Conditions[i]
				break
			}
		}

		// Condition status changed?
		if oldCond == nil || oldCond.Status != cond.Status {
			subtype := "condition_changed"

			// Specific subtypes for important conditions
			if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
				subtype = "not_ready"
			} else if cond.Type == corev1.NodeMemoryPressure && cond.Status == corev1.ConditionTrue {
				subtype = "memory_pressure"
			} else if cond.Type == corev1.NodeDiskPressure && cond.Status == corev1.ConditionTrue {
				subtype = "disk_pressure"
			}

			s.emitDomainEvent(ctx, &domain.ObserverEvent{
				ID:        generateEventID(),
				Type:      "node",
				Subtype:   subtype,
				Source:    "k8scontext",
				Timestamp: time.Now(),
				K8sData: &domain.K8sEventData{
					ResourceKind: "Node",
					ResourceName: new.Name,
					Action:       "updated",
					Reason:       string(cond.Type),
					Message:      cond.Message,
				},
			})
		}
	}
}

// Helper functions

// getContainerImage returns the first container's image from a Deployment
func getContainerImage(deployment *appsv1.Deployment) string {
	if deployment == nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return deployment.Spec.Template.Spec.Containers[0].Image
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return uuid.New().String()
}
