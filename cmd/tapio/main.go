package main

import (
	"os"

	"github.com/yairfalse/tapio/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
