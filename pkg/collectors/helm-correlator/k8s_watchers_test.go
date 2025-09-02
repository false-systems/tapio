package helmcorrelator

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testReleaseSimple is a simple release structure for testing
type testReleaseSimple struct {
	Name      string               `json:"name"`
	Namespace string               `json:"namespace"`
	Version   int                  `json:"version"`
	Status    string               `json:"status"`
	Info      testReleaseInfoBasic `json:"info"`
	Manifest  string               `json:"manifest"`
}

type testReleaseInfoBasic struct {
	Status      string `json:"status"`
	Description string `json:"description"`
}

// Helper function to create valid Helm release data for testing
func createTestReleaseData(t *testing.T, name, status string) []byte {
	release := testReleaseSimple{
		Name:      name,
		Namespace: "default",
		Version:   1,
		Status:    status,
		Info: testReleaseInfoBasic{
			Status:      status,
			Description: "Test release",
		},
		Manifest: "apiVersion: v1\nkind: Service\n",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(release)
	require.NoError(t, err)

	// Gzip compress
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err = gz.Write(jsonData)
	require.NoError(t, err)
	err = gz.Close()
	require.NoError(t, err)

	// Base64 encode
	return []byte(base64.StdEncoding.EncodeToString(buf.Bytes()))
}

func TestOnSecretAdd(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name         string
		obj          interface{}
		expectCached bool
	}{
		{
			name: "helm release secret",
			obj: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sh.helm.release.v1.myapp.v1",
					Namespace: "default",
				},
				Type: "helm.sh/release.v1",
				Data: map[string][]byte{
					// This creates a valid gzipped JSON structure
					"release": createTestReleaseData(t, "myapp", "deployed"),
				},
			},
			expectCached: true,
		},
		{
			name: "non-helm secret",
			obj: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-secret",
					Namespace: "default",
				},
				Type: "Opaque",
			},
			expectCached: false,
		},
		{
			name:         "non-secret object",
			obj:          &v1.Pod{},
			expectCached: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.onSecretAdd(tt.obj)

			// Check if release was cached
			if tt.expectCached {
				// Allow some time for processing
				time.Sleep(10 * time.Millisecond)

				cached := false
				collector.releaseCache.Range(func(key, value interface{}) bool {
					cached = true
					return false
				})
				assert.True(t, cached, "Expected release to be cached")
			}
		})
	}
}

func TestOnSecretUpdate(t *testing.T) {
	config := DefaultConfig()
	config.StuckReleaseTimeout = 1 * time.Second // Short timeout for testing
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	oldSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1.myapp.v1",
			Namespace: "default",
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": createTestReleaseData(t, "myapp", "deployed"),
		},
	}

	newSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1.myapp.v2",
			Namespace: "default",
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": createTestReleaseData(t, "myapp", "failed"),
		},
	}

	// Test update
	collector.onSecretUpdate(oldSecret, newSecret)

	// Check if new release was cached
	time.Sleep(10 * time.Millisecond)
	cached := false
	collector.releaseCache.Range(func(key, value interface{}) bool {
		cached = true
		return false
	})
	assert.True(t, cached, "Expected updated release to be cached")
}

func TestOnSecretDelete(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	// Add a release to cache first
	release := &HelmRelease{
		Name:      "myapp",
		Namespace: "default",
		Version:   1,
	}
	collector.releaseCache.Store("default/myapp", release)

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1.myapp.v1",
			Namespace: "default",
		},
		Type: "helm.sh/release.v1",
	}

	// Delete the secret
	collector.onSecretDelete(secret)

	// Check if release was removed from cache
	_, exists := collector.releaseCache.Load("default/myapp")
	assert.False(t, exists, "Expected release to be removed from cache")
}

