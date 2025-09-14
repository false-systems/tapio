package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArchitectureCompliance verifies the pkg/ structure follows 5-level hierarchy
func TestArchitectureCompliance(t *testing.T) {
	tests := []struct {
		name        string
		pkgPath     string
		level       int
		shouldExist bool
	}{
		{"Level 0 - Domain", "pkg/domain", 0, true},
		{"Level 4 - Interfaces", "pkg/interfaces", 4, true},
		{"Level 0 - Config", "pkg/config", 0, true},
		{"Level 0 - Version", "pkg/version", 0, true},

		// These should NOT exist in pkg/ after refactoring
		{"Observers moved to internal", "pkg/observers", 1, false},
		{"Intelligence moved to internal", "pkg/intelligence", 2, false},
		{"Integrations moved to internal", "pkg/integrations", 3, false},
		{"Pipeline moved to internal", "pkg/pipeline", 1, false},
		{"Testutil moved to internal", "pkg/testutil", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := os.Stat(tt.pkgPath)
			exists := !os.IsNotExist(err)

			if tt.shouldExist {
				assert.True(t, exists, "Package %s should exist in pkg/", tt.pkgPath)
			} else {
				assert.False(t, exists, "Package %s should be moved from pkg/", tt.pkgPath)
			}
		})
	}
}

// TestInternalStructureExists verifies moved packages exist in internal/
func TestInternalStructureExists(t *testing.T) {
	expectedInternalPkgs := []string{
		"internal/observers",
		"internal/intelligence",
		"internal/integrations",
		"internal/pipeline",
		"internal/testutil",
	}

	for _, pkgPath := range expectedInternalPkgs {
		t.Run(pkgPath, func(t *testing.T) {
			_, err := os.Stat(pkgPath)
			assert.False(t, os.IsNotExist(err), "Package %s should exist in internal/", pkgPath)
		})
	}
}

// TestNoDependencyViolations verifies no package imports from higher levels
func TestNoDependencyViolations(t *testing.T) {
	// Level 0 packages should have ZERO external dependencies
	level0Packages := []string{"pkg/domain", "pkg/config", "pkg/version"}

	for _, pkgPath := range level0Packages {
		t.Run(pkgPath, func(t *testing.T) {
			if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
				t.Skip("Package does not exist yet")
			}

			violations := checkDependencyViolations(t, pkgPath, 0)
			assert.Empty(t, violations, "Level 0 package %s has dependency violations: %v", pkgPath, violations)
		})
	}
}

// TestImportsStillWork verifies all imports resolve after refactoring
func TestImportsStillWork(t *testing.T) {
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".go") || strings.Contains(path, "vendor/") {
			return nil
		}

		// Parse each Go file and check imports resolve
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			t.Errorf("Failed to parse %s: %v", path, parseErr)
			return nil
		}

		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(importPath, "github.com/yairfalse/tapio") {
				// Verify the imported package exists
				relPath := strings.TrimPrefix(importPath, "github.com/yairfalse/tapio/")
				if _, statErr := os.Stat(relPath); os.IsNotExist(statErr) {
					t.Errorf("Import %s in %s references non-existent package %s", importPath, path, relPath)
				}
			}
		}

		return nil
	})

	require.NoError(t, err)
}

// Helper function to check dependency violations
func checkDependencyViolations(t *testing.T, pkgPath string, level int) []string {
	var violations []string

	err := filepath.Walk(pkgPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".go") {
			return err
		}

		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}

		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(importPath, "github.com/yairfalse/tapio/pkg/") {
				// Level 0 should only import standard library and domain
				if level == 0 && !strings.Contains(importPath, "/domain") {
					violations = append(violations, importPath)
				}
			}
		}

		return nil
	})

	if err != nil {
		t.Errorf("Error walking package %s: %v", pkgPath, err)
	}

	return violations
}
