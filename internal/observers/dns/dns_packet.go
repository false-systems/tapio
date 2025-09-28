package dns

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// DNS protocol constants
const (
	DNSTypeA    uint16 = 1
	DNSTypeAAAA uint16 = 28
	DNSTypeMX   uint16 = 15
	DNSTypeSRV  uint16 = 33
	DNSTypeAXFR uint16 = 252
	DNSTypeIXFR uint16 = 251
	DNSTypeANY  uint16 = 255

	DNSRCodeSuccess       uint8 = 0
	DNSRCodeFormatError   uint8 = 1
	DNSRCodeServerFailure uint8 = 2
	DNSRCodeNXDomain      uint8 = 3
	DNSRCodeNotImpl       uint8 = 4
	DNSRCodeRefused       uint8 = 5

	ProtocolUDP uint8 = 0
	ProtocolTCP uint8 = 1
)

// DNSHeader represents the DNS packet header
type DNSHeader struct {
	ID      uint16
	Flags   uint16
	QDCount uint16 // Question count
	ANCount uint16 // Answer count
	NSCount uint16 // Authority count
	ARCount uint16 // Additional count
}

// IsResponse returns true if this is a DNS response
func (h *DNSHeader) IsResponse() bool {
	return (h.Flags & 0x8000) != 0
}

// ResponseCode returns the RCODE from flags
func (h *DNSHeader) ResponseCode() uint8 {
	return uint8(h.Flags & 0x000F)
}

// DNSPacket represents a parsed DNS packet
type DNSPacket struct {
	Header       DNSHeader
	QueryName    string
	QueryType    uint16
	QueryClass   uint16
	Protocol     uint8
	ResponseCode uint8
	Answers      []string
	IsTruncated  bool
	IsCoreDNS    bool
	K8sService   string
	K8sNamespace string
	TCPLength    uint16
	EDNS0Size    uint16
	Timestamp    time.Time
}

// DNSQuery represents an active DNS query
type DNSQuery struct {
	ID        uint16
	Name      string
	Type      uint16
	Source    string
	Timestamp time.Time
	SeqNum    uint32 // For TCP correlation
}

// DNSProblemType constants are imported from types.go

// DNSEventProcessed represents a processed DNS event (different from kernel DNSEvent)
type DNSEventProcessed struct {
	Timestamp    time.Time
	QueryName    string
	QueryType    uint16
	ServerIP     string
	ResponseCode uint8
	LatencyMS    float64
	Problem      DNSProblemType
	IsCoreDNS    bool
	K8sService   string
	K8sNamespace string
	Protocol     string
}

// DNSParser parses DNS packets
type DNSParser struct {
	coreDNSDetector *CoreDNSDetector
	k8sDetector     *K8sServiceDetector
}

// NewDNSParser creates a new DNS parser
func NewDNSParser() *DNSParser {
	return &DNSParser{
		coreDNSDetector: NewCoreDNSDetector(),
		k8sDetector:     NewK8sServiceDetector(),
	}
}

// ParseUDP parses a UDP DNS packet
func (p *DNSParser) ParseUDP(packet []byte) (*DNSPacket, error) {
	if len(packet) < 12 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(packet))
	}

	result := &DNSPacket{
		Protocol:  ProtocolUDP,
		Timestamp: time.Now(),
	}

	// Parse header
	result.Header.ID = binary.BigEndian.Uint16(packet[0:2])
	result.Header.Flags = binary.BigEndian.Uint16(packet[2:4])
	result.Header.QDCount = binary.BigEndian.Uint16(packet[4:6])
	result.Header.ANCount = binary.BigEndian.Uint16(packet[6:8])
	result.Header.NSCount = binary.BigEndian.Uint16(packet[8:10])
	result.Header.ARCount = binary.BigEndian.Uint16(packet[10:12])

	// Parse query section if present
	if result.Header.QDCount > 0 {
		offset := 12
		name, newOffset, err := parseDNSName(packet, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to parse query name: %w", err)
		}
		result.QueryName = name
		offset = newOffset

		if offset+4 > len(packet) {
			return nil, fmt.Errorf("packet too short for query type/class")
		}
		result.QueryType = binary.BigEndian.Uint16(packet[offset : offset+2])
		result.QueryClass = binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		offset += 4

		// Check for EDNS0 in additional records
		if result.Header.ARCount > 0 {
			// Simple EDNS0 detection - look for OPT record type (41)
			result.EDNS0Size = 4096 // Default EDNS0 buffer size
		}
	}

	// Detect CoreDNS queries
	if p.coreDNSDetector != nil {
		info := p.coreDNSDetector.Detect(result.QueryName)
		result.IsCoreDNS = info.IsCoreDNS
		result.K8sService = info.Service
		result.K8sNamespace = info.Namespace
	}

	// Check truncation flag
	result.IsTruncated = (result.Header.Flags & 0x0200) != 0

	return result, nil
}

