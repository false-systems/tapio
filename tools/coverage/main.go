// Test Coverage Enforcement for Tapio
import "github.com/yairfalse/tapio/tools/lib"
// Enforces Claude.md Rule T1: Minimum 80% test coverage for all public functions
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)


type ModuleCoverage struct {
	Path          string
	Coverage      float64
	TotalLines    int
	CoveredLines  int
	HasTests      bool
	TestFiles     []string
	PublicFuncs   []string
	UntestedFuncs []string
	Errors        []string
}

type CoverageChecker struct {
	modules         []ModuleCoverage
	totalFailed     int
	publicFuncRegex *regexp.Regexp
}

func main() {
	fmt.Printf("%s📊 Tapio Test Coverage Enforcement%s\n", lib.BOLD+lib.BLUE, lib.NC)
	fmt.Printf("Enforcing minimum 80%% test coverage for all public functions\n\n")

	checker := &CoverageChecker{
		modules:         []ModuleCoverage{},
		totalFailed:     0,
		publicFuncRegex: regexp.MustCompile(`^func\s+([A-Z][a-zA-Z0-9_]*)`), // Public functions start with capital
	}

	// Find all modules
	err := checker.findModules()
	if err != nil {
		fmt.Printf("%sError finding modules: %v%s\n", lib.RED, err, lib.NC)
		os.Exit(1)
	}

	if len(checker.modules) == 0 {
		fmt.Printf("%sNo modules found%s\n", lib.YELLOW, lib.NC)
		os.Exit(0)
	}

	fmt.Printf("Checking coverage for %d modules...\n\n", len(checker.modules))

	// Check coverage for each module
	for i := range checker.modules {
		checker.checkModuleCoverage(&checker.modules[i])
	}

	// Report results
	checker.reportResults()

	// Exit with error if coverage requirements not met
	if checker.totalFailed > 0 {
		os.Exit(1)
	}
}

func (cc *CoverageChecker) findModules() error {
	return filepath.Walk("pkg", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for go.mod files
		if info.Name() == "go.mod" {
			modulePath := filepath.Dir(path)
			module := ModuleCoverage{
				Path:          modulePath,
				HasTests:      false,
				TestFiles:     []string{},
				PublicFuncs:   []string{},
				UntestedFuncs: []string{},
				Errors:        []string{},
			}

			// Find test files
			cc.findTestFiles(&module)

			// Find public functions
			cc.findPublicFunctions(&module)

			cc.modules = append(cc.modules, module)
		}

		return nil
	})
}

func (cc *CoverageChecker) findTestFiles(module *ModuleCoverage) {
	filepath.Walk(module.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			relPath, _ := filepath.Rel(module.Path, path)
			module.TestFiles = append(module.TestFiles, relPath)
			module.HasTests = true
		}

		return nil
	})
}

func (cc *CoverageChecker) findPublicFunctions(module *ModuleCoverage) {
	filepath.Walk(module.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip test files
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Find public function declarations
			matches := cc.publicFuncRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				funcName := matches[1]
				relPath, _ := filepath.Rel(module.Path, path)
				funcEntry := fmt.Sprintf("%s:%s", relPath, funcName)
				module.PublicFuncs = append(module.PublicFuncs, funcEntry)
			}
		}

		return nil
	})
}

func (cc *CoverageChecker) checkModuleCoverage(module *ModuleCoverage) {
	fmt.Printf("%sTesting coverage: %s%s\n", lib.BLUE, module.Path, lib.NC)

	if !module.HasTests {
		fmt.Printf("  %s❌ NO TESTS FOUND%s\n", lib.RED, lib.NC)
		module.Errors = append(module.Errors, "No test files found - tests are required")
		cc.totalFailed++
		return
	}

	// Run tests with coverage
	cmd := exec.Command("go", "test", "-coverprofile=coverage.out", "-covermode=atomic", "./...")
	cmd.Dir = module.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  %s❌ TESTS FAILED%s\n", lib.RED, lib.NC)
		module.Errors = append(module.Errors, fmt.Sprintf("Tests failed: %s", string(output)))
		cc.totalFailed++
		return
	}

	// Parse coverage
	coverageFile := filepath.Join(module.Path, "coverage.out")
	if _, err := os.Stat(coverageFile); err != nil {
		fmt.Printf("  %s❌ NO COVERAGE DATA%s\n", lib.RED, lib.NC)
		module.Errors = append(module.Errors, "No coverage data generated")
		cc.totalFailed++
		return
	}

	// Get coverage percentage
	cmd = exec.Command("go", "tool", "cover", "-func=coverage.out")
	cmd.Dir = module.Path

	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  %s❌ COVERAGE PARSE FAILED%s\n", lib.RED, lib.NC)
		module.Errors = append(module.Errors, fmt.Sprintf("Failed to parse coverage: %v", err))
		cc.totalFailed++
		return
	}

	// Parse total coverage from output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "total:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				coverageStr := strings.TrimSuffix(fields[2], "%")
				coverage, err := strconv.ParseFloat(coverageStr, 64)
				if err == nil {
					module.Coverage = coverage
					break
				}
			}
		}
	}

	// Parse individual function coverage to find untested functions
	cc.parseUncoveredFunctions(module, string(output))

	// Check if coverage meets threshold
	if module.Coverage < COVERAGE_THRESHOLD {
		fmt.Printf("  %s❌ COVERAGE TOO LOW%s (%.1f%% < %.1f%%)\n", lib.RED, lib.NC, module.Coverage, COVERAGE_THRESHOLD)
		module.Errors = append(module.Errors, fmt.Sprintf("Coverage %.1f%% below required %.1f%%", module.Coverage, COVERAGE_THRESHOLD))
		cc.totalFailed++
	} else {
		fmt.Printf("  %s✅ COVERAGE OK%s (%.1f%% >= %.1f%%)\n", lib.GREEN, lib.NC, module.Coverage, COVERAGE_THRESHOLD)
	}

	// Clean up coverage file
	os.Remove(coverageFile)
}

