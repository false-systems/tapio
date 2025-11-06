package base

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// RED: Test CreateTapioResource with full config
func TestCreateTapioResource_FullConfig(t *testing.T) {
	config := TapioResourceConfig{
		ClusterID:   "test-cluster",
		Namespace:   "test-namespace",
		NodeName:    "test-node",
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Verify attributes exist
	attrs := resource.Attributes()
	assert.NotEmpty(t, attrs)
}

// RED: Test CreateTapioResource with default service name
func TestCreateTapioResource_DefaultServiceName(t *testing.T) {
	config := TapioResourceConfig{
		ClusterID: "test-cluster",
		Version:   "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Verify default service name is set
	attrs := resource.Attributes()
	var foundServiceName bool
	for _, attr := range attrs {
		if attr.Key == semconv.ServiceNameKey {
			assert.Equal(t, "tapio-agent", attr.Value.AsString())
			foundServiceName = true
		}
	}
	assert.True(t, foundServiceName)
}

// RED: Test CreateTapioResource with default version from env
func TestCreateTapioResource_DefaultVersionFromEnv(t *testing.T) {
	// Set environment variable
	os.Setenv("TAPIO_VERSION", "2.0.0-env")
	defer os.Unsetenv("TAPIO_VERSION")

	config := TapioResourceConfig{
		ServiceName: "test-service",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Verify version from env
	attrs := resource.Attributes()
	var foundVersion bool
	for _, attr := range attrs {
		if attr.Key == semconv.ServiceVersionKey {
			assert.Equal(t, "2.0.0-env", attr.Value.AsString())
			foundVersion = true
		}
	}
	assert.True(t, foundVersion)
}

// RED: Test CreateTapioResource with dev version
func TestCreateTapioResource_DevVersion(t *testing.T) {
	// Ensure TAPIO_VERSION is not set
	os.Unsetenv("TAPIO_VERSION")

	config := TapioResourceConfig{
		ServiceName: "test-service",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Verify dev version
	attrs := resource.Attributes()
	var foundVersion bool
	for _, attr := range attrs {
		if attr.Key == semconv.ServiceVersionKey {
			assert.Equal(t, "dev", attr.Value.AsString())
			foundVersion = true
		}
	}
	assert.True(t, foundVersion)
}

// RED: Test CreateTapioResource includes Kubernetes attributes
func TestCreateTapioResource_KubernetesAttributes(t *testing.T) {
	config := TapioResourceConfig{
		ClusterID:   "prod-cluster",
		Namespace:   "default",
		NodeName:    "node-1",
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	attrs := resource.Attributes()

	// Verify K8s attributes
	var foundCluster, foundNamespace, foundNode bool
	for _, attr := range attrs {
		switch attr.Key {
		case semconv.K8SClusterNameKey:
			assert.Equal(t, "prod-cluster", attr.Value.AsString())
			foundCluster = true
		case semconv.K8SNamespaceNameKey:
			assert.Equal(t, "default", attr.Value.AsString())
			foundNamespace = true
		case semconv.K8SNodeNameKey:
			assert.Equal(t, "node-1", attr.Value.AsString())
			foundNode = true
		}
	}
	assert.True(t, foundCluster)
	assert.True(t, foundNamespace)
	assert.True(t, foundNode)
}

// RED: Test CreateTapioResource includes host attributes
func TestCreateTapioResource_HostAttributes(t *testing.T) {
	config := TapioResourceConfig{
		NodeName:    "host-123",
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	attrs := resource.Attributes()

	// Verify host attributes
	var foundHostName, foundHostArch, foundOSName bool
	for _, attr := range attrs {
		switch attr.Key {
		case semconv.HostNameKey:
			assert.Equal(t, "host-123", attr.Value.AsString())
			foundHostName = true
		case "host.arch":
			assert.Equal(t, runtime.GOARCH, attr.Value.AsString())
			foundHostArch = true
		case semconv.OSNameKey:
			assert.Equal(t, runtime.GOOS, attr.Value.AsString())
			foundOSName = true
		}
	}
	assert.True(t, foundHostName)
	assert.True(t, foundHostArch)
	assert.True(t, foundOSName)
}

// RED: Test CreateTapioResource includes telemetry SDK attributes
func TestCreateTapioResource_TelemetrySDKAttributes(t *testing.T) {
	config := TapioResourceConfig{
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	attrs := resource.Attributes()

	// Verify telemetry SDK attributes
	var foundSDKName, foundSDKLanguage, foundSDKVersion bool
	for _, attr := range attrs {
		switch attr.Key {
		case semconv.TelemetrySDKNameKey:
			assert.Equal(t, "opentelemetry", attr.Value.AsString())
			foundSDKName = true
		case semconv.TelemetrySDKLanguageKey:
			assert.Equal(t, "go", attr.Value.AsString())
			foundSDKLanguage = true
		case semconv.TelemetrySDKVersionKey:
			assert.Equal(t, otel.Version(), attr.Value.AsString())
			foundSDKVersion = true
		}
	}
	assert.True(t, foundSDKName)
	assert.True(t, foundSDKLanguage)
	assert.True(t, foundSDKVersion)
}

// RED: Test CreateTapioResource with empty config
func TestCreateTapioResource_EmptyConfig(t *testing.T) {
	config := TapioResourceConfig{}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	// Should have defaults
	attrs := resource.Attributes()
	assert.NotEmpty(t, attrs)
}

// RED: Test CreateTapioResource with canceled context
func TestCreateTapioResource_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	config := TapioResourceConfig{
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(ctx, config)
	// Should still succeed even with canceled context (resource.New doesn't check context)
	require.NoError(t, err)
	require.NotNil(t, resource)
}

// RED: Test TapioResourceConfig structure
func TestTapioResourceConfig_Structure(t *testing.T) {
	config := TapioResourceConfig{
		ClusterID:   "cluster-1",
		Namespace:   "namespace-1",
		NodeName:    "node-1",
		ServiceName: "service-1",
		Version:     "v1.0.0",
	}

	assert.Equal(t, "cluster-1", config.ClusterID)
	assert.Equal(t, "namespace-1", config.Namespace)
	assert.Equal(t, "node-1", config.NodeName)
	assert.Equal(t, "service-1", config.ServiceName)
	assert.Equal(t, "v1.0.0", config.Version)
}

// RED: Test CreateTapioResource sets service instance ID
func TestCreateTapioResource_ServiceInstanceID(t *testing.T) {
	config := TapioResourceConfig{
		NodeName:    "instance-123",
		ServiceName: "test-service",
		Version:     "1.0.0",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	attrs := resource.Attributes()

	// Verify service instance ID
	var foundInstanceID bool
	for _, attr := range attrs {
		if attr.Key == semconv.ServiceInstanceIDKey {
			assert.Equal(t, "instance-123", attr.Value.AsString())
			foundInstanceID = true
		}
	}
	assert.True(t, foundInstanceID)
}

// RED: Test CreateTapioResource with version override
func TestCreateTapioResource_VersionOverride(t *testing.T) {
	// Set environment variable
	os.Setenv("TAPIO_VERSION", "env-version")
	defer os.Unsetenv("TAPIO_VERSION")

	// Config version should override env
	config := TapioResourceConfig{
		ServiceName: "test-service",
		Version:     "config-version",
	}

	resource, err := CreateTapioResource(context.Background(), config)
	require.NoError(t, err)
	require.NotNil(t, resource)

	attrs := resource.Attributes()

	// Verify config version takes precedence
	var foundVersion bool
	for _, attr := range attrs {
		if attr.Key == semconv.ServiceVersionKey {
			assert.Equal(t, "config-version", attr.Value.AsString())
			foundVersion = true
		}
	}
	assert.True(t, foundVersion)
}
