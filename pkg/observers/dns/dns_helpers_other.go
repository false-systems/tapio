//go:build !linux
// +build !linux

package dns

import "strings"

// extractQueryName safely extracts DNS query name from byte array (stub for non-Linux)
func (c *Observer) extractQueryName(queryName []byte) string {
	// Find null terminator or use full length
	var nameLen int
	for i, b := range queryName {
		if b == 0 {
			nameLen = i
			break
		}
	}
	if nameLen == 0 {
		nameLen = len(queryName)
	}

	// Convert to string and validate
	name := string(queryName[:nameLen])
	name = strings.TrimSpace(name)

	// Basic validation - ensure it looks like a domain name
	if name == "" {
		return ""
	}

	return name
}