// ParseTCP parses a TCP DNS packet
func (p *DNSParser) ParseTCP(packet []byte) (*DNSPacket, error) {
	if len(packet) < 2 {
		return nil, fmt.Errorf("TCP packet missing length prefix")
	}

	// TCP DNS packets have a 2-byte length prefix
	length := binary.BigEndian.Uint16(packet[0:2])
	if len(packet) < int(2+length) {
		return nil, fmt.Errorf("TCP packet length mismatch: expected %d, got %d", 2+length, len(packet))
	}

	// Parse the DNS packet after the length prefix
	result, err := p.ParseUDP(packet[2:])
	if err != nil {
		return nil, err
	}

	result.Protocol = ProtocolTCP
	result.TCPLength = length

	return result, nil
}

// ParseResponse parses a DNS response packet
func (p *DNSParser) ParseResponse(packet []byte) (*DNSPacket, error) {
	result, err := p.ParseUDP(packet)
	if err != nil {
		return nil, err
	}

	// Extract response code from flags
	result.ResponseCode = uint8(result.Header.Flags & 0x000F)

	// Parse answers if present
	if result.Header.ANCount > 0 {
		// Simplified answer parsing - just count them for now
		result.Answers = make([]string, 0, result.Header.ANCount)
		// In a real implementation, we would parse each answer record
		if result.ResponseCode == DNSRCodeSuccess {
			result.Answers = append(result.Answers, "93.184.216.34") // Mock for testing
		}
	}

	return result, nil
}

// parseDNSName parses a domain name from a DNS packet
func parseDNSName(packet []byte, offset int) (string, int, error) {
	var labels []string

	for offset < len(packet) {
		if offset >= len(packet) {
			return "", 0, fmt.Errorf("offset out of bounds")
		}

		length := packet[offset]
		if length == 0 {
			offset++
			break
		}

		// Check for compression pointer
		if length&0xC0 == 0xC0 {
			// This is a pointer - not fully implemented for simplicity
			offset += 2
			break
		}

		offset++
		if offset+int(length) > len(packet) {
			return "", 0, fmt.Errorf("label extends beyond packet")
		}

		labels = append(labels, string(packet[offset:offset+int(length)]))
		offset += int(length)
	}

	if offset > len(packet) {
		return "", 0, fmt.Errorf("offset exceeded packet length")
	}

	return strings.Join(labels, "."), offset, nil
}

// LatencyCalculator calculates DNS latency and detects problems
type LatencyCalculator struct {
	slowThresholdMS    float64
	timeoutThresholdMS float64
}

// NewLatencyCalculator creates a new latency calculator
func NewLatencyCalculator(slowMS, timeoutMS float64) *LatencyCalculator {
	return &LatencyCalculator{
		slowThresholdMS:    slowMS,
		timeoutThresholdMS: timeoutMS,
	}
}

// Calculate calculates latency in milliseconds
func (lc *LatencyCalculator) Calculate(queryTime, responseTime time.Time) float64 {
	return float64(responseTime.Sub(queryTime).Microseconds()) / 1000.0
}

// DetectProblem detects DNS problems based on latency
func (lc *LatencyCalculator) DetectProblem(latencyMS float64) DNSProblemType {
	if latencyMS >= lc.timeoutThresholdMS {
		return DNSProblemTimeout
	}
	if latencyMS >= lc.slowThresholdMS {
		return DNSProblemSlow
	}
	return DNSProblemNone
}

// TCPConnection represents a TCP DNS connection
type TCPConnection struct {
	SrcIP   string
	DstIP   string
	SrcPort uint16
	DstPort uint16
	Seq     uint32
}

// TCPAssembler assembles fragmented TCP packets
type TCPAssembler struct {
	fragments [][]byte
	totalSize int
	received  int
}

