//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const MIN_COVERAGE = 80.0

// Check test coverage meets minimum requirements
func main() {
	fmt.Println("📊 Checking test coverage requirements...")

	modules := []string{
		"pkg/domain",
		"pkg/collectors/ebpf",
		"pkg/collectors/k8s",
		"pkg/collectors/systemd",
		"pkg/collectors/journald",
		"pkg/intelligence/correlation",
		"pkg/integrations/otel",
		"pkg/integrations/prometheus",
		"pkg/interfaces/server",
		"pkg/interfaces/cli",
	}

	failures := 0

	for _, module := range modules {
		if _, err := os.Stat(module); os.IsNotExist(err) {
			fmt.Printf("⚠️  Module %s does not exist, skipping\n", module)
			continue
		}

		coverage := getCoverage(module)
		if coverage < MIN_COVERAGE {
			fmt.Printf("❌ %s: %.1f%% coverage (minimum: %.1f%%)\n", module, coverage, MIN_COVERAGE)
			failures++
		} else {
			fmt.Printf("✅ %s: %.1f%% coverage\n", module, coverage)
		}
	}

	if failures > 0 {
		fmt.Printf("❌ %d modules below minimum coverage threshold\n", failures)
		os.Exit(1)
	}

	fmt.Println("✅ All modules meet coverage requirements!")
}

func getCoverage(module string) float64 {
	originalDir, _ := os.Getwd()

	// Change to module directory
	if err := os.Chdir(module); err != nil {
		fmt.Printf("Warning: Failed to change to directory %s: %v\n", module, err)
		os.Chdir(originalDir)
		return 0.0
	}

	// Run tests with coverage
	cmd := exec.Command("go", "test", "-coverprofile=coverage.out", "./...")
	cmd.Run()

	// Get coverage percentage
	cmd = exec.Command("go", "tool", "cover", "-func=coverage.out")
	output, err := cmd.Output()

	// Clean up
	os.Remove("coverage.out")
	os.Chdir(originalDir)

	if err != nil {
		return 0.0
	}

	// Parse coverage from output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "total:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				coverageStr := strings.TrimSuffix(parts[2], "%")
				if coverage, err := strconv.ParseFloat(coverageStr, 64); err == nil {
					return coverage
				}
			}
		}
	}

	return 0.0
}
