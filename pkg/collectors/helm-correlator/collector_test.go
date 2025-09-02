package helmcorrelator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name:   "with default config",
			config: nil,
		},
		{
			name: "with custom config",
			config: &Config{
				Name:              "test-helm",
				BufferSize:        500,
				EnableEBPF:        false,
				EnableK8sWatching: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test", tt.config)
			require.NoError(t, err)
			require.NotNil(t, collector)

			assert.NotNil(t, collector.BaseCollector)
			assert.NotNil(t, collector.EventChannelManager)
			assert.NotNil(t, collector.LifecycleManager)
			assert.NotNil(t, collector.logger)
			assert.NotNil(t, collector.correlator)

			if tt.config == nil {
				assert.Equal(t, "helm-correlator", collector.config.Name)
				assert.Equal(t, 1000, collector.config.BufferSize)
			} else {
				assert.Equal(t, tt.config.Name, collector.config.Name)
				assert.Equal(t, tt.config.BufferSize, collector.config.BufferSize)
			}
		})
	}
}

func TestCollector_Name(t *testing.T) {
	config := &Config{Name: "helm-test"}
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	assert.Equal(t, "helm-test", collector.Name())
}

func TestCollector_StartStop(t *testing.T) {
	config := &Config{
		Name:              "test",
		EnableEBPF:        false, // Disable eBPF for testing
		EnableK8sWatching: false, // Disable K8s for testing
	}

	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Check it's healthy
	assert.True(t, collector.BaseCollector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)

	// Check it's not healthy
	assert.False(t, collector.BaseCollector.IsHealthy())
}

