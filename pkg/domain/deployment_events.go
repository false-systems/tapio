package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// DeploymentAction represents the type of deployment action
type DeploymentAction string

const (
	DeploymentCreated    DeploymentAction = "created"
	DeploymentUpdated    DeploymentAction = "updated"
	DeploymentRolledBack DeploymentAction = "rolledback"
	DeploymentScaled     DeploymentAction = "scaled"
)

// validDeploymentActions contains all valid deployment actions for validation
var validDeploymentActions = map[DeploymentAction]bool{
	DeploymentCreated:    true,
	DeploymentUpdated:    true,
	DeploymentRolledBack: true,
	DeploymentScaled:     true,
}

// validDeploymentStrategies contains valid Kubernetes deployment strategies
var validDeploymentStrategies = map[string]bool{
	"RollingUpdate": true,
	"Recreate":      true,
}

// IsValid returns true if the deployment action is valid
func (da DeploymentAction) IsValid() bool {
	return validDeploymentActions[da]
}

// DeploymentMetadata contains metadata about a deployment change
type DeploymentMetadata struct {
	OldImage    string            `json:"old_image,omitempty"`
	NewImage    string            `json:"new_image,omitempty"`
	OldReplicas int32             `json:"old_replicas,omitempty"`
	NewReplicas int32             `json:"new_replicas"`
	Strategy    string            `json:"strategy,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Validate validates the deployment metadata
func (dm *DeploymentMetadata) Validate() error {
	if dm.OldReplicas < 0 {
		return fmt.Errorf("old replicas cannot be negative: %d", dm.OldReplicas)
	}

	if dm.NewReplicas < 0 {
		return fmt.Errorf("new replicas cannot be negative: %d", dm.NewReplicas)
	}

	// Strategy validation - empty is allowed (will use default)
	if dm.Strategy != "" && !validDeploymentStrategies[dm.Strategy] {
		return fmt.Errorf("invalid strategy: %s", dm.Strategy)
	}

	return nil
}

// DeploymentEvent represents a deployment change event
type DeploymentEvent struct {
	Timestamp time.Time          `json:"timestamp"`
	Namespace string             `json:"namespace"`
	Name      string             `json:"name"`
	Action    DeploymentAction   `json:"action"`
	Metadata  DeploymentMetadata `json:"metadata"`
}

// Validate validates the deployment event
func (de *DeploymentEvent) Validate() error {
	if de.Timestamp.IsZero() {
		return fmt.Errorf("timestamp cannot be zero")
	}

	if de.Namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if de.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if !de.Action.IsValid() {
		return fmt.Errorf("invalid deployment action: %s", de.Action)
	}

	if err := de.Metadata.Validate(); err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	return nil
}

// GetResourceRef returns a resource reference for this deployment event
func (de *DeploymentEvent) GetResourceRef() ResourceRef {
	return ResourceRef{
		Kind:      "Deployment",
		Name:      de.Name,
		Namespace: de.Namespace,
	}
}

// GetEventID generates a deterministic event ID based on event properties
func (de *DeploymentEvent) GetEventID() EventID {
	// Create deterministic ID from key properties
	components := []string{
		de.Namespace,
		de.Name,
		string(de.Action),
		de.Timestamp.Format(time.RFC3339Nano),
	}

	// Include image changes in ID for uniqueness
	if de.Metadata.OldImage != "" || de.Metadata.NewImage != "" {
		components = append(components, fmt.Sprintf("images:%s->%s", de.Metadata.OldImage, de.Metadata.NewImage))
	}

	// Include replica changes in ID for uniqueness
	if de.HasScaleChange() {
		components = append(components, fmt.Sprintf("replicas:%d->%d", de.Metadata.OldReplicas, de.Metadata.NewReplicas))
	}

	idString := strings.Join(components, "|")
	hash := sha256.Sum256([]byte(idString))

	return EventID(fmt.Sprintf("deployment-%s-%x", de.Name, hash[:8]))
}

// HasImageChange returns true if this deployment event involves an image change
func (de *DeploymentEvent) HasImageChange() bool {
	return de.Metadata.OldImage != "" &&
		de.Metadata.NewImage != "" &&
		de.Metadata.OldImage != de.Metadata.NewImage
}

// HasScaleChange returns true if this deployment event involves a replica count change
func (de *DeploymentEvent) HasScaleChange() bool {
	return de.Metadata.OldReplicas != de.Metadata.NewReplicas
}