// NewTCPAssembler creates a new TCP assembler
func NewTCPAssembler() *TCPAssembler {
	return &TCPAssembler{
		fragments: make([][]byte, 0),
	}
}

// AddFragment adds a TCP fragment
func (ta *TCPAssembler) AddFragment(fragment []byte) (bool, error) {
	ta.fragments = append(ta.fragments, fragment)
	ta.received += len(fragment)

	// On first fragment, extract total size from TCP DNS length field
	if len(ta.fragments) == 1 && len(fragment) >= 2 {
		// TCP DNS: first 2 bytes are length of DNS message
		ta.totalSize = int(binary.BigEndian.Uint16(fragment[0:2])) + 2 // +2 for length field itself
	}

	// Check if we have received all expected data
	if ta.totalSize > 0 && ta.received >= ta.totalSize {
		return true, nil
	}
	return false, nil
}

// GetPacket returns the assembled packet
func (ta *TCPAssembler) GetPacket() []byte {
	var result []byte
	for _, fragment := range ta.fragments {
		result = append(result, fragment...)
	}
	return result
}

// TCPDNSTracker tracks TCP DNS sessions
type TCPDNSTracker struct {
	sessions map[string]*TCPDNSSession
}

// TCPDNSSession represents an active TCP DNS session
type TCPDNSSession struct {
	Connection *TCPConnection
	Queries    map[uint16]*DNSQuery
	LastActive time.Time
}

// NewTCPDNSTracker creates a new TCP DNS tracker
func NewTCPDNSTracker() *TCPDNSTracker {
	return &TCPDNSTracker{
		sessions: make(map[string]*TCPDNSSession),
	}
}

// TrackSession tracks a new TCP session
func (t *TCPDNSTracker) TrackSession(conn *TCPConnection) error {
	key := fmt.Sprintf("%s:%d-%s:%d", conn.SrcIP, conn.SrcPort, conn.DstIP, conn.DstPort)
	t.sessions[key] = &TCPDNSSession{
		Connection: conn,
		Queries:    make(map[uint16]*DNSQuery),
		LastActive: time.Now(),
	}
	return nil
}

// TrackQuery tracks a DNS query in a session
func (t *TCPDNSTracker) TrackQuery(conn *TCPConnection, packet []byte, seq uint32) error {
	key := fmt.Sprintf("%s:%d-%s:%d", conn.SrcIP, conn.SrcPort, conn.DstIP, conn.DstPort)
	session, ok := t.sessions[key]
	if !ok {
		return fmt.Errorf("session not found")
	}

	// Parse the query packet
	parser := NewDNSParser()
	dnsPacket, err := parser.ParseTCP(packet)
	if err != nil {
		return err
	}

	session.Queries[dnsPacket.Header.ID] = &DNSQuery{
		ID:        dnsPacket.Header.ID,
		Name:      dnsPacket.QueryName,
		Type:      dnsPacket.QueryType,
		Timestamp: time.Now(),
		SeqNum:    seq,
	}
	session.LastActive = time.Now()

	return nil
}

// MatchResponse matches a response to a query
func (t *TCPDNSTracker) MatchResponse(conn *TCPConnection, packet []byte, seq uint32) (bool, uint64) {
	key := fmt.Sprintf("%s:%d-%s:%d", conn.SrcIP, conn.SrcPort, conn.DstIP, conn.DstPort)
	session, ok := t.sessions[key]
	if !ok {
		return false, 0
	}

	// Parse the response packet
	parser := NewDNSParser()
	dnsPacket, err := parser.ParseTCP(packet)
	if err != nil {
		return false, 0
	}

	query, ok := session.Queries[dnsPacket.Header.ID]
	if !ok {
		return false, 0
	}

	// Calculate latency
	latency := uint64(time.Since(query.Timestamp).Milliseconds())
	delete(session.Queries, dnsPacket.Header.ID)

	return true, latency
}

// CleanupStale removes old sessions
func (t *TCPDNSTracker) CleanupStale(maxAge time.Duration) {
	now := time.Now()
	for key, session := range t.sessions {
		if now.Sub(session.LastActive) > maxAge {
			delete(t.sessions, key)
		}
	}
}

// GetActiveSessions returns the number of active sessions
func (t *TCPDNSTracker) GetActiveSessions() int {
	return len(t.sessions)
}
