package helmcorrelator

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
)

// onSecretAdd handles new secrets (potential new Helm releases)
func (c *Collector) onSecretAdd(obj interface{}) {
	secret, ok := obj.(*v1.Secret)
	if !ok {
		return
	}

	// Check if it's a Helm secret
	if secret.Type != "helm.sh/release.v1" {
		return
	}

	c.logger.Debug("Helm secret added",
		zap.String("name", secret.Name),
		zap.String("namespace", secret.Namespace),
	)

	// Decode the release
	decoder := NewHelmSecretDecoder(c.logger)
	release, err := decoder.DecodeSecret(secret)
	if err != nil {
		c.logger.Error("Failed to decode Helm secret",
			zap.String("secret", secret.Name),
			zap.Error(err),
		)
		return
	}

	// Cache the release
	cacheKey := fmt.Sprintf("%s/%s", release.Namespace, release.Name)
	c.releaseCache.Store(cacheKey, release)

	// Check if this is a failure
	if c.isFailedRelease(release) {
		c.logger.Warn("New Helm release in failed state",
			zap.String("release", release.Name),
			zap.String("status", release.Status),
		)

		// Try to correlate with recent operations
		c.correlateReleaseFailure(release)
	}

	// Track releases (could add metrics here if needed)
}

// onSecretUpdate handles secret updates (release status changes)
func (c *Collector) onSecretUpdate(oldObj, newObj interface{}) {
	newSecret, ok := newObj.(*v1.Secret)
	if !ok || newSecret.Type != "helm.sh/release.v1" {
		return
	}

	oldSecret, ok := oldObj.(*v1.Secret)
	if !ok {
		return
	}

	decoder := NewHelmSecretDecoder(c.logger)

	// Decode both versions
	oldRelease, err := decoder.DecodeSecret(oldSecret)
	if err != nil {
		c.logger.Error("Failed to decode old Helm secret", zap.Error(err))
		return
	}

	newRelease, err := decoder.DecodeSecret(newSecret)
	if err != nil {
		c.logger.Error("Failed to decode new Helm secret", zap.Error(err))
		return
	}

	// Compare releases
	changes := decoder.CompareReleases(oldRelease, newRelease)
	if len(changes) > 0 {
		c.logger.Info("Helm release changed",
			zap.String("release", newRelease.Name),
			zap.String("namespace", newRelease.Namespace),
			zap.Strings("changes", changes),
		)
	}

	// Cache the new release
	cacheKey := fmt.Sprintf("%s/%s", newRelease.Namespace, newRelease.Name)
	c.releaseCache.Store(cacheKey, newRelease)

	// Check for state transitions that indicate problems
	if c.detectProblematicTransition(oldRelease, newRelease) {
		c.logger.Warn("Problematic Helm release transition detected",
			zap.String("release", newRelease.Name),
			zap.String("old_status", oldRelease.Status),
			zap.String("new_status", newRelease.Status),
		)

		// Correlate with operations and K8s events
		c.correlateReleaseTransition(oldRelease, newRelease)
	}
}

// onSecretDelete handles secret deletion
func (c *Collector) onSecretDelete(obj interface{}) {
	secret, ok := obj.(*v1.Secret)
	if !ok || secret.Type != "helm.sh/release.v1" {
		return
	}

	decoder := NewHelmSecretDecoder(c.logger)
	releaseName, version := decoder.ParseHelmSecretName(secret.Name)

	c.logger.Info("Helm release deleted",
		zap.String("release", releaseName),
		zap.Int("version", version),
		zap.String("namespace", secret.Namespace),
	)

	// Remove from cache
	cacheKey := fmt.Sprintf("%s/%s", secret.Namespace, releaseName)
	c.releaseCache.Delete(cacheKey)
}

// onEventAdd handles new K8s events
func (c *Collector) onEventAdd(obj interface{}) {
	event, ok := obj.(*v1.Event)
	if !ok {
		return
	}

	// Only track Warning events
	if event.Type != "Warning" {
		return
	}

	// Store event for correlation
	k8sEvent := K8sEvent{
		Timestamp: event.FirstTimestamp.Time,
		Type:      event.Type,
		Reason:    event.Reason,
		Object:    fmt.Sprintf("%s/%s", strings.ToLower(event.InvolvedObject.Kind), event.InvolvedObject.Name),
		Message:   event.Message,
		FirstSeen: event.FirstTimestamp.Time,
		LastSeen:  event.LastTimestamp.Time,
		Count:     event.Count,
	}

	// Check if this event is related to Helm operations
	if c.isHelmRelatedEvent(event) {
		c.logger.Debug("Helm-related K8s event",
			zap.String("reason", event.Reason),
			zap.String("object", k8sEvent.Object),
			zap.String("message", event.Message),
		)

		// Store for correlation
		c.correlator.AddEvent(k8sEvent)
	}
}

