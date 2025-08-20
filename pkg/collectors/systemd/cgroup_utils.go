package systemd

import (
	"strings"
)

// extractUnitFromCgroup extracts the systemd unit name from cgroup data
func (c *Collector) extractUnitFromCgroup(cgroupData string) string {
	if cgroupData == "" {
		return "unknown"
	}

	lines := strings.Split(cgroupData, "\n")
	var bestUnit string
	var bestDepth int

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parse cgroup line format: "hierarchy:controller:path"
		// Use SplitN to handle paths with colons (e.g., dbus services)
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		path := parts[2]

		// Skip non-systemd paths
		if !strings.Contains(path, ".slice") && !strings.Contains(path, ".service") &&
			!strings.Contains(path, ".scope") && !strings.Contains(path, ".timer") &&
			!strings.Contains(path, ".socket") && !strings.Contains(path, ".mount") &&
			!strings.Contains(path, ".target") {
			continue
		}

		// Extract all units from the path
		segments := strings.Split(path, "/")
		for i := len(segments) - 1; i >= 0; i-- {
			segment := segments[i]

			// Check for systemd unit patterns
			if strings.HasSuffix(segment, ".service") ||
				strings.HasSuffix(segment, ".scope") ||
				strings.HasSuffix(segment, ".timer") ||
				strings.HasSuffix(segment, ".socket") ||
				strings.HasSuffix(segment, ".mount") ||
				strings.HasSuffix(segment, ".target") {

				// Calculate depth (prefer deeper units except in container scenarios)
				depth := i

				// Prioritize by unit type
				priority := getUnitPriority(segment)

				// Select best unit based on priority and depth
				if bestUnit == "" || priority > getUnitPriority(bestUnit) ||
					(priority == getUnitPriority(bestUnit) && depth > bestDepth) {
					bestUnit = segment
					bestDepth = depth
				}
			}
		}
	}

	if bestUnit == "" {
		return "unknown"
	}
	return bestUnit
}

// getUnitPriority returns priority for unit types (higher = more important)
func getUnitPriority(unit string) int {
	switch {
	case strings.HasSuffix(unit, ".timer"):
		return 5
	case strings.HasSuffix(unit, ".socket"):
		return 4
	case strings.HasSuffix(unit, ".mount"):
		return 3
	case strings.HasSuffix(unit, ".target"):
		return 3
	case strings.HasSuffix(unit, ".service"):
		return 2
	case strings.HasSuffix(unit, ".scope"):
		return 1
	default:
		return 0
	}
}

// isValidSystemdProcess checks if a process name is systemd-related
func (c *Collector) isValidSystemdProcess(comm string) bool {
	if comm == "" || len(comm) >= 100 {
		return false
	}

	// Check for systemd-related process names
	systemdProcesses := []string{
		"systemd",
		"systemd-logind",
		"systemd-resolve",
		"systemd-networkd",
		"systemd-journal",
		"systemd-udevd",
		"systemd-timesyncd",
		"systemd-machined",
		"systemd-hostnamed",
	}

	for _, proc := range systemdProcesses {
		if comm == proc || strings.HasPrefix(comm, proc) {
			return true
		}
	}

	return false
}
