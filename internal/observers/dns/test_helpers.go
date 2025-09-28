package dns

import (
	"encoding/binary"
	"strings"
)

// Test helper functions for building DNS packets

// buildDNSQuery builds a DNS query packet for testing
func buildDNSQuery(domain string, qtype uint16, id uint16, isResponse bool) []byte {
	packet := make([]byte, 12) // Header

	// ID
	binary.BigEndian.PutUint16(packet[0:2], id)

	// Flags
	flags := uint16(0x0100) // Standard query, recursion desired
	if isResponse {
		flags = 0x8180 // Response, no error
	}
	binary.BigEndian.PutUint16(packet[2:4], flags)

	// Question count
	binary.BigEndian.PutUint16(packet[4:6], 1)

	// Answer, Authority, Additional counts (0 for query)
	binary.BigEndian.PutUint16(packet[6:8], 0)
	binary.BigEndian.PutUint16(packet[8:10], 0)
	binary.BigEndian.PutUint16(packet[10:12], 0)

	// Add domain name
	packet = appendDomainName(packet, domain)

	// Add query type and class
	qtypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint16(qtypeBytes[0:2], qtype)
	binary.BigEndian.PutUint16(qtypeBytes[2:4], 1) // IN class
	packet = append(packet, qtypeBytes...)

	return packet
}

// buildDNSResponse builds a DNS response packet for testing
func buildDNSResponse(domain string, qtype uint16, id uint16, rcode uint8, answers []string) []byte {
	packet := make([]byte, 12) // Header

	// ID
	binary.BigEndian.PutUint16(packet[0:2], id)

	// Flags with response code
	flags := uint16(0x8180 | uint16(rcode)) // Response flag + rcode
	binary.BigEndian.PutUint16(packet[2:4], flags)

	// Question count
	binary.BigEndian.PutUint16(packet[4:6], 1)

	// Answer count
	answerCount := uint16(0)
	if len(answers) > 0 && rcode == DNSRCodeSuccess {
		answerCount = uint16(len(answers))
	}
	binary.BigEndian.PutUint16(packet[6:8], answerCount)

	// Authority, Additional counts
	binary.BigEndian.PutUint16(packet[8:10], 0)
	binary.BigEndian.PutUint16(packet[10:12], 0)

	// Add question section
	packet = appendDomainName(packet, domain)
	qtypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint16(qtypeBytes[0:2], qtype)
	binary.BigEndian.PutUint16(qtypeBytes[2:4], 1) // IN class
	packet = append(packet, qtypeBytes...)

	// Add answer section if present
	if answerCount > 0 {
		for _, answer := range answers {
			// Name (use compression pointer to question)
			packet = append(packet, 0xC0, 0x0C) // Pointer to offset 12

			// Type, Class, TTL, Data length
			answerHeader := make([]byte, 10)
			binary.BigEndian.PutUint16(answerHeader[0:2], qtype)
			binary.BigEndian.PutUint16(answerHeader[2:4], 1)   // IN class
			binary.BigEndian.PutUint32(answerHeader[4:8], 300) // TTL

			// For A records, add IP address
			if qtype == DNSTypeA && len(answer) > 0 {
				binary.BigEndian.PutUint16(answerHeader[8:10], 4) // 4 bytes for IPv4
				packet = append(packet, answerHeader...)
				// Convert IP string to bytes (simplified)
				packet = append(packet, 93, 184, 216, 34) // example.com IP
			}
		}
	}

	return packet
}

// buildTCPDNSQuery builds a TCP DNS query with length prefix
func buildTCPDNSQuery(domain string, qtype uint16, id uint16, isResponse bool) []byte {
	udpPacket := buildDNSQuery(domain, qtype, id, isResponse)

	// Add TCP length prefix
	tcpPacket := make([]byte, 2+len(udpPacket))
	binary.BigEndian.PutUint16(tcpPacket[0:2], uint16(len(udpPacket)))
	copy(tcpPacket[2:], udpPacket)

	return tcpPacket
}

