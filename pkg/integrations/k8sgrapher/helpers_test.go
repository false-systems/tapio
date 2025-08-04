package k8sgrapher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		expected string
	}{
		{
			name:     "empty selector",
			selector: map[string]string{},
			expected: "",
		},
		{
			name: "single label",
			selector: map[string]string{
				"app": "frontend",
			},
			expected: "app=frontend",
		},
		{
			name: "multiple labels",
			selector: map[string]string{
				"app":     "frontend",
				"version": "v1",
			},
			// Labels are sorted alphabetically
			expected: "app=frontend,version=v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSelector(tt.selector)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "empty labels",
			labels: map[string]string{},
		},
		{
			name: "single label",
			labels: map[string]string{
				"app": "test",
			},
		},
		{
			name: "multiple labels",
			labels: map[string]string{
				"app":         "test",
				"environment": "prod",
				"version":     "1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLabels(tt.labels)
			assert.Equal(t, len(tt.labels), len(result))
			for k, v := range tt.labels {
				assert.Equal(t, v, result[k])
			}
		})
	}
}

func TestFormatLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector *metav1.LabelSelector
		expected string
	}{
		{
			name:     "nil selector",
			selector: nil,
			expected: "",
		},
		{
			name: "match labels only",
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "frontend",
				},
			},
			expected: "app=frontend",
		},
		{
			name: "match expressions",
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"production", "staging"},
					},
				},
			},
			expected: "environment in (production,staging)",
		},
		{
			name: "both match labels and expressions",
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "frontend",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "version",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			expected: "app=frontend,version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLabelSelector(tt.selector)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOwnerInfo(t *testing.T) {
	tests := []struct {
		name         string
		pod          *corev1.Pod
		expectedKind string
		expectedName string
	}{
		{
			name: "no owner",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expectedKind: "",
			expectedName: "",
		},
		{
			name: "single owner",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "ReplicaSet",
							Name: "test-rs",
						},
					},
				},
			},
			expectedKind: "ReplicaSet",
			expectedName: "test-rs",
		},
		{
			name: "multiple owners",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "ReplicaSet",
							Name: "test-rs",
						},
						{
							Kind: "Deployment",
							Name: "test-deploy",
						},
					},
				},
			},
			expectedKind: "ReplicaSet",
			expectedName: "test-rs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, name := getOwnerInfo(tt.pod)
			assert.Equal(t, tt.expectedKind, kind)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "not ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "pod without ready condition",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "pod with no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPodReady(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetMapKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected int
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: 0,
		},
		{
			name: "single key",
			input: map[string]string{
				"key1": "value1",
			},
			expected: 1,
		},
		{
			name: "multiple keys",
			input: map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMapKeys(tt.input)
			assert.Len(t, result, tt.expected)

			// Verify all keys are present
			for key := range tt.input {
				assert.Contains(t, result, key)
			}
		})
	}
}

func TestGetSecretKeys(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string][]byte
		expected int
	}{
		{
			name:     "empty secret",
			data:     map[string][]byte{},
			expected: 0,
		},
		{
			name: "single key",
			data: map[string][]byte{
				"password": []byte("secret"),
			},
			expected: 1,
		},
		{
			name: "multiple keys",
			data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret"),
				"token":    []byte("abc123"),
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSecretKeys(tt.data)
			assert.Len(t, result, tt.expected)

			// Verify all keys are present
			for key := range tt.data {
				assert.Contains(t, result, key)
			}
		})
	}
}

func TestSanitizeForNeo4j(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "string with single quotes",
			input:    "hello 'world'",
			expected: "hello \\'world\\'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple quotes",
			input:    "it's a 'test' string",
			expected: "it\\'s a \\'test\\' string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeForNeo4j(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildResourceID(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		resName   string
		expected  string
	}{
		{
			name:      "simple case",
			namespace: "default",
			resName:   "my-pod",
			expected:  "default/my-pod",
		},
		{
			name:      "empty namespace",
			namespace: "",
			resName:   "my-pod",
			expected:  "/my-pod",
		},
		{
			name:      "complex names",
			namespace: "kube-system",
			resName:   "coredns-565d847f94-abc123",
			expected:  "kube-system/coredns-565d847f94-abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildResourceID(tt.namespace, tt.resName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseResourceID(t *testing.T) {
	tests := []struct {
		name              string
		resourceID        string
		expectedNamespace string
		expectedName      string
	}{
		{
			name:              "simple case",
			resourceID:        "default/my-pod",
			expectedNamespace: "default",
			expectedName:      "my-pod",
		},
		{
			name:              "no namespace",
			resourceID:        "my-pod",
			expectedNamespace: "",
			expectedName:      "my-pod",
		},
		{
			name:              "empty namespace",
			resourceID:        "/my-pod",
			expectedNamespace: "",
			expectedName:      "my-pod",
		},
		{
			name:              "name with slash",
			resourceID:        "default/my-pod/with-slash",
			expectedNamespace: "default",
			expectedName:      "my-pod/with-slash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, name := parseResourceID(tt.resourceID)
			assert.Equal(t, tt.expectedNamespace, namespace)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}
