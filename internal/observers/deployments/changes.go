package deployments

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
)

// ChangeType represents the type of change detected
type ChangeType string

const (
	ChangeTypeScale    ChangeType = "scale"
	ChangeTypeImage    ChangeType = "image"
	ChangeTypeConfig   ChangeType = "config"
	ChangeTypeResource ChangeType = "resource"
	ChangeTypeStrategy ChangeType = "strategy"
)

// Change represents a detected change between two deployment states
type Change struct {
	Type        ChangeType
	Field       string
	OldValue    string
	NewValue    string
	Description string
}

// detectChanges identifies meaningful changes between old and new deployments
// Returns empty slice if no meaningful changes detected
func detectChanges(old, new *appsv1.Deployment) []Change {
	if old == nil || new == nil {
		return []Change{}
	}

	changes := []Change{}

	// Detect replica changes
	if old.Spec.Replicas != nil && new.Spec.Replicas != nil {
		if *old.Spec.Replicas != *new.Spec.Replicas {
			changes = append(changes, Change{
				Type:        ChangeTypeScale,
				Field:       "spec.replicas",
				OldValue:    fmt.Sprintf("%d", *old.Spec.Replicas),
				NewValue:    fmt.Sprintf("%d", *new.Spec.Replicas),
				Description: fmt.Sprintf("Scaled from %d to %d replicas", *old.Spec.Replicas, *new.Spec.Replicas),
			})
		}
	}

	// Detect image changes
	changes = append(changes, detectImageChanges(old, new)...)

	// Detect strategy changes
	if old.Spec.Strategy.Type != new.Spec.Strategy.Type {
		changes = append(changes, Change{
			Type:        ChangeTypeStrategy,
			Field:       "spec.strategy.type",
			OldValue:    string(old.Spec.Strategy.Type),
			NewValue:    string(new.Spec.Strategy.Type),
			Description: fmt.Sprintf("Strategy changed from %s to %s", old.Spec.Strategy.Type, new.Spec.Strategy.Type),
		})
	}

	// Detect resource limit changes
	changes = append(changes, detectResourceChanges(old, new)...)

	return changes
}

// detectImageChanges finds container image changes
func detectImageChanges(old, new *appsv1.Deployment) []Change {
	changes := []Change{}

	oldContainers := old.Spec.Template.Spec.Containers
	newContainers := new.Spec.Template.Spec.Containers

	// Check each container
	for i, newContainer := range newContainers {
		if i >= len(oldContainers) {
			// New container added
			changes = append(changes, Change{
				Type:        ChangeTypeImage,
				Field:       fmt.Sprintf("spec.template.spec.containers[%d]", i),
				OldValue:    "",
				NewValue:    newContainer.Image,
				Description: fmt.Sprintf("Container '%s' added with image %s", newContainer.Name, newContainer.Image),
			})
			continue
		}

		oldContainer := oldContainers[i]
		if oldContainer.Image != newContainer.Image {
			changes = append(changes, Change{
				Type:        ChangeTypeImage,
				Field:       fmt.Sprintf("spec.template.spec.containers[%d].image", i),
				OldValue:    oldContainer.Image,
				NewValue:    newContainer.Image,
				Description: fmt.Sprintf("Container '%s' image updated from %s to %s", newContainer.Name, oldContainer.Image, newContainer.Image),
			})
		}
	}

	return changes
}

// detectResourceChanges finds resource limit/request changes
func detectResourceChanges(old, new *appsv1.Deployment) []Change {
	changes := []Change{}

	oldContainers := old.Spec.Template.Spec.Containers
	newContainers := new.Spec.Template.Spec.Containers

	for i, newContainer := range newContainers {
		if i >= len(oldContainers) {
			continue
		}

		oldContainer := oldContainers[i]

		// Check CPU limits
		oldCPU := oldContainer.Resources.Limits.Cpu()
		newCPU := newContainer.Resources.Limits.Cpu()
		if oldCPU != nil && newCPU != nil && !oldCPU.Equal(*newCPU) {
			changes = append(changes, Change{
				Type:        ChangeTypeResource,
				Field:       fmt.Sprintf("spec.template.spec.containers[%d].resources.limits.cpu", i),
				OldValue:    oldCPU.String(),
				NewValue:    newCPU.String(),
				Description: fmt.Sprintf("Container '%s' CPU limit changed from %s to %s", newContainer.Name, oldCPU.String(), newCPU.String()),
			})
		}

		// Check memory limits
		oldMem := oldContainer.Resources.Limits.Memory()
		newMem := newContainer.Resources.Limits.Memory()
		if oldMem != nil && newMem != nil && !oldMem.Equal(*newMem) {
			changes = append(changes, Change{
				Type:        ChangeTypeResource,
				Field:       fmt.Sprintf("spec.template.spec.containers[%d].resources.limits.memory", i),
				OldValue:    oldMem.String(),
				NewValue:    newMem.String(),
				Description: fmt.Sprintf("Container '%s' memory limit changed from %s to %s", newContainer.Name, oldMem.String(), newMem.String()),
			})
		}
	}

	return changes
}

// getImpactLevel determines impact level based on changes
func getImpactLevel(changes []Change) string {
	if len(changes) == 0 {
		return "low"
	}

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeImage, ChangeTypeStrategy:
			return "high"
		case ChangeTypeResource:
			return "high"
		}
	}

	return "medium"
}

// getPrimaryChangeType returns the most significant change type
func getPrimaryChangeType(changes []Change) string {
	if len(changes) == 0 {
		return "none"
	}

	// Priority: image > scale > resource > strategy > config
	for _, change := range changes {
		if change.Type == ChangeTypeImage {
			return "image"
		}
	}
	for _, change := range changes {
		if change.Type == ChangeTypeScale {
			return "scale"
		}
	}
	for _, change := range changes {
		if change.Type == ChangeTypeResource {
			return "resource"
		}
	}

	return string(changes[0].Type)
}

// requiresRestart determines if changes require pod restart
func requiresRestart(changes []Change) bool {
	for _, change := range changes {
		switch change.Type {
		case ChangeTypeImage, ChangeTypeConfig, ChangeTypeResource:
			return true
		}
	}
	return false
}

// getRelatedEventTypes returns event types that may correlate with these changes
func getRelatedEventTypes(changes []Change) []string {
	if len(changes) == 0 {
		return []string{}
	}

	relatedTypes := make(map[string]bool)

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeImage:
			relatedTypes["container.oom"] = true
			relatedTypes["container.restart"] = true
			relatedTypes["container.exit"] = true
			relatedTypes["network.connection"] = true
		case ChangeTypeScale:
			relatedTypes["container.create"] = true
			relatedTypes["network.connection"] = true
			relatedTypes["memory.allocation"] = true
		case ChangeTypeConfig:
			relatedTypes["container.restart"] = true
			relatedTypes["k8s.configmap"] = true
		case ChangeTypeResource:
			relatedTypes["container.oom"] = true
			relatedTypes["memory.allocation"] = true
		}
	}

	// Convert map to slice
	types := make([]string, 0, len(relatedTypes))
	for t := range relatedTypes {
		types = append(types, t)
	}

	return types
}
