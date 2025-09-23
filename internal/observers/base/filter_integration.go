package base

import (
	"github.com/yairfalse/tapio/pkg/domain"
)

// ShouldProcess checks if an event should be processed based on filters
// Returns true if event passes filters, false if it should be dropped
func (bc *BaseObserver) ShouldProcess(event *domain.CollectorEvent) bool {
	if bc.filterManager == nil || !bc.useFilters {
		return true // No filters, process everything
	}

	allowed := bc.filterManager.ShouldAllow(event)
	if !allowed {
		bc.eventsFiltered.Add(1)
		bc.RecordFilteredEvent(event)
	}
	return allowed
}

// AddAllowFilter adds a named allow filter at runtime
func (bc *BaseObserver) AddAllowFilter(name string, filter FilterFunc) {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	bc.filterManager.AddAllowFilter(name, filter)
}

// AddDenyFilter adds a named deny filter at runtime
func (bc *BaseObserver) AddDenyFilter(name string, filter FilterFunc) {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	bc.filterManager.AddDenyFilter(name, filter)
}

// RemoveFilter removes a filter by name
func (bc *BaseObserver) RemoveFilter(name string) {
	if bc.filterManager != nil {
		bc.filterManager.RemoveFilter(name)
	}
}

// GetFilterStatistics returns filter statistics
func (bc *BaseObserver) GetFilterStatistics() *FilterStatistics {
	if bc.filterManager != nil {
		stats := bc.filterManager.GetStatistics()
		return &stats
	}
	return nil
}

// LoadFiltersFromFile loads filters from a YAML file
func (bc *BaseObserver) LoadFiltersFromFile(path string) error {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	return bc.filterManager.LoadFromFile(path)
}

// StopFilters stops the filter manager (stops watching config file)
func (bc *BaseObserver) StopFilters() {
	if bc.filterManager != nil {
		bc.filterManager.Stop()
	}
}
