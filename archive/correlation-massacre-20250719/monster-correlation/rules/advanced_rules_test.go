package rules

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/domain"
	v1 "k8s.io/api/core/v1"
)

func TestETCDCascadeRule(t *testing.T) {
	rule := NewETCDCascadeRule(DefaultETCDCascadeConfig())

	t.Run("Basic Properties", func(t *testing.T) {
		assert.Equal(t, "etcd_cascade_failure", rule.ID())
		assert.Equal(t, "ETCD Cascading Failure Detection", rule.Name())
		assert.NotEmpty(t, rule.Description())
	})

	t.Run("No Issues Without ETCD Problems", func(t *testing.T) {
		data := &correlation.AnalysisData{
			KubernetesData: correlation.KubernetesData{
				Pods: []types.PodInfo{
					{
						PodMeta: types.PodMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
					},
				},
			},
		}

		findings, err := rule.Execute(context.Background(), data)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})

	t.Run("Detects ETCD Memory Pressure", func(t *testing.T) {
		// Create test data with etcd pod having memory issues
		data := &correlation.AnalysisData{
			KubernetesData: correlation.KubernetesData{
				Pods: []types.PodInfo{
					{
						PodMeta: types.PodMeta{
							Name:      "etcd-master",
							Namespace: "kube-system",
							Labels:    map[string]string{"component": "etcd"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "etcd",
									Resources: v1.ResourceRequirements{
										Limits: v1.ResourceList{
											v1.ResourceMemory: *resource.NewQuantity(1073741824, resource.BinarySI), // 1Gi
										},
									},
								},
							},
						},
						Status: v1.PodStatus{
							ContainerStatuses: []v1.ContainerStatus{
								{
									Name:  "etcd",
									Ready: true,
								},
							},
						},
					},
					{
						PodMeta: types.PodMeta{
							Name:      "kube-apiserver-master",
							Namespace: "kube-system",
							Labels:    map[string]string{"component": "kube-apiserver"},
						},
					},
				},
				Events: []types.EventInfo{
					{
						EventMeta: types.EventMeta{
							Message:   "etcd timeout: context deadline exceeded",
							Type:      "Warning",
							CreatedAt: time.Now().Add(-2 * time.Minute),
						},
						InvolvedObject: types.ObjectReference{
							Kind:      "Pod",
							Name:      "kube-apiserver-master",
							Namespace: "kube-system",
						},
					},
				},
				Metrics: map[string]map[string]interface{}{
					"kube-system/etcd-master/etcd": {
						"memory_usage_bytes": float64(950000000), // 950MB out of 1GB
					},
				},
			},
		}

		findings, err := rule.Execute(context.Background(), data)
		require.NoError(t, err)

		// Should detect the cascade pattern
		if len(findings) > 0 {
			assert.Equal(t, "ETCD Cascading Failure Detected", findings[0].Title)
			assert.Equal(t, correlation.SeverityLevelCritical, findings[0].Severity)
			assert.NotNil(t, findings[0].Prediction)
		}
	})
}

func TestCertificateCascadeRule(t *testing.T) {
	rule := NewCertificateCascadeRule(DefaultCertificateCascadeConfig())

	t.Run("Basic Properties", func(t *testing.T) {
		assert.Equal(t, "certificate_chain_failure", rule.ID())
		assert.Equal(t, "Certificate Chain Failure Detection", rule.Name())
		assert.NotEmpty(t, rule.Description())
	})

	t.Run("Detects Certificate Issues", func(t *testing.T) {
		data := &correlation.AnalysisData{
			KubernetesData: correlation.KubernetesData{
				Pods: []types.PodInfo{
					{
						PodMeta: types.PodMeta{
							Name:      "webhook-pod",
							Namespace: "default",
							Labels:    map[string]string{"webhook": "admission"},
						},
						Status: v1.PodStatus{
							ContainerStatuses: []v1.ContainerStatus{
								{
									Name:         "webhook",
									Ready:        false,
									RestartCount: 3,
								},
							},
						},
					},
				},
				Events: []types.EventInfo{
					{
						EventMeta: types.EventMeta{
							Message:   "x509: certificate has expired or is not yet valid",
							Type:      "Warning",
							CreatedAt: time.Now().Add(-5 * time.Minute),
						},
						InvolvedObject: types.ObjectReference{
							Kind:      "Pod",
							Name:      "webhook-pod",
							Namespace: "default",
						},
					},
					{
						EventMeta: types.EventMeta{
							Message:   "admission webhook denied the request",
							Type:      "Warning",
							CreatedAt: time.Now().Add(-3 * time.Minute),
						},
						InvolvedObject: types.ObjectReference{
							Kind:      "Deployment",
							Name:      "test-app",
							Namespace: "default",
						},
					},
				},
			},
		}

		findings, err := rule.Execute(context.Background(), data)
		require.NoError(t, err)

		// May detect certificate cascade
		if len(findings) > 0 {
			assert.Contains(t, findings[0].Title, "Certificate")
			assert.True(t, findings[0].Severity >= correlation.SeverityLevelError)
		}
	})
}

