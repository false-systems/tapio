package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show Tapio version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("🌲 Tapio v%s\n", getVersion())
		fmt.Printf("Git Commit: %s\n", getGitCommit())
		fmt.Printf("Build Date: %s\n", getBuildDate())
	},
}

// These will be set by build scripts
var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
)

func getVersion() string   { return version }
func getGitCommit() string { return gitCommit }
func getBuildDate() string { return buildDate }