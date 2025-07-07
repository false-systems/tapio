package root

import (
	"fmt"

	"github.com/falseyair/tapio/cmd/tapio/check"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tapio",
	Short: "Beautiful Kubernetes health monitoring",
	Long: `Tapio - Your friendly Kubernetes health assistant
	
A beautiful, human-friendly tool for monitoring and understanding 
your Kubernetes cluster health. Tapio makes complex cluster analysis 
simple and delightful.`,
	Version: "0.1.0",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(check.Cmd)
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetVersionTemplate(fmt.Sprintf("Tapio version %s\n", rootCmd.Version))
}