// onPodUpdate handles pod updates
func (c *Collector) onPodUpdate(oldObj, newObj interface{}) {
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		return
	}

	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		return
	}

	// Check if this is a Helm hook pod
	if !c.isHelmHookPod(newPod) {
		return
	}

	// Check for status changes that indicate failure
	if oldPod.Status.Phase != newPod.Status.Phase {
		c.logger.Debug("Helm hook pod status changed",
			zap.String("pod", newPod.Name),
			zap.String("old_phase", string(oldPod.Status.Phase)),
			zap.String("new_phase", string(newPod.Status.Phase)),
		)

		// Convert to our PodStatus type
		podStatus := c.convertPodStatus(newPod)

		// Check for failures
		if c.isPodFailed(podStatus) {
			c.logger.Warn("Helm hook pod failed",
				zap.String("pod", newPod.Name),
				zap.String("reason", podStatus.Reason),
				zap.String("message", podStatus.Message),
			)

			// Store for correlation
			c.correlator.AddPodFailure(podStatus)
		}
	}
}

// onJobUpdate handles job updates (Helm hooks are usually Jobs)
func (c *Collector) onJobUpdate(oldObj, newObj interface{}) {
	newJob, ok := newObj.(*batchv1.Job)
	if !ok {
		return
	}

	// Check if this is a Helm hook job
	if !c.isHelmHookJob(newJob) {
		return
	}

	jobStatus := c.convertJobStatus(newJob)

	// Check for completion or failure
	if newJob.Status.Succeeded > 0 {
		c.logger.Debug("Helm hook job succeeded",
			zap.String("job", newJob.Name),
			zap.String("namespace", newJob.Namespace),
		)
	} else if newJob.Status.Failed > 0 {
		c.logger.Warn("Helm hook job failed",
			zap.String("job", newJob.Name),
			zap.String("namespace", newJob.Namespace),
			zap.Int32("failed", newJob.Status.Failed),
		)

		// Store for correlation
		c.correlator.AddJobFailure(jobStatus)

		// Try immediate correlation if we have a matching operation
		c.correlateJobFailure(jobStatus)
	}
}

// Helper methods

// isFailedRelease checks if a release is in a failed state
func (c *Collector) isFailedRelease(release *HelmRelease) bool {
	status := strings.ToLower(release.Status)
	return strings.Contains(status, "failed") ||
		strings.Contains(status, "pending") ||
		strings.Contains(status, "superseded")
}

// detectProblematicTransition detects problematic state transitions
func (c *Collector) detectProblematicTransition(old, new *HelmRelease) bool {
	// Transitions that indicate problems
	problemTransitions := map[string]map[string]bool{
		"deployed": {
			"failed":           true,
			"pending-upgrade":  true,
			"pending-rollback": true,
		},
		"pending-upgrade": {
			"failed": true,
		},
		"pending-install": {
			"failed": true,
		},
		"pending-rollback": {
			"failed": true,
		},
	}

	if transitions, exists := problemTransitions[strings.ToLower(old.Status)]; exists {
		return transitions[strings.ToLower(new.Status)]
	}

	// Also check if stuck in pending for too long
	if strings.Contains(strings.ToLower(new.Status), "pending") {
		if new.Info != nil && time.Since(new.Info.LastDeployed) > c.config.StuckReleaseTimeout {
			return true
		}
	}

	return false
}

// isHelmRelatedEvent checks if a K8s event is related to Helm operations
func (c *Collector) isHelmRelatedEvent(event *v1.Event) bool {
	// Check annotations
	if event.Annotations != nil {
		if _, exists := event.Annotations["meta.helm.sh/release-name"]; exists {
			return true
		}
	}

	// Check labels
	if event.Labels != nil {
		if _, exists := event.Labels["app.kubernetes.io/managed-by"]; exists {
			return event.Labels["app.kubernetes.io/managed-by"] == "Helm"
		}
	}

	// Check if it's a Job/Pod that looks like a Helm hook
	if event.InvolvedObject.Kind == "Job" || event.InvolvedObject.Kind == "Pod" {
		name := event.InvolvedObject.Name
		// Helm hooks often have patterns like "release-name-hook-*"
		if strings.Contains(name, "-hook-") ||
			strings.Contains(name, "-pre-") ||
			strings.Contains(name, "-post-") {
			return true
		}
	}

	return false
}

// isHelmHookPod checks if a pod is a Helm hook
func (c *Collector) isHelmHookPod(pod *v1.Pod) bool {
	// Check annotations
	if pod.Annotations != nil {
		if _, exists := pod.Annotations["helm.sh/hook"]; exists {
			return true
		}
	}

	// Check labels
	if pod.Labels != nil {
		if pod.Labels["app.kubernetes.io/managed-by"] == "Helm" {
			return true
		}
	}

	// Check name patterns
	return strings.Contains(pod.Name, "-hook-") ||
		strings.Contains(pod.Name, "-pre-") ||
		strings.Contains(pod.Name, "-post-")
}

