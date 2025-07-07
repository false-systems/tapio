package main

import (
	"os"

	"github.com/falseyair/tapio/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
