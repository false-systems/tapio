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

// K8sObject represents a structured Kubernetes object for safe parsing
type K8sObject struct {
	Kind       string            `json:"kind"`
	APIVersion string            `json:"apiVersion"`
	Metadata   K8sObjectMetadata `json:"metadata"`
}

type K8sObjectMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Labels    map[string]string `json:"labels"`
}

// parseEventData attempts to extract K8s info from event data using structured types
func (e *K8sEnricher) parseEventData(event *collectors.RawEvent) *K8sObjectInfo {
	// Try to unmarshal as structured K8s object
	var k8sObj K8sObject
	if err := json.Unmarshal(event.Data, &k8sObj); err != nil {
		e.logger.Debug("Failed to parse event data as K8s object",
			zap.Error(err),
			zap.String("event_type", event.Type))
		return nil
	}

	// Only return if we found useful K8s data
	if k8sObj.Kind == "" && k8sObj.Metadata.Name == "" {
		return nil
	}

	// Create K8s object info from structured data
	info := &K8sObjectInfo{
		Kind:      k8sObj.Kind,
		Name:      k8sObj.Metadata.Name,
		Namespace: k8sObj.Metadata.Namespace,
		UID:       k8sObj.Metadata.UID,
		Labels:    make(map[string]string),
	}

	// Copy labels safely
	if k8sObj.Metadata.Labels != nil {
		for k, v := range k8sObj.Metadata.Labels {
			info.Labels[k] = v
		}
	}

	return info
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
