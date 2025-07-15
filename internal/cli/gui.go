package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Launch Tapio's native GUI for visual Kubernetes observability",
	Long: `🖥️ Tapio GUI - Native Desktop Interface

Launch the Tapio native desktop application built with Wails and Vue.js.
The GUI provides a story-based interface for visualizing Kubernetes issues,
applying fixes, and monitoring cluster health in real-time.

Features:
  • Story-based visualization of Kubernetes issues
  • Real-time correlation engine integration
  • One-click fix application with safety controls
  • Beautiful native desktop experience
  • Automatic story updates every 5 seconds
  • Cluster health overview and metrics`,

	Example: `  # Launch the GUI application
  tapio gui

  # Launch GUI in development mode (if available)
  tapio gui --dev`,

	RunE: runGUI,
}

var (
	devMode bool
)

func init() {
	guiCmd.Flags().BoolVar(&devMode, "dev", false, "Launch in development mode (requires Wails dev environment)")
}

func runGUI(cmd *cobra.Command, args []string) error {
	// Check if we're in development mode
	if devMode {
		return launchGUIDev()
	}

	// Look for the built GUI binary
	guiPath := findGUIBinary()
	if guiPath == "" {
		return NewCLIError(
			"gui binary",
			"Tapio GUI binary not found",
			"The GUI component may not be installed or built",
		).WithHelp().WithExamples(
			"# Build the GUI (if you have the source)",
			"cd gui/tapio-gui && wails build",
			"",
			"# Or download a release with GUI included",
			"https://github.com/yairfalse/tapio/releases",
		)
	}

	fmt.Printf("🚀 Launching Tapio GUI...\n")
	fmt.Printf("   Binary: %s\n", guiPath)

	// Launch the GUI binary
	cmd := exec.Command(guiPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return NewCLIError(
			"gui launch",
			fmt.Sprintf("Failed to launch GUI: %v", err),
			"Check if the GUI binary has proper permissions",
		).WithExamples(
			"chmod +x " + guiPath,
		)
	}

	fmt.Printf("✅ Tapio GUI launched successfully\n")
	fmt.Printf("   PID: %d\n", cmd.Process.Pid)
	fmt.Printf("   The GUI will connect to tapio-server on localhost:9090\n")

	// Don't wait for the GUI to exit - let it run independently
	return nil
}

func launchGUIDev() error {
	// Look for Wails development setup
	guiDir := filepath.Join("gui", "tapio-gui")
	if _, err := os.Stat(guiDir); os.IsNotExist(err) {
		return NewCLIError(
			"gui development",
			"GUI development directory not found",
			"Development mode requires the Wails source project",
		).WithExamples(
			"# Clone the full project to use development mode",
			"git clone https://github.com/yairfalse/tapio.git",
			"cd tapio/gui/tapio-gui",
			"wails dev",
		)
	}

	fmt.Printf("🔧 Launching Tapio GUI in development mode...\n")
	fmt.Printf("   Directory: %s\n", guiDir)

	// Launch wails dev
	cmd := exec.Command("wails", "dev")
	cmd.Dir = guiDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return NewCLIError(
			"gui development",
			fmt.Sprintf("Failed to launch GUI in dev mode: %v", err),
			"Make sure Wails CLI is installed and the project is properly set up",
		).WithExamples(
			"# Install Wails CLI",
			"go install github.com/wailsapp/wails/v2/cmd/wails@latest",
			"",
			"# Setup the GUI project",
			"cd gui/tapio-gui",
			"go mod tidy",
			"npm install",
		)
	}

	return nil
}

func findGUIBinary() string {
	// Possible locations for the GUI binary
	possiblePaths := []string{
		// Relative to current directory (development)
		"gui/tapio-gui/build/bin/tapio-gui",
		"gui/tapio-gui/build/bin/tapio-gui.exe",
		"gui/tapio-gui/build/bin/tapio-gui.app/Contents/MacOS/tapio-gui",

		// Relative to tapio binary location (installed)
		"tapio-gui",
		"tapio-gui.exe",
		"tapio-gui.app/Contents/MacOS/tapio-gui",

		// System-wide installation
		"/usr/local/bin/tapio-gui",
		"/opt/tapio/bin/tapio-gui",

		// macOS app bundle
		"/Applications/tapio-gui.app/Contents/MacOS/tapio-gui",
	}

	// Get the directory where the tapio binary is located
	if tapioPath, err := os.Executable(); err == nil {
		tapioDir := filepath.Dir(tapioPath)

		// Add paths relative to tapio binary
		possiblePaths = append(possiblePaths,
			filepath.Join(tapioDir, "tapio-gui"),
			filepath.Join(tapioDir, "tapio-gui.exe"),
			filepath.Join(tapioDir, "tapio-gui.app", "Contents", "MacOS", "tapio-gui"),
		)
	}

	// Check each possible path
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			// Check if it's executable
			if isExecutable(path) {
				abs, _ := filepath.Abs(path)
				return abs
			}
		}
	}

	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// On Windows, check for .exe extension
	if runtime.GOOS == "windows" {
		return filepath.Ext(path) == ".exe"
	}

	// On Unix-like systems, check execute permission
	return info.Mode()&0111 != 0
}
