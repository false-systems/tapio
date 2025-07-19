// Module Independence Validation for Tapio
// Ensures each area builds and runs independently according to Claude.md Rule A5
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	RED    = "\033[0;31m"
	GREEN  = "\033[0;32m"
	YELLOW = "\033[0;33m"
	BLUE   = "\033[0;34m"
	NC     = "\033[0m"
	BOLD   = "\033[1m"
)

type ModuleTest struct {
	Path        string
	Name        string
	HasGoMod    bool
	CanBuild    bool
	CanTest     bool
	HasCmd      bool
	BuildTime   time.Duration
	TestTime    time.Duration
	Errors      []string
}

type IndependenceChecker struct {
	modules []ModuleTest
	failed  int
}

func main() {
	fmt.Printf("%s🏗️  Tapio Module Independence Validation%s\n", BOLD+BLUE, NC)
	fmt.Printf("Ensuring each area builds and runs independently...\n\n")

	checker := &IndependenceChecker{
		modules: []ModuleTest{},
		failed:  0,
	}

	// Find all modules in the pkg/ hierarchy
	err := checker.findModules()
	if err != nil {
		fmt.Printf("%sError finding modules: %v%s\n", RED, err, NC)
		os.Exit(1)
	}

	if len(checker.modules) == 0 {
		fmt.Printf("%sNo modules found in pkg/ hierarchy%s\n", YELLOW, NC)
		os.Exit(0)
	}

	// Test each module independently
	fmt.Printf("Testing %d modules for independence...\n\n", len(checker.modules))
	
	for i := range checker.modules {
		checker.testModule(&checker.modules[i])
	}

	// Report results
	checker.reportResults()

	// Exit with error if any modules failed
	if checker.failed > 0 {
		os.Exit(1)
	}
}

func (ic *IndependenceChecker) findModules() error {
	return filepath.Walk("pkg", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for go.mod files
		if info.Name() == "go.mod" {
			modulePath := filepath.Dir(path)
			module := ModuleTest{
				Path:     modulePath,
				Name:     strings.Replace(modulePath, "/", "-", -1),
				HasGoMod: true,
				Errors:   []string{},
			}
			
			// Check if module has cmd/ directory
			cmdDir := filepath.Join(modulePath, "cmd")
			if _, err := os.Stat(cmdDir); err == nil {
				module.HasCmd = true
			}

			ic.modules = append(ic.modules, module)
		}

		return nil
	})
}

func (ic *IndependenceChecker) testModule(module *ModuleTest) {
	fmt.Printf("%sTesting module: %s%s\n", BLUE, module.Path, NC)

	// Test 1: Independent Build
	module.CanBuild = ic.testBuild(module)
	
	// Test 2: Independent Test
	module.CanTest = ic.testTest(module)

	// Test 3: Standalone Executables (if has cmd/)
	if module.HasCmd {
		ic.testStandaloneExecutables(module)
	}

	// Determine overall result
	if !module.CanBuild || !module.CanTest {
		ic.failed++
	}

	fmt.Printf("\n")
}

func (ic *IndependenceChecker) testBuild(module *ModuleTest) bool {
	fmt.Printf("  📦 Testing independent build...")

	start := time.Now()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = module.Path
	
	// Capture output
	output, err := cmd.CombinedOutput()
	module.BuildTime = time.Since(start)

	if err != nil {
		fmt.Printf(" %s❌ FAILED%s\n", RED, NC)
		module.Errors = append(module.Errors, fmt.Sprintf("Build failed: %s", string(output)))
		return false
	}

	fmt.Printf(" %s✅ PASSED%s (%.2fs)\n", GREEN, NC, module.BuildTime.Seconds())
	return true
}

func (ic *IndependenceChecker) testTest(module *ModuleTest) bool {
	fmt.Printf("  🧪 Testing independent tests...")

	start := time.Now()
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = module.Path
	
	// Capture output
	output, err := cmd.CombinedOutput()
	module.TestTime = time.Since(start)

	if err != nil {
		// Check if it's because no tests exist
		outputStr := string(output)
		if strings.Contains(outputStr, "no test files") || strings.Contains(outputStr, "[no test files]") {
			fmt.Printf(" %s⚠️  NO TESTS%s (%.2fs)\n", YELLOW, NC, module.TestTime.Seconds())
			module.Errors = append(module.Errors, "No test files found - tests should be added")
			return true // Not a failure, but needs attention
		}

		fmt.Printf(" %s❌ FAILED%s\n", RED, NC)
		module.Errors = append(module.Errors, fmt.Sprintf("Tests failed: %s", outputStr))
		return false
	}

	fmt.Printf(" %s✅ PASSED%s (%.2fs)\n", GREEN, NC, module.TestTime.Seconds())
	return true
}

