package cli

import (
	"github.com/spf13/cobra"
)

var (
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "tapio",
	Short: "The Forest Guardian for Kubernetes",
	Long: `Tapio makes Kubernetes and eBPF accessible to everyone.

Named after the Finnish god of forests, Tapio protects your digital forest
by making complex cluster debugging simple and human-readable.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Add subcommands
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(whyCmd)
	rootCmd.AddCommand(prometheusCmd)
	rootCmd.AddCommand(versionCmd)
}