func TestOnEventAdd(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	now := metav1.Now()

	tests := []struct {
		name        string
		event       *v1.Event
		shouldStore bool
	}{
		{
			name: "warning event",
			event: &v1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-event",
				},
				Type:   "Warning",
				Reason: "FailedPullImage",
				InvolvedObject: v1.ObjectReference{
					Kind: "Pod",
					Name: "myapp-hook-pre-upgrade",
				},
				Message:        "Failed to pull image",
				FirstTimestamp: now,
				LastTimestamp:  now,
				Count:          1,
			},
			shouldStore: true,
		},
		{
			name: "normal event",
			event: &v1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-event",
				},
				Type:   "Normal",
				Reason: "Started",
				InvolvedObject: v1.ObjectReference{
					Kind: "Pod",
					Name: "myapp",
				},
				Message: "Started container",
			},
			shouldStore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.onEventAdd(tt.event)
			// We can't easily verify event storage without exposing internal state
			// This test mainly ensures no panic
		})
	}
}

func TestOnPodUpdate(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	oldPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-pre-upgrade-hook",
			Namespace: "default",
			Annotations: map[string]string{
				"helm.sh/hook": "pre-upgrade",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}

	newPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-pre-upgrade-hook",
			Namespace: "default",
			Annotations: map[string]string{
				"helm.sh/hook": "pre-upgrade",
			},
		},
		Status: v1.PodStatus{
			Phase:  v1.PodFailed,
			Reason: "Error",
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name: "hook",
					State: v1.ContainerState{
						Terminated: &v1.ContainerStateTerminated{
							ExitCode: 1,
							Reason:   "Error",
						},
					},
				},
			},
		},
	}

	// Test pod update
	collector.onPodUpdate(oldPod, newPod)
	// This test ensures no panic and proper handling
}

func TestOnJobUpdate(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name string
		job  *batchv1.Job
	}{
		{
			name: "successful helm hook job",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myapp-pre-upgrade",
					Namespace: "default",
					Annotations: map[string]string{
						"helm.sh/hook": "pre-upgrade",
					},
				},
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			},
		},
		{
			name: "failed helm hook job",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myapp-post-install",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "Helm",
					},
				},
				Status: batchv1.JobStatus{
					Failed: 1,
				},
			},
		},
		{
			name: "non-helm job",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "regular-job",
					Namespace: "default",
				},
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.onJobUpdate(tt.job, tt.job)
			// Ensures no panic and proper handling
		})
	}
}

func TestIsHelmRelatedEvent(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name     string
		event    *v1.Event
		expected bool
	}{
		{
			name: "event with helm annotation",
			event: &v1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"meta.helm.sh/release-name": "myapp",
					},
				},
			},
			expected: true,
		},
		{
			name: "event with helm label",
			event: &v1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "Helm",
					},
				},
			},
			expected: true,
		},
		{
			name: "hook job event",
			event: &v1.Event{
				InvolvedObject: v1.ObjectReference{
					Kind: "Job",
					Name: "myapp-pre-upgrade-hook",
				},
			},
			expected: true,
		},
		{
			name: "regular event",
			event: &v1.Event{
				InvolvedObject: v1.ObjectReference{
					Kind: "Pod",
					Name: "regular-pod",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isHelmRelatedEvent(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHelmHookPod(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "pod with helm hook annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"helm.sh/hook": "pre-upgrade",
					},
				},
			},
			expected: true,
		},
		{
			name: "pod managed by helm",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "Helm",
					},
				},
			},
			expected: true,
		},
		{
			name: "pod with hook in name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "myapp-pre-install-hook",
				},
			},
			expected: true,
		},
		{
			name: "regular pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "regular-pod",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isHelmHookPod(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHelmHookJob(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name     string
		job      *batchv1.Job
		expected bool
	}{
		{
			name: "job with helm hook annotation",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"helm.sh/hook": "post-install",
					},
				},
			},
			expected: true,
		},
		{
			name: "job managed by helm",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "Helm",
					},
				},
			},
			expected: true,
		},
		{
			name: "regular job",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "regular-job",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isHelmHookJob(tt.job)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertPodStatus(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pod",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.PodStatus{
			Phase:   v1.PodRunning,
			Reason:  "Running",
			Message: "Pod is running",
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:         "container1",
					Ready:        true,
					RestartCount: 2,
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{},
					},
				},
				{
					Name:         "container2",
					Ready:        false,
					RestartCount: 0,
					State: v1.ContainerState{
						Waiting: &v1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "Back-off pulling image",
						},
					},
				},
			},
		},
	}

	status := collector.convertPodStatus(pod)

	assert.Equal(t, "test-pod", status.Name)
	assert.Equal(t, "default", status.Namespace)
	assert.Equal(t, "Running", status.Phase)
	assert.Len(t, status.ContainerStatuses, 2)

	assert.Equal(t, "container1", status.ContainerStatuses[0].Name)
	assert.True(t, status.ContainerStatuses[0].Ready)
	assert.Equal(t, int32(2), status.ContainerStatuses[0].RestartCount)
	assert.Equal(t, "running", status.ContainerStatuses[0].State)

	assert.Equal(t, "container2", status.ContainerStatuses[1].Name)
	assert.False(t, status.ContainerStatuses[1].Ready)
	assert.Equal(t, "waiting", status.ContainerStatuses[1].State)
	assert.Equal(t, "ImagePullBackOff", status.ContainerStatuses[1].Reason)
}