// isHelmHookJob checks if a job is a Helm hook
func (c *Collector) isHelmHookJob(job *batchv1.Job) bool {
	// Check annotations
	if job.Annotations != nil {
		if _, exists := job.Annotations["helm.sh/hook"]; exists {
			return true
		}
	}

	// Check labels
	if job.Labels != nil {
		if job.Labels["app.kubernetes.io/managed-by"] == "Helm" {
			return true
		}
	}

	return false
}

// convertPodStatus converts K8s pod to our PodStatus type
func (c *Collector) convertPodStatus(pod *v1.Pod) PodStatus {
	status := PodStatus{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
		Reason:    pod.Status.Reason,
		Message:   pod.Status.Message,
		CreatedAt: pod.CreationTimestamp.Time,
	}

	// Convert container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		containerStatus := ContainerStatus{
			Name:         cs.Name,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}

		// Get current state
		if cs.State.Waiting != nil {
			containerStatus.State = "waiting"
			containerStatus.Reason = cs.State.Waiting.Reason
			containerStatus.Message = cs.State.Waiting.Message
		} else if cs.State.Running != nil {
			containerStatus.State = "running"
		} else if cs.State.Terminated != nil {
			containerStatus.State = "terminated"
			containerStatus.Reason = cs.State.Terminated.Reason
			containerStatus.Message = cs.State.Terminated.Message
			containerStatus.ExitCode = cs.State.Terminated.ExitCode
		}

		status.ContainerStatuses = append(status.ContainerStatuses, containerStatus)
	}

	return status
}

// convertJobStatus converts K8s job to our JobStatus type
func (c *Collector) convertJobStatus(job *batchv1.Job) JobStatus {
	status := JobStatus{
		Name:      job.Name,
		Namespace: job.Namespace,
		CreatedAt: job.CreationTimestamp.Time,
		Failed:    job.Status.Failed > 0,
		Succeeded: job.Status.Succeeded,
	}

	if job.Status.CompletionTime != nil {
		status.CompletedAt = job.Status.CompletionTime.Time
	}

	if job.Spec.BackoffLimit != nil {
		status.BackoffLimit = *job.Spec.BackoffLimit
	}

	if job.Spec.Completions != nil {
		status.Completions = *job.Spec.Completions
	}

	return status
}

// isPodFailed checks if a pod has failed
func (c *Collector) isPodFailed(pod PodStatus) bool {
	if pod.Phase == "Failed" {
		return true
	}

	// Check container statuses
	for _, cs := range pod.ContainerStatuses {
		if cs.State == "waiting" &&
			(cs.Reason == "ImagePullBackOff" ||
				cs.Reason == "ErrImagePull" ||
				cs.Reason == "CrashLoopBackOff") {
			return true
		}
		if cs.State == "terminated" && cs.ExitCode != 0 {
			return true
		}
	}

	return false
}

// Correlation triggers

func (c *Collector) correlateReleaseFailure(release *HelmRelease) {
	// Build correlation context
	ctx := &CorrelationContext{
		Release: release,
		TimeWindow: TimeWindow{
			Start: time.Now().Add(-c.config.CorrelationWindow),
			End:   time.Now(),
		},
	}

	// Look for matching operations
	c.operations.Range(func(key, value interface{}) bool {
		op := value.(*HelmOperation)
		if op.ReleaseName == release.Name && op.Namespace == release.Namespace {
			ctx.Operation = op
			return false // Found it, stop iterating
		}
		return true
	})

	// Run correlation
	rootCause := c.correlator.Correlate(ctx)
	if rootCause != nil {
		c.emitFailureEvent(rootCause)
	}
}

func (c *Collector) correlateReleaseTransition(old, new *HelmRelease) {
	// Similar to correlateReleaseFailure but with transition context
	startTime := time.Now().Add(-c.config.CorrelationWindow)
	if old.Info != nil && !old.Info.LastDeployed.IsZero() {
		startTime = old.Info.LastDeployed
	}

	ctx := &CorrelationContext{
		Release:         new,
		PreviousRelease: old,
		TimeWindow: TimeWindow{
			Start: startTime,
			End:   time.Now(),
		},
	}

	rootCause := c.correlator.Correlate(ctx)
	if rootCause != nil {
		c.emitFailureEvent(rootCause)
	}
}

func (c *Collector) correlateJobFailure(job JobStatus) {
	// Quick correlation for job failures
	// This is called immediately when a job fails
	// Full correlation happens in the correlation engine
	c.logger.Debug("Correlating job failure",
		zap.String("job", job.Name),
		zap.String("namespace", job.Namespace),
	)
}
