package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	serverURL string
	rootCmd   = &cobra.Command{
		Use:   "tapio",
		Short: "Tapio CLI - Minimal observability platform",
		Long:  `Tapio is a minimal observability platform with REST API support`,
	}
)

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "http://localhost:8080", "Tapio server URL")

	// Bind flags to viper
	viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server"))
}

func initConfig() {
	// Set defaults
	viper.SetDefault("server", "http://localhost:8080")

	// Read from environment
	viper.SetEnvPrefix("TAPIO")
	viper.AutomaticEnv()
}
