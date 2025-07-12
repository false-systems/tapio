//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/pkg/k8s"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
)

// TestEnvironment manages the test cluster
type TestEnvironment struct {
	client    kubernetes.Interface
	k8sClient *k8s.Client
	namespace string
}

// SetupTestEnvironment creates a test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	// Check if we're in CI or local environment
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	// Create Kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err, "Failed to load kubeconfig")

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "Failed to create Kubernetes client")

	// Create Tapio k8s client
	k8sClient, err := k8s.NewClient(kubeconfig)
	require.NoError(t, err, "Failed to create Tapio k8s client")

	// Create test namespace
	namespace := fmt.Sprintf("tapio-e2e-%d", time.Now().Unix())
	_, err = client.CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create test namespace")

	return &TestEnvironment{
		client:    client,
		k8sClient: k8sClient,
		namespace: namespace,
	}
}

// Cleanup removes test resources
func (te *TestEnvironment) Cleanup(t *testing.T) {
	ctx := context.Background()
	err := te.client.CoreV1().Namespaces().Delete(ctx, te.namespace, metav1.DeleteOptions{})
	assert.NoError(t, err, "Failed to delete test namespace")
}

func TestE2EHealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.Cleanup(t)

	ctx := context.Background()

	t.Run("HealthyPod", func(t *testing.T) {
		// Deploy a healthy pod
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "healthy-nginx",
				Namespace: env.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "nginx",
						Image: "nginx:alpine",
						Resources: v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("50Mi"),
								v1.ResourceCPU:    resource.MustParse("50m"),
							},
						},
					},
				},
			},
		}

		_, err := env.client.CoreV1().Pods(env.namespace).Create(ctx, pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for pod to be ready
		waitForPodReady(t, env.client, env.namespace, pod.Name)

		// Run Tapio check
		checker, err := simple.NewChecker()
		require.NoError(t, err)

		result, err := checker.Check(ctx, &types.CheckRequest{
			Namespace: env.namespace,
			PodName:   pod.Name,
		})
		require.NoError(t, err)

		// Verify no problems found
		assert.Empty(t, result.Problems)
		assert.Equal(t, 1, result.Summary.HealthyPods)
	})

	t.Run("OOMKillScenario", func(t *testing.T) {
		// Deploy a pod that will OOM
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "memory-hog",
				Namespace: env.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "memory-hog",
						Image: "polinux/stress",
						Args: []string{
							"stress",
							"--vm", "1",
							"--vm-bytes", "100M",
							"--vm-hang", "1",
						},
						Resources: v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("50Mi"),
							},
						},
					},
				},
				RestartPolicy: v1.RestartPolicyAlways,
			},
		}

		_, err := env.client.CoreV1().Pods(env.namespace).Create(ctx, pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for OOM to occur
		time.Sleep(10 * time.Second)

		// Run Tapio check
		checker, err := simple.NewChecker()
		require.NoError(t, err)

		result, err := checker.Check(ctx, &types.CheckRequest{
			Namespace: env.namespace,
			PodName:   pod.Name,
		})
		require.NoError(t, err)

		// Verify OOM problem detected
		require.NotEmpty(t, result.Problems)

		foundOOM := false
		for _, problem := range result.Problems {
			if problem.Type == "OOMKilled" || problem.Type == "HighMemoryUsage" {
				foundOOM = true
				assert.Equal(t, types.SeverityCritical, problem.Severity)
				break
			}
		}
		assert.True(t, foundOOM, "Expected OOM problem not found")
	})

	t.Run("CrashLoopBackOff", func(t *testing.T) {
		// Deploy a pod that crashes immediately
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "crashing-pod",
				Namespace: env.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "crash",
						Image:   "busybox",
						Command: []string{"sh", "-c", "exit 1"},
					},
				},
				RestartPolicy: v1.RestartPolicyAlways,
			},
		}

		_, err := env.client.CoreV1().Pods(env.namespace).Create(ctx, pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for crash loop
		time.Sleep(30 * time.Second)

		// Run Tapio check
		checker, err := simple.NewChecker()
		require.NoError(t, err)

		result, err := checker.Check(ctx, &types.CheckRequest{
			Namespace: env.namespace,
			PodName:   pod.Name,
		})
		require.NoError(t, err)

		// Verify crash loop detected
		require.NotEmpty(t, result.Problems)

		foundCrashLoop := false
		for _, problem := range result.Problems {
			if problem.Type == "CrashLoopBackOff" || problem.Type == "ContainerRestarting" {
				foundCrashLoop = true
				assert.Equal(t, types.SeverityCritical, problem.Severity)
				break
			}
		}
		assert.True(t, foundCrashLoop, "Expected CrashLoopBackOff problem not found")
	})
}

func TestE2ETapioCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Ensure tapio binary is built
	buildCmd := exec.Command("go", "build", "-o", "./tapio", "./cmd/tapio")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build tapio binary")
	defer os.Remove("./tapio")

	env := SetupTestEnvironment(t)
	defer env.Cleanup(t)

	t.Run("CLICheckCommand", func(t *testing.T) {
		// Run tapio check command
		cmd := exec.Command("./tapio", "check", "--namespace", env.namespace)
		output, err := cmd.CombinedOutput()

		assert.NoError(t, err, "tapio check failed: %s", string(output))
		assert.Contains(t, string(output), "Checking namespace")
	})

	t.Run("CLIWhyCommand", func(t *testing.T) {
		// Create a problematic pod first
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: env.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "test",
						Image:   "busybox",
						Command: []string{"sh", "-c", "exit 1"},
					},
				},
			},
		}
		_, err := env.client.CoreV1().Pods(env.namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		require.NoError(t, err)

		time.Sleep(5 * time.Second)

		// Run tapio why command
		cmd := exec.Command("./tapio", "why", "test-pod", "--namespace", env.namespace)
		output, err := cmd.CombinedOutput()

		// The command might fail if the pod is in a bad state, but should provide output
		assert.Contains(t, string(output), "test-pod")
	})
}

func TestE2EPrometheusMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	t.Run("MetricsExport", func(t *testing.T) {
		// Start Prometheus exporter
		cmd := exec.Command("./tapio", "prometheus", "--addr", ":19090")
		err := cmd.Start()
		require.NoError(t, err)
		defer cmd.Process.Kill()

		// Wait for server to start
		time.Sleep(2 * time.Second)

		// Check metrics endpoint
		resp, err := http.Get("http://localhost:19090/metrics")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Read metrics
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Verify Tapio metrics are present
		metrics := string(body)
		assert.Contains(t, metrics, "tapio_pod_health_status")
		assert.Contains(t, metrics, "tapio_cluster_health_score")
	})
}

// Helper functions

func waitForPodReady(t *testing.T, client kubernetes.Interface, namespace, name string) {
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && isPodReady(pod) {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("Pod %s/%s did not become ready in time", namespace, name)
}

func isPodReady(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
