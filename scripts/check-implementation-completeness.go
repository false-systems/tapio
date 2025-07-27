//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Check for incomplete implementations (TODOs, stubs, panics)
func main() {
	fmt.Println("🚨 Checking implementation completeness...")

	// Patterns to detect incomplete implementations
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)todo`),
		regexp.MustCompile(`(?i)fixme`),
		regexp.MustCompile(`(?i)hack`),
		regexp.MustCompile(`panic\s*\(`),
		regexp.MustCompile(`(?i)not\s+implemented`),
		regexp.MustCompile(`(?i)stub`),
		regexp.MustCompile(`return\s+nil,\s+nil`), // Common stub pattern
		regexp.MustCompile(`return\s+errors\.New\("not implemented"\)`),
	}

	violations := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor, .git, etc.
		if strings.Contains(path, "vendor/") ||
			strings.Contains(path, ".git/") ||
			strings.Contains(path, "node_modules/") {
			return nil
		}

		// Only check Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())

			// Skip comments (but check TODO in comments)
			if strings.HasPrefix(line, "//") {
				for _, pattern := range patterns {
					if pattern.MatchString(line) && strings.Contains(strings.ToLower(line), "todo") {
						fmt.Printf("❌ %s:%d - TODO found: %s\n", path, lineNum, line)
						violations++
					}
				}
				continue
			}

			// Check for incomplete patterns in code
			for _, pattern := range patterns {
				if pattern.MatchString(line) {
					fmt.Printf("❌ %s:%d - Incomplete implementation: %s\n", path, lineNum, line)
					violations++
				}
			}
		}

		return scanner.Err()
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if violations > 0 {
		fmt.Printf("❌ Found %d incomplete implementations\n", violations)
		fmt.Println("All TODOs, stubs, and incomplete functions must be implemented")
		os.Exit(1)
	}

	fmt.Println("✅ Implementation completeness check passed!")
}
