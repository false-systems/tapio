package helmcorrelator

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HelmCorrelationEngine correlates Helm failures across eBPF and K8s data
type HelmCorrelationEngine struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Event storage for correlation
	events      []K8sEvent
	podFailures []PodStatus
	jobFailures []JobStatus

	// Failure patterns
	patterns []FailurePattern

	// Configuration
	maxEvents      int
	correlationAge time.Duration
}

// NewHelmCorrelationEngine creates a new correlation engine
func NewHelmCorrelationEngine(logger *zap.Logger) *HelmCorrelationEngine {
	ce := &HelmCorrelationEngine{
		logger:         logger,
		events:         make([]K8sEvent, 0, 1000),
		podFailures:    make([]PodStatus, 0, 100),
		jobFailures:    make([]JobStatus, 0, 100),
		maxEvents:      1000,
		correlationAge: 10 * time.Minute,
	}

	// Register all failure patterns
	ce.registerPatterns()

	return ce
}

// registerPatterns registers all known Helm failure patterns
func (ce *HelmCorrelationEngine) registerPatterns() {
	ce.patterns = []FailurePattern{
		{
			Name:        "Hook Failed - Image Pull",
			Description: "Pre/post hook failed due to image pull issues",
			Detector:    ce.detectHookImagePullFailure,
		},
		{
			Name:        "Hook Failed - Job Timeout",
			Description: "Hook job exceeded timeout",
			Detector:    ce.detectHookTimeout,
		},
		{
			Name:        "Stuck Release - Previous Operation Failed",
			Description: "Release stuck in pending state from previous failure",
			Detector:    ce.detectStuckRelease,
		},
		{
			Name:        "Template Rendering Error",
			Description: "Helm template rendering failed",
			Detector:    ce.detectTemplateError,
		},
		{
			Name:        "Partial Deployment",
			Description: "Some resources deployed, others failed",
			Detector:    ce.detectPartialDeployment,
		},
		{
			Name:        "Resource Conflict",
			Description: "Resources already exist or immutable field change",
			Detector:    ce.detectResourceConflict,
		},
		{
			Name:        "Timeout Waiting for Resources",
			Description: "Helm timed out waiting for resources to be ready",
			Detector:    ce.detectWaitTimeout,
		},
		{
			Name:        "CRD Version Mismatch",
			Description: "CRD version incompatible with chart",
			Detector:    ce.detectCRDMismatch,
		},
	}
}

// AddEvent adds a K8s event for correlation
func (ce *HelmCorrelationEngine) AddEvent(event K8sEvent) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	ce.events = append(ce.events, event)

	// Trim old events
	if len(ce.events) > ce.maxEvents {
		ce.events = ce.events[len(ce.events)-ce.maxEvents:]
	}
}

// AddPodFailure adds a pod failure for correlation
func (ce *HelmCorrelationEngine) AddPodFailure(pod PodStatus) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	ce.podFailures = append(ce.podFailures, pod)

	// Keep only recent failures
	if len(ce.podFailures) > 100 {
		ce.podFailures = ce.podFailures[len(ce.podFailures)-100:]
	}
}

// AddJobFailure adds a job failure for correlation
func (ce *HelmCorrelationEngine) AddJobFailure(job JobStatus) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	ce.jobFailures = append(ce.jobFailures, job)

	if len(ce.jobFailures) > 100 {
		ce.jobFailures = ce.jobFailures[len(ce.jobFailures)-100:]
	}
}

// Correlate performs correlation on the given context
func (ce *HelmCorrelationEngine) Correlate(ctx *CorrelationContext) *RootCause {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	// Fill context with relevant events from our storage
	ce.fillContext(ctx)

	// Try each pattern detector
	var bestMatch *RootCause
	var bestConfidence float32

	for _, pattern := range ce.patterns {
		if rootCause := pattern.Detector(ctx.Operation, ctx); rootCause != nil {
			ce.logger.Debug("Pattern matched",
				zap.String("pattern", pattern.Name),
				zap.Float32("confidence", rootCause.Confidence),
			)

			if rootCause.Confidence > bestConfidence {
				bestMatch = rootCause
				bestConfidence = rootCause.Confidence
			}
		}
	}

	if bestMatch != nil {
		// Enhance with additional context
		ce.enhanceRootCause(bestMatch, ctx)
		return bestMatch
	}

	// No specific pattern matched, return generic failure
	return ce.genericFailure(ctx)
}

