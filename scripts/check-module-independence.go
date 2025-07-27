//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Module independence checker - ensures each module can build standalone
func main() {
	fmt.Println("🔧 Checking module independence...")

	modules := []string{
		"pkg/domain",
		"pkg/collectors/ebpf",
		"pkg/collectors/k8s",
		"pkg/collectors/systemd",
		"pkg/collectors/journald",
		"pkg/intelligence/correlation",
		"pkg/intelligence/patterns",
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

		fmt.Printf("🔍 Testing module: %s\n", module)

		// Change to module directory
		originalDir, _ := os.Getwd()
		if err := os.Chdir(module); err != nil {
			fmt.Printf("❌ Failed to change to directory %s: %v\n", module, err)
			failures++
			continue
		}

		// Try to build the module
		cmd := exec.Command("go", "build", "./...")
		output, err := cmd.CombinedOutput()

		// Change back to original directory
		os.Chdir(originalDir)

		if err != nil {
			fmt.Printf("❌ Module %s failed to build independently:\n%s\n", module, string(output))
			failures++
		} else {
			fmt.Printf("✅ Module %s builds independently\n", module)
		}
	}

	if failures > 0 {
		fmt.Printf("❌ %d modules failed independence test\n", failures)
		os.Exit(1)
	}

	fmt.Println("✅ All modules are independent!")
}
