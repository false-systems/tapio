package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/config"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Tapio configuration",
	Long: `Configuration Management - Zero-config experience with bulletproof setup

The config command helps you initialize, validate, and manage Tapio configuration.
Tapio works out-of-the-box with zero configuration, but you can customize
behavior through configuration files and environment variables.

Configuration sources (in priority order):
  1. Command line flags
  2. Environment variables (TAPIO_*)
  3. Configuration file
  4. Sensible defaults`,

	Example: `  # Initialize configuration file
  tapio config init

  # Show current effective configuration
  tapio config show

  # Validate configuration
  tapio config validate

  # Set a configuration value
  tapio config set log_level debug

  # Get a specific value
  tapio config get features.enable_ebpf`,
}

var configInitCmd = &cobra.Command{
	Use:   "init [template]",
	Short: "Initialize configuration file",
	Long: `Initialize a new configuration file with sensible defaults.

Templates available:
  • minimal    - Minimal configuration (zero-config equivalent)
  • default    - Standard configuration with common options
  • production - Production-ready configuration with all features
  • development - Development configuration with debugging enabled`,

	Example: `  # Create default configuration
  tapio config init

  # Create minimal configuration
  tapio config init minimal

  # Create production configuration
  tapio config init production

  # Specify custom location
  tapio config init --file ~/.tapio/custom.yaml`,

	Args: cobra.MaximumNArgs(1),
	RunE: runConfigInit,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	Long: `Display the current effective configuration, showing values from all sources.

This shows the final configuration that Tapio will use, merged from:
  • Default values
  • Configuration file
  • Environment variables
  • Command line flags`,

	Example: `  # Show full configuration
  tapio config show

  # Show as JSON
  tapio config show --format json

  # Show configuration sources
  tapio config show --sources`,

	RunE: runConfigShow,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate configuration",
	Long: `Validate configuration file syntax and values.

Checks for:
  • Valid YAML syntax
  • Required fields
  • Value ranges and formats
  • Deprecated options
  • Common misconfigurations`,

	Example: `  # Validate current configuration
  tapio config validate

  # Validate specific file
  tapio config validate ~/.tapio/config.yaml`,

	Args: cobra.MaximumNArgs(1),
	RunE: runConfigValidate,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set configuration value",
	Long: `Set a configuration value in the configuration file.

The key uses dot notation to specify nested values.
If no configuration file exists, one will be created.`,

	Example: `  # Set log level
  tapio config set log_level debug

  # Enable eBPF
  tapio config set features.enable_ebpf true

  # Set Kubernetes context
  tapio config set kubernetes.context production`,

	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get configuration value",
	Long: `Get a configuration value from the effective configuration.

The key uses dot notation to specify nested values.
Shows the final value after merging all configuration sources.`,

	Example: `  # Get log level
  tapio config get log_level

  # Get eBPF status
  tapio config get features.enable_ebpf

  # Get entire features section
  tapio config get features`,

	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

// Config command flags
var (
	configFile    string
	configFormat  string
	configSources bool
	configGlobal  bool
)

func init() {
	// Add subcommands
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)

	// Init command flags
	configInitCmd.Flags().StringVar(&configFile, "file", "", "Configuration file path")
	configInitCmd.Flags().BoolVar(&configGlobal, "global", false, "Create global configuration")

	// Show command flags
	configShowCmd.Flags().StringVar(&configFormat, "format", "yaml", "Output format (yaml, json)")
	configShowCmd.Flags().BoolVar(&configSources, "sources", false, "Show configuration sources")

	// Set command flags
	configSetCmd.Flags().StringVar(&configFile, "file", "", "Configuration file path")
	configSetCmd.Flags().BoolVar(&configGlobal, "global", false, "Set in global configuration")
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	template := "default"
	if len(args) > 0 {
		template = args[0]
	}

	// Validate template
	validTemplates := []string{"minimal", "default", "production", "development"}
	isValid := false
	for _, valid := range validTemplates {
		if template == valid {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid template '%s'. Valid templates: %s", template, strings.Join(validTemplates, ", "))
	}

	// Determine config file path
	loader := config.NewLoader()
	var configPath string

	if configFile != "" {
		configPath = configFile
	} else if configGlobal {
		configPath = loader.GetDefaultConfigPath()
	} else {
		// Check if we're in a project directory
		if _, err := os.Stat("./tapio.yaml"); err == nil {
			return fmt.Errorf("configuration file already exists in current directory")
		}
		if _, err := os.Stat("./tapio.yml"); err == nil {
			return fmt.Errorf("configuration file already exists in current directory")
		}
		configPath = "./tapio.yaml"
	}

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("configuration file already exists: %s\nUse --force to overwrite or choose a different path", configPath)
	}

	// Initialize configuration
	if err := loader.InitConfig(configPath, template); err != nil {
		return fmt.Errorf("failed to initialize configuration: %v", err)
	}

	fmt.Printf("✅ Configuration initialized: %s\n", configPath)
	fmt.Printf("📋 Template: %s\n", template)
	fmt.Printf("🔧 Edit the file to customize your setup\n")
	fmt.Printf("🩺 Run 'tapio doctor' to validate your setup\n")

	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	loader := config.NewLoader()
	if configFile != "" {
		loader = loader.WithConfigFile(configFile)
	}

	// Get effective configuration and sources
	cfg, sources, err := loader.GetEffectiveConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}

	// Show sources if requested
	if configSources {
		fmt.Println("Configuration sources:")
		for i, source := range sources {
			fmt.Printf("  %d. %s\n", i+1, source)
		}
		fmt.Println()
	}

	// Format and display configuration
	switch configFormat {
	case "yaml", "yml":
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal configuration: %v", err)
		}
		fmt.Print(string(data))

	case "json":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal configuration: %v", err)
		}
		fmt.Println(string(data))

	default:
		return fmt.Errorf("unsupported format: %s (use yaml or json)", configFormat)
	}

	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	var filePath string

	if len(args) > 0 {
		filePath = args[0]
	} else if configFile != "" {
		filePath = configFile
	} else {
		// Find configuration file automatically
		loader := config.NewLoader()
		cfg, sources, err := loader.GetEffectiveConfig()
		if err != nil {
			return fmt.Errorf("no configuration file to validate: %v", err)
		}

		// Extract file path from sources
		for _, source := range sources {
			if strings.HasPrefix(source, "file: ") {
				filePath = strings.TrimPrefix(source, "file: ")
				break
			}
		}

		if filePath == "" {
			fmt.Println("✅ No configuration file found - using defaults only")
			fmt.Println("💡 Run 'tapio config init' to create a configuration file")
			return nil
		}

		// Also validate the loaded configuration
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("configuration validation failed: %v", err)
		}
	}

	// Validate the specific file
	loader := config.NewLoader()
	if err := loader.ValidateConfigFile(filePath); err != nil {
		if configErr, ok := err.(config.ConfigError); ok {
			fmt.Printf("❌ Configuration validation failed: %s\n", configErr.Message)
			fmt.Printf("💡 %s\n", configErr.Suggestion)
			return nil
		}
		return err
	}

	fmt.Printf("✅ Configuration is valid: %s\n", filePath)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	// Determine config file path
	loader := config.NewLoader()
	var configPath string

	if configFile != "" {
		configPath = configFile
	} else if configGlobal {
		configPath = loader.GetDefaultConfigPath()
	} else {
		// Try to find existing config file
		if _, err := os.Stat("./tapio.yaml"); err == nil {
			configPath = "./tapio.yaml"
		} else if _, err := os.Stat("./tapio.yml"); err == nil {
			configPath = "./tapio.yml"
		} else {
			configPath = "./tapio.yaml" // Create new file
		}
	}

	// Load existing configuration or create default
	var cfg *config.Config
	if _, err := os.Stat(configPath); err == nil {
		// Load existing
		loader := config.NewLoader().WithConfigFile(configPath)
		loadedCfg, err := loader.Load()
		if err != nil {
			return fmt.Errorf("failed to load existing configuration: %v", err)
		}
		cfg = loadedCfg
	} else {
		// Create new
		cfg = config.DefaultConfig()
	}

	// Set the value using dot notation
	if err := setConfigValue(cfg, key, value); err != nil {
		return fmt.Errorf("failed to set configuration value: %v", err)
	}

	// Save configuration
	if err := loader.SaveConfig(cfg, configPath); err != nil {
		return fmt.Errorf("failed to save configuration: %v", err)
	}

	fmt.Printf("✅ Configuration updated: %s = %s\n", key, value)
	fmt.Printf("📁 File: %s\n", configPath)

	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Load configuration
	loader := config.NewLoader()
	if configFile != "" {
		loader = loader.WithConfigFile(configFile)
	}

	cfg, _, err := loader.GetEffectiveConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}

	// Get the value using dot notation
	value, err := getConfigValue(cfg, key)
	if err != nil {
		return fmt.Errorf("failed to get configuration value: %v", err)
	}

	fmt.Println(value)
	return nil
}

