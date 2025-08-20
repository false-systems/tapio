package cri

import "time"

// ContainerEventData represents processed container event data
type ContainerEventData struct {
	ContainerID string
	Name        string
	State       string
	Image       string
	Labels      map[string]string
	CreatedAt   time.Time
}
