//go:build linux
// +build linux

package bpf

import (
	"fmt"
)

// ServicemonitorObjects is an exported alias for the generated type
type ServicemonitorObjects = servicemonitorObjects

// ServicemonitorMaps is an exported alias for the generated maps
type ServicemonitorMaps = servicemonitorMaps

// ServicemonitorPrograms is an exported alias for the generated programs
type ServicemonitorPrograms = servicemonitorPrograms

// LoadServicemonitorObjects loads the service monitor eBPF collection with options
func LoadServicemonitorObjects(obj interface{}, opts interface{}) error {
	// Type assert opts to the correct type or pass nil
	if opts == nil {
		return loadServicemonitorObjects(obj, nil)
	}
	// For now, just pass nil since we don't use options
	return loadServicemonitorObjects(obj, nil)
}

// LoadServicemonitor loads the service monitoring eBPF collection
func LoadServicemonitor() (*ServicemonitorObjects, error) {
	var objs servicemonitorObjects
	if err := loadServicemonitorObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading service monitor BPF objects: %w", err)
	}
	return &objs, nil
}
