package patterns

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// PatternLibraryConfig configures how patterns are loaded
type PatternLibraryConfig struct {
	// Sources to load from
	UseEmbedded    bool   // Load embedded patterns
	LocalDirectory string // Load from local directory
	RemoteEndpoint string // Load from API
	DatabaseURL    string // Load from database

	// Behavior
	FailOnError     bool // Fail if any source fails
	EnableDiscovery bool // Enable ML pattern discovery

	// Repository for dynamic updates
	Repository PatternRepository
}

// DefaultPatternLibraryConfig returns sensible defaults
func DefaultPatternLibraryConfig() *PatternLibraryConfig {
	return &PatternLibraryConfig{
		UseEmbedded:     true,
		LocalDirectory:  "/etc/tapio/patterns",
		FailOnError:     false,
		EnableDiscovery: true,
		Repository:      NewInMemoryPatternRepository(),
	}
}

// CreatePatternLibrary creates a fully configured pattern library
func CreatePatternLibrary(ctx context.Context, config *PatternLibraryConfig, logger *zap.Logger) (*K8sPatternLibrary, error) {
	if config == nil {
		config = DefaultPatternLibraryConfig()
	}

	// Create empty library
	library := NewK8sPatternLibrary()
	loader := NewPatternLoader(library)

	loadedCount := 0

	// 1. Load embedded patterns (compiled into binary)
	if config.UseEmbedded {
		if err := loadEmbeddedPatterns(loader); err != nil {
			logger.Error("Failed to load embedded patterns", zap.Error(err))
			if config.FailOnError {
				return nil, fmt.Errorf("failed to load embedded patterns: %w", err)
			}
		} else {
			count := len(library.patterns)
			logger.Info("Loaded embedded patterns", zap.Int("count", count))
			loadedCount += count
		}
	}

	// 2. Load from local directory
	if config.LocalDirectory != "" {
		if err := loader.LoadFromDirectory(config.LocalDirectory); err != nil {
			logger.Warn("Failed to load patterns from directory",
				zap.String("dir", config.LocalDirectory),
				zap.Error(err))
			if config.FailOnError {
				return nil, fmt.Errorf("failed to load from directory: %w", err)
			}
		} else {
			newCount := len(library.patterns) - loadedCount
			logger.Info("Loaded patterns from directory",
				zap.String("dir", config.LocalDirectory),
				zap.Int("count", newCount))
			loadedCount = len(library.patterns)
		}
	}

	// 3. Load from remote API
	if config.RemoteEndpoint != "" {
		if err := loadFromAPI(library, config.RemoteEndpoint); err != nil {
			logger.Warn("Failed to load patterns from API",
				zap.String("endpoint", config.RemoteEndpoint),
				zap.Error(err))
			if config.FailOnError {
				return nil, fmt.Errorf("failed to load from API: %w", err)
			}
		} else {
			newCount := len(library.patterns) - loadedCount
			logger.Info("Loaded patterns from API",
				zap.String("endpoint", config.RemoteEndpoint),
				zap.Int("count", newCount))
			loadedCount = len(library.patterns)
		}
	}

	// 4. Load from database
	if config.DatabaseURL != "" {
		if err := loadFromDatabase(library, config.DatabaseURL); err != nil {
			logger.Warn("Failed to load patterns from database",
				zap.String("url", config.DatabaseURL),
				zap.Error(err))
			if config.FailOnError {
				return nil, fmt.Errorf("failed to load from database: %w", err)
			}
		}
	}

	// 5. Load from repository if provided
	if config.Repository != nil {
		patterns, err := config.Repository.List(ctx, PatternFilter{})
		if err != nil {
			logger.Warn("Failed to load patterns from repository", zap.Error(err))
		} else {
			for _, pattern := range patterns {
				library.addPattern(pattern)
			}
			logger.Info("Loaded patterns from repository", zap.Int("count", len(patterns)))
		}

		// Subscribe to updates
		config.Repository.Subscribe(ctx, func(pattern *K8sPattern) {
			library.addPattern(pattern)
			logger.Debug("Pattern updated", zap.String("id", pattern.ID))
		})
	}

	// Validate we have at least some patterns
	if len(library.patterns) == 0 {
		return nil, fmt.Errorf("no patterns loaded from any source")
	}

	logger.Info("Pattern library initialized",
		zap.Int("total_patterns", len(library.patterns)),
		zap.Int("categories", len(library.byCategory)))

	return library, nil
}

// Placeholder functions for API and database loading
func loadFromAPI(library *K8sPatternLibrary, endpoint string) error {
	// TODO: Implement HTTP client to fetch patterns
	return fmt.Errorf("API loading not implemented")
}

func loadFromDatabase(library *K8sPatternLibrary, url string) error {
	// TODO: Implement database client to fetch patterns
	return fmt.Errorf("database loading not implemented")
}
