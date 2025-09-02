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
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test data structures to avoid map[string]interface{}
type testReleaseInfo struct {
	Status      string `json:"status"`
	Description string `json:"description"`
}

type testChartMetadata struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
}

type testChartData struct {
	Metadata testChartMetadata `json:"metadata"`
}

type testReleaseFullData struct {
	Name      string          `json:"name"`
	Namespace string          `json:"namespace"`
	Version   float64         `json:"version"`
	Status    string          `json:"status,omitempty"`
	Info      testReleaseInfo `json:"info"`
	Manifest  string          `json:"manifest"`
	Chart     testChartData   `json:"chart"`
}

func TestHelmSecretDecoder_IsHelmSecret(t *testing.T) {
	decoder := NewHelmSecretDecoder(nil)

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
			name: "wrong type",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sh.helm.release.v1.myapp.v1",
				},
				Type: "Opaque",
			},
			expected: false,
		},
		{
			name: "wrong name format",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "myapp-secret",
				},
				Type: "helm.sh/release.v1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decoder.isHelmSecret(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHelmSecretDecoder_ParseHelmSecretName(t *testing.T) {
	decoder := NewHelmSecretDecoder(nil)

	tests := []struct {
		name            string
		secretName      string
		expectedRelease string
		expectedVersion int
	}{
		{
			name:            "simple release name",
			secretName:      "sh.helm.release.v1.myapp.v1",
			expectedRelease: "myapp",
			expectedVersion: 1,
		},
		{
			name:            "release with dashes",
			secretName:      "sh.helm.release.v1.my-app-backend.v42",
			expectedRelease: "my-app-backend",
			expectedVersion: 42,
		},
		{
			name:            "release with dots",
			secretName:      "sh.helm.release.v1.app.example.com.v3",
			expectedRelease: "app.example.com",
			expectedVersion: 3,
		},
		{
			name:            "invalid format",
			secretName:      "not-a-helm-secret",
			expectedRelease: "",
			expectedVersion: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, version := decoder.ParseHelmSecretName(tt.secretName)
			assert.Equal(t, tt.expectedRelease, release)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

func TestHelmSecretDecoder_DecodeRelease(t *testing.T) {
	logger := zap.NewNop()
	decoder := NewHelmSecretDecoder(logger)

	// Create test release data as JSON
	testRelease := testReleaseFullData{
		Name:      "test-app",
		Namespace: "default",
		Version:   1.0,
		Info: testReleaseInfo{
			Status:      "deployed",
			Description: "Install complete",
		},
		Manifest: "apiVersion: v1\nkind: Service\n",
		Chart: testChartData{
			Metadata: testChartMetadata{
				Name:       "test-app",
				Version:    "1.0.0",
				AppVersion: "v1.0",
			},
		},
	}

	// Encode it similar to how Helm does (but simplified)
	data, err := encodeTestReleaseStruct(testRelease)
	require.NoError(t, err)

	// Test decoding
	decoded, err := decoder.decodeRelease(data)
	require.NoError(t, err)
	assert.Equal(t, "test-app", decoded.Name)
	assert.Equal(t, 1, decoded.Version)
	assert.Equal(t, "deployed", decoded.Status)
	assert.Contains(t, decoded.Manifest, "kind: Service")
}

func TestHelmSecretDecoder_CompareReleases(t *testing.T) {
	decoder := NewHelmSecretDecoder(nil)

	oldValues := &HelmValues{
		Raw: json.RawMessage(`{"replicas": 1}`),
	}

	newValues := &HelmValues{
		Raw: json.RawMessage(`{"replicas": 3}`),
	}

	old := &HelmRelease{
		Name:     "myapp",
		Version:  1,
		Status:   "deployed",
		Chart:    "myapp-1.0.0",
		Manifest: "old manifest",
		Values:   oldValues,
	}

	new := &HelmRelease{
		Name:     "myapp",
		Version:  2,
		Status:   "failed",
		Chart:    "myapp-1.1.0",
		Manifest: "new manifest with more content",
		Values:   newValues,
	}

	changes := decoder.CompareReleases(old, new)

	assert.Contains(t, changes, "Status: deployed → failed")
	assert.Contains(t, changes, "Version: 1 → 2")
	assert.Contains(t, changes, "Chart: myapp-1.0.0 → myapp-1.1.0")
	assert.Contains(t, changes, "Values changed")
	assert.Len(t, changes, 5) // Including manifest size change
}

func TestHelmSecretDecoder_ExtractFailureInfo(t *testing.T) {
	decoder := NewHelmSecretDecoder(nil)

	tests := []struct {
		name        string
		release     *HelmRelease
		expectError bool
	}{
		{
			name: "failed release",
			release: &HelmRelease{
				Status: "failed",
				Info: &ReleaseInfo{
					Description:  "upgrade failed: timeout waiting for condition",
					LastDeployed: time.Now(),
				},
				Hooks: []HelmHook{
					{
						Name:   "pre-upgrade-hook",
						Phase:  "pre-upgrade",
						Kind:   "Job",
						Events: "failed",
					},
				},
			},
			expectError: true,
		},
		{
			name: "successful release",
			release: &HelmRelease{
				Status: "deployed",
				Info: &ReleaseInfo{
					Description:  "Install complete",
					LastDeployed: time.Now(),
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := decoder.ExtractFailureInfo(tt.release)
			assert.Equal(t, tt.expectError, info.HasError)
			assert.Equal(t, tt.release.Status, info.Status)
			if tt.release.Info != nil {
				assert.Equal(t, tt.release.Info.Description, info.Description)
			}
		})
	}
}

func TestHelmSecretDecoder_DecodeSecret_FullFlow(t *testing.T) {
	logger := zap.NewNop()
	decoder := NewHelmSecretDecoder(logger)

	// Create a complete test release
	testRelease := testReleaseFullData{
		Name:      "production-api",
		Namespace: "production",
		Version:   5.0,
		Info: testReleaseInfo{
			Status:      "failed",
			Description: "Upgrade failed: pre-upgrade hook failed",
		},
		Chart: testChartData{
			Metadata: testChartMetadata{
				Name:       "api-chart",
				Version:    "2.1.0",
				AppVersion: "v1.5.0",
			},
		},
		Manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n",
	}

	// Encode the release
	data, err := encodeTestReleaseStruct(testRelease)
	require.NoError(t, err)

	// Create a secret with the encoded data
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1.production-api.v5",
			Namespace: "production",
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": data,
		},
	}

	// Decode the secret
	decoded, err := decoder.DecodeSecret(secret)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, "production-api", decoded.Name)
	assert.Equal(t, "production", decoded.Namespace)
	assert.Equal(t, 5, decoded.Version)
	assert.Equal(t, "failed", decoded.Status)
	assert.Equal(t, "api-chart-2.1.0", decoded.Chart)
	assert.Equal(t, "v1.5.0", decoded.AppVersion)
	assert.Contains(t, decoded.Manifest, "kind: Deployment")

	require.NotNil(t, decoded.Info)
	assert.Equal(t, "Upgrade failed: pre-upgrade hook failed", decoded.Info.Description)
}

func TestHelmSecretDecoder_ExtractBasicInfo(t *testing.T) {
	decoder := NewHelmSecretDecoder(nil)

	// Simulate protobuf-like data
	protoData := []byte("some binary\x00name:myapp\x00status:failed\x00other data")

	release := decoder.extractBasicInfo(protoData)
	assert.Equal(t, "failed", release.Status)
	assert.Equal(t, "myapp", release.Name)
}

// Helper function to encode a test release similar to Helm
func encodeTestReleaseStruct(release interface{}) ([]byte, error) {
	// Marshal to JSON (instead of protobuf for testing)
	jsonData, err := json.Marshal(release)
	if err != nil {
		return nil, err
	}

	// Gzip compress
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonData); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	// Base64 encode
	return []byte(base64.StdEncoding.EncodeToString(buf.Bytes())), nil
}