// fillContext fills the correlation context with relevant events
func (ce *HelmCorrelationEngine) fillContext(ctx *CorrelationContext) {
	// Get events within time window
	for _, event := range ce.events {
		if event.Timestamp.After(ctx.TimeWindow.Start) &&
			event.Timestamp.Before(ctx.TimeWindow.End) {
			ctx.K8sEvents = append(ctx.K8sEvents, event)
		}
	}

	// Get pod failures
	for _, pod := range ce.podFailures {
		if pod.CreatedAt.After(ctx.TimeWindow.Start) &&
			pod.CreatedAt.Before(ctx.TimeWindow.End) {
			// Check if pod is related to this release
			if ce.isPodRelated(pod, ctx) {
				ctx.Pods = append(ctx.Pods, pod)
			}
		}
	}

	// Get job failures
	for _, job := range ce.jobFailures {
		if job.CreatedAt.After(ctx.TimeWindow.Start) &&
			job.CreatedAt.Before(ctx.TimeWindow.End) {
			// Check if job is related to this release
			if ce.isJobRelated(job, ctx) {
				ctx.Jobs = append(ctx.Jobs, job)
			}
		}
	}
}

// Pattern Detectors

func (ce *HelmCorrelationEngine) detectHookImagePullFailure(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	if op == nil {
		return nil
	}

	// Look for hook job with image pull failures
	for _, job := range ctx.Jobs {
		// Check if this is a hook job
		if !strings.Contains(job.Name, "-hook-") &&
			!strings.Contains(job.Name, "-pre-") &&
			!strings.Contains(job.Name, "-post-") {
			continue
		}

		// Look for related pod with image pull issues
		for _, pod := range ctx.Pods {
			if !strings.HasPrefix(pod.Name, job.Name) {
				continue
			}

			for _, cs := range pod.ContainerStatuses {
				if cs.Reason == "ImagePullBackOff" || cs.Reason == "ErrImagePull" {
					return &RootCause{
						OperationID: op.ID,
						ReleaseName: op.ReleaseName,
						Namespace:   op.Namespace,
						Pattern:     "Hook Failed - Image Pull",
						Confidence:  0.95,
						Operation:   op.Action,
						Status:      ctx.Release.Status,

						Summary: fmt.Sprintf("Pre-upgrade hook failed: Cannot pull image for job '%s'", job.Name),
						Details: fmt.Sprintf("Container '%s' in pod '%s' failed to pull image. Error: %s",
							cs.Name, pod.Name, cs.Message),

						Evidence: []string{
							fmt.Sprintf("Hook job '%s' created at %s", job.Name, job.CreatedAt.Format(time.RFC3339)),
							fmt.Sprintf("Pod '%s' in %s state", pod.Name, cs.Reason),
							fmt.Sprintf("Image pull error: %s", cs.Message),
							fmt.Sprintf("Container has restarted %d times", cs.RestartCount),
						},

						Impact:     fmt.Sprintf("Helm %s blocked - hook job cannot start", op.Action),
						Resolution: ce.getImagePullResolution(cs),

						HookFailure: &HookFailureDetails{
							HookName: job.Name,
							JobName:  job.Name,
							PodName:  pod.Name,
							Phase:    ce.extractHookPhase(job.Name),
							Error:    cs.Message,
						},

						PodFailure: &PodFailureDetails{
							PodName:       pod.Name,
							Phase:         pod.Phase,
							Reason:        cs.Reason,
							Message:       cs.Message,
							ContainerName: cs.Name,
						},

						FailureTime: job.CreatedAt,
						Duration:    time.Since(job.CreatedAt),
					}
				}
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectHookTimeout(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	// Check if operation exists and timed out
	if op == nil {
		return nil
	}

	if op.ExitCode != 0 && op.Signal == 15 { // SIGTERM usually means timeout
		// Look for incomplete hook jobs
		for _, job := range ctx.Jobs {
			if job.Succeeded == 0 && !job.Failed {
				duration := time.Since(job.CreatedAt)

				return &RootCause{
					OperationID: op.ID,
					ReleaseName: op.ReleaseName,
					Namespace:   op.Namespace,
					Pattern:     "Hook Timeout",
					Confidence:  0.85,
					Operation:   op.Action,

					Summary: fmt.Sprintf("Helm timeout: Hook job '%s' did not complete within %s",
						job.Name, op.Arguments["timeout"]),

					Evidence: []string{
						fmt.Sprintf("Helm process killed with SIGTERM after %s", op.Duration),
						fmt.Sprintf("Hook job '%s' running for %s", job.Name, duration),
						fmt.Sprintf("Job has %d/%d completions", job.Succeeded, job.Completions),
					},

					Impact: "Release stuck in pending state",
					Resolution: fmt.Sprintf("1. Check job logs: kubectl logs -n %s job/%s\n"+
						"2. If safe, delete job: kubectl delete job %s -n %s\n"+
						"3. Retry with longer timeout: helm upgrade --timeout 20m",
						op.Namespace, job.Name, job.Name, op.Namespace),

					FailureTime: op.EndTime,
					Duration:    op.Duration,
				}
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectStuckRelease(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	if ctx.Release == nil {
		return nil
	}

	// Check if release is stuck in pending state
	if strings.Contains(strings.ToLower(ctx.Release.Status), "pending") {
		timeSinceDeploy := time.Since(ctx.Release.Info.LastDeployed)

		if timeSinceDeploy > 10*time.Minute {
			// Look for previous failed operation
			previousError := ""
			if ctx.Release.Info != nil {
				previousError = ctx.Release.Info.Description
			}

			return &RootCause{
				OperationID: op.ID,
				ReleaseName: ctx.Release.Name,
				Namespace:   ctx.Release.Namespace,
				Pattern:     "Stuck Release",
				Confidence:  0.8,
				Status:      ctx.Release.Status,

				Summary: fmt.Sprintf("Release stuck in %s for %s",
					ctx.Release.Status, timeSinceDeploy.Round(time.Minute)),

				Details: fmt.Sprintf("Previous operation failed and left release in pending state. %s",
					previousError),

				Evidence: []string{
					fmt.Sprintf("Release status: %s", ctx.Release.Status),
					fmt.Sprintf("Last deployed: %s", ctx.Release.Info.LastDeployed),
					fmt.Sprintf("Stuck for: %s", timeSinceDeploy.Round(time.Minute)),
				},

				Impact: "All Helm operations blocked for this release",

				Resolution: fmt.Sprintf(
					"Option 1: Force upgrade with --force flag\n"+
						"  helm upgrade %s ./chart --force\n\n"+
						"Option 2: Delete and reinstall\n"+
						"  helm delete %s\n"+
						"  helm install %s ./chart\n\n"+
						"Option 3: Manually fix secret\n"+
						"  kubectl delete secret sh.helm.release.v1.%s.v%d",
					ctx.Release.Name, ctx.Release.Name, ctx.Release.Name,
					ctx.Release.Name, ctx.Release.Version),

				FailureTime: ctx.Release.Info.LastDeployed,
				Duration:    timeSinceDeploy,
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectTemplateError(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	if op == nil {
		return nil
	}

	// Check if operation failed very quickly (template errors fail fast)
	if op.Duration < 5*time.Second && op.ExitCode != 0 {
		// Look for template-related file access
		var templateFiles []string
		for _, file := range op.FilesRead {
			if file.FileType == "template" || strings.Contains(file.Path, "template") {
				templateFiles = append(templateFiles, file.Path)
			}
		}

		if len(templateFiles) > 0 {
			return &RootCause{
				OperationID: op.ID,
				ReleaseName: op.ReleaseName,
				Namespace:   op.Namespace,
				Pattern:     "Template Error",
				Confidence:  0.7,

				Summary: "Helm template rendering failed",
				Details: "Template syntax error or missing values prevented rendering",

				Evidence: []string{
					fmt.Sprintf("Helm exited with code %d after %s", op.ExitCode, op.Duration),
					fmt.Sprintf("Read %d template files", len(templateFiles)),
					"Failed during template rendering phase",
				},

				Impact: "Deployment cannot proceed",

				Resolution: "1. Debug templates: helm template ./chart --debug\n" +
					"2. Check values: helm get values " + op.ReleaseName + "\n" +
					"3. Validate YAML syntax in templates",

				FailureTime: op.EndTime,
				Duration:    op.Duration,
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectPartialDeployment(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	if op == nil {
		return nil
	}

	// Look for mix of successful and failed resources
	var created, failed int
	var failedResources []string

	for _, event := range ctx.K8sEvents {
		if event.Type == "Normal" && strings.Contains(event.Reason, "Created") {
			created++
		} else if event.Type == "Warning" {
			failed++
			failedResources = append(failedResources, event.Object)
		}
	}

	if created > 0 && failed > 0 {
		return &RootCause{
			OperationID: op.ID,
			ReleaseName: op.ReleaseName,
			Namespace:   op.Namespace,
			Pattern:     "Partial Deployment",
			Confidence:  0.75,

			Summary: fmt.Sprintf("Partial deployment: %d resources created, %d failed",
				created, failed),

			Evidence: append([]string{
				fmt.Sprintf("%d resources successfully created", created),
				fmt.Sprintf("%d resources failed to create", failed),
			}, failedResources...),

			Impact: "Application may be in inconsistent state",

			Resolution: "1. Check failed resources: kubectl get all -n " + op.Namespace + "\n" +
				"2. Rollback if needed: helm rollback " + op.ReleaseName + "\n" +
				"3. Fix issues and retry upgrade",

			FailureTime: op.EndTime,
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectResourceConflict(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	// Look for conflict events
	for _, event := range ctx.K8sEvents {
		if strings.Contains(event.Message, "already exists") ||
			strings.Contains(event.Message, "immutable") ||
			strings.Contains(event.Message, "forbidden") {
			return &RootCause{
				OperationID: op.ID,
				ReleaseName: op.ReleaseName,
				Namespace:   op.Namespace,
				Pattern:     "Resource Conflict",
				Confidence:  0.85,

				Summary: "Resource conflict: " + event.Message,

				Evidence: []string{
					fmt.Sprintf("Conflict on %s", event.Object),
					event.Message,
					fmt.Sprintf("Event at %s", event.Timestamp),
				},

				Impact: "Resource cannot be updated",

				Resolution: ce.getConflictResolution(event),

				FailureTime: event.Timestamp,
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectWaitTimeout(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	if op == nil {
		return nil
	}

	// Check if Helm was waiting for resources
	if op.Duration > 1*time.Minute && op.ExitCode != 0 {
		// Look for pods not becoming ready
		var notReadyPods []string
		for _, pod := range ctx.Pods {
			if pod.Phase != "Running" {
				notReadyPods = append(notReadyPods, fmt.Sprintf("%s (%s)", pod.Name, pod.Phase))
			}
		}

		if len(notReadyPods) > 0 {
			return &RootCause{
				OperationID: op.ID,
				ReleaseName: op.ReleaseName,
				Namespace:   op.Namespace,
				Pattern:     "Wait Timeout",
				Confidence:  0.7,

				Summary: fmt.Sprintf("Helm timed out waiting for %d pods to be ready",
					len(notReadyPods)),

				Evidence: append([]string{
					fmt.Sprintf("Waited %s before timeout", op.Duration),
				}, notReadyPods...),

				Impact: "Deployment incomplete",

				Resolution: "1. Check pod status: kubectl get pods -n " + op.Namespace + "\n" +
					"2. Check pod logs for errors\n" +
					"3. Increase timeout: --timeout 15m",

				FailureTime: op.EndTime,
				Duration:    op.Duration,
			}
		}
	}

	return nil
}

func (ce *HelmCorrelationEngine) detectCRDMismatch(op *HelmOperation, ctx *CorrelationContext) *RootCause {
	// Look for CRD-related errors
	for _, event := range ctx.K8sEvents {
		if strings.Contains(event.Message, "no matches for kind") ||
			strings.Contains(event.Message, "apiVersion") {
			return &RootCause{
				OperationID: op.ID,
				ReleaseName: op.ReleaseName,
				Namespace:   op.Namespace,
				Pattern:     "CRD Mismatch",
				Confidence:  0.8,

				Summary: "CRD version mismatch: " + event.Message,

				Evidence: []string{
					event.Message,
					"Chart requires CRD that doesn't exist or wrong version",
				},

				Impact: "Resources depending on CRD cannot be created",

				Resolution: "1. Check installed CRDs: kubectl get crd\n" +
					"2. Install/update required CRDs\n" +
					"3. Check chart requirements for CRD versions",

				FailureTime: event.Timestamp,
			}
		}
	}

	return nil
}

// Helper methods

func (ce *HelmCorrelationEngine) isPodRelated(pod PodStatus, ctx *CorrelationContext) bool {
	if ctx.Release == nil {
		return false
	}

	// Check if pod name contains release name
	return strings.Contains(pod.Name, ctx.Release.Name)
}

func (ce *HelmCorrelationEngine) isJobRelated(job JobStatus, ctx *CorrelationContext) bool {
	if ctx.Release == nil {
		return false
	}

	// Check if job name contains release name
	return strings.Contains(job.Name, ctx.Release.Name)
}

func (ce *HelmCorrelationEngine) extractHookPhase(jobName string) string {
	if strings.Contains(jobName, "-pre-install") {
		return "pre-install"
	}
	if strings.Contains(jobName, "-post-install") {
		return "post-install"
	}
	if strings.Contains(jobName, "-pre-upgrade") {
		return "pre-upgrade"
	}
	if strings.Contains(jobName, "-post-upgrade") {
		return "post-upgrade"
	}
	if strings.Contains(jobName, "-pre-rollback") {
		return "pre-rollback"
	}
	if strings.Contains(jobName, "-post-rollback") {
		return "post-rollback"
	}
	if strings.Contains(jobName, "-pre-delete") {
		return "pre-delete"
	}
	if strings.Contains(jobName, "-post-delete") {
		return "post-delete"
	}
	return "unknown"
}

func (ce *HelmCorrelationEngine) getImagePullResolution(cs ContainerStatus) string {
	resolution := "Image pull failed. Possible fixes:\n"

	if strings.Contains(cs.Message, "not found") {
		resolution += "1. Image doesn't exist. Check image name and tag\n"
	} else if strings.Contains(cs.Message, "unauthorized") {
		resolution += "1. Authentication required. Create/update image pull secret\n"
		resolution += "   kubectl create secret docker-registry ...\n"
	} else if strings.Contains(cs.Message, "rate limit") ||
		strings.Contains(cs.Message, "429") {
		resolution += "1. Registry rate limit hit. Wait and retry\n"
		resolution += "2. Use authenticated pulls for higher limits\n"
		resolution += "3. Use a different registry mirror\n"
	}

	resolution += "\nGeneral fixes:\n"
	resolution += "- Verify image exists: docker pull <image>\n"
	resolution += "- Check image pull secrets are configured\n"
	resolution += "- Verify registry is accessible from cluster"

	return resolution
}

func (ce *HelmCorrelationEngine) getConflictResolution(event K8sEvent) string {
	if strings.Contains(event.Message, "immutable") {
		return "Immutable field cannot be changed. Options:\n" +
			"1. Delete the resource and let Helm recreate it\n" +
			"2. Use --force flag to replace resources\n" +
			"3. Change field back to original value"
	}

	if strings.Contains(event.Message, "already exists") {
		return "Resource already exists. Options:\n" +
			"1. Use 'helm upgrade' instead of 'helm install'\n" +
			"2. Delete existing resource first\n" +
			"3. Use --force flag to overwrite"
	}

	return "Resource conflict detected. Check resource specifications."
}

func (ce *HelmCorrelationEngine) enhanceRootCause(rc *RootCause, ctx *CorrelationContext) {
	// Add operation details if available
	if ctx.Operation != nil {
		rc.OperationID = ctx.Operation.ID
		if rc.ReleaseName == "" {
			rc.ReleaseName = ctx.Operation.ReleaseName
		}
		if rc.Namespace == "" {
			rc.Namespace = ctx.Operation.Namespace
		}
	}

	// Add release details
	if ctx.Release != nil {
		rc.FromVersion = ctx.Release.Version
		if ctx.PreviousRelease != nil {
			rc.FromVersion = ctx.PreviousRelease.Version
			rc.ToVersion = ctx.Release.Version
		}
	}

	// Build event chain
	rc.EventChain = ce.buildEventChain(ctx)
}

func (ce *HelmCorrelationEngine) buildEventChain(ctx *CorrelationContext) []string {
	chain := []string{}

	if ctx.Operation != nil {
		chain = append(chain, fmt.Sprintf("Helm %s started", ctx.Operation.Action))
	}

	if ctx.Release != nil {
		chain = append(chain, fmt.Sprintf("Release status: %s", ctx.Release.Status))
	}

	for _, job := range ctx.Jobs {
		if job.Failed {
			chain = append(chain, fmt.Sprintf("Job %s failed", job.Name))
		}
	}

	for _, pod := range ctx.Pods {
		if pod.Phase == "Failed" {
			chain = append(chain, fmt.Sprintf("Pod %s failed", pod.Name))
		}
	}

	return chain
}

func (ce *HelmCorrelationEngine) genericFailure(ctx *CorrelationContext) *RootCause {
	summary := "Helm operation failed"
	if ctx.Operation != nil {
		summary = fmt.Sprintf("Helm %s failed", ctx.Operation.Action)
	}

	return &RootCause{
		Pattern:     "Unknown",
		Confidence:  0.3,
		Summary:     summary,
		Details:     "Unable to determine specific root cause",
		Evidence:    []string{"Pattern matching did not identify specific cause"},
		Impact:      "Operation did not complete successfully",
		Resolution:  "Check helm status and logs for more information",
		FailureTime: time.Now(),
	}
}
