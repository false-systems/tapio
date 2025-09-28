package dns

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

// ParseDNSPacketData parses raw DNS packet data
func ParseDNSPacketData(data []byte) (*DNSHeader, string, error) {
	if len(data) < 12 {
		return nil, "", fmt.Errorf("packet too small for DNS header")
	}

	// Parse header
	header := &DNSHeader{
		ID:      binary.BigEndian.Uint16(data[0:2]),
		Flags:   binary.BigEndian.Uint16(data[2:4]),
		QDCount: binary.BigEndian.Uint16(data[4:6]),
		ANCount: binary.BigEndian.Uint16(data[6:8]),
		NSCount: binary.BigEndian.Uint16(data[8:10]),
		ARCount: binary.BigEndian.Uint16(data[10:12]),
	}

	// Parse query name (if there's a question)
	name := ""
	if header.QDCount > 0 && len(data) > 12 {
		name = extractDNSName(data, 12)
	}

	return header, name, nil
}

// extractDNSName extracts domain name from DNS packet
func extractDNSName(data []byte, offset int) string {
	var labels []string
	pos := offset

	for pos < len(data) && len(labels) < 10 {
		length := int(data[pos])

		if length == 0 {
			break
		}

		// Handle compression pointer
		if length&0xC0 == 0xC0 {
			// Compressed name, skip for now
			break
		}

		pos++
		if pos+length > len(data) {
			break
		}

		labels = append(labels, string(data[pos:pos+length]))
		pos += length
	}

	return strings.Join(labels, ".")
}

// DNSTracker tracks DNS queries and matches responses
type DNSTracker struct {
	queries map[uint64]*TrackedQuery
}

// TrackedQuery represents a DNS query in flight
type TrackedQuery struct {
	ID        uint16
	Name      string
	Timestamp time.Time
	SrcIP     net.IP
	DstIP     net.IP
}

// NewDNSTracker creates a new DNS tracker
func NewDNSTracker() *DNSTracker {
	return &DNSTracker{
		queries: make(map[uint64]*TrackedQuery),
	}
}

// makeKey creates a unique key for tracking queries
func makeKey(saddr, daddr uint32, sport uint16, id uint16) uint64 {
	return uint64(saddr)<<32 | uint64(sport)<<16 | uint64(id)
}

// TrackQuery records a DNS query
func (t *DNSTracker) TrackQuery(saddr, daddr uint32, sport uint16, header *DNSHeader, name string) {
	key := makeKey(saddr, daddr, sport, header.ID)
	t.queries[key] = &TrackedQuery{
		ID:        header.ID,
		Name:      name,
		Timestamp: time.Now(),
		SrcIP:     intToIP(saddr),
		DstIP:     intToIP(daddr),
	}
}

// MatchResponse matches a DNS response to its query
func (t *DNSTracker) MatchResponse(saddr, daddr uint32, dport uint16, header *DNSHeader) (*TrackedQuery, time.Duration) {
	// For response, source/dest are swapped
	key := makeKey(daddr, saddr, dport, header.ID)

	query, exists := t.queries[key]
	if !exists {
		return nil, 0
	}

	latency := time.Since(query.Timestamp)
	delete(t.queries, key) // Clean up

	return query, latency
}

// CleanupOld removes queries older than timeout
func (t *DNSTracker) CleanupOld(timeout time.Duration) {
	cutoff := time.Now().Add(-timeout)
	for key, query := range t.queries {
		if query.Timestamp.Before(cutoff) {
			delete(t.queries, key)
		}
	}
}

func intToIP(addr uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, addr)
	return ip
}
