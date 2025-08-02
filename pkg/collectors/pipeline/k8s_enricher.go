package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sEnricher adds Kubernetes context to events
type K8sEnricher struct {
	logger    *zap.Logger
	clientset kubernetes.Interface
}

// NewK8sEnricher creates a new K8s enricher
func NewK8sEnricher(logger *zap.Logger) (*K8sEnricher, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not in cluster, skip enrichment
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	return &K8sEnricher{
		logger:    logger,
		clientset: clientset,
	}, nil
}

// GetObjectInfo extracts K8s object information from event
func (e *K8sEnricher) GetObjectInfo(event *collectors.RawEvent) *K8sObjectInfo {
	// Try to extract from metadata
	if event.Metadata != nil {
		if kind, ok := event.Metadata["k8s_kind"]; ok {
			return &K8sObjectInfo{
				Kind:      kind,
				Name:      event.Metadata["k8s_name"],
				Namespace: event.Metadata["k8s_namespace"],
				UID:       event.Metadata["k8s_uid"],
				Labels:    extractLabels(event.Metadata),
			}
		}
	}

	// Try to parse from event data
	if info := e.parseEventData(event); info != nil {
		return info
	}

	return nil
}

// parseEventData attempts to extract K8s info from event data
func (e *K8sEnricher) parseEventData(event *collectors.RawEvent) *K8sObjectInfo {
	// Try to unmarshal as JSON
	var data map[string]interface{}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return nil
	}

	// Look for K8s object patterns
	info := &K8sObjectInfo{
		Labels: make(map[string]string),
	}

	// Extract common K8s fields
	if kind, ok := getString(data, "kind"); ok {
		info.Kind = kind
	}
	if name, ok := getString(data, "name"); ok {
		info.Name = name
	}
	if ns, ok := getString(data, "namespace"); ok {
		info.Namespace = ns
	}
	if uid, ok := getString(data, "uid"); ok {
		info.UID = uid
	}

	// Try metadata.name pattern
	if metadata, ok := data["metadata"].(map[string]interface{}); ok {
		if name, ok := getString(metadata, "name"); ok {
			info.Name = name
		}
		if ns, ok := getString(metadata, "namespace"); ok {
			info.Namespace = ns
		}
		if uid, ok := getString(metadata, "uid"); ok {
			info.UID = uid
		}
		if labels, ok := metadata["labels"].(map[string]interface{}); ok {
			for k, v := range labels {
				if str, ok := v.(string); ok {
					info.Labels[k] = str
				}
			}
		}
	}

	// Only return if we found something useful
	if info.Kind != "" || info.Name != "" {
		return info
	}

	return nil
}

func getString(data map[string]interface{}, key string) (string, bool) {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

func extractLabels(metadata map[string]string) map[string]string {
	labels := make(map[string]string)
	for k, v := range metadata {
		if strings.HasPrefix(k, "label_") {
			labelKey := strings.TrimPrefix(k, "label_")
			labels[labelKey] = v
		}
	}
	return labels
}
