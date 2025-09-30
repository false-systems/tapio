package containerruntime

import (
	"fmt"

	"go.uber.org/zap"
)

// OnContainerStart handles container start events
func (c *Observer) OnContainerStart(containerID string, metadata *ContainerMetadata) error {
	if containerID == "" {
		return fmt.Errorf("empty container ID")
	}
	if metadata == nil {
		return fmt.Errorf("nil metadata")
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Store container metadata in cache
	c.containerCache[containerID] = metadata

	c.logger.Debug("Container started",
		zap.String("container_id", containerID),
		zap.String("pod_name", metadata.PodName),
		zap.String("namespace", metadata.Namespace))

	return nil
}

// OnContainerStop handles container stop events
func (c *Observer) OnContainerStop(containerID string) error {
	if containerID == "" {
		return fmt.Errorf("empty container ID")
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Remove from cache
	delete(c.containerCache, containerID)

	c.logger.Debug("Container stopped",
		zap.String("container_id", containerID))

	return nil
}