func (ic *IndependenceChecker) testStandaloneExecutables(module *ModuleTest) {
	fmt.Printf("  🚀 Testing standalone executables...")

	cmdDir := filepath.Join(module.Path, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		fmt.Printf(" %s❌ FAILED%s (cannot read cmd dir)\n", RED, NC)
		module.Errors = append(module.Errors, fmt.Sprintf("Cannot read cmd directory: %v", err))
		return
	}

	executableCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			execPath := filepath.Join(cmdDir, entry.Name())
			
			// Try to build the executable
			cmd := exec.Command("go", "build", "-o", "/dev/null", ".")
			cmd.Dir = execPath
			
			if err := cmd.Run(); err != nil {
				module.Errors = append(module.Errors, fmt.Sprintf("Executable %s failed to build: %v", entry.Name(), err))
			} else {
				executableCount++
			}
		}
	}

	if executableCount > 0 {
		fmt.Printf(" %s✅ PASSED%s (%d executables)\n", GREEN, NC, executableCount)
	} else {
		fmt.Printf(" %s⚠️  NO EXECUTABLES%s\n", YELLOW, NC)
		module.Errors = append(module.Errors, "No buildable executables found in cmd/")
	}
}

func (ic *IndependenceChecker) reportResults() {
	fmt.Printf("%s" + strings.Repeat("=", 60) + "%s\n", BLUE, NC)
	fmt.Printf("%sModule Independence Report%s\n", BOLD+BLUE, NC)
	fmt.Printf("%s" + strings.Repeat("=", 60) + "%s\n\n", BLUE, NC)

	if ic.failed == 0 {
		fmt.Printf("%s✅ ALL MODULES INDEPENDENT%s\n", GREEN+BOLD, NC)
		fmt.Printf("All %d modules can build and run independently\n\n", len(ic.modules))
	} else {
		fmt.Printf("%s❌ INDEPENDENCE VIOLATIONS FOUND%s\n", RED+BOLD, NC)
		fmt.Printf("%d/%d modules failed independence requirements\n\n", ic.failed, len(ic.modules))
	}

	// Detailed results table
	fmt.Printf("%-30s %-8s %-8s %-8s %-12s %-12s\n", "Module", "Build", "Test", "Cmd", "Build Time", "Test Time")
	fmt.Printf("%s\n", strings.Repeat("-", 80))

	for _, module := range ic.modules {
		buildStatus := "✅"
		if !module.CanBuild {
			buildStatus = "❌"
		}

		testStatus := "✅"
		if !module.CanTest {
			testStatus = "❌"
		}

		cmdStatus := "N/A"
		if module.HasCmd {
			cmdStatus = "✅"
		}

		fmt.Printf("%-30s %-8s %-8s %-8s %-12s %-12s\n",
			module.Path,
			buildStatus,
			testStatus,
			cmdStatus,
			fmt.Sprintf("%.2fs", module.BuildTime.Seconds()),
			fmt.Sprintf("%.2fs", module.TestTime.Seconds()),
		)
	}

	// Error details
	if ic.failed > 0 {
		fmt.Printf("\n%sError Details:%s\n", RED+BOLD, NC)
		for _, module := range ic.modules {
			if len(module.Errors) > 0 {
				fmt.Printf("\n%s%s:%s\n", YELLOW, module.Path, NC)
				for i, err := range module.Errors {
					fmt.Printf("  %d. %s\n", i+1, err)
				}
			}
		}
	}

	// Remediation guidance
	if ic.failed > 0 {
		fmt.Printf("\n%sTo fix independence violations:%s\n", BLUE+BOLD, NC)
		fmt.Printf("1. Ensure each module has its own go.mod\n")
		fmt.Printf("2. Remove dependencies on other same-level modules\n")
		fmt.Printf("3. Add standalone executables in cmd/ directories\n")
		fmt.Printf("4. Implement graceful degradation when dependencies unavailable\n")
		fmt.Printf("5. Add comprehensive tests for each module\n")
	}

	// Performance summary
	totalBuildTime := 0.0
	totalTestTime := 0.0
	for _, module := range ic.modules {
		totalBuildTime += module.BuildTime.Seconds()
		totalTestTime += module.TestTime.Seconds()
	}

	fmt.Printf("\n%sPerformance Summary:%s\n", BLUE, NC)
	fmt.Printf("Total build time: %.2fs (avg: %.2fs per module)\n", totalBuildTime, totalBuildTime/float64(len(ic.modules)))
	fmt.Printf("Total test time: %.2fs (avg: %.2fs per module)\n", totalTestTime, totalTestTime/float64(len(ic.modules)))
}