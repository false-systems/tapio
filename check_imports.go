//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"go/build"
	"log"
	"strings"
)

func main() {
	packages := []string{
		"github.com/yairfalse/tapio/pkg/collector",
		"github.com/yairfalse/tapio/pkg/ebpf",
		"github.com/yairfalse/tapio/pkg/collectors",
		"github.com/yairfalse/tapio/pkg/collectors/ebpf",
	}

	fmt.Println("Checking imports for packages...")
	for _, pkg := range packages {
		fmt.Printf("\n%s:\n", pkg)
		p, err := build.Import(pkg, "", 0)
		if err != nil {
			log.Printf("Error importing %s: %v", pkg, err)
			continue
		}

		fmt.Println("  Imports:")
		for _, imp := range p.Imports {
			if strings.Contains(imp, "tapio") {
				fmt.Printf("    - %s\n", imp)
			}
		}
	}
}