func TestCollector_IsHelmSecret(t *testing.T) {
	_, err := NewCollector("test", &Config{
		EnableK8sWatching: false,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		secret   *v1.Secret
		expected bool
	}{
		{
			name: "valid helm secret",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sh.helm.release.v1.myapp.v1",
				},
				Type: "helm.sh/release.v1",
			},
			expected: true,
		},
		{
			name: "non-helm secret",
			secret: &v1.Secret{
				Type: "Opaque",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := NewHelmSecretDecoder(nil)
			result := decoder.isHelmSecret(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollector_IsFailedRelease(t *testing.T) {
	collector, err := NewCollector("test", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		release  *HelmRelease
		expected bool
	}{
		{
			name:     "failed status",
			release:  &HelmRelease{Status: "failed"},
			expected: true,
		},
		{
			name:     "pending status",
			release:  &HelmRelease{Status: "pending-upgrade"},
			expected: true,
		},
		{
			name:     "deployed status",
			release:  &HelmRelease{Status: "deployed"},
			expected: false,
		},
		{
			name:     "superseded status",
			release:  &HelmRelease{Status: "superseded"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isFailedRelease(tt.release)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollector_DetectProblematicTransition(t *testing.T) {
	collector, err := NewCollector("test", &Config{
		StuckReleaseTimeout: 10 * time.Minute,
	})
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
			name:     "deployed to pending",
			old:      &HelmRelease{Status: "deployed"},
			new:      &HelmRelease{Status: "pending-upgrade"},
			expected: true,
		},
		{
			name:     "pending to failed",
			old:      &HelmRelease{Status: "pending-upgrade"},
			new:      &HelmRelease{Status: "failed"},
			expected: true,
		},
		{
			name:     "deployed to deployed",
			old:      &HelmRelease{Status: "deployed"},
			new:      &HelmRelease{Status: "deployed"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.detectProblematicTransition(tt.old, tt.new)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollector_IsHelmHookPod(t *testing.T) {
	collector, err := NewCollector("test", nil)
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
					Name: "myapp-pre-upgrade-hook",
				},
			},
			expected: true,
		},
		{
			name: "regular pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "myapp-deployment-abc123",
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

func TestCollector_IsHelmHookJob(t *testing.T) {
	collector, err := NewCollector("test", nil)
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
					Name: "cronjob-backup",
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

func TestCollector_ConvertPodStatus(t *testing.T) {
	collector, err := NewCollector("test", nil)
	require.NoError(t, err)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			CreationTimestamp: metav1.Time{
				Time: time.Now().Add(-5 * time.Minute),
			},
		},
		Status: v1.PodStatus{
			Phase:   v1.PodRunning,
			Reason:  "Started",
			Message: "Pod is running",
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:         "app",
					Ready:        true,
					RestartCount: 2,
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{},
					},
				},
				{
					Name:         "sidecar",
					Ready:        false,
					RestartCount: 5,
					State: v1.ContainerState{
						Waiting: &v1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "Back-off restarting failed container",
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

	// Check first container
	assert.Equal(t, "app", status.ContainerStatuses[0].Name)
	assert.True(t, status.ContainerStatuses[0].Ready)
	assert.Equal(t, int32(2), status.ContainerStatuses[0].RestartCount)
	assert.Equal(t, "running", status.ContainerStatuses[0].State)

	// Check second container
	assert.Equal(t, "sidecar", status.ContainerStatuses[1].Name)
	assert.False(t, status.ContainerStatuses[1].Ready)
	assert.Equal(t, int32(5), status.ContainerStatuses[1].RestartCount)
	assert.Equal(t, "waiting", status.ContainerStatuses[1].State)
	assert.Equal(t, "CrashLoopBackOff", status.ContainerStatuses[1].Reason)
}

func TestCollector_IsPodFailed(t *testing.T) {
	collector, err := NewCollector("test", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		pod      PodStatus
		expected bool
	}{
		{
			name: "failed phase",
			pod: PodStatus{
				Phase: "Failed",
			},
			expected: true,
		},
		{
			name: "image pull backoff",
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
			name: "crash loop backoff",
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
			name: "terminated with error",
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
			name: "running normally",
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

func TestCollector_EmitFailureEvent(t *testing.T) {
	collector, err := NewCollector("test", &Config{
		EnableK8sWatching: false,
	})
	require.NoError(t, err)

	// Create a channel to capture events
	eventChan := collector.Events()

	rootCause := &RootCause{
		OperationID: "op-123",
		ReleaseName: "myapp",
		Namespace:   "production",
		Pattern:     "Hook Failed",
		Confidence:  0.95,
		Operation:   "upgrade",
		Status:      "failed",
		Summary:     "Pre-upgrade hook failed",
		Details:     "Database migration failed",
		Resolution:  "Check migration logs",
		Impact:      "Upgrade blocked",
		Evidence:    []string{"Job failed", "Pod crashed"},
		EventChain:  []string{"Helm started", "Hook failed"},
		FailureTime: time.Now(),
	}

	// Emit the event
	collector.emitFailureEvent(rootCause)

	// Check event was emitted
	select {
	case event := <-eventChan:
		assert.Equal(t, domain.EventTypeK8sEvent, event.Type)
		assert.Equal(t, domain.EventSeverityError, event.Severity)
		assert.Equal(t, "helm-correlator", event.Source)
		assert.Contains(t, event.Metadata.Tags, "helm-failure")
		assert.Contains(t, event.Metadata.Tags, "Hook Failed")
		assert.Equal(t, "myapp", event.Metadata.Labels["release"])
		assert.Equal(t, "production", event.Metadata.Labels["namespace"])
		assert.Equal(t, "Pre-upgrade hook failed", event.Metadata.Labels["root_cause"])
	case <-time.After(1 * time.Second):
		t.Fatal("Event not emitted")
	}
}

// Benchmark tests

func BenchmarkCollector_ConvertPodStatus(b *testing.B) {
	collector, _ := NewCollector("test", nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			ContainerStatuses: []v1.ContainerStatus{
				{Name: "container1", Ready: true},
				{Name: "container2", Ready: false},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.convertPodStatus(pod)
	}
}

func BenchmarkCollector_IsHelmHookPod(b *testing.B) {
	collector, _ := NewCollector("test", nil)

	pods := []*v1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "regular-pod"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "myapp-hook-pod"}},
		{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"helm.sh/hook": "pre-upgrade"},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.isHelmHookPod(pods[i%len(pods)])
	}
}
