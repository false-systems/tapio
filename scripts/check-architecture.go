//go:build ignore

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// 5-Level Architecture Enforcement
// Level 0: pkg/domain/          # Zero dependencies
// Level 1: pkg/collectors/      # Domain only
// Level 2: pkg/intelligence/    # Domain + L1
// Level 3: pkg/integrations/    # Domain + L1 + L2
// Level 4: pkg/interfaces/      # All above

var levelMap = map[string]int{
	"pkg/domain":       0,
	"pkg/collectors":   1,
	"pkg/intelligence": 2,
	"pkg/integrations": 3,
	"pkg/interfaces":   4,
}

var allowedImports = map[int][]string{
	0: {}, // Domain: no internal dependencies
	1: {"pkg/domain"},
	2: {"pkg/domain", "pkg/collectors"},
	3: {"pkg/domain", "pkg/collectors", "pkg/intelligence"},
	4: {"pkg/domain", "pkg/collectors", "pkg/intelligence", "pkg/integrations"},
}

func main() {
	fmt.Println("🏗️  Checking 5-level architecture enforcement...")

	violations := 0
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".go") || strings.Contains(path, "vendor/") {
			return nil
		}

		// Determine the level of this file
		level := getLevel(path)
		if level == -1 {
			return nil // Not in a tracked package
		}

		// Check imports
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "github.com/yairfalse/tapio/") {
				internalPath := strings.TrimPrefix(importPath, "github.com/yairfalse/tapio/")
				if !isAllowedImport(level, internalPath) {
					fmt.Printf("❌ VIOLATION: %s (level %d) imports %s\n", path, level, internalPath)
					violations++
				}
			}
		}

		return nil
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if violations > 0 {
		fmt.Printf("❌ Found %d architecture violations\n", violations)
		os.Exit(1)
	}

	fmt.Println("✅ Architecture enforcement passed!")
}

func getLevel(path string) int {
	for prefix, level := range levelMap {
		if strings.HasPrefix(path, prefix) {
			return level
		}
	}
	return -1
}

func isAllowedImport(level int, importPath string) bool {
	allowed := allowedImports[level]
	for _, allowedPrefix := range allowed {
		if strings.HasPrefix(importPath, allowedPrefix) {
			return true
		}
	}
	return false
}