// setConfigValue sets a value in the configuration using dot notation
func setConfigValue(cfg *config.Config, key, value string) error {
	// This is a simplified implementation - a full version would use reflection
	// or a more sophisticated approach to handle all configuration fields

	parts := strings.Split(key, ".")
	switch parts[0] {
	case "log_level":
		cfg.LogLevel = value
	case "log_format":
		cfg.LogFormat = value
	case "features":
		if len(parts) > 1 {
			switch parts[1] {
			case "enable_ebpf":
				cfg.Features.EnableEBPF = parseBoolValue(value)
			case "enable_prediction":
				cfg.Features.EnablePrediction = parseBoolValue(value)
			case "enable_metrics":
				cfg.Features.EnableMetrics = parseBoolValue(value)
			case "enable_correlation":
				cfg.Features.EnableCorrelation = parseBoolValue(value)
			default:
				return fmt.Errorf("unknown features key: %s", parts[1])
			}
		}
	case "kubernetes":
		if len(parts) > 1 {
			switch parts[1] {
			case "context":
				cfg.Kubernetes.Context = value
			case "kubeconfig":
				cfg.Kubernetes.Kubeconfig = value
			case "in_cluster":
				cfg.Kubernetes.InCluster = parseBoolValue(value)
			default:
				return fmt.Errorf("unknown kubernetes key: %s", parts[1])
			}
		}
	case "output":
		if len(parts) > 1 {
			switch parts[1] {
			case "format":
				cfg.Output.Format = value
			case "color":
				cfg.Output.Color = parseBoolValue(value)
			case "verbose":
				cfg.Output.Verbose = parseBoolValue(value)
			default:
				return fmt.Errorf("unknown output key: %s", parts[1])
			}
		}
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	return nil
}

// getConfigValue gets a value from the configuration using dot notation
func getConfigValue(cfg *config.Config, key string) (string, error) {
	parts := strings.Split(key, ".")
	switch parts[0] {
	case "log_level":
		return cfg.LogLevel, nil
	case "log_format":
		return cfg.LogFormat, nil
	case "features":
		if len(parts) > 1 {
			switch parts[1] {
			case "enable_ebpf":
				return fmt.Sprintf("%v", cfg.Features.EnableEBPF), nil
			case "enable_prediction":
				return fmt.Sprintf("%v", cfg.Features.EnablePrediction), nil
			case "enable_metrics":
				return fmt.Sprintf("%v", cfg.Features.EnableMetrics), nil
			case "enable_correlation":
				return fmt.Sprintf("%v", cfg.Features.EnableCorrelation), nil
			default:
				return "", fmt.Errorf("unknown features key: %s", parts[1])
			}
		}
		// Return whole features section as YAML
		data, _ := yaml.Marshal(cfg.Features)
		return string(data), nil
	case "kubernetes":
		if len(parts) > 1 {
			switch parts[1] {
			case "context":
				return cfg.Kubernetes.Context, nil
			case "kubeconfig":
				return cfg.Kubernetes.Kubeconfig, nil
			case "in_cluster":
				return fmt.Sprintf("%v", cfg.Kubernetes.InCluster), nil
			default:
				return "", fmt.Errorf("unknown kubernetes key: %s", parts[1])
			}
		}
		// Return whole kubernetes section as YAML
		data, _ := yaml.Marshal(cfg.Kubernetes)
		return string(data), nil
	case "output":
		if len(parts) > 1 {
			switch parts[1] {
			case "format":
				return cfg.Output.Format, nil
			case "color":
				return fmt.Sprintf("%v", cfg.Output.Color), nil
			case "verbose":
				return fmt.Sprintf("%v", cfg.Output.Verbose), nil
			default:
				return "", fmt.Errorf("unknown output key: %s", parts[1])
			}
		}
		// Return whole output section as YAML
		data, _ := yaml.Marshal(cfg.Output)
		return string(data), nil
	default:
		return "", fmt.Errorf("unknown configuration key: %s", key)
	}
}

// parseBoolValue parses a string value as boolean
func parseBoolValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "true", "1", "yes", "on", "enabled", "enable":
		return true
	default:
		return false
	}
}
