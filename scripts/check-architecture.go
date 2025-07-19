// Architecture Enforcement Script for Tapio
// Validates the 5-level dependency hierarchy with zero tolerance
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// ANSI color codes
	RED    = "\033[0;31m"
	GREEN  = "\033[0;32m"
	YELLOW = "\033[0;33m"
	BLUE   = "\033[0;34m"
	NC     = "\033[0m" // No Color
	BOLD   = "\033[1m"
)

// Level definitions according to Claude.md
var levelHierarchy = map[string]int{
	"pkg/domain":       0, // Zero dependencies
	"pkg/collectors":   1, // Domain only
	"pkg/intelligence": 2, // Domain + Level 1
	"pkg/integrations": 3, // Domain + Level 1 + Level 2
	"pkg/interfaces":   4, // All above levels
}

type ArchitectureViolation struct {
	File        string
	Line        int
	Import      string
	FromLevel   int
	ToLevel     int
	Violation   string
	Severity    string
}

type ArchitectureChecker struct {
	violations []ArchitectureViolation
	fileSet    *token.FileSet
}

func main() {
	fmt.Printf("%s🏗️  Tapio Architecture Enforcement%s\n", BOLD+BLUE, NC)
	fmt.Printf("Validating 5-level dependency hierarchy...\n\n")

	checker := &ArchitectureChecker{
		violations: []ArchitectureViolation{},
		fileSet:    token.NewFileSet(),
	}

	// Check all Go files in the project
	err := filepath.Walk(".", checker.walkFunc)
	if err != nil {
		fmt.Printf("%sError walking directory: %v%s\n", RED, err, NC)
		os.Exit(1)
	}

	// Report results
	checker.reportResults()

	// Exit with error code if violations found
	if len(checker.violations) > 0 {
		os.Exit(1)
	}
}

func (ac *ArchitectureChecker) walkFunc(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	// Skip non-Go files and test files
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return nil
	}

	// Skip vendor and generated files
	if strings.Contains(path, "vendor/") || strings.Contains(path, ".git/") {
		return nil
	}

	// Only check files in pkg/ hierarchy
	if !strings.HasPrefix(path, "pkg/") {
		return nil
	}

	return ac.checkFile(path)
}

func (ac *ArchitectureChecker) checkFile(filePath string) error {
	// Parse the Go file
	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	file, err := parser.ParseFile(ac.fileSet, filePath, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}

	// Determine the current file's level
	currentLevel, currentLevelName := ac.getFileLevel(filePath)
	if currentLevel == -1 {
		return nil // Not in hierarchy
	}

	// Check all imports
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		ac.validateImport(filePath, currentLevel, currentLevelName, importPath, ac.fileSet.Position(imp.Pos()).Line)
	}

	return nil
}

func (ac *ArchitectureChecker) getFileLevel(filePath string) (int, string) {
	for levelName, level := range levelHierarchy {
		if strings.HasPrefix(filePath, levelName+"/") {
			return level, levelName
		}
	}
	return -1, ""
}

func (ac *ArchitectureChecker) validateImport(filePath string, currentLevel int, currentLevelName, importPath string, line int) {
	// Skip standard library and external imports
	if !strings.HasPrefix(importPath, "github.com/yairfalse/tapio/pkg/") {
		return
	}

	// Extract the imported level
	importedLevel, importedLevelName := ac.getImportLevel(importPath)
	if importedLevel == -1 {
		return // Not in hierarchy
	}

	// Rule 1: No same-level imports (except within same component)
	if currentLevel == importedLevel {
		if !ac.isSameComponent(filePath, importPath) {
			ac.addViolation(ArchitectureViolation{
				File:      filePath,
				Line:      line,
				Import:    importPath,
				FromLevel: currentLevel,
				ToLevel:   importedLevel,
				Violation: fmt.Sprintf("FORBIDDEN: Same-level import between different components (%s -> %s)", currentLevelName, importedLevelName),
				Severity:  "CRITICAL",
			})
		}
		return
	}

	// Rule 2: No upward imports (higher level importing lower level)
	if importedLevel > currentLevel {
		ac.addViolation(ArchitectureViolation{
			File:      filePath,
			Line:      line,
			Import:    importPath,
			FromLevel: currentLevel,
			ToLevel:   importedLevel,
			Violation: fmt.Sprintf("FORBIDDEN: Upward dependency (%s[L%d] -> %s[L%d])", currentLevelName, currentLevel, importedLevelName, importedLevel),
			Severity:  "CRITICAL",
		})
		return
	}

	// Rule 3: Validate allowed downward imports
	allowedLevels := ac.getAllowedLevels(currentLevel)
	if !contains(allowedLevels, importedLevel) {
		ac.addViolation(ArchitectureViolation{
			File:      filePath,
			Line:      line,
			Import:    importPath,
			FromLevel: currentLevel,
			ToLevel:   importedLevel,
			Violation: fmt.Sprintf("FORBIDDEN: Invalid dependency level (%s[L%d] -> %s[L%d])", currentLevelName, currentLevel, importedLevelName, importedLevel),
			Severity:  "CRITICAL",
		})
	}
}

