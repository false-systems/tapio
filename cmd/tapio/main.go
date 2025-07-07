package main

import (
	"os"

	"github.com/falseyair/tapio/cmd/tapio/root"
)

func main() {
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}