package patterns

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PatternRepository defines the interface for pattern storage
type PatternRepository interface {
	// CRUD operations
	Create(ctx context.Context, pattern *K8sPattern) error
	Get(ctx context.Context, id string) (*K8sPattern, error)
	Update(ctx context.Context, pattern *K8sPattern) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter PatternFilter) ([]*K8sPattern, error)

	// Bulk operations
	BulkCreate(ctx context.Context, patterns []*K8sPattern) error
	BulkUpdate(ctx context.Context, patterns []*K8sPattern) error

	// Version control
	GetVersion(ctx context.Context, id string, version int) (*K8sPattern, error)
	ListVersions(ctx context.Context, id string) ([]PatternVersion, error)

	// Pattern discovery
	Subscribe(ctx context.Context, callback func(*K8sPattern)) error
}

// PatternFilter for querying patterns
type PatternFilter struct {
	Categories []PatternCategory
	Tags       []string
	Severity   []string
	Enabled    *bool
	Search     string
	Limit      int
	Offset     int
}

// PatternVersion represents a version of a pattern
type PatternVersion struct {
	Version   int
	Pattern   *K8sPattern
	UpdatedAt time.Time
	UpdatedBy string
	ChangeLog string
}

// InMemoryPatternRepository is a simple in-memory implementation
type InMemoryPatternRepository struct {
	mu          sync.RWMutex
	patterns    map[string]*K8sPattern
	versions    map[string][]PatternVersion
	subscribers []func(*K8sPattern)
}

// NewInMemoryPatternRepository creates a new in-memory repository
func NewInMemoryPatternRepository() *InMemoryPatternRepository {
	return &InMemoryPatternRepository{
		patterns:    make(map[string]*K8sPattern),
		versions:    make(map[string][]PatternVersion),
		subscribers: make([]func(*K8sPattern), 0),
	}
}

// Create adds a new pattern
func (r *InMemoryPatternRepository) Create(ctx context.Context, pattern *K8sPattern) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.patterns[pattern.ID]; exists {
		return fmt.Errorf("pattern %s already exists", pattern.ID)
	}

	r.patterns[pattern.ID] = pattern
	r.notifySubscribers(pattern)

	// Create initial version
	r.versions[pattern.ID] = []PatternVersion{
		{
			Version:   1,
			Pattern:   pattern,
			UpdatedAt: time.Now(),
			UpdatedBy: "system",
			ChangeLog: "Initial creation",
		},
	}

	return nil
}

// Get retrieves a pattern by ID
func (r *InMemoryPatternRepository) Get(ctx context.Context, id string) (*K8sPattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pattern, exists := r.patterns[id]
	if !exists {
		return nil, fmt.Errorf("pattern %s not found", id)
	}

	return pattern, nil
}

// Update modifies an existing pattern
func (r *InMemoryPatternRepository) Update(ctx context.Context, pattern *K8sPattern) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, exists := r.patterns[pattern.ID]
	if !exists {
		return fmt.Errorf("pattern %s not found", pattern.ID)
	}

	// Store new version
	versions := r.versions[pattern.ID]
	newVersion := PatternVersion{
		Version:   len(versions) + 1,
		Pattern:   pattern,
		UpdatedAt: time.Now(),
		UpdatedBy: "system",
		ChangeLog: "Updated pattern",
	}
	r.versions[pattern.ID] = append(versions, newVersion)

	r.patterns[pattern.ID] = pattern
	r.notifySubscribers(pattern)

	_ = existing // Could compare for changelog
	return nil
}

// Delete removes a pattern
func (r *InMemoryPatternRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.patterns[id]; !exists {
		return fmt.Errorf("pattern %s not found", id)
	}

	delete(r.patterns, id)
	delete(r.versions, id)

	return nil
}

