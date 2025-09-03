package helmcorrelator

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SimplifiedHelmCorrelator focuses on the 80% problems: hooks and stuck releases
type SimplifiedHelmCorrelator struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Only track what matters for hooks
	recentJobs      map[string]*JobStatus  // release -> job
	recentPods      map[string]*PodStatus  // job -> pod
	releaseStates   map[string]*ReleaseState
	
	// Simple configuration
	correlationWindow time.Duration
}

// NewSimplifiedHelmCorrelator creates our MVP correlator
func NewSimplifiedHelmCorrelator(logger *zap.Logger) *SimplifiedHelmCorrelator {
	return &SimplifiedHelmCorrelator{
		logger:            logger,
		recentJobs:        make(map[string]*JobStatus),
		recentPods:        make(map[string]*PodStatus),
		releaseStates:     make(map[string]*ReleaseState),
		correlationWindow: 5 * time.Minute,
	}
}

// CorrelateFailure - The ONLY public method that matters
func (s *SimplifiedHelmCorrelator) CorrelateFailure(helmCmd *HelmOperation) *RootCause {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Priority 1: Hook failures (60% of problems)
	if hookCause := s.checkHookFailure(helmCmd); hookCause != nil {
		return hookCause
	}

	// Priority 2: Stuck releases (30% of problems)
	if stuckCause := s.checkStuckRelease(helmCmd); stuckCause != nil {
		return stuckCause
	}

	// Priority 3: Fast template failures (10% of problems)
	if helmCmd.Duration < 5*time.Second && helmCmd.ExitCode != 0 {
		return &RootCause{
			Pattern:    "Likely Template Error",
			Confidence: 0.6, // Medium confidence
			Summary:    "Helm failed quickly - likely a template or validation error",
			Details:    fmt.Sprintf("Helm exited with code %d in %v", helmCmd.ExitCode, helmCmd.Duration),
			Resolution: "Run 'helm template' to check for template errors, or 'helm upgrade --dry-run' for validation",
		}
	}

	// We don't know - BE HONEST
	return &RootCause{
		Pattern:    "Unknown Failure",
		Confidence: 0.3,
		Summary:    "Helm failed but we couldn't determine the specific cause",
		Details:    fmt.Sprintf("Exit code: %d, Duration: %v", helmCmd.ExitCode, helmCmd.Duration),
		Resolution: "Check 'helm status' and 'kubectl get events' for more details",
	}
}

// checkHookFailure - THE MOST IMPORTANT PATTERN
func (s *SimplifiedHelmCorrelator) checkHookFailure(helmCmd *HelmOperation) *RootCause {
	// Look for hook jobs related to this release
	hookJob, exists := s.recentJobs[helmCmd.ReleaseName]
	if !exists || hookJob == nil {
		return nil
	}

	// Is it a hook job?
	if !strings.Contains(hookJob.Name, "hook") && 
	   !strings.Contains(hookJob.Name, "migrate") &&
	   !strings.Contains(hookJob.Name, "pre-") &&
	   !strings.Contains(hookJob.Name, "post-") {
		return nil
	}

	// Did it fail?
	if hookJob.Status != "Failed" {
		return nil
	}

	// Find the pod for this job
	pod, exists := s.recentPods[hookJob.Name]
	if !exists || pod == nil {
		// Job failed but no pod info - still useful
		return &RootCause{
			Pattern:    "Hook Job Failed",
			Confidence: 0.8,
			Summary:    fmt.Sprintf("Hook job '%s' failed during %s", hookJob.Name, helmCmd.Action),
			Details:    "Hook job failed but pod details unavailable",
			Resolution: fmt.Sprintf("Check job: kubectl describe job %s -n %s", hookJob.Name, helmCmd.Namespace),
			Evidence: []string{
				fmt.Sprintf("Helm %s started at %v", helmCmd.Action, helmCmd.StartTime),
				fmt.Sprintf("Hook job '%s' failed", hookJob.Name),
			},
		}
	}

	// We have pod info - give DETAILED diagnosis
	rootCause := &RootCause{
		Pattern:    "Hook Failed",
		Confidence: 0.95, // HIGH confidence - we have all the data
		Summary:    fmt.Sprintf("%s hook '%s' failed: %s", getHookType(hookJob.Name), hookJob.Name, pod.Reason),
		Details:    pod.Message,
		Evidence: []string{
			fmt.Sprintf("Helm %s started at %v", helmCmd.Action, helmCmd.StartTime),
			fmt.Sprintf("Hook job '%s' created", hookJob.Name),
			fmt.Sprintf("Pod status: %s", pod.Reason),
		},
	}

	// Specific resolution based on pod failure reason
	switch pod.Reason {
	case "ImagePullBackOff", "ErrImagePull":
		rootCause.Resolution = fmt.Sprintf("Cannot pull image '%s'. Check: 1) Image exists, 2) Registry credentials, 3) Rate limits", pod.Image)
		rootCause.Impact = "Deployment blocked until image is accessible"
		
	case "CrashLoopBackOff":
		rootCause.Resolution = fmt.Sprintf("Hook is crashing. Check logs: kubectl logs job/%s -n %s", hookJob.Name, helmCmd.Namespace)
		rootCause.Impact = "Hook script/command is failing"
		
	case "CreateContainerConfigError":
		rootCause.Resolution = "Check ConfigMaps/Secrets referenced by the hook"
		rootCause.Impact = "Hook cannot start due to missing configuration"
		
	case "Pending":
		if strings.Contains(pod.Message, "Insufficient") {
			rootCause.Resolution = "Cluster lacks resources. Scale up nodes or reduce resource requests"
			rootCause.Impact = "Hook cannot be scheduled"
		} else {
			rootCause.Resolution = "Hook pod is pending. Check pod events for details"
		}
		
	default:
		rootCause.Resolution = fmt.Sprintf("Check pod: kubectl describe pod -l job-name=%s -n %s", hookJob.Name, helmCmd.Namespace)
	}

	return rootCause
}

