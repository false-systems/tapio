package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "tapio",
	Short: "The Forest Guardian for Kubernetes",
	Long: `🌲 Tapio - Your Kubernetes Health Assistant

Tapio makes Kubernetes monitoring and debugging accessible to everyone.
Named after the Finnish god of forests, Tapio protects your digital forest
by making complex cluster debugging simple and human-readable.

Features:
  • Intelligent health checking with problem prediction
  • Root cause analysis with plain English explanations
  • Real-time metrics and alerting
  • eBPF-powered deep insights (optional)
  • Beautiful, human-friendly output`,

	Example: `  # Check your cluster health
  tapio check

  # Understand why a pod is failing
  tapio why my-broken-pod

  # Start Prometheus metrics export
  tapio prometheus

  # Switch to a different namespace
  tapio use production`,

	SilenceUsage:  true,
	SilenceErrors: true,

	// Add version info in help
	Version: getVersion(),

	// Handle unknown subcommands gracefully
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}

		unknownCmd := args[0]
		validCommands := []string{"check", "why", "prometheus", "context", "use", "version", "diagnose"}
		suggestion := DidYouMeanSuggestion(unknownCmd, validCommands)

		err := NewCLIError(
			"command",
			fmt.Sprintf("Unknown command '%s'", unknownCmd),
			"Run 'tapio --help' to see available commands",
		)

		if suggestion != "" {
			err.Suggestion = suggestion
		}

		return err.WithExamples(
			"tapio check",
			"tapio why my-pod",
			"tapio --help",
		)
	},
}

// Execute runs the root command with improved error handling
func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		HandleCLIError(rootCmd, err)
	}
	return err
}

func init() {
	// Global flags with better descriptions
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"Enable verbose output with detailed information and timing")

	// Set custom help template
	rootCmd.SetHelpTemplate(getCustomHelpTemplate())

	// Set custom usage template
	rootCmd.SetUsageTemplate(getCustomUsageTemplate())

	// Add subcommands
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(whyCmd)
	rootCmd.AddCommand(prometheusCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(useCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(sniffCmd)
	rootCmd.AddCommand(diagnoseCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(configCmd)

	// Set custom error handling
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return NewCLIError(
			"flag parsing",
			err.Error(),
			"Check the command usage with --help",
		).WithHelp()
	})
}

// getVersion returns the current version
func getVersion() string {
	// This will be set by build process
	version := os.Getenv("TAPIO_VERSION")
	if version == "" {
		version = "dev"
	}
	return version
}

// getCustomHelpTemplate returns a custom help template
func getCustomHelpTemplate() string {
	return `{{with .Name}}{{printf "%s" .}}{{end}}{{if .Version}} {{.Version}}{{end}}

{{.Short}}

{{if .Long}}{{.Long}}

{{end}}{{if .Example}}Examples:
{{.Example}}

{{end}}{{if .Runnable}}Usage:
  {{.UseLine}}

{{end}}{{if .HasAvailableSubCommands}}Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{end}}{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}{{if .HasAvailableInheritedFlags}}Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}{{if .HasHelpSubCommands}}Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{end}}{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.

{{end}}💡 Need help? Visit: https://github.com/yairfalse/tapio
`
}

// getCustomUsageTemplate returns a custom usage template
func getCustomUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}

{{if gt (len .Aliases) 0}}Aliases:
  {{.NameAndAliases}}

{{end}}{{if .HasExample}}Examples:
{{.Example}}

{{end}}{{if .HasAvailableSubCommands}}Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{end}}{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}{{if .HasAvailableInheritedFlags}}Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}{{if .HasHelpSubCommands}}Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{end}}{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`
}