// List returns patterns matching the filter
func (r *InMemoryPatternRepository) List(ctx context.Context, filter PatternFilter) ([]*K8sPattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*K8sPattern

	for _, pattern := range r.patterns {
		if r.matchesFilter(pattern, filter) {
			results = append(results, pattern)
		}
	}

	// Apply pagination
	start := filter.Offset
	end := start + filter.Limit
	if filter.Limit == 0 {
		end = len(results)
	}

	if start > len(results) {
		return []*K8sPattern{}, nil
	}
	if end > len(results) {
		end = len(results)
	}

	return results[start:end], nil
}

// BulkCreate creates multiple patterns
func (r *InMemoryPatternRepository) BulkCreate(ctx context.Context, patterns []*K8sPattern) error {
	for _, pattern := range patterns {
		if err := r.Create(ctx, pattern); err != nil {
			return fmt.Errorf("failed to create pattern %s: %w", pattern.ID, err)
		}
	}
	return nil
}

// BulkUpdate updates multiple patterns
func (r *InMemoryPatternRepository) BulkUpdate(ctx context.Context, patterns []*K8sPattern) error {
	for _, pattern := range patterns {
		if err := r.Update(ctx, pattern); err != nil {
			return fmt.Errorf("failed to update pattern %s: %w", pattern.ID, err)
		}
	}
	return nil
}

// GetVersion retrieves a specific version of a pattern
func (r *InMemoryPatternRepository) GetVersion(ctx context.Context, id string, version int) (*K8sPattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.versions[id]
	if !exists {
		return nil, fmt.Errorf("pattern %s not found", id)
	}

	if version < 1 || version > len(versions) {
		return nil, fmt.Errorf("version %d not found for pattern %s", version, id)
	}

	return versions[version-1].Pattern, nil
}

// ListVersions returns all versions of a pattern
func (r *InMemoryPatternRepository) ListVersions(ctx context.Context, id string) ([]PatternVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.versions[id]
	if !exists {
		return nil, fmt.Errorf("pattern %s not found", id)
	}

	return versions, nil
}

// Subscribe registers a callback for pattern updates
func (r *InMemoryPatternRepository) Subscribe(ctx context.Context, callback func(*K8sPattern)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.subscribers = append(r.subscribers, callback)
	return nil
}

// matchesFilter checks if a pattern matches the filter criteria
func (r *InMemoryPatternRepository) matchesFilter(pattern *K8sPattern, filter PatternFilter) bool {
	// Check categories
	if len(filter.Categories) > 0 {
		found := false
		for _, cat := range filter.Categories {
			if pattern.Category == cat {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check severity
	if len(filter.Severity) > 0 {
		found := false
		for _, sev := range filter.Severity {
			if pattern.Impact.Severity == sev {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check search term
	if filter.Search != "" {
		// Simple search in name and description
		if !contains(pattern.Name, filter.Search) &&
			!contains(pattern.Description, filter.Search) {
			return false
		}
	}

	return true
}

// notifySubscribers notifies all subscribers of a pattern change
func (r *InMemoryPatternRepository) notifySubscribers(pattern *K8sPattern) {
	for _, callback := range r.subscribers {
		go callback(pattern)
	}
}

// contains performs case-insensitive string search
func contains(s, substr string) bool {
	return len(substr) == 0 || strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// PatternSource represents where a pattern came from
type PatternSource struct {
	Type      string // "builtin", "file", "api", "ml", "user"
	Location  string // file path, API endpoint, etc.
	Version   string // source version
	UpdatedAt time.Time
	Metadata  map[string]string
}

// EnrichedPattern includes source and metadata
type EnrichedPattern struct {
	*K8sPattern
	Source     PatternSource
	Statistics PatternStatistics
}

// PatternStatistics tracks pattern performance
type PatternStatistics struct {
	MatchCount         int64
	LastMatch          time.Time
	FalsePositives     int64
	TruePositives      int64
	AverageLatency     time.Duration
	EffectivenessScore float64
}