// checkStuckRelease - THE SECOND MOST IMPORTANT PATTERN
func (s *SimplifiedHelmCorrelator) checkStuckRelease(helmCmd *HelmOperation) *RootCause {
	release, exists := s.releaseStates[helmCmd.ReleaseName]
	if !exists || release == nil {
		return nil
	}

	// Check for stuck states
	stuckStates := []string{"pending-install", "pending-upgrade", "pending-rollback"}
	isStuck := false
	for _, state := range stuckStates {
		if strings.ToLower(release.Status) == state {
			isStuck = true
			break
		}
	}

	if !isStuck {
		return nil
	}

	// Calculate how long it's been stuck
	stuckDuration := time.Since(release.LastUpdated)
	
	return &RootCause{
		Pattern:    "Stuck Release",
		Confidence: 0.9,
		Summary:    fmt.Sprintf("Release '%s' stuck in %s state for %v", helmCmd.ReleaseName, release.Status, stuckDuration),
		Details:    fmt.Sprintf("Previous operation was interrupted or failed, leaving release in %s", release.Status),
		Impact:     "No Helm operations can proceed until release is unlocked",
		Resolution: fmt.Sprintf("Option 1: helm rollback %s\nOption 2: helm delete %s --no-hooks\nOption 3: kubectl delete secret -l name=%s", 
			helmCmd.ReleaseName, helmCmd.ReleaseName, helmCmd.ReleaseName),
		Evidence: []string{
			fmt.Sprintf("Release status: %s", release.Status),
			fmt.Sprintf("Stuck since: %v", release.LastUpdated),
			fmt.Sprintf("Current operation: %s failed", helmCmd.Action),
		},
	}
}

// Helper functions
func getHookType(jobName string) string {
	switch {
	case strings.Contains(jobName, "pre-install"):
		return "Pre-install"
	case strings.Contains(jobName, "post-install"):
		return "Post-install"
	case strings.Contains(jobName, "pre-upgrade"):
		return "Pre-upgrade"
	case strings.Contains(jobName, "post-upgrade"):
		return "Post-upgrade"
	case strings.Contains(jobName, "pre-rollback"):
		return "Pre-rollback"
	case strings.Contains(jobName, "post-rollback"):
		return "Post-rollback"
	case strings.Contains(jobName, "pre-delete"):
		return "Pre-delete"
	case strings.Contains(jobName, "post-delete"):
		return "Post-delete"
	default:
		return "Hook"
	}
}

// TrackJob - Called when we see a job related to a helm release
func (s *SimplifiedHelmCorrelator) TrackJob(releaseName string, job *JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.recentJobs[releaseName] = job
	
	// Clean old entries
	s.cleanOldEntries()
}

// TrackPod - Called when we see a pod related to a helm job
func (s *SimplifiedHelmCorrelator) TrackPod(jobName string, pod *PodStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.recentPods[jobName] = pod
}

// TrackRelease - Called when we see a helm release state change
func (s *SimplifiedHelmCorrelator) TrackRelease(release *ReleaseState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.releaseStates[release.Name] = release
}

// cleanOldEntries - Simple cleanup to prevent memory growth
func (s *SimplifiedHelmCorrelator) cleanOldEntries() {
	cutoff := time.Now().Add(-s.correlationWindow)
	
	// Clean old jobs
	for name, job := range s.recentJobs {
		if job.CreatedAt.Before(cutoff) {
			delete(s.recentJobs, name)
		}
	}
	
	// Clean old pods
	for name, pod := range s.recentPods {
		if pod.CreatedAt.Before(cutoff) {
			delete(s.recentPods, name)
		}
	}
}