func (ac *ArchitectureChecker) getImportLevel(importPath string) (int, string) {
	// Remove the base prefix
	localPath := strings.TrimPrefix(importPath, "github.com/yairfalse/tapio/")
	
	for levelName, level := range levelHierarchy {
		if strings.HasPrefix(localPath, levelName+"/") {
			return level, levelName
		}
	}
	return -1, ""
}

func (ac *ArchitectureChecker) isSameComponent(filePath, importPath string) bool {
	// Extract component paths
	fileComponent := ac.getComponentPath(filePath)
	importComponent := ac.getComponentPath(strings.TrimPrefix(importPath, "github.com/yairfalse/tapio/"))
	
	return fileComponent == importComponent
}

func (ac *ArchitectureChecker) getComponentPath(path string) string {
	// Extract component from path like pkg/collectors/ebpf/... -> pkg/collectors/ebpf
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[0] == "pkg" {
		return strings.Join(parts[:3], "/")
	}
	if len(parts) >= 2 && parts[0] == "pkg" {
		return strings.Join(parts[:2], "/")
	}
	return path
}

func (ac *ArchitectureChecker) getAllowedLevels(currentLevel int) []int {
	switch currentLevel {
	case 0: // Domain
		return []int{} // No dependencies
	case 1: // Collectors
		return []int{0} // Domain only
	case 2: // Intelligence
		return []int{0, 1} // Domain + Collectors
	case 3: // Integrations
		return []int{0, 1, 2} // Domain + Collectors + Intelligence
	case 4: // Interfaces
		return []int{0, 1, 2, 3} // All levels
	default:
		return []int{}
	}
}

func (ac *ArchitectureChecker) addViolation(violation ArchitectureViolation) {
	ac.violations = append(ac.violations, violation)
}

func (ac *ArchitectureChecker) reportResults() {
	if len(ac.violations) == 0 {
		fmt.Printf("%s✅ Architecture validation PASSED%s\n", GREEN, NC)
		fmt.Printf("All %d levels respect the dependency hierarchy\n", len(levelHierarchy))
		ac.printHierarchy()
		return
	}

	fmt.Printf("%s❌ Architecture validation FAILED%s\n", RED+BOLD, NC)
	fmt.Printf("Found %d architecture violations:\n\n", len(ac.violations))

	// Group violations by severity
	critical := []ArchitectureViolation{}
	warnings := []ArchitectureViolation{}

	for _, v := range ac.violations {
		if v.Severity == "CRITICAL" {
			critical = append(critical, v)
		} else {
			warnings = append(warnings, v)
		}
	}

	// Report critical violations
	if len(critical) > 0 {
		fmt.Printf("%s🚨 CRITICAL VIOLATIONS (Build-blocking):%s\n", RED+BOLD, NC)
		for i, v := range critical {
			fmt.Printf("%s%d. %s%s\n", RED, i+1, v.Violation, NC)
			fmt.Printf("   File: %s:%d\n", v.File, v.Line)
			fmt.Printf("   Import: %s\n", v.Import)
			fmt.Printf("\n")
		}
	}

	// Report warnings
	if len(warnings) > 0 {
		fmt.Printf("%s⚠️  WARNINGS:%s\n", YELLOW+BOLD, NC)
		for i, v := range warnings {
			fmt.Printf("%s%d. %s%s\n", YELLOW, i+1, v.Violation, NC)
			fmt.Printf("   File: %s:%d\n", v.File, v.Line)
			fmt.Printf("   Import: %s\n", v.Import)
			fmt.Printf("\n")
		}
	}

	fmt.Printf("%sArchitecture Rules:%s\n", BLUE+BOLD, NC)
	ac.printHierarchy()
	
	fmt.Printf("\n%sTo fix these violations:%s\n", YELLOW+BOLD, NC)
	fmt.Printf("1. Remove forbidden imports\n")
	fmt.Printf("2. Communicate via APIs, not Go imports\n")
	fmt.Printf("3. Use message queues or HTTP/gRPC\n")
	fmt.Printf("4. Follow the 5-level hierarchy strictly\n")
}

func (ac *ArchitectureChecker) printHierarchy() {
	fmt.Printf("\n%sAllowed Dependency Hierarchy:%s\n", BLUE, NC)
	
	levels := make([]string, 0, len(levelHierarchy))
	for level := range levelHierarchy {
		levels = append(levels, level)
	}
	
	sort.Slice(levels, func(i, j int) bool {
		return levelHierarchy[levels[i]] < levelHierarchy[levels[j]]
	})
	
	for _, level := range levels {
		levelNum := levelHierarchy[level]
		allowedLevels := ac.getAllowedLevels(levelNum)
		
		fmt.Printf("  L%d: %s", levelNum, level)
		if len(allowedLevels) == 0 {
			fmt.Printf(" → %sZero dependencies%s\n", GREEN, NC)
		} else {
			fmt.Printf(" → Can import: ")
			for i, allowed := range allowedLevels {
				if i > 0 {
					fmt.Printf(", ")
				}
				for _, num := range levelHierarchy {
					if num == allowed {
						fmt.Printf("L%d", num)
						break
					}
				}
			}
			fmt.Printf("\n")
		}
	}
}

func contains(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}