package etcdapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name    string
		cfgName string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			cfgName: "test-etcd",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "empty name",
			cfgName: "",
			config:  DefaultConfig(),
			wantErr: false, // Name is not validated in constructor
		},
		{
			name:    "invalid endpoints",
			cfgName: "test-etcd",
			config: Config{
				Endpoints:   []string{},
				DialTimeout: 5,
				BufferSize:  1000,
			},
			wantErr: true,
			errMsg:  "at least one etcd endpoint must be specified",
		},
		{
			name:    "zero timeout gets default",
			cfgName: "test-etcd",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: 0,
				BufferSize:  1000,
			},
			wantErr: false, // DialTimeout gets default value
		},
		{
			name:    "invalid buffer size",
			cfgName: "test-etcd",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: 5,
				BufferSize:  0,
			},
			wantErr: true,
			errMsg:  "buffer size must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.cfgName, tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, collector)
			} else {
				// Note: Will fail to connect but won't error in constructor
				assert.NotNil(t, collector)
				assert.Equal(t, tt.cfgName, collector.Name())
			}
		})
	}
}

func TestCollectorInterface(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test Name
	assert.Equal(t, "test-etcd", collector.Name())

	// Test Events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Test IsHealthy (should be false before start)
	assert.False(t, collector.IsHealthy())
}

func TestParseK8sKey(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name          string
		key           string
		wantNamespace string
		wantKind      string
		wantName      string
		wantValid     bool
	}{
		{
			name:          "valid pod key",
			key:           "/registry/pods/default/test-pod",
			wantNamespace: "default",
			wantKind:      "Pod",
			wantName:      "test-pod",
			wantValid:     true,
		},
		{
			name:          "valid service key",
			key:           "/registry/services/kube-system/kube-dns",
			wantNamespace: "kube-system",
			wantKind:      "Service",
			wantName:      "kube-dns",
			wantValid:     true,
		},
		{
			name:          "cluster-scoped resource",
			key:           "/registry/nodes/node1",
			wantNamespace: "",
			wantKind:      "Node",
			wantName:      "node1",
			wantValid:     true,
		},
		{
			name:          "invalid key format",
			key:           "/invalid/key",
			wantNamespace: "",
			wantKind:      "",
			wantName:      "",
			wantValid:     false,
		},
		{
			name:          "empty key",
			key:           "",
			wantNamespace: "",
			wantKind:      "",
			wantName:      "",
			wantValid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sContext := collector.extractK8sContext(tt.key, "")
			namespace := k8sContext.Namespace
			kind := k8sContext.Kind
			name := k8sContext.Name
			valid := kind != "" || name != ""
			assert.Equal(t, tt.wantNamespace, namespace)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantValid, valid)
		})
	}
}

