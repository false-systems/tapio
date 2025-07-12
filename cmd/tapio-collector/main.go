package main

import (
	"fmt"
	"os"

	"github.com/yairfalse/tapio/pkg/unified"
)

func main() {
	cli := unified.NewCLI()
	
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}