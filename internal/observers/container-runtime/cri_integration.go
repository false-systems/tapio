package containerruntime

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// CRIIntegration handles integration between CRI and CRI-eBPF observers
type CRIIntegration struct {
	ebpfObserver *Observer
	logger       *zap.Logger
}

// NewCRIIntegration creates a new integration handler
func NewCRIIntegration(ebpfObserver *Observer) *CRIIntegration {
	logger, _ := zap.NewProduction()
	return &CRIIntegration{
		ebpfObserver: ebpfObserver,
		logger:       logger.Named("container-runtime-integration"),
	}
}

// HandleCRIEvent processes events from the CRI observer
// This should be called by the orchestrator when CRI events are received
func (ci *CRIIntegration) HandleCRIEvent(ctx context.Context, event *domain.CollectorEvent) error {
	// Only care about container lifecycle events
	switch event.Type {
	case domain.EventTypeContainerCreate, domain.EventTypeContainerStart:
		return ci.handleContainerStart(ctx, event)
	case domain.EventTypeContainerStop:
		return ci.handleContainerStop(ctx, event)
	default:
		// Ignore other events
		return nil
	}
}

// handleContainerStart processes container start events
func (ci *CRIIntegration) handleContainerStart(ctx context.Context, event *domain.CollectorEvent) error {
	// Extract container data
	if event.EventData.Container == nil {
		return fmt.Errorf("invalid container event data")
	}

	container := event.EventData.Container
	containerID := container.ContainerID
	if containerID == "" {
		return fmt.Errorf("container ID is empty")
	}

	// Extract K8s metadata from the event
	k8sMeta := &ContainerMetadata{
		ContainerID: containerID,
	}

	// Extract K8s context if available
	if event.K8sContext != nil {
		k8sMeta.PodUID = event.K8sContext.UID
		k8sMeta.PodName = event.K8sContext.Name
		k8sMeta.Namespace = event.K8sContext.Namespace
	}

	// Note: ContainerData doesn't include resource limits
	// Those would typically come from the CRI API or K8s metadata
	k8sMeta.MemoryLimit = 0 // Will be populated from other sources

	// Call OnContainerStart to populate eBPF maps
	if err := ci.ebpfObserver.OnContainerStart(containerID, k8sMeta); err != nil {
		ci.logger.Error("Failed to handle container start",
			zap.String("container_id", containerID),
			zap.Error(err))
		return err
	}

	ci.logger.Info("Successfully tracked container start",
		zap.String("container_id", containerID),
		zap.String("pod_name", k8sMeta.PodName),
		zap.String("namespace", k8sMeta.Namespace))

	return nil
}

// handleContainerStop processes container stop events
func (ci *CRIIntegration) handleContainerStop(ctx context.Context, event *domain.CollectorEvent) error {
	// Extract container data
	if event.EventData.Container == nil {
		return fmt.Errorf("invalid container event data")
	}

	containerID := event.EventData.Container.ContainerID
	if containerID == "" {
		return fmt.Errorf("container ID is empty")
	}

	// Call OnContainerStop to clean up
	if err := ci.ebpfObserver.OnContainerStop(containerID); err != nil {
		ci.logger.Error("Failed to handle container stop",
			zap.String("container_id", containerID),
			zap.Error(err))
		return err
	}

	ci.logger.Info("Successfully handled container stop",
		zap.String("container_id", containerID))

	return nil
}