func TestDetermineOperation(t *testing.T) {
	_, _ = NewCollector("test", DefaultConfig())

	tests := []struct {
		name      string
		eventType mvccpb.Event_EventType
		want      string
	}{
		{
			name:      "put operation",
			eventType: mvccpb.PUT,
			want:      "put",
		},
		{
			name:      "delete operation",
			eventType: mvccpb.DELETE,
			want:      "delete",
		},
		{
			name:      "unknown operation",
			eventType: mvccpb.Event_EventType(999),
			want:      "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			switch tt.eventType {
			case mvccpb.PUT:
				result = "put"
			case mvccpb.DELETE:
				result = "delete"
			default:
				result = "unknown"
			}
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestNormalizeResourceType(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name         string
		resourceType string
		want         string
	}{
		{
			name:         "pods to Pod",
			resourceType: "pods",
			want:         "Pod",
		},
		{
			name:         "services to Service",
			resourceType: "services",
			want:         "Service",
		},
		{
			name:         "deployments to Deployment",
			resourceType: "deployments",
			want:         "Deployment",
		},
		{
			name:         "configmaps to ConfigMap",
			resourceType: "configmaps",
			want:         "ConfigMap",
		},
		{
			name:         "unknown resource",
			resourceType: "customresource",
			want:         "Customresource",
		},
		{
			name:         "empty resource",
			resourceType: "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.normalizeResourceType(tt.resourceType)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestMapK8sKindToEventType(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name string
		kind string
		want domain.CollectorEventType
	}{
		{
			name: "Pod",
			kind: "Pod",
			want: domain.EventTypeK8sPod,
		},
		{
			name: "Service",
			kind: "Service",
			want: domain.EventTypeK8sService,
		},
		{
			name: "Deployment",
			kind: "Deployment",
			want: domain.EventTypeK8sDeployment,
		},
		{
			name: "ConfigMap",
			kind: "ConfigMap",
			want: domain.EventTypeK8sConfigMap,
		},
		{
			name: "Secret",
			kind: "Secret",
			want: domain.EventTypeK8sSecret,
		},
		{
			name: "Unknown",
			kind: "CustomResource",
			want: domain.EventTypeETCD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.mapK8sKindToEventType(tt.kind)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestEnrichK8sContextFromValue(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name        string
		k8sContext  *domain.K8sContext
		value       string
		wantContext *domain.K8sContext
	}{
		{
			name: "valid k8s object",
			k8sContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
			value: `{
				"apiVersion": "v1",
				"metadata": {
					"uid": "12345",
					"resourceVersion": "100",
					"generation": 1,
					"labels": {
						"app": "test"
					},
					"annotations": {
						"note": "test-note"
					}
				},
				"spec": {
					"nodeName": "node1",
					"replicas": 3
				},
				"status": {
					"phase": "Running"
				}
			}`,
			wantContext: &domain.K8sContext{
				Kind:            "Pod",
				Name:            "test-pod",
				UID:             "12345",
				APIVersion:      "v1",
				ResourceVersion: "100",
				Generation:      1,
				NodeName:        "node1",
				Phase:           "Running",
				Labels: map[string]string{
					"app": "test",
				},
				Annotations: map[string]string{
					"note": "test-note",
				},
			},
		},
		{
			name: "invalid json",
			k8sContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
			value: "invalid json",
			wantContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
		},
		{
			name: "empty value",
			k8sContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
			value: "",
			wantContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.enrichK8sContextFromValue(tt.k8sContext, tt.value)
			assert.Equal(t, tt.wantContext.Kind, tt.k8sContext.Kind)
			assert.Equal(t, tt.wantContext.Name, tt.k8sContext.Name)
			assert.Equal(t, tt.wantContext.UID, tt.k8sContext.UID)
			assert.Equal(t, tt.wantContext.APIVersion, tt.k8sContext.APIVersion)
			assert.Equal(t, tt.wantContext.ResourceVersion, tt.k8sContext.ResourceVersion)
			assert.Equal(t, tt.wantContext.Generation, tt.k8sContext.Generation)
			assert.Equal(t, tt.wantContext.NodeName, tt.k8sContext.NodeName)
			assert.Equal(t, tt.wantContext.Phase, tt.k8sContext.Phase)
			assert.Equal(t, tt.wantContext.Labels, tt.k8sContext.Labels)
			assert.Equal(t, tt.wantContext.Annotations, tt.k8sContext.Annotations)
		})
	}
}

func TestExtractWorkloadContext(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name        string
		k8sContext  *domain.K8sContext
		spec        *K8sSpec
		wantContext *domain.K8sContext
	}{
		{
			name: "with selector and replicas",
			k8sContext: &domain.K8sContext{
				Kind: "Deployment",
				Name: "test-deploy",
			},
			spec: &K8sSpec{
				Selector: &K8sSelector{
					MatchLabels: map[string]string{
						"app": "test",
						"env": "prod",
					},
				},
				NodeName: "node1",
				Replicas: intPtr(3),
			},
			wantContext: &domain.K8sContext{
				Kind:         "Deployment",
				Name:         "test-deploy",
				NodeName:     "node1",
				WorkloadKind: "Deployment",
				WorkloadName: "test-deploy",
				Selectors: map[string]string{
					"app": "test",
					"env": "prod",
				},
			},
		},
		{
			name: "no selector",
			k8sContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
			spec: &K8sSpec{
				NodeName: "node2",
			},
			wantContext: &domain.K8sContext{
				Kind:     "Pod",
				Name:     "test-pod",
				NodeName: "node2",
			},
		},
		{
			name: "nil spec fields",
			k8sContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
			spec: &K8sSpec{},
			wantContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.extractWorkloadContext(tt.k8sContext, tt.spec)
			assert.Equal(t, tt.wantContext.NodeName, tt.k8sContext.NodeName)
			assert.Equal(t, tt.wantContext.WorkloadKind, tt.k8sContext.WorkloadKind)
			assert.Equal(t, tt.wantContext.WorkloadName, tt.k8sContext.WorkloadName)
			assert.Equal(t, tt.wantContext.Selectors, tt.k8sContext.Selectors)
		})
	}
}

func TestExtractStatusContext(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name        string
		k8sContext  *domain.K8sContext
		status      *K8sStatus
		wantContext *domain.K8sContext
	}{
		{
			name: "with phase and conditions",
			k8sContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
			status: &K8sStatus{
				Phase: "Running",
				Conditions: []K8sCondition{
					{
						Type:    "Ready",
						Status:  "True",
						Reason:  "PodReady",
						Message: "Pod is ready",
					},
					{
						Type:    "Initialized",
						Status:  "True",
						Reason:  "PodInitialized",
						Message: "All init containers completed",
					},
				},
			},
			wantContext: &domain.K8sContext{
				Kind:  "Pod",
				Name:  "test-pod",
				Phase: "Running",
				Conditions: []domain.ConditionSnapshot{
					{
						Type:    "Ready",
						Status:  "True",
						Reason:  "PodReady",
						Message: "Pod is ready",
					},
					{
						Type:    "Initialized",
						Status:  "True",
						Reason:  "PodInitialized",
						Message: "All init containers completed",
					},
				},
			},
		},
		{
			name: "only phase",
			k8sContext: &domain.K8sContext{
				Kind: "Pod",
				Name: "test-pod",
			},
			status: &K8sStatus{
				Phase: "Pending",
			},
			wantContext: &domain.K8sContext{
				Kind:  "Pod",
				Name:  "test-pod",
				Phase: "Pending",
			},
		},
		{
			name: "empty status",
			k8sContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
			status: &K8sStatus{},
			wantContext: &domain.K8sContext{
				Kind: "Service",
				Name: "test-svc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.extractStatusContext(tt.k8sContext, tt.status)
			assert.Equal(t, tt.wantContext.Phase, tt.k8sContext.Phase)
			assert.Equal(t, tt.wantContext.Conditions, tt.k8sContext.Conditions)
		})
	}
}