// buildDNSQueryWithEDNS0 builds a DNS query with EDNS0 OPT record
func buildDNSQueryWithEDNS0(domain string, qtype uint16, id uint16, bufferSize uint16) []byte {
	packet := make([]byte, 12) // Header

	// ID
	binary.BigEndian.PutUint16(packet[0:2], id)

	// Flags
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)

	// Counts
	binary.BigEndian.PutUint16(packet[4:6], 1)   // Question
	binary.BigEndian.PutUint16(packet[6:8], 0)   // Answer
	binary.BigEndian.PutUint16(packet[8:10], 0)  // Authority
	binary.BigEndian.PutUint16(packet[10:12], 1) // Additional (OPT record)

	// Question section
	packet = appendDomainName(packet, domain)
	qtypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint16(qtypeBytes[0:2], qtype)
	binary.BigEndian.PutUint16(qtypeBytes[2:4], 1)
	packet = append(packet, qtypeBytes...)

	// OPT record in additional section
	packet = append(packet, 0) // Root domain
	optRecord := make([]byte, 10)
	binary.BigEndian.PutUint16(optRecord[0:2], 41)         // OPT type
	binary.BigEndian.PutUint16(optRecord[2:4], bufferSize) // UDP payload size
	binary.BigEndian.PutUint32(optRecord[4:8], 0)          // Extended RCODE and flags
	binary.BigEndian.PutUint16(optRecord[8:10], 0)         // RDLEN
	packet = append(packet, optRecord...)

	return packet
}

// buildTruncatedResponse builds a truncated DNS response
func buildTruncatedResponse(domain string, qtype uint16, id uint16) []byte {
	packet := make([]byte, 12)

	// ID
	binary.BigEndian.PutUint16(packet[0:2], id)

	// Flags with truncation bit set
	flags := uint16(0x8380) // Response + Truncated + No error
	binary.BigEndian.PutUint16(packet[2:4], flags)

	// Question count
	binary.BigEndian.PutUint16(packet[4:6], 1)

	// No answers due to truncation
	binary.BigEndian.PutUint16(packet[6:8], 0)
	binary.BigEndian.PutUint16(packet[8:10], 0)
	binary.BigEndian.PutUint16(packet[10:12], 0)

	// Question section
	packet = appendDomainName(packet, domain)
	qtypeBytes := make([]byte, 4)
	binary.BigEndian.PutUint16(qtypeBytes[0:2], qtype)
	binary.BigEndian.PutUint16(qtypeBytes[2:4], 1)
	packet = append(packet, qtypeBytes...)

	return packet
}

// buildBadTCPPacket builds a malformed TCP DNS packet
func buildBadTCPPacket() []byte {
	packet := make([]byte, 100)
	binary.BigEndian.PutUint16(packet[0:2], 200) // Wrong length
	return packet
}

// buildLargeDNSQuery builds a large DNS query for fragmentation testing
func buildLargeDNSQuery(size int) []byte {
	return make([]byte, size)
}

// fragmentTCPPacket simulates TCP packet fragmentation
func fragmentTCPPacket(packet []byte, mtu int) [][]byte {
	var fragments [][]byte
	for i := 0; i < len(packet); i += mtu {
		end := i + mtu
		if end > len(packet) {
			end = len(packet)
		}
		fragments = append(fragments, packet[i:end])
	}
	return fragments
}

// calculateDNSLength calculates expected DNS packet length
func calculateDNSLength(domain string) uint16 {
	// Header (12) + domain length + 2 (for length bytes) + 4 (type/class)
	return uint16(12 + len(domain) + 2 + 4)
}

// appendDomainName appends a domain name in DNS wire format
func appendDomainName(packet []byte, domain string) []byte {
	if domain == "" {
		return append(packet, 0)
	}

	labels := splitDomain(domain)
	for _, label := range labels {
		packet = append(packet, byte(len(label)))
		packet = append(packet, []byte(label)...)
	}
	packet = append(packet, 0) // Root label

	return packet
}

// splitDomain splits a domain into labels
func splitDomain(domain string) []string {
	if domain == "" {
		return []string{}
	}
	// Remove trailing dot if present
	if domain[len(domain)-1] == '.' {
		domain = domain[:len(domain)-1]
	}
	return strings.Split(domain, ".")
}

// buildTCPDNSResponse builds a TCP DNS response with length prefix
func buildTCPDNSResponse(domain string, qtype uint16, id uint16, rcode uint8, answers []string) []byte {
	// Build UDP packet first
	udpPacket := buildDNSResponse(domain, qtype, id, rcode, answers)

	// Add TCP length prefix (2 bytes)
	tcpPacket := make([]byte, 2+len(udpPacket))
	binary.BigEndian.PutUint16(tcpPacket[0:2], uint16(len(udpPacket)))
	copy(tcpPacket[2:], udpPacket)

	return tcpPacket
}
