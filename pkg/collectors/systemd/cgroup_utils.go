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

		// Security: Skip lines containing null bytes
		if strings.Contains(line, "\x00") {
			continue
		}

		// Parse cgroup line format: "hierarchy:controller:path"
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		path := parts[2]

		// Security: Check for path traversal attempts
		if strings.Contains(path, "..") {
			continue
		}

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

			// Security: Validate segment doesn't contain null bytes or traversal
			if strings.Contains(segment, "\x00") || strings.Contains(segment, "..") {
				continue
			}

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
	if comm == "" || len(comm) > 30 {
		return false
	}

	// Check for exact systemd process matches and valid prefixes
	if comm == "systemd" || comm == "systemctl" {
		return true
	}

	// Check for systemd- prefix but reject overly long names
	if strings.HasPrefix(comm, "systemd-") && len(comm) <= 20 {
		return true
	}

	return false
}
