package patterns

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Embed pattern files directly into the binary
//
//go:embed patterns/*.yaml patterns/*.json
var embeddedPatterns embed.FS

// BootstrapPatternLibrary creates a library without any hardcoded patterns
func BootstrapPatternLibrary() (*K8sPatternLibrary, error) {
	library := &K8sPatternLibrary{
		patterns:   make(map[string]*K8sPattern),
		byCategory: make(map[PatternCategory][]*K8sPattern),
	}

	loader := NewPatternLoader(library)

	// Load from embedded files
	if err := loadEmbeddedPatterns(loader); err != nil {
		return nil, fmt.Errorf("failed to load embedded patterns: %w", err)
	}

	// Load from environment-specified directory
	if patternDir := getPatternDirectory(); patternDir != "" {
		if err := loader.LoadFromDirectory(patternDir); err != nil {
			// Log but don't fail - embedded patterns are enough to start
			fmt.Printf("Warning: failed to load patterns from %s: %v\n", patternDir, err)
		}
	}

	// Load from remote source if configured
	if apiEndpoint := getPatternAPIEndpoint(); apiEndpoint != "" {
		if err := loadRemotePatterns(library, apiEndpoint); err != nil {
			// Log but don't fail
			fmt.Printf("Warning: failed to load remote patterns: %v\n", err)
		}
	}

	return library, nil
}

// loadEmbeddedPatterns loads patterns from embedded files
func loadEmbeddedPatterns(loader *PatternLoader) error {
	entries, err := embeddedPatterns.ReadDir("patterns")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".json") {
			data, err := embeddedPatterns.ReadFile(filepath.Join("patterns", name))
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", name, err)
			}

			if err := loader.LoadFromBytes(name, data); err != nil {
				return fmt.Errorf("failed to load pattern from %s: %w", name, err)
			}
		}
	}

	return nil
}

// getPatternDirectory returns the pattern directory from env or config
func getPatternDirectory() string {
	// Could read from:
	// - Environment variable: TAPIO_PATTERN_DIR
	// - Config file
	// - Default locations: /etc/tapio/patterns, ./patterns
	return os.Getenv("TAPIO_PATTERN_DIR")
}

// getPatternAPIEndpoint returns the API endpoint for patterns
func getPatternAPIEndpoint() string {
	return os.Getenv("TAPIO_PATTERN_API")
}

// loadRemotePatterns loads patterns from a remote API
func loadRemotePatterns(library *K8sPatternLibrary, endpoint string) error {
	// This would make HTTP calls to fetch patterns
	// For now, placeholder
	return nil
}