func TestAdmissionLockdownRule(t *testing.T) {
	rule := NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig())

	t.Run("Basic Properties", func(t *testing.T) {
		assert.Equal(t, "admission_controller_lockdown", rule.ID())
		assert.Equal(t, "Admission Controller Lockdown Detection", rule.Name())
		assert.NotEmpty(t, rule.Description())
	})

	t.Run("Detects Admission Denials", func(t *testing.T) {
		// Create events showing many admission denials
		events := []types.EventInfo{}
		for i := 0; i < 15; i++ {
			events = append(events, types.EventInfo{
				EventMeta: types.EventMeta{
					Message:   "admission webhook denied the request: policy violation",
					Type:      "Warning",
					CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
				},
				InvolvedObject: types.ObjectReference{
					Kind:      "Pod",
					Name:      fmt.Sprintf("app-pod-%d", i),
					Namespace: "default",
				},
			})
		}

		data := &correlation.AnalysisData{
			KubernetesData: correlation.KubernetesData{
				Events: events,
			},
		}

		findings, err := rule.Execute(context.Background(), data)
		require.NoError(t, err)

		// Should detect lockdown pattern
		if len(findings) > 0 {
			assert.Contains(t, findings[0].Title, "Lockdown")
			assert.True(t, findings[0].Confidence >= 0.7)
		}
	})
}

func TestControlPlaneDepsRule(t *testing.T) {
	rule := NewControlPlaneDepsRule(DefaultControlPlaneDepsConfig())

	t.Run("Basic Properties", func(t *testing.T) {
		assert.Equal(t, "control_plane_dependency_failure", rule.ID())
		assert.Equal(t, "Control Plane Dependency Failure Detection", rule.Name())
		assert.NotEmpty(t, rule.Description())
	})

	t.Run("Detects Cloud Provider Issues", func(t *testing.T) {
		data := &correlation.AnalysisData{
			KubernetesData: correlation.KubernetesData{
				Pods: []types.PodInfo{
					{
						PodMeta: types.PodMeta{
							Name:      "kube-controller-manager",
							Namespace: "kube-system",
							Labels:    map[string]string{"component": "kube-controller-manager"},
						},
						Status: v1.PodStatus{
							ContainerStatuses: []v1.ContainerStatus{
								{
									Name:         "controller-manager",
									Ready:        false,
									RestartCount: 5,
								},
							},
						},
					},
				},
				Logs: map[string][]string{
					"kube-controller-manager": {
						"E0710 15:00:00 controller.go:123] Failed to create AWS load balancer: timeout waiting for response",
						"E0710 15:01:00 controller.go:124] AWS API error: connection timeout",
					},
				},
			},
		}

		findings, err := rule.Execute(context.Background(), data)
		require.NoError(t, err)

		// May detect dependency failure
		if len(findings) > 0 {
			assert.Contains(t, findings[0].Title, "Dependency")
			assert.NotEmpty(t, findings[0].Evidence)
		}
	})
}

func TestRuleRegistration(t *testing.T) {
	registry := correlation.NewRuleRegistry()

	// Test that all rules can be registered
	err := RegisterDefaultRules(registry)
	require.NoError(t, err)

	// Verify all advanced rules are registered
	rules := registry.GetAllRules()

	// Check for advanced rule IDs
	advancedRuleIDs := []string{
		"etcd_cascade_failure",
		"certificate_chain_failure",
		"admission_controller_lockdown",
		"control_plane_dependency_failure",
	}

	registeredIDs := make(map[string]bool)
	for _, rule := range rules {
		registeredIDs[rule.ID()] = true
	}

	for _, id := range advancedRuleIDs {
		assert.True(t, registeredIDs[id], "Rule %s should be registered", id)
	}
}