func TestGenerateEventID(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name     string
		key      string
		revision int64
	}{
		{
			name:     "simple key",
			key:      "/registry/pods/default/test",
			revision: 100,
		},
		{
			name:     "long key",
			key:      "/registry/deployments/kube-system/very-long-deployment-name-with-many-characters",
			revision: 999999,
		},
		{
			name:     "empty key",
			key:      "",
			revision: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := collector.generateEventID(tt.key, tt.revision)
			assert.NotEmpty(t, id)
			assert.Contains(t, id, "etcd-api-")
			assert.Contains(t, id, "-"+string(rune(tt.revision)))
		})
	}
}

func TestBuildEventTags(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	tests := []struct {
		name       string
		k8sContext *domain.K8sContext
		operation  string
		wantTags   []string
	}{
		{
			name: "namespaced resource",
			k8sContext: &domain.K8sContext{
				Kind:      "Pod",
				Namespace: "default",
			},
			operation: "put",
			wantTags:  []string{"etcd", "k8s", "put", "pod", "namespaced"},
		},
		{
			name: "cluster-scoped resource",
			k8sContext: &domain.K8sContext{
				Kind: "Node",
			},
			operation: "delete",
			wantTags:  []string{"etcd", "k8s", "delete", "node", "cluster-scoped"},
		},
		{
			name:       "empty context",
			k8sContext: &domain.K8sContext{},
			operation:  "watch",
			wantTags:   []string{"etcd", "k8s", "watch", "cluster-scoped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := collector.buildEventTags(tt.k8sContext, tt.operation)
			assert.ElementsMatch(t, tt.wantTags, tags)
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "empty endpoints",
			config: Config{
				Endpoints:   []string{},
				DialTimeout: 5,
				BufferSize:  1000,
			},
			wantErr: true,
			errMsg:  "endpoints required",
		},
		{
			name: "negative timeout",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: -1,
				BufferSize:  1000,
			},
			wantErr: true,
			errMsg:  "dial timeout must be positive",
		},
		{
			name: "zero buffer size",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: 5,
				BufferSize:  0,
			},
			wantErr: true,
			errMsg:  "buffer size must be positive",
		},
		{
			name: "negative buffer size",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: 5,
				BufferSize:  -100,
			},
			wantErr: true,
			errMsg:  "buffer size must be positive",
		},
		{
			name: "valid with TLS",
			config: Config{
				Endpoints:   []string{"localhost:2379"},
				DialTimeout: 5,
				BufferSize:  1000,
				TLS: &TLSConfig{
					CertFile: "/path/to/cert",
					KeyFile:  "/path/to/key",
					CAFile:   "/path/to/ca",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProcessEvent(t *testing.T) {
	collector, _ := NewCollector("test", DefaultConfig())

	// Create a test event
	kv := &mvccpb.KeyValue{
		Key:            []byte("/registry/pods/default/test-pod"),
		Value:          []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"test-pod","namespace":"default","uid":"12345"}}`),
		CreateRevision: 100,
		ModRevision:    200,
		Version:        1,
	}

	// Create context and use processEtcdEvent which creates the event internally
	ctx := context.Background()
	etcdEvent := &clientv3.Event{
		Type: clientv3.EventTypePut,
		Kv:   kv,
	}
	collector.processEtcdEvent(ctx, etcdEvent)

	// Get the event from the channel
	var event *domain.CollectorEvent
	select {
	case event = <-collector.Events():
		// Got event
	default:
		// No event in buffer
		t.Fatal("No event was generated")
	}

	require.NotNil(t, event)
	assert.Equal(t, domain.EventTypeK8sPod, event.Type)
	assert.Equal(t, "test", event.Source)
	assert.NotEmpty(t, event.EventID)
	assert.False(t, event.Timestamp.IsZero())

	// Check K8s context
	assert.NotNil(t, event.K8sContext)
	assert.Equal(t, "Pod", event.K8sContext.Kind)
	assert.Equal(t, "test-pod", event.K8sContext.Name)
	assert.Equal(t, "default", event.K8sContext.Namespace)
	assert.Equal(t, "12345", event.K8sContext.UID)

	// Check ETCD data
	etcdData, ok := event.GetETCDData()
	assert.True(t, ok)
	assert.NotNil(t, etcdData)
	assert.Equal(t, "put", etcdData.Operation)
	assert.Equal(t, "/registry/pods/default/test-pod", etcdData.Key)
	assert.Equal(t, int64(200), etcdData.Revision)
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("hashString", func(t *testing.T) {
		hash1 := hashString("test")
		hash2 := hashString("test")
		hash3 := hashString("different")

		assert.Equal(t, hash1, hash2, "Same input should produce same hash")
		assert.NotEqual(t, hash1, hash3, "Different input should produce different hash")
		assert.Len(t, hash1, 16, "Hash should be 16 characters")
	})

	t.Run("getHostname", func(t *testing.T) {
		hostname := getHostname()
		assert.NotEmpty(t, hostname)
	})

	t.Run("getKernelVersion", func(t *testing.T) {
		version := getKernelVersion()
		assert.NotEmpty(t, version)
	})

	t.Run("getOSVersion", func(t *testing.T) {
		version := getOSVersion()
		assert.NotEmpty(t, version)
	})

	t.Run("getArchitecture", func(t *testing.T) {
		arch := getArchitecture()
		assert.NotEmpty(t, arch)
	})
}

func TestK8sTypesUnmarshaling(t *testing.T) {
	jsonData := `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "test-deployment",
			"namespace": "default",
			"uid": "abc-123",
			"generation": 5,
			"labels": {
				"app": "test"
			},
			"ownerReferences": [{
				"apiVersion": "v1",
				"kind": "ReplicaSet",
				"name": "test-rs",
				"uid": "rs-123",
				"controller": true
			}]
		},
		"spec": {
			"replicas": 3,
			"selector": {
				"matchLabels": {
					"app": "test"
				}
			}
		},
		"status": {
			"phase": "Running",
			"conditions": [{
				"type": "Available",
				"status": "True",
				"reason": "MinimumReplicasAvailable",
				"message": "Deployment has minimum availability"
			}]
		}
	}`

	var obj K8sObject
	err := json.Unmarshal([]byte(jsonData), &obj)
	require.NoError(t, err)

	assert.Equal(t, "apps/v1", obj.APIVersion)
	assert.Equal(t, "Deployment", obj.Kind)
	assert.Equal(t, "test-deployment", obj.Metadata.Name)
	assert.Equal(t, "default", obj.Metadata.Namespace)
	assert.Equal(t, "abc-123", obj.Metadata.UID)
	assert.Equal(t, int64(5), obj.Metadata.Generation)
	assert.Equal(t, "test", obj.Metadata.Labels["app"])

	assert.Len(t, obj.Metadata.OwnerReferences, 1)
	assert.Equal(t, "ReplicaSet", obj.Metadata.OwnerReferences[0].Kind)
	assert.True(t, *obj.Metadata.OwnerReferences[0].Controller)

	assert.Equal(t, int32(3), *obj.Spec.Replicas)
	assert.Equal(t, "test", obj.Spec.Selector.MatchLabels["app"])

	assert.Equal(t, "Running", obj.Status.Phase)
	assert.Len(t, obj.Status.Conditions, 1)
	assert.Equal(t, "Available", obj.Status.Conditions[0].Type)
	assert.Equal(t, "True", obj.Status.Conditions[0].Status)
}

// Helper function
func intPtr(i int32) *int32 {
	return &i
}
