package parsers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// KubeAPIEvent represents Kubernetes API events
type KubeAPIEvent struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Operation  string `json:"operation"` // CREATE, UPDATE, DELETE
	APIVersion string `json:"api_version"`
	UID        string `json:"uid"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
	Node       string `json:"node,omitempty"`
	PodName    string `json:"pod_name,omitempty"`
	Service    string `json:"service,omitempty"`
}

// KubeAPIParser parses Kubernetes API events
type KubeAPIParser struct{}

// NewKubeAPIParser creates a new KubeAPI event parser
func NewKubeAPIParser() *KubeAPIParser {
	return &KubeAPIParser{}
}

// Source returns the source this parser handles
func (p *KubeAPIParser) Source() string {
	return "kubeapi"
}

// Parse converts a KubeAPI RawEvent to an ObservationEvent
func (p *KubeAPIParser) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("cannot parse nil event")
	}

	if raw.Source != "kubeapi" {
		return nil, fmt.Errorf("invalid source: expected kubeapi, got %s", raw.Source)
	}

	// Parse the KubeAPI event from Data
	var kubeEvent KubeAPIEvent
	if err := json.Unmarshal(raw.Data, &kubeEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal KubeAPI event: %w", err)
	}

	// Create observation event
	obs := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Timestamp: raw.Timestamp,
		Source:    "kubeapi",
		Type:      fmt.Sprintf("k8s.%s.%s", strings.ToLower(kubeEvent.Kind), strings.ToLower(kubeEvent.Operation)),
	}

	// Add correlation keys
	if kubeEvent.Namespace != "" {
		obs.Namespace = &kubeEvent.Namespace
	}

	if kubeEvent.PodName != "" {
		obs.PodName = &kubeEvent.PodName
	} else if kubeEvent.Kind == "Pod" {
		// If this is a Pod event, use Name as PodName
		obs.PodName = &kubeEvent.Name
	}

	if kubeEvent.Service != "" {
		obs.ServiceName = &kubeEvent.Service
	} else if kubeEvent.Kind == "Service" {
		// If this is a Service event, use Name as ServiceName
		obs.ServiceName = &kubeEvent.Name
	}

	if kubeEvent.Node != "" {
		obs.NodeName = &kubeEvent.Node
	}

	// Set action and target
	action := strings.ToLower(kubeEvent.Operation)
	obs.Action = &action

	target := fmt.Sprintf("%s/%s", kubeEvent.Kind, kubeEvent.Name)
	obs.Target = &target

	// Set result and reason
	result := "success"
	if kubeEvent.Reason != "" {
		result = kubeEvent.Reason
		obs.Reason = &kubeEvent.Reason
	}
	obs.Result = &result

	// Add additional data
	obs.Data = map[string]string{
		"kind":        kubeEvent.Kind,
		"name":        kubeEvent.Name,
		"api_version": kubeEvent.APIVersion,
		"uid":         kubeEvent.UID,
	}

	if kubeEvent.Message != "" {
		obs.Data["message"] = kubeEvent.Message
	}

	return obs, nil
}
