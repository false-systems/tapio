//go:build linux
// +build linux

package dns

import (
	"strings"
	"unsafe"
)

// extractQueryName extracts the DNS query name from the BPF event data
func (c *Collector) extractQueryName(queryNameBytes []byte) string {
	// Find null terminator
	nameEnd := len(queryNameBytes)
	for i, b := range queryNameBytes {
		if b == 0 {
			nameEnd = i
			break
		}
	}

	if nameEnd == 0 {
		return ""
	}

	// Convert bytes to string and clean up
	queryName := string(queryNameBytes[:nameEnd])

	// Basic validation - DNS names shouldn't have null bytes or control chars
	if strings.Contains(queryName, "\x00") {
		return ""
	}

	// Convert DNS wire format if needed (labels prefixed with length)
	// This is a simplified version - real DNS parsing is more complex
	cleaned := c.cleanDNSName(queryName)

	return cleaned
}

// cleanDNSName cleans up DNS names from wire format to readable format
func (c *Collector) cleanDNSName(name string) string {
	// If the name looks like wire format (starts with length byte), parse it
	if len(name) > 0 && name[0] >= 1 && name[0] <= 63 {
		return c.parseDNSWireFormat(name)
	}

	// Otherwise, just clean up any invalid characters
	cleaned := strings.Map(func(r rune) rune {
		if r >= 32 && r <= 126 && r != 127 { // Printable ASCII
			return r
		}
		return -1 // Remove invalid chars
	}, name)

	return cleaned
}

// parseDNSWireFormat parses DNS wire format to readable domain name
func (c *Collector) parseDNSWireFormat(wireData string) string {
	if len(wireData) == 0 {
		return ""
	}

	var parts []string
	pos := 0

	for pos < len(wireData) {
		length := int(wireData[pos])
		if length == 0 {
			break // End of name
		}

		if length > 63 || pos+1+length > len(wireData) {
			break // Invalid length or overflow
		}

		pos++ // Move past length byte
		label := wireData[pos : pos+length]

		// Validate label contains only valid DNS characters
		if c.isValidDNSLabel(label) {
			parts = append(parts, label)
		}

		pos += length
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, ".")
}

// isValidDNSLabel checks if a DNS label contains valid characters
func (c *Collector) isValidDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}

	for _, char := range label {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return false
		}
	}

	return true
}

// getProtocolName returns the protocol name string
func (c *Collector) getProtocolName(protocol uint8) string {
	switch protocol {
	case 17: // IPPROTO_UDP
		return "UDP"
	case 6: // IPPROTO_TCP
		return "TCP"
	default:
		return "unknown"
	}
}

// getDNSTypeName returns the DNS query type name
func (c *Collector) getDNSTypeName(qtype uint16) string {
	switch qtype {
	case 1:
		return "A"
	case 2:
		return "NS"
	case 5:
		return "CNAME"
	case 6:
		return "SOA"
	case 12:
		return "PTR"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 28:
		return "AAAA"
	case 33:
		return "SRV"
	default:
		return "unknown"
	}
}

// getDNSRcodeName returns the DNS response code name
func (c *Collector) getDNSRcodeName(rcode uint8) string {
	switch rcode {
	case 0:
		return "NOERROR"
	case 1:
		return "FORMERR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 4:
		return "NOTIMP"
	case 5:
		return "REFUSED"
	default:
		return "unknown"
	}
}

// Compile-time check that the helper functions don't allocate unnecessarily
var _ = [0]func(){
	func() { _ = (*Collector)(nil).extractQueryName(nil) },
	func() { _ = (*Collector)(nil).getProtocolName(0) },
}

// Size check to ensure our structures are reasonable
var _ = [512 - unsafe.Sizeof(BPFDNSEvent{})]byte // Ensure BPFDNSEvent fits in reasonable size