func TestConvertJobStatus(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	backoffLimit := int32(3)
	completions := int32(1)
	completionTime := metav1.Now()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-job",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Completions:  &completions,
		},
		Status: batchv1.JobStatus{
			Succeeded:      1,
			Failed:         0,
			CompletionTime: &completionTime,
		},
	}

	status := collector.convertJobStatus(job)

	assert.Equal(t, "test-job", status.Name)
	assert.Equal(t, "default", status.Namespace)
	assert.False(t, status.Failed)
	assert.Equal(t, int32(1), status.Succeeded)
	assert.Equal(t, backoffLimit, status.BackoffLimit)
	assert.Equal(t, completions, status.Completions)
	assert.Equal(t, completionTime.Time, status.CompletedAt)
}

func TestIsPodFailed(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name     string
		pod      PodStatus
		expected bool
	}{
		{
			name: "failed pod",
			pod: PodStatus{
				Phase: "Failed",
			},
			expected: true,
		},
		{
			name: "pod with image pull error",
			pod: PodStatus{
				Phase: "Pending",
				ContainerStatuses: []ContainerStatus{
					{
						State:  "waiting",
						Reason: "ImagePullBackOff",
					},
				},
			},
			expected: true,
		},
		{
			name: "pod with crash loop",
			pod: PodStatus{
				Phase: "Running",
				ContainerStatuses: []ContainerStatus{
					{
						State:  "waiting",
						Reason: "CrashLoopBackOff",
					},
				},
			},
			expected: true,
		},
		{
			name: "terminated container with non-zero exit",
			pod: PodStatus{
				Phase: "Running",
				ContainerStatuses: []ContainerStatus{
					{
						State:    "terminated",
						ExitCode: 1,
					},
				},
			},
			expected: true,
		},
		{
			name: "running pod",
			pod: PodStatus{
				Phase: "Running",
				ContainerStatuses: []ContainerStatus{
					{
						State: "running",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isPodFailed(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectProblematicTransition(t *testing.T) {
	config := DefaultConfig()
	config.StuckReleaseTimeout = 1 * time.Hour
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		old      *HelmRelease
		new      *HelmRelease
		expected bool
	}{
		{
			name:     "deployed to failed",
			old:      &HelmRelease{Status: "deployed"},
			new:      &HelmRelease{Status: "failed"},
			expected: true,
		},
		{
			name:     "deployed to pending-upgrade",
			old:      &HelmRelease{Status: "deployed"},
			new:      &HelmRelease{Status: "pending-upgrade"},
			expected: true,
		},
		{
			name:     "pending-install to failed",
			old:      &HelmRelease{Status: "pending-install"},
			new:      &HelmRelease{Status: "failed"},
			expected: true,
		},
		{
			name:     "deployed to deployed",
			old:      &HelmRelease{Status: "deployed"},
			new:      &HelmRelease{Status: "deployed"},
			expected: false,
		},
		{
			name: "stuck in pending",
			old:  &HelmRelease{Status: "deployed"},
			new: &HelmRelease{
				Status: "pending-upgrade",
				Info: &ReleaseInfo{
					LastDeployed: time.Now().Add(-2 * time.Hour),
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.detectProblematicTransition(tt.old, tt.new)
			assert.Equal(t, tt.expected, result)
		})
	}
}
