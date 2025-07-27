//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Check for type safety violations
func main() {
	fmt.Println("🛡️  Checking type safety...")

	violations := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".go") || strings.Contains(path, "vendor/") {
			return nil
		}

		violations += checkTypeViolations(path)
		violations += checkCodePatterns(path)

		return nil
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if violations > 0 {
		fmt.Printf("❌ Found %d type safety violations\n", violations)
		os.Exit(1)
	}

	fmt.Println("✅ Type safety check passed!")
}

func checkTypeViolations(path string) int {
	violations := 0

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return 0
	}

	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.InterfaceType:
			// Check for interface{} abuse in public APIs
			if len(x.Methods.List) == 0 {
				pos := fset.Position(x.Pos())
				fmt.Printf("❌ %s:%d - Empty interface (interface{}) usage\n", path, pos.Line)
				violations++
			}
		case *ast.MapType:
			// Check for map[string]interface{} in public APIs
			if keyType, ok := x.Key.(*ast.Ident); ok && keyType.Name == "string" {
				if valType, ok := x.Value.(*ast.InterfaceType); ok && len(valType.Methods.List) == 0 {
					pos := fset.Position(x.Pos())
					fmt.Printf("❌ %s:%d - map[string]interface{} usage detected\n", path, pos.Line)
					violations++
				}
			}
		}
		return true
	})

	return violations
}

func checkCodePatterns(path string) int {
	violations := 0

	// Patterns to detect type safety issues
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`interface\{\}`),
		regexp.MustCompile(`map\[string\]interface\{\}`),
		regexp.MustCompile(`\.\(\s*\*?\w+\s*\)`), // Type assertions
	}

	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments
		if strings.HasPrefix(line, "//") {
			continue
		}

		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				// Allow some exceptions
				if strings.Contains(line, "// Type safety exception") ||
					strings.Contains(line, "json.Unmarshal") ||
					strings.Contains(line, "context.Value") {
					continue
				}

				fmt.Printf("⚠️  %s:%d - Potential type safety issue: %s\n", path, lineNum, line)
				// Don't count as violation for now, just warn
			}
		}
	}

	return violations
}
