package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// parseBool parses a string to boolean with flexible input handling
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "yes", "on", "enabled", "enable":
		return true
	case "false", "0", "no", "off", "disabled", "disable":
		return false
	default:
		// Default to false for invalid input
		return false
	}
}

// parseInt parses a string to integer with error handling
func parseInt(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// parseFloat parses a string to float64 with error handling
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// parseDuration parses a string to time.Duration with flexible input
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	// Handle common suffixes that time.ParseDuration doesn't support
	replacements := map[string]string{
		"sec":     "s",
		"secs":    "s",
		"second":  "s",
		"seconds": "s",
		"min":     "m",
		"mins":    "m",
		"minute":  "m",
		"minutes": "m",
		"hr":      "h",
		"hrs":     "h",
		"hour":    "h",
		"hours":   "h",
		"day":     "h",
		"days":    "h",
	}

	for old, new := range replacements {
		if strings.HasSuffix(s, old) {
			s = strings.TrimSuffix(s, old) + new

			// Handle day conversion (multiply by 24)
			if new == "h" && (old == "day" || old == "days") {
				if duration, err := time.ParseDuration(s); err == nil {
					return duration * 24, nil
				}
			}
			break
		}
	}

	return time.ParseDuration(s)
}

// parseStringSlice parses a comma-separated string to slice
func parseStringSlice(s string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// normalizeFilePath normalizes a file path by expanding environment variables and home directory
func normalizeFilePath(path string) string {
	// Expand environment variables
	path = os.ExpandEnv(path)

	// Expand home directory
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	return filepath.Clean(path)
}

// validatePort checks if a port number is valid
func validatePort(port int) bool {
	return port > 0 && port <= 65535
}

// validateLogLevel checks if a log level is valid
func validateLogLevel(level string) bool {
	validLevels := []string{"debug", "info", "warn", "error", "fatal", "panic", "trace"}
	level = strings.ToLower(level)

	for _, valid := range validLevels {
		if level == valid {
			return true
		}
	}

	return false
}

// validateOutputFormat checks if an output format is valid
func validateOutputFormat(format string) bool {
	validFormats := []string{"human", "json", "yaml", "table", "csv"}
	format = strings.ToLower(format)

	for _, valid := range validFormats {
		if format == valid {
			return true
		}
	}

	return false
}

// mergeStringSlices merges multiple string slices, removing duplicates
func mergeStringSlices(slices ...[]string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, slice := range slices {
		for _, item := range slice {
			if !seen[item] {
				seen[item] = true
				result = append(result, item)
			}
		}
	}

	return result
}

// splitKeyValue splits a "key=value" string
func splitKeyValue(s string) (string, string, bool) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// formatSize formats a size in bytes to human-readable format
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration to human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.String()
	}

	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}

	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}

	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

// sanitizeConfigValue sanitizes a configuration value for safe display
func sanitizeConfigValue(key, value string) string {
	sensitiveKeys := []string{
		"password", "passwd", "secret", "token", "key", "credential",
		"auth", "bearer", "api_key", "apikey",
	}

	key = strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(key, sensitive) {
			if len(value) <= 4 {
				return "***"
			}
			return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
		}
	}

	return value
}

// getConfigType returns the configuration type based on file extension
func getConfigType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	default:
		return "yaml" // Default to YAML
	}
}

// ensureDirectory ensures a directory exists, creating it if necessary
func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0755)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isExecutable checks if a file is executable
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode()&0111 != 0
}

// findExecutable finds an executable in PATH
func findExecutable(name string) string {
	path := os.Getenv("PATH")
	if path == "" {
		return ""
	}

	for _, dir := range strings.Split(path, string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}

		fullPath := filepath.Join(dir, name)
		if isExecutable(fullPath) {
			return fullPath
		}

		// Also check with .exe extension on Windows
		if runtime.GOOS == "windows" {
			fullPath += ".exe"
			if isExecutable(fullPath) {
				return fullPath
			}
		}
	}

	return ""
}

// getEffectiveUser returns the effective user information
func getEffectiveUser() (uid, gid int, username string) {
	if user, err := user.Current(); err == nil {
		uid, _ = strconv.Atoi(user.Uid)
		gid, _ = strconv.Atoi(user.Gid)
		username = user.Username
	}
	return
}

// hasCapability checks if the current process has a specific capability (Linux only)
func hasCapability(cap string) bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// This is a simplified check - real implementation would use libcap
	// For now, just check if running as root
	uid, _, _ := getEffectiveUser()
	return uid == 0
}

// detectContainerRuntime detects the container runtime being used
func detectContainerRuntime() string {
	// Check for containerd
	if fileExists("/run/containerd/containerd.sock") {
		return "containerd"
	}

	// Check for Docker
	if fileExists("/var/run/docker.sock") {
		return "docker"
	}

	// Check for CRI-O
	if fileExists("/var/run/crio/crio.sock") {
		return "crio"
	}

	// Check for Podman
	if findExecutable("podman") != "" {
		return "podman"
	}

	return "unknown"
}

// getSystemInfo returns basic system information
func getSystemInfo() map[string]interface{} {
	info := map[string]interface{}{
		"os":                runtime.GOOS,
		"arch":              runtime.GOARCH,
		"go_version":        runtime.Version(),
		"cpu_count":         runtime.NumCPU(),
		"container_runtime": detectContainerRuntime(),
	}

	// Add user information
	uid, gid, username := getEffectiveUser()
	info["user"] = map[string]interface{}{
		"uid":      uid,
		"gid":      gid,
		"username": username,
		"is_root":  uid == 0,
	}

	return info
}