func (cc *CoverageChecker) parseUncoveredFunctions(module *ModuleCoverage, coverageOutput string) {
	lines := strings.Split(coverageOutput, "\n")

	for _, line := range lines {
		if strings.Contains(line, "0.0%") {
			// Function with 0% coverage
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				funcPath := fields[0]
				module.UntestedFuncs = append(module.UntestedFuncs, funcPath)
			}
		}
	}
}

func (cc *CoverageChecker) reportResults() {
	fmt.Printf("%s"+strings.Repeat("=", 70)+"%s\n", lib.BLUE, lib.NC)
	fmt.Printf("%sTest Coverage Report%s\n", lib.BOLD+lib.BLUE, lib.NC)
	fmt.Printf("%s"+strings.Repeat("=", 70)+"%s\n\n", lib.BLUE, lib.NC)

	passed := len(cc.modules) - cc.totalFailed

	if cc.totalFailed == 0 {
		fmt.Printf("%s✅ ALL MODULES MEET COVERAGE REQUIREMENTS%s\n", lib.GREEN+lib.BOLD, lib.NC)
		fmt.Printf("All %d modules have >= %.1f%% test coverage\n\n", len(cc.modules), COVERAGE_THRESHOLD)
	} else {
		fmt.Printf("%s❌ COVERAGE REQUIREMENTS NOT MET%s\n", lib.RED+lib.BOLD, lib.NC)
		fmt.Printf("%d/%d modules failed coverage requirements\n\n", cc.totalFailed, len(cc.modules))
	}

	// Summary table
	fmt.Printf("%-35s %-12s %-12s %-15s %-10s\n", "Module", "Coverage", "Status", "Test Files", "Public Funcs")
	fmt.Printf("%s\n", strings.Repeat("-", 85))

	totalCoverage := 0.0
	validModules := 0

	for _, module := range cc.modules {
		status := "✅ PASS"
		if module.Coverage < COVERAGE_THRESHOLD || !module.HasTests {
			status = "❌ FAIL"
		}

		if module.HasTests && module.Coverage > 0 {
			totalCoverage += module.Coverage
			validModules++
		}

		fmt.Printf("%-35s %-12s %-12s %-15s %-10s\n",
			module.Path,
			fmt.Sprintf("%.1f%%", module.Coverage),
			status,
			fmt.Sprintf("%d files", len(module.TestFiles)),
			fmt.Sprintf("%d funcs", len(module.PublicFuncs)),
		)
	}

	// Overall statistics
	if validModules > 0 {
		avgCoverage := totalCoverage / float64(validModules)
		fmt.Printf("\n%sOverall Statistics:%s\n", lib.BLUE+lib.BOLD, lib.NC)
		fmt.Printf("Average coverage: %.1f%%\n", avgCoverage)
		fmt.Printf("Modules with tests: %d/%d\n", validModules, len(cc.modules))
		fmt.Printf("Required threshold: %.1f%%\n", COVERAGE_THRESHOLD)
	}

	// Error details
	if cc.totalFailed > 0 {
		fmt.Printf("\n%sDetailed Issues:%s\n", lib.RED+lib.BOLD, lib.NC)

		for _, module := range cc.modules {
			if len(module.Errors) > 0 || len(module.UntestedFuncs) > 0 {
				fmt.Printf("\n%s%s:%s\n", lib.YELLOW, module.Path, lib.NC)

				for _, err := range module.Errors {
					fmt.Printf("  ❌ %s\n", err)
				}

				if len(module.UntestedFuncs) > 0 {
					fmt.Printf("  📋 Untested functions (%d):\n", len(module.UntestedFuncs))
					for i, fn := range module.UntestedFuncs {
						if i < 10 { // Limit output
							fmt.Printf("    - %s\n", fn)
						} else if i == 10 {
							fmt.Printf("    - ... and %d more\n", len(module.UntestedFuncs)-10)
							break
						}
					}
				}
			}
		}
	}

	// Remediation guidance
	if cc.totalFailed > 0 {
		fmt.Printf("\n%sTo fix coverage violations:%s\n", lib.BLUE+lib.BOLD, lib.NC)
		fmt.Printf("1. Add test files for modules without tests\n")
		fmt.Printf("2. Write unit tests for all public functions\n")
		fmt.Printf("3. Add integration tests for external dependencies\n")
		fmt.Printf("4. Use table-driven tests for multiple scenarios\n")
		fmt.Printf("5. Add benchmark tests for performance-critical code\n")
		fmt.Printf("6. Ensure test coverage >= %.1f%% for all modules\n", COVERAGE_THRESHOLD)

		fmt.Printf("\n%sExample test structure:%s\n", lib.GREEN, lib.NC)
		fmt.Printf(`func TestCollector_CollectEvents(t *testing.T) {
    tests := []struct {
        name     string
        criteria Criteria
        want     []Event
        wantErr  bool
    }{
        {
            name:     "valid criteria",
            criteria: validCriteria(),
            want:     expectedEvents(),
            wantErr:  false,
        },
        // More test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}`)
	}
}
